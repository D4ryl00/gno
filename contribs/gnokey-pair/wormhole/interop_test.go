package wormhole

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/relaytest"
)

// pythonPeer runs testdata/peer.py and returns its output lines by prefix.
type pythonPeer struct {
	lines chan string
	cmd   *exec.Cmd
}

func startPython(t *testing.T, ctx context.Context, relayURL, arg string) *pythonPeer {
	t.Helper()
	cmd := exec.CommandContext(ctx, relaytest.Python(), "testdata/peer.py", relayURL, arg)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &pythonPeer{lines: make(chan string, 8), cmd: cmd}
	go func() {
		s := bufio.NewScanner(out)
		for s.Scan() {
			p.lines <- s.Text()
		}
		close(p.lines)
	}()
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return p
}

// next returns the value of the next line, which must start with key.
func (p *pythonPeer) next(t *testing.T, key string) string {
	t.Helper()
	l, ok := <-p.lines
	if !ok {
		t.Fatalf("python exited before printing %q", key)
	}
	v, found := strings.CutPrefix(l, key+" ")
	if !found {
		t.Fatalf("python printed %q, want %q", l, key)
	}
	return v
}

// interop runs the exchange after both handshakes started: verifier and
// versions agree, and one message goes each way.
func interop(t *testing.T, ctx context.Context, ch *Channel, py *pythonPeer) {
	t.Helper()
	if err := ch.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	if v := py.next(t, "verifier"); v != hex.EncodeToString(ch.Verifier()) {
		t.Fatalf("verifier: python %s, go %x", v, ch.Verifier())
	}
	var pyVersions, goVersions map[string]any
	if err := json.Unmarshal([]byte(py.next(t, "versions")), &pyVersions); err != nil || pyVersions["gnokey-pair"] == nil {
		t.Fatalf("python saw versions %v: %v", pyVersions, err)
	}
	if err := json.Unmarshal(ch.PeerVersions(), &goVersions); err != nil || goVersions["gnokey-pair"] == nil {
		t.Fatalf("go saw versions %s: %v", ch.PeerVersions(), err)
	}
	expect(t, ctx, ch, "from python")
	send(t, ch, "from go")
	if got := py.next(t, "received"); got != "from go" {
		t.Fatalf("python received %q", got)
	}
	if err := ch.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := py.cmd.Wait(); err != nil {
		t.Fatalf("python: %v", err)
	}
}

func TestInteropPythonJoins(t *testing.T) {
	relayURL := relaytest.Shared(t)
	t.Parallel()
	ctx := testContext(t, 30*time.Second)
	ch := allocate(t, ctx, testConfig(relayURL))
	interop(t, ctx, ch, startPython(t, ctx, relayURL, ch.Code()))
}

func TestInteropPythonAllocates(t *testing.T) {
	relayURL := relaytest.Shared(t)
	t.Parallel()
	ctx := testContext(t, 30*time.Second)
	py := startPython(t, ctx, relayURL, "allocate")
	interop(t, ctx, join(t, ctx, testConfig(relayURL), py.next(t, "code")), py)
}

// wss:// through an HTTP CONNECT proxy, with TLS terminated in front of the
// relay. The proxy is set explicitly: Go never proxies loopback addresses,
// so HTTPS_PROXY cannot be exercised against a local relay.
func TestThroughConnectProxy(t *testing.T) {
	relayURL := relaytest.Shared(t)
	t.Parallel()
	ctx := testContext(t, 20*time.Second)

	up, err := url.Parse(strings.Replace(relayURL, "ws://", "http://", 1))
	if err != nil {
		t.Fatal(err)
	}
	tlsRelay := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: up.Host}))
	t.Cleanup(tlsRelay.Close)

	var tunnels atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		tunnels.Add(1)
		dst, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer dst.Close()
		w.WriteHeader(http.StatusOK)
		src, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer src.Close()
		go io.Copy(dst, src)
		io.Copy(src, dst)
	}))
	t.Cleanup(proxy.Close)

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	roots := tlsRelay.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	proxied := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{RootCAs: roots},
	}}
	direct := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	wss := strings.Replace(tlsRelay.URL, "https://", "wss://", 1) + "/v1"

	deskCfg, phoneCfg := testConfig(wss), testConfig(wss)
	deskCfg.HTTPClient, phoneCfg.HTTPClient = proxied, direct
	desk := allocate(t, ctx, deskCfg)
	if tunnels.Load() == 0 {
		t.Fatal("dial did not go through the proxy")
	}
	phone := join(t, ctx, phoneCfg, desk.Code())
	mustHandshake(t, ctx, desk, phone)
	send(t, phone, "request")
	expect(t, ctx, desk, "request")
}
