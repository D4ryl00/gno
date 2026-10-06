package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/gnolang/gno/contribs/gnokey-pair/internal/relaytest"
	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/contribs/gnokey-pair/wormhole"
	"github.com/gnolang/gno/gno.land/pkg/integration"
	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	rpcclient "github.com/gnolang/gno/tm2/pkg/bft/rpc/client"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/crypto/keys"
	"github.com/gnolang/gno/tm2/pkg/crypto/secp256k1"
	"github.com/gnolang/gno/tm2/pkg/log"
	"github.com/gnolang/gno/tm2/pkg/sdk/auth"
	"github.com/gnolang/gno/tm2/pkg/sdk/bank"
	"github.com/gnolang/gno/tm2/pkg/std"
)

var (
	binDir    string
	gnokeyBld sync.Once
	errGnokey error
)

func TestMain(m *testing.M) {
	relaytest.Main(m, func() {
		if binDir != "" {
			os.RemoveAll(binDir)
		}
	})
}

// buildGnokey builds the gnokey of the required gno module, the one
// gnokey-pair is tested against.
func buildGnokey(t *testing.T) string {
	t.Helper()
	gnokeyBld.Do(func() {
		if binDir, errGnokey = os.MkdirTemp("", "gnokey-pair-bin"); errGnokey != nil {
			return
		}
		out, err := exec.Command("go", "build", "-o", filepath.Join(binDir, "gnokey"), "github.com/gnolang/gno/gno.land/cmd/gnokey").CombinedOutput()
		if err != nil {
			errGnokey = fmt.Errorf("building gnokey: %w\n%s", err, out)
		}
	})
	if errGnokey != nil {
		t.Fatal(errGnokey)
	}
	return filepath.Join(binDir, "gnokey")
}

const e2ePassword = "correct horse battery staple"

