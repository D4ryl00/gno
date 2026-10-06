package main

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/tm2/pkg/amino"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// signedPoll is how often byHand looks at the file the user's gnokey signs.
const signedPoll = 500 * time.Millisecond

// byHand gives the user the gnokey command instead of running gnokey: the
// command does what gnokey-pair would otherwise do. For sendtx it signs,
// dry-runs and broadcasts, and the phone watches the chain. Otherwise it only
// signs, and gnokey-pair takes the signed file back, checks it and returns it.
func (p *pairing) byHand(ctx context.Context, req *txRequest, acc *std.BaseAccount, remote string) (*protocol.Result, error) {
	dir, path, err := writeTx(req.tx)
	if err != nil {
		return nil, err
	}
	sign := p.gnokeyCommand("sign")
	if p.cfg.home != "" {
		sign = append(sign, "-home", p.cfg.home)
	}
	sign = append(sign, "-tx-path", path, "-chainid", req.ChainID,
		"-account-number", strconv.FormatUint(acc.AccountNumber, 10),
		"-account-sequence", strconv.FormatUint(acc.Sequence, 10), req.signer.String())

	if req.Mode == protocol.ModeSendTx {
		p.pending = dir // the command still needs the file: run removes it
		cmd := shellJoin(sign) +
			" \\\n  && " + shellJoin(append(p.gnokeyCommand("broadcast"), "-dry-run", "-remote", remote, path)) +
			" \\\n  && " + shellJoin(append(p.gnokeyCommand("broadcast"), "-remote", remote, path))
		p.printf("\nRun this in another terminal; it signs and broadcasts. Your phone watches the chain.\n\n%s\n", cmd)
		return &protocol.Result{Status: protocol.StatusSuccess, Manual: true}, nil
	}

	defer os.RemoveAll(dir)
	p.printf("\nRun this in another terminal; gnokey-pair then sends the signed transaction to your phone.\n\n%s\n\nWaiting for the signature (at most %s, Ctrl-C to cancel)...\n", shellJoin(sign), p.cfg.timeout)
	signed, err := waitSigned(ctx, path, p.cfg.timeout)
	if err != nil {
		return nil, err
	}
	if err := verifySigned(signed, req.tx, req.signer, req.ChainID, acc.AccountNumber, acc.Sequence); err != nil {
		return nil, failf(protocol.CodeTxFailed, "the signed file is not what you approved, so nothing is sent: %v", err)
	}
	return p.deliver(ctx, nil, protocol.ModeSignTx, signed)
}

// gnokeyCommand starts a command line with the gnokey gnokey-pair would run.
func (p *pairing) gnokeyCommand(sub string) []string {
	bin := p.cfg.gnokey
	if bin == "" {
		bin = "gnokey"
	}
	return []string{bin, sub}
}

// waitSigned waits for path to hold a signed transaction. A half-written
// file does not decode, so it is read again at the next poll.
func waitSigned(ctx context.Context, path string, timeout time.Duration) (std.Tx, error) {
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(signedPoll)
	defer tick.Stop()
	for {
		var tx std.Tx
		if bz, err := os.ReadFile(path); err == nil && amino.UnmarshalJSON(bz, &tx) == nil && len(tx.Signatures) > 0 {
			return tx, nil
		}
		select {
		case <-wctx.Done():
			if ctx.Err() != nil {
				return tx, ctx.Err()
			}
			return tx, failf(protocol.CodeTxFailed, "no signature in %s after %s", path, timeout)
		case <-tick.C:
		}
	}
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}
