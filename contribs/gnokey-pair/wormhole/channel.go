// Package wormhole implements the magic-wormhole generic protocol: a short
// code, SPAKE2 over a mailbox server, then encrypted numbered messages. It
// interoperates with the Python reference client and server.
//
// Unlike other Go clients, a Channel survives a dropped connection: it
// redials with backoff, binds again with the same side, reopens its mailbox
// and resends what the server had not acknowledged.
package wormhole

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"
)

// AppID keeps gnokey-pair traffic apart on a shared mailbox server; both
// sides must use the same one.
const AppID = "gno.land/gnokey-pair/v1"

// MaxMessageSize bounds what Send accepts.
const MaxMessageSize = 1 << 20

var (
	ErrBadCode     = errors.New("wormhole: malformed code")
	ErrWrongCode   = errors.New("wormhole: key exchange failed: wrong code, or someone else used it")
	ErrCrowded     = errors.New("wormhole: code already in use by two other devices")
	ErrMailboxLost = errors.New("wormhole: the relay no longer has this channel")
	ErrClosed      = errors.New("wormhole: channel closed")
	ErrNotReady    = errors.New("wormhole: handshake not done")
)

var (
	minBackoff      = 250 * time.Millisecond
	maxBackoff      = 10 * time.Second
	reattachTimeout = 30 * time.Second
	abortTimeout    = 5 * time.Second
)

// Moods reported to the server on close, as the reference clients do.
const (
	moodHappy  = "happy"
	moodLonely = "lonely"
	moodScary  = "scary"
	moodErrory = "errory"
)

type Config struct {
	// RelayURL is the mailbox server, ws:// or wss://.
	RelayURL string
	// AppID defaults to the package's AppID.
	AppID string
	// HTTPClient dials the relay. nil means http.DefaultClient, which honors
	// HTTPS_PROXY. Its Timeout must be zero.
	HTTPClient *http.Client
	// Versions is this side's app_versions, sent once the key is agreed. It
	// must marshal to a JSON object; nil sends {}.
	Versions any
}

// Channel is one side of a wormhole. Recv must not be called concurrently;
// the other methods are safe for concurrent use. A Channel must be closed.
type Channel struct {
	cfg        Config
	side       string
	code       string
	nameplate  string
	mailbox    string
	versionMsg []byte

	ctx    context.Context // cancelled to stop run
	cancel context.CancelFunc
	kick   chan struct{} // wakes run to write what is queued
	done   chan struct{} // closed when run returns

	mu      sync.Mutex
	changed chan struct{} // closed and replaced on every state change
	dialFn  func(context.Context) (*conn, error)
	conn    *conn // nil while reconnecting
	gen     int   // generation of conn
	lastErr error // last connection failure, for diagnostics
	err     error // terminal: Handshake, Send and Recv fail with it

	// outbox holds our commands in order. Each one is written once per
	// connection until the server acknowledges it.
	outbox    []command
	echoed    bool // the server has echoed one of our messages
	replaying bool // between open and pong, checking that the mailbox survived
	sawOwn    bool // one of our messages came back since open
	peer      string
	inbox     map[string]string // the peer's bodies by phase; the first copy wins

	handshaking bool
	releasing   bool
	mood        string // set once Close is requested
	closed      bool

	key          []byte
	ready        bool // versions exchanged
	peerVersions json.RawMessage
	sendPhase    int
	recvPhase    int
}

type command struct {
	msg map[string]any
	gen int // connection generation that last carried it
}

// Allocate asks the relay for a new code. Show Code to the user, then call
// Handshake to wait for the peer.
func Allocate(ctx context.Context, cfg Config) (*Channel, error) {
	return start(ctx, cfg, func(c *conn) (string, error) {
		f, err := c.call(ctx, map[string]any{"type": "allocate"}, "allocated")
		if err != nil {
			return "", err
		}
		return newCode(f.Nameplate), nil
	})
}

// Join enters a code allocated by the peer. Call Handshake next.
func Join(ctx context.Context, cfg Config, code string) (*Channel, error) {
	if _, err := parseCode(code); err != nil {
		return nil, err
	}
	return start(ctx, cfg, func(*conn) (string, error) { return code, nil })
}

// start connects, claims the nameplate of the code that getCode returns, and
// runs the channel.
func start(ctx context.Context, cfg Config, getCode func(*conn) (string, error)) (*Channel, error) {
	ch, err := newChannel(cfg)
	if err != nil {
		return nil, err
	}
	c, err := ch.dialFn(ctx)
	if err == nil {
		if err = ch.claim(ctx, c, getCode); err != nil {
			c.close()
		}
	}
	if err != nil {
		ch.cancel()
		return nil, err
	}
	go ch.run(c)
	return ch, nil
}

