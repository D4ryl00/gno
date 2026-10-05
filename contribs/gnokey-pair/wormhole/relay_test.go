package wormhole

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The tests run against the reference mailbox server and client, from the
// Python venv that `make test-deps` builds. Without it they skip, unless
// WORMHOLE_REQUIRE_RELAY is set (.github/workflows/ci-gnokey-pair-relay.yml).

var (
	python    = findPython()
	relay     sync.Once
	relayURL  string // shared mailbox server, started by the first needRelay
	stopRelay = func() {}
	errRelay  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	stopRelay()
	os.Exit(code)
}

func findPython() string {
	if p := os.Getenv("WORMHOLE_PYTHON"); p != "" {
		return p
	}
	p, err := filepath.Abs("../.venv/bin/python")
	if err != nil {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// startRelay runs a reference mailbox server on a free port. env is added to
// its environment (see testdata/mailbox.py).
func startRelay(env []string) (url string, stop func(), err error) {
	dir, err := os.MkdirTemp("", "wormhole-relay")
	if err != nil {
		return "", nil, err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	addr := l.Addr().String()
	l.Close()

	cmd := exec.Command(python, "testdata/mailbox.py",
		"--port", fmt.Sprintf("tcp:%d:interface=127.0.0.1", l.Addr().(*net.TCPAddr).Port),
		"--channel-db", filepath.Join(dir, "channel.sqlite"))
	cmd.Env = append(os.Environ(), env...)
	var out io.Writer = io.Discard
	if p := os.Getenv("WORMHOLE_RELAY_LOG"); p != "" {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return "", nil, err
		}
		out = f
	}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	stop = func() {
		cmd.Process.Kill()
		cmd.Wait()
		os.RemoveAll(dir)
	}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return "ws://" + addr + "/v1", stop, nil
		}
		if time.Now().After(deadline) {
			stop()
			return "", nil, errors.New("mailbox server did not start")
		}
	}
}

func needRelay(t *testing.T) {
	t.Helper()
	switch {
	case python == "" && os.Getenv("WORMHOLE_REQUIRE_RELAY") != "":
		t.Fatal("no Python venv for the reference mailbox server: run `make test-deps` in contribs/gnokey-pair")
	case python == "":
		t.Skip("no Python venv for the reference mailbox server: run `make test-deps` in contribs/gnokey-pair")
	}
	relay.Do(func() { relayURL, stopRelay, errRelay = startRelay(nil) })
	if errRelay != nil {
		t.Fatal(errRelay)
	}
}

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