func newKeybase(t *testing.T, names ...string) string {
	t.Helper()
	home := t.TempDir()
	kb, err := keys.NewKeyBaseFromDir(home)
	if err != nil {
		t.Fatal(err)
	}
	defer kb.CloseDB() // gnokey opens it next
	for _, name := range names {
		if _, err := kb.CreateAccount(name, integration.DefaultAccount_Seed, "", e2ePassword, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// The signer pre-check parses gnokey list, which is not an API: this pins it
// against the gnokey that the module requires.
func TestGnokeyList(t *testing.T) {
	t.Parallel()
	home := newKeybase(t, "main (old)")
	g := &gnokeyBin{path: buildGnokey(t), home: home}
	keys, err := g.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Name != "main (old)" || keys[0].Type != "local" || keys[0].Address != integration.DefaultAccount_Address {
		t.Fatalf("%+v", keys)
	}
	if _, err := crypto.PubKeyFromBech32(keys[0].PubKey); err != nil {
		t.Fatal(err)
	}
	if v, err := g.Version(context.Background()); err != nil || v == "" {
		t.Fatalf("version %q, %v", v, err)
	}
}

// e2e is an in-memory gno.land node, the real gnokey with a keyfile key, the
// reference relay, and a phone played by the wormhole package.
type e2e struct {
	relay, remote, chainID, home, gnokey string
	signer                               crypto.Address
	rpc                                  *rpcclient.RPCClient
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	e := &e2e{relay: relaytest.Shared(t), gnokey: buildGnokey(t)}
	cfg := integration.TestingMinimalNodeConfig(gnoenv.RootDir())
	n, remote := integration.TestingInMemoryNode(t, log.NewNoopLogger(), cfg)
	t.Cleanup(func() { n.Stop() })
	e.remote, e.chainID = remote, cfg.Genesis.ChainID
	e.home = newKeybase(t, "test1")
	e.signer = crypto.MustAddressFromString(integration.DefaultAccount_Address)
	var err error
	if e.rpc, err = rpcclient.NewHTTPClient(remote); err != nil {
		t.Fatal(err)
	}
	return e
}

// terminal collects what gnokey-pair and gnokey print on the pty.
type terminal struct {
	mu   sync.Mutex
	buf  strings.Builder
	read int // end of what wait already matched
	more chan struct{}
}

func newTerminal(ptmx *os.File) *terminal {
	term := &terminal{more: make(chan struct{}, 1)}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := ptmx.Read(b)
			term.mu.Lock()
			term.buf.Write(b[:n])
			term.mu.Unlock()
			select {
			case term.more <- struct{}{}:
			default:
			}
			if err != nil {
				return
			}
		}
	}()
	return term
}

// wait returns the first match of re after the previous one.
func (term *terminal) wait(t *testing.T, ctx context.Context, re *regexp.Regexp) []string {
	t.Helper()
	for {
		term.mu.Lock()
		s := term.buf.String()
		if loc := re.FindStringSubmatchIndex(s[term.read:]); loc != nil {
			m := make([]string, len(loc)/2)
			for i := range m {
				if loc[2*i] >= 0 {
					m[i] = s[term.read+loc[2*i] : term.read+loc[2*i+1]]
				}
			}
			term.read += loc[1]
			term.mu.Unlock()
			return m
		}
		term.mu.Unlock()
		select {
		case <-term.more:
		case <-ctx.Done():
			t.Fatalf("waiting for %s on the terminal:\n%s", re, s)
		}
	}
}

var (
	codeLine     = regexp.MustCompile(`\n {4}(\d+-[a-z]+-[a-z]+)\r?\n`)
	signPrompt   = regexp.MustCompile(`Sign\? \[y/N\]`)
	passPrompt   = regexp.MustCompile(`Enter password`)
	reviewSigner = regexp.MustCompile(`Signer +(g1\w+)`)
)

// pair runs gnokey-pair against a phone that sends req, waits for each of
// review on the terminal, approves, types the password, and returns what the
// phone received.
func (e *e2e) pair(t *testing.T, req protocol.Request, review ...*regexp.Regexp) protocol.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer ptmx.Close()
	defer tty.Close()
	term := newTerminal(ptmx)

	p := &pairing{
		cfg:  config{remote: e.remote, relay: e.relay, gnokey: e.gnokey, timeout: time.Minute, linger: 30 * time.Second},
		in:   bufio.NewReader(tty),
		out:  tty,
		keys: &gnokeyBin{path: e.gnokey, home: e.home, stdin: tty, stdout: tty, stderr: tty},
		dial: dialNode,
		now:  time.Now,
	}
	runErr := make(chan error, 1)
	go func() { runErr <- p.run(ctx) }()

	code := term.wait(t, ctx, codeLine)[1]
	phone, err := wormhole.Join(ctx, wormhole.Config{RelayURL: e.relay, Versions: protocol.AppVersions(protocol.Versions{V: 1, Kinds: []string{protocol.KindTx}})}, code)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close(ctx)
	if err := phone.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	term.wait(t, ctx, regexp.MustCompile(`Check words: `+regexp.QuoteMeta(phone.CheckWords())))
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := phone.Send(raw); err != nil {
		t.Fatal(err)
	}
	if got := term.wait(t, ctx, reviewSigner)[1]; got != req.Signer {
		t.Fatalf("review shows signer %s", got)
	}
	for _, re := range review {
		term.wait(t, ctx, re)
	}
	term.wait(t, ctx, signPrompt)
	ptmx.WriteString("y\n")
	term.wait(t, ctx, passPrompt)
	ptmx.WriteString(e2ePassword + "\n")

	b, err := phone.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var res protocol.Result
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	received, _ := json.Marshal(protocol.Received{Received: true})
	if err := phone.Send(received); err != nil {
		t.Fatal(err)
	}
	if err := phone.Close(ctx); err != nil {
		t.Fatal(err)
	}
	err = <-runErr
	term.mu.Lock()
	t.Log(term.buf.String()) // shown with -v or on failure
	term.mu.Unlock()
	if (err == nil) != (res.Status == protocol.StatusSuccess) {
		t.Fatalf("gnokey-pair returned %v for %+v", err, res)
	}
	return res
}

var e2eFee = std.Fee{GasWanted: 10_000_000, GasFee: std.Coin{Denom: "ugnot", Amount: 100_000}}

func (e *e2e) request(t *testing.T, mode string, msgs ...std.Msg) protocol.Request {
	t.Helper()
	tx := std.Tx{Msgs: msgs, Fee: e2eFee}
	return protocol.Request{
		Kind: protocol.KindTx, Mode: mode, ChainID: e.chainID, Signer: e.signer.String(),
		Tx: txJSON(t, tx), Requester: &protocol.Requester{Name: "e2e"},
	}
}