func (ch *Channel) claim(ctx context.Context, c *conn, getCode func(*conn) (string, error)) error {
	code, err := getCode(c)
	if err != nil {
		return err
	}
	ch.code = code
	if ch.nameplate, err = parseCode(code); err != nil {
		return err
	}
	f, err := c.call(ctx, map[string]any{"type": "claim", "nameplate": ch.nameplate}, "claimed")
	if err != nil {
		return err
	}
	ch.mailbox = f.Mailbox
	return nil
}

func newChannel(cfg Config) (*Channel, error) {
	if cfg.RelayURL == "" {
		return nil, errors.New("wormhole: no relay URL")
	}
	if cfg.AppID == "" {
		cfg.AppID = AppID
	}
	versions := cfg.Versions
	if versions == nil {
		versions = struct{}{}
	}
	v, err := json.Marshal(map[string]any{"app_versions": versions})
	if err != nil {
		return nil, fmt.Errorf("wormhole: versions: %w", err)
	}
	side := make([]byte, 5)
	rand.Read(side)
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: Close calls it
	ch := &Channel{
		cfg:        cfg,
		side:       hex.EncodeToString(side),
		versionMsg: v,
		ctx:        ctx,
		cancel:     cancel,
		kick:       make(chan struct{}, 1),
		done:       make(chan struct{}),
		changed:    make(chan struct{}),
		inbox:      map[string]string{},
	}
	ch.dialFn = func(ctx context.Context) (*conn, error) { return dial(ctx, &ch.cfg, ch.side) }
	return ch, nil
}

// Code is the code both sides type or scan.
func (ch *Channel) Code() string { return ch.code }

// Handshake runs SPAKE2 on the code and exchanges versions. A wrong code
// returns ErrWrongCode, and the channel is closed.
func (ch *Channel) Handshake(ctx context.Context) error {
	ch.mu.Lock()
	again := ch.handshaking
	ch.handshaking = true
	ch.mu.Unlock()
	if again {
		return errors.New("wormhole: Handshake called twice")
	}
	s, body := newPake(ch.cfg.AppID, ch.code)
	ch.update(func() { ch.queueLocked(addCmd("pake", body)) })
	peerPake, err := ch.await(ctx, "pake")
	if err != nil {
		return err
	}
	// The peer is here: free the nameplate, as the reference clients do.
	ch.update(ch.releaseLocked)

	key, err := finishPake(s, peerPake)
	if err != nil {
		return ch.abort(ctx, fmt.Errorf("%w: %w", ErrWrongCode, err))
	}
	var peer string
	ch.update(func() {
		ch.key, peer = key, ch.peer
		ch.queueLocked(addCmd("version", seal(key, ch.side, "version", ch.versionMsg)))
	})

	peerVersion, err := ch.await(ctx, "version")
	if err != nil {
		return err
	}
	plain, err := unseal(key, peer, "version", peerVersion)
	if err != nil {
		return ch.abort(ctx, ErrWrongCode)
	}
	var v struct {
		AppVersions json.RawMessage `json:"app_versions"`
	}
	if err := json.Unmarshal(plain, &v); err != nil {
		return ch.abort(ctx, fmt.Errorf("wormhole: malformed version message: %w", err))
	}
	ch.mu.Lock()
	ch.peerVersions, ch.ready = v.AppVersions, true
	ch.mu.Unlock()
	return nil
}

// abort fails the channel with err and closes it.
func (ch *Channel) abort(ctx context.Context, err error) error {
	ch.fail(err)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
	defer cancel()
	ch.Close(ctx)
	return err
}

// Verifier is the wormhole verifier: equal on both sides when they share
// the key. nil before Handshake.
func (ch *Channel) Verifier() []byte {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if !ch.ready {
		return nil
	}
	return verifier(ch.key)
}

// CheckWords renders the verifier as two words for the user to compare
// across devices. Empty before Handshake.
func (ch *Channel) CheckWords() string {
	v := ch.Verifier()
	if v == nil {
		return ""
	}
	return checkWords(v)
}

// PeerVersions is the peer's app_versions. nil before Handshake.
func (ch *Channel) PeerVersions() json.RawMessage {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.peerVersions
}

