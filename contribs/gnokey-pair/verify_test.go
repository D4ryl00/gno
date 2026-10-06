package main

import (
	"strings"
	"testing"

	"github.com/gnolang/gno/tm2/pkg/std"
)

func TestVerifySigned(t *testing.T) {
	t.Parallel()
	const chainID, accNum, seq = "test-chain", 7, 3
	reviewed := newTx(callMsg())
	good := signTx(t, reviewed, signerKey, chainID, accNum, seq)

	altered := reviewed
	altered.Fee.GasFee.Amount++
	sessionSig := good
	sessionSig.Signatures = []std.Signature{good.Signatures[0]}
	sessionSig.Signatures[0].SessionAddr = otherAddr
	noPub := good
	noPub.Signatures = []std.Signature{{Signature: good.Signatures[0].Signature}}

	for _, c := range []struct {
		name   string
		signed std.Tx
		want   string
	}{
		{"altered fee", signTx(t, altered, signerKey, chainID, accNum, seq), "differs"},
		{"altered memo", func() std.Tx { tx := good; tx.Memo = "x"; return tx }(), "differs"},
		{"second signature", signTx(t, good, otherKey, chainID, accNum, seq), "2 signatures"},
		{"no signature", reviewed, "0 signatures"},
		{"other key", signTx(t, reviewed, otherKey, chainID, accNum, seq), "signed by"},
		{"other account number", signTx(t, reviewed, signerKey, chainID, accNum+1, seq), "does not verify"},
		{"other chain", signTx(t, reviewed, signerKey, "other", accNum, seq), "does not verify"},
		{"session signature", sessionSig, "session"},
		{"no public key", noPub, "no public key"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := verifySigned(c.signed, reviewed, signerAddr, chainID, accNum, seq)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
	if err := verifySigned(good, reviewed, signerAddr, chainID, accNum, seq); err != nil {
		t.Fatal(err)
	}
}
