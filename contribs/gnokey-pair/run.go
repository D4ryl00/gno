package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/contribs/gnokey-pair/wormhole"
	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	"github.com/gnolang/gno/tm2/pkg/amino"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/sdk/auth"
	"github.com/gnolang/gno/tm2/pkg/std"
)

var versions = protocol.Versions{
	V:     1,
	Kinds: []string{protocol.KindTx},
	Modes: []string{protocol.ModeSendTx, protocol.ModeSignTx},
}

const (
	allocateTimeout = 30 * time.Second
	closeTimeout    = 30 * time.Second
)

var errCancelled = errors.New("cancelled")

type config struct {
	remote  string
	relay   string
	gnokey  string
	home    string
	timeout time.Duration
	linger  time.Duration
}

// pairing serves one request: one code, one request, one result.
type pairing struct {
	cfg  config
	in   *bufio.Reader // the terminal; gnokey reads the password from it too
	out  io.Writer
	keys keybase
	dial func(remote string) (node, error)
	now  func() time.Time
}

func (p *pairing) printf(format string, args ...any) { fmt.Fprintf(p.out, format, args...) }

// run pairs with the phone, serves its request and answers. It returns an
// error unless the request succeeded.
func (p *pairing) run(ctx context.Context) error {
	v, err := p.keys.Version(ctx)
	if err != nil {
		return err
	}
	p.printf("Signing with %s (gnokey %s)\n", p.cfg.gnokey, esc(v))

	actx, cancel := context.WithTimeout(ctx, allocateTimeout)
	defer cancel()
	ch, err := wormhole.Allocate(actx, wormhole.Config{RelayURL: p.cfg.relay, Versions: protocol.AppVersions(versions)})
	if err != nil {
		return fmt.Errorf("relay %s: %w", p.cfg.relay, err)
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()
		ch.Close(cctx)
	}()
	qr, err := renderQR(protocol.PairingURI(ch.Code(), p.cfg.relay))
	if err != nil {
		return err
	}
	p.printf("\nOn your phone, choose \"Send to my computer\" and scan this code, or type it:\n\n%s\n    %s\n\nWaiting for the phone (%s)...\n", qr, ch.Code(), p.cfg.timeout)

	wctx, cancel := context.WithTimeout(ctx, p.cfg.timeout)
	defer cancel()
	if err := ch.Handshake(wctx); err != nil {
		if errors.Is(err, wormhole.ErrWrongCode) {
			return errors.New("someone used a wrong code, so this code is burned; run gnokey-pair again")
		}
		return err
	}
	p.printf("Connected. Check words: %s (your phone shows the same)\n", ch.CheckWords())
	raw, err := ch.Recv(wctx)
	if err != nil {
		return fmt.Errorf("waiting for the request: %w", err)
	}

	res, err := p.serve(ctx, raw, ch.CheckWords())
	b, _ := json.Marshal(res)
	if err := ch.Send(b); err != nil {
		return fmt.Errorf("answering the phone: %w", err)
	}
	if ctx.Err() == nil {
		p.printf("Waiting for the phone to read the answer (at most %s, Ctrl-C to quit)...\n", p.cfg.linger)
		lctx, cancel := context.WithTimeout(ctx, p.cfg.linger)
		defer cancel()
		ch.Recv(lctx) // received; nothing depends on it
	}
	return err
}

// serve turns a request into its result, and also returns why it did not
// succeed. Ctrl-C at any point is cancelled.
func (p *pairing) serve(ctx context.Context, raw []byte, words string) (protocol.Result, error) {
	res, err := p.process(ctx, raw, words)
	var f *failure
	switch {
	case err == nil:
		p.printf("\nDone.\n")
		return *res, nil
	case ctx.Err() != nil, errors.Is(err, errCancelled):
		p.printf("\nCancelled.\n")
		return protocol.Result{Status: protocol.StatusCancelled}, errCancelled
	case errors.As(err, &f):
	default:
		f = &failure{code: protocol.CodeTxFailed, detail: err.Error()}
	}
	p.printf("\nFailed: %s\n", esc(f.detail))
	return protocol.Result{Status: protocol.StatusError, Code: f.code, Detail: f.detail}, f
}