// Send queues the next message ("0", "1", ...) for the peer. It is delivered
// even across reconnections; Close waits until the relay has it.
func (ch *Channel) Send(data []byte) error {
	if len(data) > MaxMessageSize {
		return fmt.Errorf("wormhole: message of %d bytes exceeds %d", len(data), MaxMessageSize)
	}
	ch.mu.Lock()
	defer ch.wake()
	defer ch.mu.Unlock()
	switch {
	case ch.err != nil:
		return ch.err
	case ch.mood != "":
		return ErrClosed
	case !ch.ready:
		return ErrNotReady
	}
	phase := strconv.Itoa(ch.sendPhase)
	ch.sendPhase++
	ch.queueLocked(addCmd(phase, seal(ch.key, ch.side, phase, data)))
	return nil
}

// Recv returns the peer's next message, in the order it sent them.
func (ch *Channel) Recv(ctx context.Context) ([]byte, error) {
	ch.mu.Lock()
	if !ch.ready {
		ch.mu.Unlock()
		return nil, ErrNotReady
	}
	phase, key, peer := strconv.Itoa(ch.recvPhase), ch.key, ch.peer
	ch.mu.Unlock()
	body, err := ch.await(ctx, phase)
	if err != nil {
		return nil, err
	}
	plain, err := unseal(key, peer, phase, body)
	if err != nil {
		return nil, fmt.Errorf("message %s: %w", phase, err)
	}
	ch.mu.Lock()
	ch.recvPhase++
	ch.mu.Unlock()
	return plain, nil
}

// Close releases the nameplate and closes the mailbox, after the relay has
// every message sent so far. ctx bounds the wait for an unreachable relay.
func (ch *Channel) Close(ctx context.Context) error {
	ch.update(func() {
		if ch.mood != "" {
			return
		}
		ch.mood = ch.moodLocked()
		ch.releaseLocked()
		ch.queueLocked(map[string]any{"type": "close", "mailbox": ch.mailbox, "mood": ch.mood})
	})
	err := ch.wait(ctx, func() bool { return ch.closed })
	ch.cancel()
	<-ch.done
	return err
}

func (ch *Channel) moodLocked() string {
	switch {
	case errors.Is(ch.err, ErrWrongCode):
		return moodScary
	case ch.peer == "":
		return moodLonely
	case ch.ready && ch.err == nil:
		return moodHappy
	default:
		return moodErrory
	}
}

func addCmd(phase, body string) map[string]any {
	return map[string]any{"type": "add", "phase": phase, "body": body}
}

// update runs f under the lock, then wakes run to write what f queued.
func (ch *Channel) update(f func()) {
	ch.mu.Lock()
	f()
	ch.mu.Unlock()
	ch.wake()
}

func (ch *Channel) queueLocked(msg map[string]any) {
	ch.outbox = append(ch.outbox, command{msg: msg})
}

// releaseLocked queues the nameplate release. Closing a mailbox whose
// nameplate is still claimed breaks the reference server, so it must come
// before close.
func (ch *Channel) releaseLocked() {
	if !ch.releasing {
		ch.releasing = true
		ch.queueLocked(map[string]any{"type": "release", "nameplate": ch.nameplate})
	}
}

// ackLocked drops the queued command that the server acknowledged.
func (ch *Channel) ackLocked(typ, phase string) {
	ch.outbox = slices.DeleteFunc(ch.outbox, func(c command) bool {
		return c.msg["type"] == typ && (phase == "" || c.msg["phase"] == phase)
	})
}

func (ch *Channel) wake() {
	select {
	case ch.kick <- struct{}{}:
	default:
	}
}

func (ch *Channel) notifyLocked() {
	close(ch.changed)
	ch.changed = make(chan struct{})
}

func (ch *Channel) fail(err error) {
	ch.mu.Lock()
	if ch.err == nil {
		ch.err = err
	}
	ch.notifyLocked()
	ch.mu.Unlock()
}

func (ch *Channel) setLastErr(err error) {
	ch.mu.Lock()
	ch.lastErr = err
	ch.mu.Unlock()
}

