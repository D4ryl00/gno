package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
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
			if !strings.Contains(out, "aardvark  adroitness") || !strings.Contains(out, `move = "rock"`) {
				t.Fatalf("review missing from the output:\n%s", out)
			}
			var signed std.Tx
			if c.mode == protocol.ModeSignTx {
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
