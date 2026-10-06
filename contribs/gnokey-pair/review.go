package main

import (
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/sdk/auth"
	"github.com/gnolang/gno/tm2/pkg/sdk/bank"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// review is everything the review shows. All of it comes from the decoded
// transaction, the chain or the keybase, except the requester, which is only
// a claimed label.
type review struct {
	req        *txRequest
	checkWords string
	remote     string
	key        *keyInfo         // nil when gnokey list could not be read
	params     map[int][]string // MsgCall parameter names by message index
	balance    std.Coins
	minFee     *std.Coin // nil when the gas price is unknown
	sim        simulation
	now        time.Time
}

type simulation struct {
	gasUsed int64
	err     string // the chain's reason when it would fail
	skipped string // why it did not run
}

// line writes a label in column, then the value. Top-level labels and
// message details use their own columns.
func line(b *strings.Builder, column, label, format string, args ...any) {
	fmt.Fprintf(b, column, label)
	fmt.Fprintf(b, format+"\n", args...)
}

const (
	topColumn = "%-12s"
	msgColumn = "   %-13s"
)

func (r *review) render(w io.Writer) {
	var b strings.Builder
	tx := r.req.tx
	b.WriteString("gnokey-pair: request from your phone\n")
	fmt.Fprintf(&b, "Check words:  %s      (compare with the phone; stop if they differ)\n\n", strings.Join(strings.Fields(r.checkWords), "  "))

	switch rq := r.req.Requester; {
	case rq == nil || rq.Name == "" && rq.For == "":
		line(&b, topColumn, "Requester", "not stated")
	case rq.For == "":
		line(&b, topColumn, "Requester", "%s   (claimed, not verified)", quote(rq.Name))
	default:
		line(&b, topColumn, "Requester", "%s for %s   (claimed, not verified)", quote(rq.Name), quote(rq.For))
	}
	line(&b, topColumn, "Network", "%s via %s", esc(r.req.ChainID), esc(r.remote))
	signer := r.req.signer.String()
	if r.key != nil {
		signer += "  " + quote(r.key.Name)
		if r.key.Type == "ledger" {
			signer += " (Ledger: confirm on the device)"
		}
	}
	line(&b, topColumn, "Signer", "%s", signer)
	if r.req.Mode == protocol.ModeSignTx {
		line(&b, topColumn, "Action", "sign only; the phone broadcasts")
	} else {
		line(&b, topColumn, "Action", "sign and broadcast")
	}
	b.WriteString("\n")

	for i, msg := range tx.Msgs {
		r.renderMsg(&b, i, msg)
	}
	b.WriteString("\n")

	if tx.Memo != "" {
		line(&b, topColumn, "Memo", "%s", quote(tx.Memo))
	}
	fee := tx.Fee.GasFee
	line(&b, topColumn, "Fee", "%s (gas %s), balance %s", formatCoin(fee), formatInt(tx.Fee.GasWanted),
		formatAmount(big.NewInt(r.balance.AmountOf(fee.Denom)), fee.Denom))
	if fee.Amount > r.balance.AmountOf(fee.Denom) {
		line(&b, topColumn, "", "warning: the balance cannot pay this fee")
	}
	if r.minFee != nil && r.minFee.Denom == fee.Denom && big.NewInt(fee.Amount).Cmp(new(big.Int).Mul(big.NewInt(r.minFee.Amount), big.NewInt(10))) > 0 {
		line(&b, topColumn, "", "warning: more than 10 times the chain's minimum, %s", formatCoin(*r.minFee))
	}
	switch {
	case r.sim.skipped != "":
		line(&b, topColumn, "Simulation", "not run: %s", esc(r.sim.skipped))
	case r.sim.err != "":
		line(&b, topColumn, "Simulation", "would fail: %s", esc(r.sim.err))
	default:
		line(&b, topColumn, "Simulation", "ok, %s gas", formatInt(r.sim.gasUsed))
	}
	b.WriteString("\n")
	io.WriteString(w, b.String())
}

func (r *review) renderMsg(b *strings.Builder, i int, msg std.Msg) {
	n := fmt.Sprintf("%d. ", i+1)
	sub := func(label, format string, args ...any) { line(b, msgColumn, label, format, args...) }
	switch m := msg.(type) {
	case vm.MsgCall:
		fmt.Fprintf(b, "%sCall  %s.%s\n", n, esc(m.PkgPath), esc(m.Func))
		names := r.params[i]
		for j, arg := range m.Args {
			label := fmt.Sprintf("#%d", j+1)
			if len(names) == len(m.Args) {
				label = esc(names[j])
			}
			sub("", "%s = %s", label, quote(arg))
		}
		if len(m.Args) > 0 && len(names) != len(m.Args) {
			sub("", "(parameter names unavailable)")
		}
		if !m.Send.IsZero() {
			sub("Send", "%s", formatCoins(m.Send))
		}
		if !m.MaxDeposit.IsZero() {
			sub("Max deposit", "%s", formatCoins(m.MaxDeposit))
		}
	case bank.MsgSend:
		fmt.Fprintf(b, "%sSend  %s to %s\n", n, formatCoins(m.Amount), m.ToAddress)
	case bank.MsgMultiSend:
		fmt.Fprintf(b, "%sSend to several recipients\n", n)
		for _, in := range m.Inputs {
			sub("From", "%s  %s", in.Address, formatCoins(in.Coins))
		}
		for _, out := range m.Outputs {
			sub("To", "%s  %s", out.Address, formatCoins(out.Coins))
		}
	case auth.MsgCreateSession:
		fmt.Fprintf(b, "%sCreate session  %s (%s)\n", n, crypto.PubKeyToBech32(m.SessionKey), m.SessionKey.Address())
		for _, e := range m.AllowPaths {
			if w := allowWarning(e); w != "" {
				sub("Allowed", "%s   warning: %s", esc(e), w)
			} else {
				sub("Allowed", "%s", esc(e))
			}
		}
		sub("Budget", "%s", r.budget(m))
		if m.ExpiresAt == 0 {
			sub("Expires", "never   warning: valid until revoked")
		} else {
			sub("Expires", "%s (in %s)", formatTime(m.ExpiresAt), formatDuration(m.ExpiresAt-r.now.Unix()))
		}
	case auth.MsgRevokeSession:
		fmt.Fprintf(b, "%sRevoke session  %s (%s)\n", n, crypto.PubKeyToBech32(m.SessionKey), m.SessionKey.Address())
	case auth.MsgRevokeAllSessions:
		fmt.Fprintf(b, "%sRevoke all sessions\n", n)
	}
}

// budget states the spend limit in plain terms and its worst case over the
// whole lifetime, since the period resets for as long as the session lives.
func (r *review) budget(m auth.MsgCreateSession) string {
	switch {
	case m.SpendLimit.IsZero():
		return "none: the session cannot spend, not even its own gas"
	case m.SpendPeriod == 0:
		return formatCoins(m.SpendLimit) + " in total"
	}
	perPeriod := formatCoins(m.SpendLimit) + " per " + formatPeriod(m.SpendPeriod)
	if m.ExpiresAt == 0 {
		return perPeriod + ", with no upper bound: the session never expires"
	}
	life := m.ExpiresAt - r.now.Unix()
	windows := big.NewInt((life + m.SpendPeriod - 1) / m.SpendPeriod)
	worst := make([]string, len(m.SpendLimit))
	for i, c := range m.SpendLimit {
		worst[i] = formatAmount(new(big.Int).Mul(big.NewInt(c.Amount), windows), c.Denom)
	}
	return fmt.Sprintf("%s, at most %s over %s", perPeriod, strings.Join(worst, ", "), formatDuration(life))
}

// allowWarning flags the entries that let a session reach arbitrary code or
// move funds directly, as GnoConnect's review requires.
func allowWarning(entry string) string {
	switch entry {
	case "*":
		return "any message, including moving funds"
	case "vm/exec":
		return "calls to any realm"
	case "vm/run":
		return "runs arbitrary code"
	case "bank/send", "bank/multisend":
		return "moves funds directly"
	}
	return ""
}