// await returns the peer's body for phase, once it has arrived.
func (ch *Channel) await(ctx context.Context, phase string) (string, error) {
	if err := ch.wait(ctx, func() bool {
		_, ok := ch.inbox[phase]
		return ok || ch.err != nil
	}); err != nil {
		return "", err
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if body, ok := ch.inbox[phase]; ok {
		return body, nil
	}
	return "", ch.err
}

// wait blocks until cond, called with mu held, is true.
func (ch *Channel) wait(ctx context.Context, cond func() bool) error {
	for {
		ch.mu.Lock()
		ok, changed := cond(), ch.changed
		ch.mu.Unlock()
		if ok {
			return nil
		}
		select {
		case <-changed:
		case <-ch.done:
			ch.mu.Lock()
			defer ch.mu.Unlock()
			if cond() {
				return nil
			}
			return ErrClosed
		case <-ctx.Done():
			ch.mu.Lock()
			defer ch.mu.Unlock()
			if ch.conn == nil && ch.lastErr != nil {
				return fmt.Errorf("%w (relay unreachable: %w)", ctx.Err(), ch.lastErr)
			}
			return ctx.Err()
		}
	}
}

// run owns the connection: it serves it, and redials when it drops.
func (ch *Channel) run(c *conn) {
	defer func() {
		ch.fail(ErrClosed)
		close(ch.done)
	}()
	attempt := 0
	for {
		if c != nil {
			done, err := ch.serve(c)
			c.close()
			if done {
				return
			}
			ch.setLastErr(err)
			attempt = 0
		}
		select {
		case <-ch.ctx.Done():
			return
		case <-time.After(backoff(attempt)):
		}
		attempt++
		ch.mu.Lock()
		dialFn := ch.dialFn
		ch.mu.Unlock()
		ctx, cancel := context.WithTimeout(ch.ctx, reattachTimeout)
		var err error
		c, err = dialFn(ctx)
		cancel()
		if err != nil {
			ch.setLastErr(err)
		}
	}
}

func backoff(attempt int) time.Duration {
	if attempt == 0 {
		return 0
	}
	return min(minBackoff<<min(attempt-1, 16), maxBackoff)
}

// serve runs c until it drops (false, why) or the channel is done (true).
func (ch *Channel) serve(c *conn) (bool, error) {
	ch.mu.Lock()
	ch.gen++
	gen := ch.gen
	ch.conn, ch.lastErr = c, nil
	ch.replaying, ch.sawOwn = ch.echoed, false
	ch.notifyLocked()
	ch.mu.Unlock()
	defer func() {
		ch.mu.Lock()
		ch.conn = nil
		ch.mu.Unlock()
	}()

	// Open subscribes and replays the whole mailbox; the pong marks the end
	// of the replay (see handle).
	for _, m := range []map[string]any{
		{"type": "open", "mailbox": ch.mailbox},
		{"type": "ping", "ping": gen},
	} {
		if err := c.send(ch.ctx, m); err != nil {
			return false, err
		}
	}
	for {
		if err := ch.flush(c, gen); err != nil {
			return false, err
		}
		select {
		case f, ok := <-c.frames:
			if !ok {
				return false, c.err
			}
			if ch.handle(f) {
				return true, nil
			}
		case <-ch.kick:
		case <-ch.ctx.Done():
			return true, nil
		}
	}
}

// flush writes the queued commands that connection gen has not carried yet.
func (ch *Channel) flush(c *conn, gen int) error {
	var msgs []map[string]any
	ch.mu.Lock()
	for i := range ch.outbox {
		if cmd := &ch.outbox[i]; cmd.gen != gen {
			cmd.gen = gen
			msgs = append(msgs, cmd.msg)
		}
	}
	ch.mu.Unlock()
	for _, m := range msgs {
		if err := c.send(ch.ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// handle applies a server frame; it reports whether the mailbox is closed.
func (ch *Channel) handle(f frame) bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	switch f.Type {
	case "message":
		if f.Side == ch.side {
			ch.echoed, ch.sawOwn = true, true
			ch.ackLocked("add", f.Phase)
			return false
		}
		if ch.peer == "" {
			ch.peer = f.Side
		}
		if _, dup := ch.inbox[f.Phase]; f.Side == ch.peer && !dup {
			ch.inbox[f.Phase] = f.Body
			ch.notifyLocked()
		}
	case "pong":
		// None of our messages came back: the server pruned the mailbox
		// while we were away, and open created an empty one.
		if ch.replaying && !ch.sawOwn && ch.err == nil {
			ch.err = ErrMailboxLost
			ch.notifyLocked()
		}
		ch.replaying = false
	case "released":
		ch.ackLocked("release", "")
	case "closed":
		ch.closed = true
		ch.notifyLocked()
		return true
	case "error":
		if ch.err == nil {
			ch.err = relayError(f)
		}
		ch.notifyLocked()
	}
	return false
}
