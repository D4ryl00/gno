package wormhole

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/relaytest"
)

// drop kills ch's socket without telling the server, like a phone going to
// sleep. hold keeps it from redialing until resume is called.
func drop(t *testing.T, ch *Channel, hold bool) (resume func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	resume = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(resume)

	if err := ch.wait(testContext(t, 5*time.Second), func() bool { return ch.conn != nil }); err != nil {
		t.Fatalf("not connected: %v", err)
	}
	ch.mu.Lock()
	c := ch.conn
	if hold {
		dial := ch.dialFn
		ch.dialFn = func(ctx context.Context) (*conn, error) {
			select {
			case <-gate:
				return dial(ctx)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	ch.mu.Unlock()
	c.ws.CloseNow()
	return resume
}

func generation(ch *Channel) int {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.gen
}

// The phone loses its socket at each step of a pairing; every time it
// reattaches, the exchange completes and no message is delivered twice.
func TestReconnect(t *testing.T) {
	relayURL := relaytest.Shared(t)
	t.Parallel()
	for _, at := range []string{"before handshake", "before request", "during review", "before received"} {
		t.Run(at, func(t *testing.T) {
			t.Parallel()
			ctx := testContext(t, 30*time.Second)
			desk := allocate(t, ctx, testConfig(relayURL))
			phone := join(t, ctx, testConfig(relayURL), desk.Code())
			step := func(name string) {
				if name == at {
					time.AfterFunc(300*time.Millisecond, drop(t, phone, true))
				}
			}

			step("before handshake")
			mustHandshake(t, ctx, desk, phone)
			step("before request")
			send(t, phone, "request")
			expect(t, ctx, desk, "request")
			step("during review")
			send(t, desk, "result")
			expect(t, ctx, phone, "result")
			step("before received")
			send(t, phone, "received")
			expect(t, ctx, desk, "received")

			expectNothing(t, desk)
			expectNothing(t, phone)
			if g := generation(phone); g < 2 {
				t.Fatalf("phone never reconnected (generation %d)", g)
			}
			closeAll(t, ctx, phone, desk)
		})
	}
}

// The phone is away when the result is posted, and gnokey-pair closes and
// exits; the phone comes back and still gets the result, once.
func TestResultOutlivesDesktop(t *testing.T) {
	relayURL := relaytest.Shared(t)
	t.Parallel()
	ctx := testContext(t, 30*time.Second)
	desk, phone := pair(t, ctx, relayURL)
	send(t, phone, "request")
	expect(t, ctx, desk, "request")

	resume := drop(t, phone, true)
	send(t, desk, "result")
	if err := desk.Close(ctx); err != nil {
		t.Fatal(err)
	}
	resume()

	expect(t, ctx, phone, "result")
	expectNothing(t, phone)
	send(t, phone, "received") // nobody reads it; it must not block Close
	if err := phone.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// Messages sent while disconnected are queued and delivered on reconnect,
// and Close waits for them.
func TestSendWhileDisconnected(t *testing.T) {
	relayURL := relaytest.Shared(t)
	t.Parallel()
	ctx := testContext(t, 30*time.Second)
	desk, phone := pair(t, ctx, relayURL)

	resume := drop(t, desk, true)
	send(t, desk, "result")
	closed := make(chan error, 1)
	go func() { closed <- desk.Close(ctx) }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned while disconnected: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	resume()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	expect(t, ctx, phone, "result")
}

// Away for longer than the server keeps an abandoned mailbox: the phone
// reports the channel lost instead of waiting forever.
func TestMailboxLost(t *testing.T) {
	relaytest.Need(t)
	t.Parallel()
	url, stop, err := relaytest.Start("MAILBOX_EXPIRE=0.5")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	ctx := testContext(t, 30*time.Second)
	desk, phone := pair(t, ctx, url)
	send(t, phone, "request")
	expect(t, ctx, desk, "request")

	resume := drop(t, phone, true)
	send(t, desk, "result")
	if err := desk.Close(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // expiry 0.5s, checked every 0.25s
	resume()

	if _, err := phone.Recv(ctx); !errors.Is(err, ErrMailboxLost) {
		t.Fatalf("Recv: %v", err)
	}
	if err := phone.Send([]byte("received")); !errors.Is(err, ErrMailboxLost) {
		t.Fatalf("Send: %v", err)
	}
	if err := phone.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// A relay that cannot be reached is reported once the caller gives up.
func TestRelayUnreachable(t *testing.T) {
	relayURL := relaytest.Shared(t)
	t.Parallel()
	ctx := testContext(t, 30*time.Second)
	_, phone := pair(t, ctx, relayURL)
	phone.mu.Lock()
	phone.dialFn = func(context.Context) (*conn, error) { return nil, errors.New("network down") }
	phone.mu.Unlock()
	drop(t, phone, false)

	_, err := phone.Recv(testContext(t, time.Second))
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("Recv: %v", err)
	}
	if err := phone.Close(testContext(t, 300*time.Millisecond)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close: %v", err)
	}
}

// Documents why both sides release the nameplate before closing: when the
// last side closes a mailbox whose nameplate is still claimed, the reference
// server fails on a foreign key and drops the connection without "closed".
func TestCloseWithoutReleaseBreaksServer(t *testing.T) {
	relayURL := relaytest.Shared(t)
	t.Parallel()
	ctx := testContext(t, 10*time.Second)
	cfg := testConfig(relayURL)
	cfg.AppID = AppID
	a, err := dial(ctx, &cfg, "aaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	f, err := a.call(ctx, map[string]any{"type": "allocate"}, "allocated")
	if err != nil {
		t.Fatal(err)
	}
	nameplate := f.Nameplate
	if f, err = a.call(ctx, map[string]any{"type": "claim", "nameplate": nameplate}, "claimed"); err != nil {
		t.Fatal(err)
	}
	mailbox := f.Mailbox

	b, err := dial(ctx, &cfg, "bbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	if _, err := b.call(ctx, map[string]any{"type": "claim", "nameplate": nameplate}, "claimed"); err != nil {
		t.Fatal(err)
	}

	closeCmd := map[string]any{"type": "close", "mailbox": mailbox, "mood": moodHappy}
	if _, err := a.call(ctx, closeCmd, "closed"); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if _, err := b.call(ctx, closeCmd, "closed"); err == nil {
		t.Fatal("the server closed a mailbox whose nameplate was still claimed; the release rule may no longer be needed")
	} else {
		t.Logf("last close without release: %v", err)
	}
}
