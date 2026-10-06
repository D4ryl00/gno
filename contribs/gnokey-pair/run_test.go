package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/tm2/pkg/amino"
	abci "github.com/gnolang/gno/tm2/pkg/bft/abci/types"
	ctypes "github.com/gnolang/gno/tm2/pkg/bft/rpc/core/types"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/std"
)

type fakeNode struct {
	chainID      string
	acc          *std.BaseAccount
	signedSimErr string // the signed simulation fails with it
	sessions     []session

	mu         sync.Mutex
	broadcasts []std.Tx
}

func (n *fakeNode) ChainID(context.Context) (string, error) { return n.chainID, nil }
func (n *fakeNode) Account(context.Context, crypto.Address) (*std.BaseAccount, error) {
	return n.acc, nil
}

func (n *fakeNode) GasPrice(context.Context) (std.GasPrice, error) {
	return std.GasPrice{Gas: 1000, Price: std.Coin{Denom: "ugnot", Amount: 1}}, nil
}

func (n *fakeNode) Sessions(context.Context, crypto.Address) ([]session, error) {
	return n.sessions, nil
}

func (n *fakeNode) FuncParams(context.Context, string) (map[string][]string, error) {
	return map[string][]string{"Play": {"move", "rounds"}}, nil
}

func (n *fakeNode) Simulate(_ context.Context, tx std.Tx) (abci.ResponseDeliverTx, error) {
	var res abci.ResponseDeliverTx
	if len(tx.Signatures[0].Signature) > 0 && n.signedSimErr != "" {
		res.Error = abci.StringError(n.signedSimErr)
		res.Log = n.signedSimErr
		return res, nil
	}
	res.GasUsed = 1234
	return res, nil
}

func (n *fakeNode) Broadcast(_ context.Context, tx std.Tx) (*ctypes.ResultBroadcastTxCommit, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.broadcasts = append(n.broadcasts, tx)
	return &ctypes.ResultBroadcastTxCommit{Hash: []byte{1, 2, 3}, Height: 42}, nil
}

// fakeKeys plays gnokey: it signs with key, after tamper if set.
type fakeKeys struct {
	keys    []keyInfo
	listErr error
	signErr error
	key     crypto.PrivKey
	tamper  func(*std.Tx)
	signed  int
}

func (k *fakeKeys) Version(context.Context) (string, error) { return "fake", nil }
func (k *fakeKeys) List(context.Context) ([]keyInfo, error) { return k.keys, k.listErr }

func (k *fakeKeys) Sign(_ context.Context, path, chainID string, accNum, seq uint64, _ string) error {
	k.signed++
	if k.signErr != nil {
		return k.signErr
	}
	bz, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var tx std.Tx
	if err := amino.UnmarshalJSON(bz, &tx); err != nil {
		return err
	}
	if k.tamper != nil {
		k.tamper(&tx)
	}
	if tx, err = signWith(tx, k.key, chainID, accNum, seq); err != nil {
		return err
	}
	return os.WriteFile(path, amino.MustMarshalJSON(tx), 0o600)
}

type pipelineCase struct {
	node    *fakeNode
	keys    *fakeKeys
	remote  string
	answers string
	dialed  string
}

func newPipelineCase() *pipelineCase {
	return &pipelineCase{
		node: &fakeNode{
			chainID: "test-chain",
			acc:     &std.BaseAccount{Address: signerAddr, Coins: std.Coins{{Denom: "ugnot", Amount: 1e9}}, AccountNumber: 7, Sequence: 3},
		},
		keys: &fakeKeys{
			keys: []keyInfo{{Name: "main", Type: "local", Address: signerAddr.String(), PubKey: crypto.PubKeyToBech32(signerKey.PubKey())}},
			key:  signerKey,
		},
		remote:  "http://node:26657",
		answers: "y\n",
	}
}

