package wormhole

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/coder/websocket"
)

const (
	readLimit     = 4 << 20
	writeTimeout  = 10 * time.Second
	pingInterval  = 30 * time.Second
	pingTimeout   = 10 * time.Second
	clientName    = "gnokey-pair"
	clientVersion = "1"
)

// RelayError is an "error" response from the mailbox server.
type RelayError struct {
	Msg  string
	Orig string // type of the command that caused it
}

func (e *RelayError) Error() string {
	if e.Orig != "" {
		return fmt.Sprintf("relay refused %s: %s", e.Orig, e.Msg)
	}
	return "relay: " + e.Msg
}

// frame is a server response; which fields are set depends on Type.
type frame struct {
	Type    string `json:"type"`
	Welcome struct {
		Error string `json:"error"`
	} `json:"welcome"`
	Nameplate string `json:"nameplate"`
	Mailbox   string `json:"mailbox"`
	Side      string `json:"side"`
	Phase     string `json:"phase"`
	Body      string `json:"body"`
	Error     string `json:"error"`
	Orig      struct {
		Type string `json:"type"`
	} `json:"orig"`
}

// conn is one WebSocket connection to the mailbox server, bound to an app ID
// and a side.
type conn struct {
	ws     *websocket.Conn
	frames chan frame // closed when the socket is gone; err says why
	err    error
	ctx    context.Context // cancelled by close
	cancel context.CancelFunc
}

func dial(ctx context.Context, cfg *Config, side string) (*conn, error) {
	ws, _, err := websocket.Dial(ctx, cfg.RelayURL, &websocket.DialOptions{HTTPClient: cfg.HTTPClient})
	if err != nil {
		return nil, fmt.Errorf("dial relay: %w", err)
	}
	ws.SetReadLimit(readLimit)
	cctx, cancel := context.WithCancel(context.Background())
	c := &conn{ws: ws, frames: make(chan frame, 16), ctx: cctx, cancel: cancel}
	go c.read()
	go c.keepalive()

	f, err := c.next(ctx)
	if err == nil && f.Type != "welcome" {
		err = fmt.Errorf("relay: expected welcome, got %q", f.Type)
	}
	if err == nil && f.Welcome.Error != "" {
		err = &RelayError{Msg: f.Welcome.Error}
	}
	if err == nil {
		err = c.send(ctx, map[string]any{
			"type": "bind", "appid": cfg.AppID, "side": side,
			"client_version": []string{clientName, clientVersion},
		})
	}
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *conn) read() {
	defer close(c.frames)
	for {
		_, b, err := c.ws.Read(c.ctx)
		if err != nil {
			c.err = err
			return
		}
		var f frame
		if err := json.Unmarshal(b, &f); err != nil {
			c.err = fmt.Errorf("relay sent malformed JSON: %w", err)
			c.ws.CloseNow()
			return
		}
		select {
		case c.frames <- f:
		case <-c.ctx.Done():
			c.err = c.ctx.Err()
			return
		}
	}
}

// keepalive kills a socket that stops answering pings, so that a connection
// that died silently (a phone resuming from sleep) is noticed and redialed.
func (c *conn) keepalive() {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			ctx, cancel := context.WithTimeout(c.ctx, pingTimeout)
			err := c.ws.Ping(ctx)
			cancel()
			if err != nil {
				c.ws.CloseNow()
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

// next returns the next frame, skipping acks.
func (c *conn) next(ctx context.Context) (frame, error) {
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				return frame{}, fmt.Errorf("relay connection lost: %w", c.err)
			}
			if f.Type != "ack" {
				return f, nil
			}
		case <-ctx.Done():
			return frame{}, ctx.Err()
		}
	}
}

// send writes m with a fresh id; m itself is not modified.
func (c *conn) send(ctx context.Context, m map[string]any) error {
	id := make([]byte, 4)
	rand.Read(id)
	m = maps.Clone(m)
	m["id"] = hex.EncodeToString(id)
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// call sends m and waits for a response of type want. Only valid while no
// mailbox is open, when no unsolicited frame can arrive.
func (c *conn) call(ctx context.Context, m map[string]any, want string) (frame, error) {
	if err := c.send(ctx, m); err != nil {
		return frame{}, err
	}
	for {
		f, err := c.next(ctx)
		switch {
		case err != nil:
			return frame{}, err
		case f.Type == want:
			return f, nil
		case f.Type == "error":
			return frame{}, relayError(f)
		}
	}
}

func relayError(f frame) error {
	err := &RelayError{Msg: f.Error, Orig: f.Orig.Type}
	if f.Error == "crowded" {
		return fmt.Errorf("%w: %w", ErrCrowded, err)
	}
	return err
}

func (c *conn) close() {
	c.cancel()
	c.ws.CloseNow()
}
