package main

import (
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/tm2/pkg/sdk/auth"
	"github.com/gnolang/gno/tm2/pkg/sdk/bank"
	"github.com/gnolang/gno/tm2/pkg/std"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func baseReview(msgs ...std.Msg) review {
	return review{
		req: &txRequest{
			Request: protocol.Request{
				ChainID:   "test-chain",
				Mode:      protocol.ModeSendTx,
				Requester: &protocol.Requester{Name: "Gnokey Mobile", For: "game.example"},
			},
			tx:     newTx(msgs...),
			signer: signerAddr,
		},
		checkWords: "aardvark adroitness",
		remote:     "https://rpc.example:443",
		key:        &keyInfo{Name: "main", Type: "local"},
		balance:    std.Coins{{Denom: "ugnot", Amount: 152_300_000}},
		minFee:     &std.Coin{Denom: "ugnot", Amount: 2000},
		sim:        simulation{gasUsed: 1_412_330},
		now:        testNow,
	}
}

func TestReviewGolden(t *testing.T) {
	t.Parallel()
	ctrl := "\x1b[2J\x1b[Hevil‮txt\r\n"
	for name, rv := range map[string]func() review{
		"create_session": func() review {
			rv := baseReview(createSessionMsg(), auth.MsgRevokeSession{Creator: signerAddr, SessionKey: otherKey.PubKey()})
			rv.key.Type = "ledger"
			rv.sessions = &[]session{{Address: otherAddr, ExpiresAt: testNow.Unix() - 86400, AllowPaths: []string{"vm/exec:gno.land/r/demo/game"}}}
			return rv
		},
		"session_risky": func() review {
			m := createSessionMsg()
			m.AllowPaths = []string{"*", "vm/exec", "vm/run", "bank/send", "bank/multisend", "vm/exec:gno.land/r/x"}
			m.ExpiresAt = 0
			lifetime := createSessionMsg()
			lifetime.SpendPeriod = 0
			lifetime.ExpiresAt = testNow.Unix() + 90_000
			none := createSessionMsg()
			none.SpendLimit = nil
			rv := baseReview(m, lifetime, none, auth.MsgRevokeAllSessions{Creator: signerAddr})
			rv.sessions = &[]session{
				{Address: otherAddr, AllowPaths: []string{"*"}},
				{Address: sessionKey.PubKey().Address(), ExpiresAt: testNow.Unix() + 3600, AllowPaths: []string{"vm/exec:gno.land/r/a", "bank/send"}},
			}
			return rv
		},
		"revoke": func() review {
			// Not on chain, then none to revoke.
			rv := baseReview(auth.MsgRevokeSession{Creator: signerAddr, SessionKey: otherKey.PubKey()}, auth.MsgRevokeAllSessions{Creator: signerAddr})
			rv.sessions = &[]session{}
			return rv
		},
		"revoke_unknown": func() review {
			return baseReview(auth.MsgRevokeSession{Creator: signerAddr, SessionKey: otherKey.PubKey()}, auth.MsgRevokeAllSessions{Creator: signerAddr})
		},
		"call": func() review {
			m := callMsg()
			m.Send = std.Coins{{Denom: "ugnot", Amount: 1_000_000}}
			m.MaxDeposit = std.Coins{{Denom: "ugnot", Amount: 1500}}
			rv := baseReview(m, callMsg())
			rv.params = map[int][]string{0: {"move", "rounds"}} // the second has none
			rv.req.Mode = protocol.ModeSignTx
			rv.req.Requester = nil
			return rv
		},
		"send": func() review {
			rv := baseReview(
				bank.MsgSend{FromAddress: signerAddr, ToAddress: otherAddr, Amount: std.Coins{{Denom: "ugnot", Amount: 12_345_678}, {Denom: "foo", Amount: 7}}},
				bank.MsgMultiSend{
					Inputs:  []bank.Input{{Address: signerAddr, Coins: std.Coins{{Denom: "ugnot", Amount: 3}}}},
					Outputs: []bank.Output{{Address: otherAddr, Coins: std.Coins{{Denom: "ugnot", Amount: 3}}}},
				},
			)
			rv.req.tx.Fee.GasFee.Amount = 200_000_000 // over 10x the minimum and the balance
			rv.sim = simulation{err: "insufficient funds"}
			rv.key = nil
			return rv
		},
		"control_characters": func() review {
			m := callMsg()
			m.PkgPath = "gno.land/r/" + ctrl
			m.Func = "F" + ctrl
			m.Args = []string{ctrl}
			s := createSessionMsg()
			s.AllowPaths = []string{"vm/exec:" + ctrl}
			rv := baseReview(m, s, auth.MsgRevokeSession{Creator: signerAddr, SessionKey: otherKey.PubKey()}, auth.MsgRevokeAllSessions{Creator: signerAddr})
			rv.sessions = &[]session{{Address: otherAddr, AllowPaths: []string{ctrl}}}
			rv.params = map[int][]string{0: {"p" + ctrl}}
			rv.req.Requester = &protocol.Requester{Name: ctrl, For: ctrl}
			rv.req.ChainID = ctrl
			rv.remote = ctrl
			rv.key.Name = ctrl
			rv.req.tx.Memo = ctrl
			rv.sim = simulation{skipped: ctrl}
			return rv
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			rv := rv()
			rv.render(&b)
			got := b.String()
			if strings.ContainsAny(got, "\x1b\r‮") {
				t.Fatalf("control character in the review:\n%q", got)
			}
			path := filepath.Join("testdata", "review", name+".txt")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("review differs from %s (go test -update):\n%s", path, got)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ got, want string }{
		{formatInt(0), "0"},
		{formatInt(999), "999"},
		{formatInt(1000), "1,000"},
		{formatInt(-1234567), "-1,234,567"},
		{formatCoin(std.Coin{Denom: "ugnot", Amount: 2100}), "0.0021 GNOT"},
		{formatCoin(std.Coin{Denom: "ugnot", Amount: 3600e6}), "3,600 GNOT"},
		{formatAmount(new(big.Int).Lsh(big.NewInt(1), 70), "ugnot"), "1,180,591,620,717,411.303424 GNOT"},
		{formatCoin(std.Coin{Denom: "foo\x1b", Amount: 5}), `5foo\x1b`},
		{formatCoins(nil), "nothing"},
		{formatDuration(59), "59 seconds"},
		{formatDuration(3600), "1 hour"},
		{formatDuration(3601), "about 1 hour"},
		{formatDuration(90_000), "25 hours"},
		{formatDuration(90_001), "about 1 day"},
		{formatDuration(30 * 86400), "30 days"},
		{formatPeriod(3600), "hour"},
		{formatPeriod(7200), "2 hours"},
		{esc("a\nb\x1b"), `a\nb\x1b`},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