// sendDirect broadcasts msg signed by the signer outside gnokey-pair, as
// another device holding the identity would.
func (e *e2e) sendDirect(t *testing.T, msg std.Msg) {
	t.Helper()
	key, err := integration.GeneratePrivKeyFromMnemonic(integration.DefaultAccount_Seed, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	acc := e.account(t, e.signer)
	tx := signTx(t, std.Tx{Msgs: []std.Msg{msg}, Fee: e2eFee}, key, e.chainID, acc.AccountNumber, acc.Sequence)
	bres, err := rpcNode{e.rpc}.Broadcast(context.Background(), tx)
	if err != nil || bres.CheckTx.IsErr() || bres.DeliverTx.IsErr() {
		t.Fatalf("%v %+v", err, bres)
	}
}

func (e *e2e) account(t *testing.T, addr crypto.Address) *std.BaseAccount {
	t.Helper()
	acc, err := rpcNode{e.rpc}.Account(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

// One node and one signer for every step, so each request finds a sequence
// the previous one moved: gnokey-pair reads it just before signing.
func TestEndToEnd(t *testing.T) {
	e := newE2E(t)
	session := secp256k1.GenPrivKey().PubKey()
	recipient := secp256k1.GenPrivKey().PubKey().Address()

	t.Run("sendtx creates a session", func(t *testing.T) {
		res := e.pair(t, e.request(t, protocol.ModeSendTx, auth.MsgCreateSession{
			Creator:     e.signer,
			SessionKey:  session,
			ExpiresAt:   time.Now().Add(time.Hour).Unix(),
			AllowPaths:  []string{"vm/exec:gno.land/r/demo/counter"},
			SpendLimit:  std.Coins{{Denom: "ugnot", Amount: 1_000_000}},
			SpendPeriod: 3600,
		}))
		if res.Status != protocol.StatusSuccess || res.Hash == "" || res.Height == 0 {
			t.Fatalf("%+v", res)
		}
		q, err := e.rpc.ABCIQuery(context.Background(), fmt.Sprintf("auth/accounts/%s/session/%s", e.signer, session.Address()), nil)
		if err != nil || q.Response.Error != nil || string(q.Response.Data) == "null" {
			t.Fatalf("no session on chain: %v %+v", err, q)
		}
	})

	t.Run("the review shows the session revoked", func(t *testing.T) {
		res := e.pair(t, e.request(t, protocol.ModeSendTx, auth.MsgRevokeSession{Creator: e.signer, SessionKey: session}),
			regexp.MustCompile(`Allowed +vm/exec:gno\.land/r/demo/counter`),
			regexp.MustCompile(`Expires +\d{4}-\d\d-\d\d \d\d:\d\d UTC \(in `))
		if res.Status != protocol.StatusSuccess {
			t.Fatalf("%+v", res)
		}
		sessions, err := rpcNode{e.rpc}.Sessions(context.Background(), e.signer)
		if err != nil || len(sessions) != 0 {
			t.Fatalf("sessions left on chain: %v %+v", err, sessions)
		}
	})

	t.Run("signtx returns bytes that broadcast as is", func(t *testing.T) {
		res := e.pair(t, e.request(t, protocol.ModeSignTx, bank.MsgSend{
			FromAddress: e.signer, ToAddress: recipient, Amount: std.Coins{{Denom: "ugnot", Amount: 1234}},
		}))
		if res.Status != protocol.StatusSuccess || res.SignedTx == "" || res.Hash != "" {
			t.Fatalf("%+v", res)
		}
		if acc := e.account(t, recipient); acc != nil {
			t.Fatal("signtx broadcast")
		}
		bz, err := base64.StdEncoding.DecodeString(res.SignedTx)
		if err != nil {
			t.Fatal(err)
		}
		bres, err := e.rpc.BroadcastTxCommit(context.Background(), bz)
		if err != nil || bres.CheckTx.IsErr() || bres.DeliverTx.IsErr() {
			t.Fatalf("%v %+v", err, bres)
		}
		if acc := e.account(t, recipient); acc == nil || acc.Coins.AmountOf("ugnot") != 1234 {
			t.Fatalf("recipient %+v", acc)
		}
	})

	t.Run("a failing simulation broadcasts nothing", func(t *testing.T) {
		before := e.account(t, e.signer)
		res := e.pair(t, e.request(t, protocol.ModeSendTx, bank.MsgSend{
			FromAddress: e.signer, ToAddress: recipient, Amount: std.Coins{{Denom: "ugnot", Amount: 1e18}},
		}))
		if res.Status != protocol.StatusError || res.Code != protocol.CodeTxFailed || !strings.Contains(res.Detail, "nothing was broadcast") {
			t.Fatalf("%+v", res)
		}
		if after := e.account(t, e.signer); after.Sequence != before.Sequence || !after.Coins.IsEqual(before.Coins) {
			t.Fatalf("account moved: %+v -> %+v", before, after)
		}
	})

	t.Run("the identity sent something since the phone composed the request", func(t *testing.T) {
		recipient := secp256k1.GenPrivKey().PubKey().Address()
		req := e.request(t, protocol.ModeSendTx, bank.MsgSend{
			FromAddress: e.signer, ToAddress: recipient, Amount: std.Coins{{Denom: "ugnot", Amount: 1}},
		})
		before := e.account(t, e.signer)
		e.sendDirect(t, bank.MsgSend{FromAddress: e.signer, ToAddress: recipient, Amount: std.Coins{{Denom: "ugnot", Amount: 1}}})
		if res := e.pair(t, req); res.Status != protocol.StatusSuccess {
			t.Fatalf("%+v", res)
		}
		if after := e.account(t, e.signer); after.Sequence != before.Sequence+2 {
			t.Fatalf("sequence %d -> %d", before.Sequence, after.Sequence)
		}
	})
}
