package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	"github.com/gnolang/gno/tm2/pkg/sdk/auth"
	"github.com/gnolang/gno/tm2/pkg/sdk/bank"
	"github.com/gnolang/gno/tm2/pkg/std"
)

func TestParseRequest(t *testing.T) {
	t.Parallel()
	expired := createSessionMsg()
	expired.ExpiresAt = testNow.Unix() - 1
	otherCall := callMsg()
	otherCall.Caller = otherAddr
	badSend := bank.MsgSend{FromAddress: signerAddr, ToAddress: otherAddr}
	noFee := newTx(callMsg())
	noFee.Fee.GasWanted = 0

	for _, c := range []struct {
		name string
		raw  []byte
		want string // substring of the detail; empty means valid
	}{
		{"valid sendtx", requestJSON(t, newTx(callMsg(), createSessionMsg()), nil), ""},
		{"valid signtx", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) { r.Mode = protocol.ModeSignTx }), ""},
		{"all reviewable types", requestJSON(t, newTx(
			callMsg(),
			bank.MsgSend{FromAddress: signerAddr, ToAddress: otherAddr, Amount: std.Coins{{Denom: "ugnot", Amount: 1}}},
			auth.MsgRevokeSession{Creator: signerAddr, SessionKey: sessionKey.PubKey()},
			auth.MsgRevokeAllSessions{Creator: signerAddr},
		), nil), ""},
		{"not JSON", []byte("{"), "malformed request"},
		{"unknown kind", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) { r.Kind = "request" }), "unsupported kind"},
		{"unknown mode", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) { r.Mode = "send" }), "unsupported mode"},
		{"no chain id", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) { r.ChainID = "" }), "missing chainid"},
		{"bad signer", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) { r.Signer = "g1nope" }), "signer"},
		{"no tx", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) { r.Tx = nil }), "missing tx"},
		{"too large", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) {
			r.Tx = json.RawMessage(`"` + strings.Repeat("x", protocol.MaxTxSize) + `"`)
		}), "exceeds"},
		{"not a tx", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) { r.Tx = json.RawMessage(`{"msg":3}`) }), "tx:"},
		{"already signed", requestJSON(t, signTx(t, newTx(callMsg()), signerKey, "test-chain", 0, 0), nil), "already signed"},
		{"no messages", requestJSON(t, newTx(), nil), "no messages"},
		{"no gas", requestJSON(t, noFee, nil), "invalid fee"},
		{"not reviewable", requestJSON(t, newTx(vm.MsgRun{Caller: signerAddr}), nil), "cannot review vm/run"},
		{"invalid message", requestJSON(t, newTx(badSend), nil), "message 1"},
		{"expired session", requestJSON(t, newTx(expired), nil), "expired"},
		{"other signer", requestJSON(t, newTx(otherCall), nil), "signed by"},
		{"extra signer", requestJSON(t, newTx(callMsg(), otherCall), nil), "signed by"},
		{"valid offline", requestJSON(t, newTx(callMsg()), offline), ""},
		{"offline sendtx", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) {
			offline(r)
			r.Mode = protocol.ModeSendTx
		}), "nobody would broadcast"},
		{"offline without account", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) {
			offline(r)
			r.Account = nil
		}), "needs the account number and sequence"},
		{"offline bad number", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) {
			offline(r)
			r.Account.Number = "-1"
		}), "account number"},
		{"offline bad sequence", requestJSON(t, newTx(callMsg()), func(r *protocol.Request) {
			offline(r)
			r.Account.Sequence = "3.5"
		}), "account sequence"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			req, err := parseRequest(c.raw, testNow)
			if c.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if req.signer != signerAddr {
					t.Fatalf("signer %s", req.signer)
				}
				if req.Offline && (req.accountNumber != 7 || req.sequence != 3) {
					t.Fatalf("account %d, sequence %d", req.accountNumber, req.sequence)
				}
				return
			}
			var f *failure
			if !errors.As(err, &f) || f.code != protocol.CodeInvalidRequest || !strings.Contains(f.detail, c.want) {
				t.Fatalf("got %v, want invalid_request containing %q", err, c.want)
			}
		})
	}
}
