package wormhole

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/relaytest"
)

func TestMain(m *testing.M) { relaytest.Main(m) }

var testVersions = map[string]any{"gnokey-pair": map[string]any{"v": 1, "kinds": []string{"tx"}}}

func testConfig(url string) Config { return Config{RelayURL: url, Versions: testVersions} }

func testContext(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d)
	t.Cleanup(cancel)
	return ctx
}

// closeOnCleanup closes ch when the test ends, so that no run loop outlives it.
func closeOnCleanup(t *testing.T, ch *Channel) *Channel {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ch.Close(ctx)
	})
	return ch
}

func allocate(t *testing.T, ctx context.Context, cfg Config) *Channel {
	t.Helper()
	ch, err := Allocate(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return closeOnCleanup(t, ch)
}

func join(t *testing.T, ctx context.Context, cfg Config, code string) *Channel {
	t.Helper()
	ch, err := Join(ctx, cfg, code)
	if err != nil {
		t.Fatal(err)
	}
	return closeOnCleanup(t, ch)
}

// handshake runs both handshakes concurrently and returns their errors.
func handshake(ctx context.Context, a, b *Channel) (errA, errB error) {
	ch := make(chan error, 1)
	go func() { ch <- b.Handshake(ctx) }()
	errA = a.Handshake(ctx)
	return errA, <-ch
}

func mustHandshake(t *testing.T, ctx context.Context, desk, phone *Channel) {
	t.Helper()
	if errA, errB := handshake(ctx, desk, phone); errA != nil || errB != nil {
		t.Fatalf("handshake: desktop %v, phone %v", errA, errB)
	}
}

// pair returns a desktop and a phone channel, handshake done.
func pair(t *testing.T, ctx context.Context, url string) (desk, phone *Channel) {
	t.Helper()
	desk = allocate(t, ctx, testConfig(url))
	phone = join(t, ctx, testConfig(url), desk.Code())
	mustHandshake(t, ctx, desk, phone)
	return desk, phone
}

// closeAll closes the channels in order.
func closeAll(t *testing.T, ctx context.Context, chs ...*Channel) {
	t.Helper()
	for _, ch := range chs {
		if err := ch.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func send(t *testing.T, ch *Channel, msg string) {
	t.Helper()
	if err := ch.Send([]byte(msg)); err != nil {
		t.Fatal(err)
	}
}

func expect(t *testing.T, ctx context.Context, ch *Channel, want string) {
	t.Helper()
	got, err := ch.Recv(ctx)
	if err != nil {
		t.Fatalf("waiting for %q: %v", want, err)
	}
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// expectNothing checks that no further message arrives, replays included.
func expectNothing(t *testing.T, ch *Channel) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if got, err := ch.Recv(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected message %q, %v", got, err)
	}
}