func (c *pipelineCase) pairing(in io.Reader, out io.Writer) *pairing {
	return &pairing{
		cfg:  config{remote: c.remote},
		in:   bufio.NewReader(in),
		out:  out,
		keys: c.keys,
		dial: func(remote string) (node, error) { c.dialed = remote; return c.node, nil },
		now:  func() time.Time { return testNow },
	}
}

func (c *pipelineCase) serve(t *testing.T, ctx context.Context, raw []byte) (protocol.Result, string) {
	t.Helper()
	var out bytes.Buffer
	res, _ := c.pairing(strings.NewReader(c.answers), &out).serve(ctx, raw, "aardvark adroitness")
	return res, out.String()
}

func TestPipeline(t *testing.T) {
	t.Parallel()
	call := newTx(callMsg())
	for _, c := range []struct {
		name       string
		edit       func(*pipelineCase)
		mode       string
		request    func(*protocol.Request)
		status     string
		code       string
		broadcasts int
		signs      int
	}{
		{name: "sendtx", status: protocol.StatusSuccess, broadcasts: 1, signs: 1},
		{name: "signtx", mode: protocol.ModeSignTx, status: protocol.StatusSuccess, signs: 1},
		{name: "declined", edit: func(c *pipelineCase) { c.answers = "\n" }, status: protocol.StatusCancelled},
		{name: "not exactly y", edit: func(c *pipelineCase) { c.answers = "sure\n" }, status: protocol.StatusCancelled},
		{
			name: "gnokey alters the fee", edit: func(c *pipelineCase) { c.keys.tamper = func(tx *std.Tx) { tx.Fee.GasFee.Amount *= 10 } },
			status: protocol.StatusError, code: protocol.CodeTxFailed, signs: 1,
		},
		{
			name: "gnokey adds a signature", edit: func(c *pipelineCase) {
				c.keys.tamper = func(tx *std.Tx) { *tx = signTx(t, *tx, otherKey, "test-chain", 7, 3) }
			},
			status: protocol.StatusError, code: protocol.CodeTxFailed, signs: 1,
		},
		{
			name: "gnokey signs with another key", edit: func(c *pipelineCase) { c.keys.key = otherKey },
			status: protocol.StatusError, code: protocol.CodeTxFailed, signs: 1,
		},
		{name: "signer not in keybase", edit: func(c *pipelineCase) { c.keys.keys = nil }, status: protocol.StatusError, code: protocol.CodeSignerUnavailable},
		{
			name: "list unreadable, key missing", edit: func(c *pipelineCase) {
				c.keys.listErr, c.keys.signErr = errUnparsable, errKeyNotFound
			},
			status: protocol.StatusError, code: protocol.CodeSignerUnavailable, signs: 1,
		},
		{name: "gnokey fails", edit: func(c *pipelineCase) { c.keys.signErr = errors.New("exit status 1") }, status: protocol.StatusError, code: protocol.CodeTxFailed, signs: 1},
		{name: "other chain", edit: func(c *pipelineCase) { c.node.chainID = "other" }, status: protocol.StatusError, code: protocol.CodeNetworkDeclined},
		{name: "suggested node declined", edit: func(c *pipelineCase) { c.remote, c.answers = "", "n\n" }, status: protocol.StatusError, code: protocol.CodeNetworkDeclined},
		{name: "suggested node accepted", edit: func(c *pipelineCase) { c.remote, c.answers = "", "y\ny\n" }, status: protocol.StatusSuccess, broadcasts: 1, signs: 1},
		{name: "no account", edit: func(c *pipelineCase) { c.node.acc = nil }, status: protocol.StatusError, code: protocol.CodeTxFailed},
		{
			name: "signed simulation fails", edit: func(c *pipelineCase) { c.node.signedSimErr = "out of gas" },
			status: protocol.StatusError, code: protocol.CodeTxFailed, signs: 1,
		},
		// The node would fail every step: offline must not touch it.
		{
			name: "offline", request: offline, status: protocol.StatusSuccess, signs: 1,
			edit: func(c *pipelineCase) { c.node.chainID, c.node.acc = "other", nil },
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pc := newPipelineCase()
			if c.edit != nil {
				c.edit(pc)
			}
			raw := requestJSON(t, call, func(r *protocol.Request) {
				if c.mode != "" {
					r.Mode = c.mode
				}
				if c.request != nil {
					c.request(r)
				}
			})
			res, out := pc.serve(t, context.Background(), raw)
			if res.Status != c.status || res.Code != c.code {
				t.Fatalf("got %+v, want %s %s\n%s", res, c.status, c.code, out)
			}
			if len(pc.node.broadcasts) != c.broadcasts || pc.keys.signed != c.signs {
				t.Fatalf("%d broadcasts, %d signatures; want %d, %d", len(pc.node.broadcasts), pc.keys.signed, c.broadcasts, c.signs)
			}
			if c.status != protocol.StatusSuccess {
				return
			}
			arg := `move = "rock"`
			if c.request != nil {
				arg = `#1 = "rock"` // no node to read the parameter names from
				if pc.dialed != "" || !strings.Contains(out, "Offline     your phone asked") {
					t.Fatalf("dialed %q offline:\n%s", pc.dialed, out)
				}
			}
			if !strings.Contains(out, "aardvark  adroitness") || !strings.Contains(out, arg) {
				t.Fatalf("review missing from the output:\n%s", out)
			}
			var signed std.Tx
			if res.SignedTx != "" {
				bz, err := base64.StdEncoding.DecodeString(res.SignedTx)
				if err != nil {
					t.Fatal(err)
				}
				if err := amino.Unmarshal(bz, &signed); err != nil {
					t.Fatal(err)
				}
			} else {
				signed = pc.node.broadcasts[0]
				if res.Hash != "AQID" || res.Height != 42 {
					t.Fatalf("result %+v", res)
				}
			}
			if err := verifySigned(signed, call, signerAddr, "test-chain", 7, 3); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// lockedBuffer is an output a test reads while the pipeline writes to it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

var txPathArg = regexp.MustCompile(`-tx-path (\S+)`)

// userSigns plays the user's gnokey on the file: sign it as gnokey sign does,
// after tamper if set.
func userSigns(t *testing.T, path string, tamper func(*std.Tx)) {
	t.Helper()
	var tx std.Tx
	if err := amino.UnmarshalJSON(mustRead(t, path), &tx); err != nil {
		t.Error(err)
		return
	}
	if tamper != nil {
		tamper(&tx)
	}
	if err := os.WriteFile(path, amino.MustMarshalJSON(signTx(t, tx, signerKey, "test-chain", 7, 3)), 0o600); err != nil {
		t.Error(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	bz, err := os.ReadFile(path)
	if err != nil {
		t.Error(err)
	}
	return bz
}

// With -manual, approving prints the gnokey command and gnokey never runs.
func TestPipelineByHand(t *testing.T) {
	t.Parallel()
	call := newTx(callMsg())
	for _, c := range []struct {
		name    string
		request func(*protocol.Request)
		sign    bool          // the user signs the file
		tamper  func(*std.Tx) // what the user's gnokey changes first
		status  string
		want    string // in the detail or the output
	}{
		{name: "sendtx: the full command", status: protocol.StatusSuccess,
			want: "-account-number 7 -account-sequence 3 " + signerAddr.String() + " \\\n  && gnokey broadcast -dry-run -remote http://node:26657 "},
		{name: "signtx: the signed file comes back", request: func(r *protocol.Request) { r.Mode = protocol.ModeSignTx },
			sign: true, status: protocol.StatusSuccess, want: "Waiting for the signature"},
		{name: "offline", request: offline, sign: true, status: protocol.StatusSuccess, want: "Offline"},
		{name: "the file signed is not the one approved", request: func(r *protocol.Request) { r.Mode = protocol.ModeSignTx },
			sign: true, tamper: func(tx *std.Tx) { tx.Fee.GasFee.Amount *= 10 },
			status: protocol.StatusError, want: "not what you approved"},
		{name: "nobody signs", request: func(r *protocol.Request) { r.Mode = protocol.ModeSignTx },
			status: protocol.StatusError, want: "no signature in"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pc := newPipelineCase()
			var out lockedBuffer
			p := pc.pairing(strings.NewReader("y\n"), &out)
			p.cfg.manual, p.cfg.timeout = true, 3*time.Second
			raw := requestJSON(t, call, c.request)

			done := make(chan protocol.Result, 1)
			go func() {
				res, _ := p.serve(context.Background(), raw, "aardvark adroitness")
				done <- res
			}()
			var res protocol.Result
			if c.sign {
				deadline := time.After(5 * time.Second)
				for m := txPathArg.FindStringSubmatch(out.String()); ; m = txPathArg.FindStringSubmatch(out.String()) {
					if m != nil {
						userSigns(t, m[1], c.tamper)
						break
					}
					select {
					case <-deadline:
						t.Fatalf("no command printed:\n%s", out.String())
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
			res = <-done
			o := out.String()
			if res.Status != c.status || !strings.Contains(res.Detail+o, c.want) {
				t.Fatalf("got %+v, want %s with %q\n%s", res, c.status, c.want, o)
			}
			if !strings.Contains(o, "Approve and print the gnokey command? [y/N]") || pc.keys.signed != 0 || len(pc.node.broadcasts) != 0 {
				t.Fatalf("gnokey ran (%d) or something was broadcast (%d):\n%s", pc.keys.signed, len(pc.node.broadcasts), o)
			}
			if c.status != protocol.StatusSuccess {
				return
			}
			if !c.sign { // sendtx: the phone watches the chain; the file waits for the user
				if !res.Manual || p.pending == "" {
					t.Fatalf("%+v, pending %q", res, p.pending)
				}
				path := txPathArg.FindStringSubmatch(o)[1]
				defer os.RemoveAll(p.pending)
				var tx std.Tx
				if filepath.Dir(path) != p.pending || amino.UnmarshalJSON(mustRead(t, path), &tx) != nil || len(tx.Signatures) != 0 {
					t.Fatalf("file %s in %s: %+v", path, p.pending, tx)
				}
				return
			}
			bz, err := base64.StdEncoding.DecodeString(res.SignedTx)
			if err != nil {
				t.Fatal(err)
			}
			var signed std.Tx
			if err := amino.Unmarshal(bz, &signed); err != nil {
				t.Fatal(err)
			}
			if err := verifySigned(signed, call, signerAddr, "test-chain", 7, 3); err != nil {
				t.Fatal(err)
			}
			if p.pending != "" {
				t.Fatalf("pending %q after the file came back", p.pending)
			}
		})
	}
}

func TestPipelineInvalidRequest(t *testing.T) {
	t.Parallel()
	pc := newPipelineCase()
	res, _ := pc.serve(t, context.Background(), []byte(`{"kind":"tx"}`))
	if res.Status != protocol.StatusError || res.Code != protocol.CodeInvalidRequest || res.Detail == "" {
		t.Fatalf("%+v", res)
	}
	if pc.dialed != "" {
		t.Fatal("dialed a node for an invalid request")
	}
}

// Ctrl-C while the review waits for an answer is cancelled.
func TestPipelineInterrupted(t *testing.T) {
	t.Parallel()
	pc := newPipelineCase()
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	res, err := pc.pairing(r, io.Discard).serve(ctx, requestJSON(t, newTx(callMsg()), nil), "")
	if !errors.Is(err, errCancelled) {
		t.Fatal(err)
	}
	if res.Status != protocol.StatusCancelled || pc.keys.signed != 0 {
		t.Fatalf("%+v, %d signatures", res, pc.keys.signed)
	}
}
