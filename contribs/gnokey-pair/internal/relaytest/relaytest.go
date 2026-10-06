// Package relaytest runs the Python reference mailbox server for tests, from
// the venv that `make test-deps` builds. Without the venv, tests that need it
// skip, unless WORMHOLE_REQUIRE_RELAY is set
// (.github/workflows/ci-gnokey-pair-relay.yml).
package relaytest

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

var (
	dir       = sourceDir()
	python    = findPython()
	shared    sync.Once
	sharedURL string
	stop      = func() {}
	errShared error
)

func sourceDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Dir(file)
}

func findPython() string {
	if p := os.Getenv("WORMHOLE_PYTHON"); p != "" {
		return p
	}
	p := filepath.Join(dir, "..", "..", ".venv", "bin", "python")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// Python is the venv interpreter, for scripts of the reference client.
// Call Need first.
func Python() string { return python }

// Main runs the tests, then stops the shared server and runs cleanup.
func Main(m *testing.M, cleanup ...func()) {
	code := m.Run()
	stop()
	for _, f := range cleanup {
		f()
	}
	os.Exit(code)
}

// Need skips the test without the venv, or fails it when
// WORMHOLE_REQUIRE_RELAY is set.
func Need(tb testing.TB) {
	tb.Helper()
	if python != "" {
		return
	}
	const msg = "no Python venv for the reference mailbox server: run `make test-deps` in contribs/gnokey-pair"
	if os.Getenv("WORMHOLE_REQUIRE_RELAY") != "" {
		tb.Fatal(msg)
	}
	tb.Skip(msg)
}

// Shared returns the URL of a server shared by the tests of the package,
// started on first use and stopped by Main.
func Shared(tb testing.TB) string {
	tb.Helper()
	Need(tb)
	shared.Do(func() { sharedURL, stop, errShared = Start() })
	if errShared != nil {
		tb.Fatal(errShared)
	}
	return sharedURL
}

// Start runs a server on a free port. env is added to its environment;
// MAILBOX_EXPIRE=<seconds> shortens how long it keeps abandoned mailboxes.
func Start(env ...string) (url string, stop func(), err error) {
	if python == "" {
		return "", nil, errors.New("no Python venv")
	}
	db, err := os.MkdirTemp("", "wormhole-relay")
	if err != nil {
		return "", nil, err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	addr := l.Addr().String()
	l.Close()

	cmd := exec.Command(python, filepath.Join(dir, "mailbox.py"),
		"--port", fmt.Sprintf("tcp:%d:interface=127.0.0.1", l.Addr().(*net.TCPAddr).Port),
		"--channel-db", filepath.Join(db, "channel.sqlite"))
	cmd.Env = append(os.Environ(), env...)
	out := io.Discard
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
		os.RemoveAll(db)
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