func (p *pairing) process(ctx context.Context, raw []byte, words string) (*protocol.Result, error) {
	now := p.now()
	req, err := parseRequest(raw, now)
	if err != nil {
		return nil, err
	}
	remote, n, err := p.network(ctx, req)
	if err != nil {
		return nil, err
	}
	key, err := p.findKey(ctx, req.signer)
	if err != nil {
		return nil, err
	}
	acc, err := n.Account(ctx, req.signer)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, failf(protocol.CodeTxFailed, "%s has no account on %s: it has never received coins", req.signer, req.ChainID)
	}

	rv := review{req: req, checkWords: words, remote: remote, key: key, balance: acc.Coins, now: now}
	if gp, err := n.GasPrice(ctx); err == nil {
		rv.minFee = minFee(req.tx.Fee.GasWanted, gp)
	}
	rv.params = map[int][]string{}
	docs := map[string]map[string][]string{} // one qdoc per realm
	needSessions := false
	for i, msg := range req.tx.Msgs {
		switch m := msg.(type) {
		case vm.MsgCall:
			if _, done := docs[m.PkgPath]; !done {
				docs[m.PkgPath], _ = n.FuncParams(ctx, m.PkgPath)
			}
			if names, ok := docs[m.PkgPath][m.Func]; ok {
				rv.params[i] = names
			}
		case auth.MsgRevokeSession, auth.MsgRevokeAllSessions:
			needSessions = true
		}
	}
	if needSessions {
		if s, err := n.Sessions(ctx, req.signer); err == nil {
			rv.sessions = &s
		}
	}
	rv.sim = simulateUnsigned(ctx, n, req.tx, acc, key)
	p.printf("\n")
	rv.render(p.out)

	ok, err := p.ask(ctx, "Sign? [y/N] ")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errCancelled
	}
	signed, err := p.sign(ctx, req, acc, key)
	if err != nil {
		return nil, err
	}
	return p.deliver(ctx, n, req.Mode, signed)
}

// deliver returns the signed bytes (signtx), or simulates and broadcasts
// them (sendtx). Only signing is delegated to gnokey: these use no key.
func (p *pairing) deliver(ctx context.Context, n node, mode string, signed std.Tx) (*protocol.Result, error) {
	if mode == protocol.ModeSignTx {
		bz, err := amino.Marshal(signed)
		if err != nil {
			return nil, err
		}
		return &protocol.Result{Status: protocol.StatusSuccess, SignedTx: base64.StdEncoding.EncodeToString(bz)}, nil
	}
	p.printf("Simulating the signed transaction...\n")
	sim, err := n.Simulate(ctx, signed)
	if err != nil {
		return nil, err
	}
	if sim.IsErr() {
		return nil, failf(protocol.CodeTxFailed, "simulation failed, nothing was broadcast: %s", reason(sim.ResponseBase))
	}
	p.printf("Broadcasting...\n")
	bres, err := n.Broadcast(ctx, signed)
	switch {
	case err != nil:
		return nil, failf(protocol.CodeTxFailed, "broadcast: %v", err)
	case bres.CheckTx.IsErr():
		return nil, failf(protocol.CodeTxFailed, "rejected: %s", reason(bres.CheckTx.ResponseBase))
	case bres.DeliverTx.IsErr():
		return nil, failf(protocol.CodeTxFailed, "failed in block %d: %s", bres.Height, reason(bres.DeliverTx.ResponseBase))
	}
	hash := base64.StdEncoding.EncodeToString(bres.Hash)
	p.printf("Included in block %d, hash %s\n", bres.Height, hash)
	return &protocol.Result{Status: protocol.StatusSuccess, Hash: hash, Height: bres.Height}, nil
}

// minFee is the chain's minimum fee for gasWanted, nil when unknown or too
// large to compare.
func minFee(gasWanted int64, gp std.GasPrice) *std.Coin {
	if gp.Gas <= 0 {
		return nil
	}
	// Rounded up, in big.Int: gasWanted comes from the phone.
	fee := new(big.Int).Mul(big.NewInt(gasWanted), big.NewInt(gp.Price.Amount))
	fee.Add(fee, big.NewInt(gp.Gas-1)).Quo(fee, big.NewInt(gp.Gas))
	if !fee.IsInt64() {
		return nil
	}
	return &std.Coin{Denom: gp.Price.Denom, Amount: fee.Int64()}
}

// network resolves the node for the request's chain. With no -remote, the
// phone's advertised rpc is only used if the user agrees.
func (p *pairing) network(ctx context.Context, req *txRequest) (string, node, error) {
	declined := func(format string, args ...any) (string, node, error) {
		return "", nil, failf(protocol.CodeNetworkDeclined, format, args...)
	}
	remote := p.cfg.remote
	if remote == "" {
		if req.RPC == "" {
			return declined("no node for %s: run gnokey-pair with -remote", req.ChainID)
		}
		if u, err := url.Parse(req.RPC); err != nil || u.Scheme != "http" && u.Scheme != "https" {
			return declined("the phone suggested an invalid node %s", quote(req.RPC))
		}
		ok, err := p.ask(ctx, fmt.Sprintf("No -remote given. Use the node the phone suggests for %s, %s? [y/N] ", esc(req.ChainID), esc(req.RPC)))
		if err != nil {
			return "", nil, err
		}
		if !ok {
			return declined("the user declined the node %s", req.RPC)
		}
		remote = req.RPC
	}
	n, err := p.dial(remote)
	var id string
	if err == nil {
		id, err = n.ChainID(ctx)
	}
	if err != nil {
		return declined("node %s: %v", remote, err)
	}
	if id != req.ChainID {
		return declined("node %s is on chain %s, not %s", remote, id, req.ChainID)
	}
	return remote, n, nil
}

// findKey looks for the signer in gnokey list. If the list cannot be read,
// it does not guess: signing goes ahead and a missing key fails there.
func (p *pairing) findKey(ctx context.Context, signer crypto.Address) (*keyInfo, error) {
	keys, err := p.keys.List(ctx)
	if err != nil {
		p.printf("warning: %s; trying to sign anyway\n", esc(err.Error()))
		return nil, nil
	}
	for _, k := range keys {
		if k.Address == signer.String() {
			return &k, nil
		}
	}
	return nil, failf(protocol.CodeSignerUnavailable, "no key for %s in the keybase", signer)
}

// simulateUnsigned runs the transaction before the user is asked anything.
// The chain does not verify signatures when simulating what v1 accepts (see
// txNeedsSimulationSignature in tm2/pkg/crypto/keys/client/maketx.go), but it
// needs the signer's public key.
func simulateUnsigned(ctx context.Context, n node, tx std.Tx, acc *std.BaseAccount, key *keyInfo) simulation {
	var sig std.Signature
	if acc.PubKey == nil {
		if key == nil {
			return simulation{skipped: "the account has no public key on chain yet"}
		}
		pub, err := crypto.PubKeyFromBech32(key.PubKey)
		if err != nil {
			return simulation{skipped: err.Error()}
		}
		sig.PubKey = pub
	}
	tx.Signatures = []std.Signature{sig}
	res, err := n.Simulate(ctx, tx)
	switch {
	case err != nil:
		return simulation{skipped: err.Error()}
	case res.IsErr():
		return simulation{err: reason(res.ResponseBase)}
	}
	return simulation{gasUsed: res.GasUsed}
}

// sign has gnokey sign the reviewed transaction in a private directory, then
// verifies what it wrote.
func (p *pairing) sign(ctx context.Context, req *txRequest, acc *std.BaseAccount, key *keyInfo) (std.Tx, error) {
	var signed std.Tx
	dir, err := os.MkdirTemp("", "gnokey-pair-") // 0700
	if err != nil {
		return signed, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "tx.json")
	bz, err := amino.MarshalJSON(req.tx)
	if err != nil {
		return signed, err
	}
	if err := os.WriteFile(path, bz, 0o600); err != nil {
		return signed, err
	}
	if key != nil && key.Type == "ledger" {
		p.printf("Confirm on the Ledger.\n")
	}
	err = p.keys.Sign(ctx, path, req.ChainID, acc.AccountNumber, acc.Sequence, req.signer.String())
	switch {
	case errors.Is(err, errKeyNotFound):
		return signed, failf(protocol.CodeSignerUnavailable, "%v", err)
	case err != nil:
		return signed, failf(protocol.CodeTxFailed, "%v", err)
	}
	if bz, err = os.ReadFile(path); err != nil {
		return signed, err
	}
	if err := amino.UnmarshalJSON(bz, &signed); err != nil {
		return signed, failf(protocol.CodeTxFailed, "gnokey wrote an unreadable transaction: %v", err)
	}
	if err := verifySigned(signed, req.tx, req.signer, req.ChainID, acc.AccountNumber, acc.Sequence); err != nil {
		return signed, failf(protocol.CodeTxFailed, "gnokey produced something else than what you approved, so nothing is sent: %v", err)
	}
	return signed, nil
}

// ask prints a yes/no question; only a typed y or yes means yes. Its reader
// goroutine only outlives it on Ctrl-C, after which nothing reads the
// terminal: one long-lived reader would steal gnokey's password.
func (p *pairing) ask(ctx context.Context, question string) (bool, error) {
	p.printf("%s", question)
	line := make(chan string, 1)
	go func() {
		s, _ := p.in.ReadString('\n')
		line <- s
	}()
	select {
	case s := <-line:
		s = strings.ToLower(strings.TrimSpace(s))
		return s == "y" || s == "yes", nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
