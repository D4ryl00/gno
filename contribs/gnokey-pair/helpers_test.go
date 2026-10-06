package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	"github.com/gnolang/gno/tm2/pkg/amino"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/crypto/secp256k1"
	"github.com/gnolang/gno/tm2/pkg/sdk/auth"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// testNow is the clock of the unit tests.
var testNow = time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)

// Deterministic keys, so golden files are stable.
var (
	signerKey  = secp256k1.GenPrivKeySecp256k1([]byte("signer"))
	otherKey   = secp256k1.GenPrivKeySecp256k1([]byte("other"))
	sessionKey = secp256k1.GenPrivKeySecp256k1([]byte("session"))
	signerAddr = signerKey.PubKey().Address()
	otherAddr  = otherKey.PubKey().Address()
)

func testFee() std.Fee {
	return std.Fee{GasWanted: 2_000_000, GasFee: std.Coin{Denom: "ugnot", Amount: 2100}}
}

func createSessionMsg() auth.MsgCreateSession {
	return auth.MsgCreateSession{
		Creator:     signerAddr,
		SessionKey:  sessionKey.PubKey(),
		ExpiresAt:   testNow.Add(30 * 24 * time.Hour).Unix(),
		AllowPaths:  []string{"vm/exec:gno.land/r/demo/game"},
		SpendLimit:  std.Coins{{Denom: "ugnot", Amount: 5_000_000}},
		SpendPeriod: 3600,
	}
}

func callMsg() vm.MsgCall {
	return vm.MsgCall{Caller: signerAddr, PkgPath: "gno.land/r/demo/game", Func: "Play", Args: []string{"rock", "3"}}
}

func newTx(msgs ...std.Msg) std.Tx { return std.Tx{Msgs: msgs, Fee: testFee()} }

func txJSON(t *testing.T, tx std.Tx) json.RawMessage {
	t.Helper()
	b, err := amino.MarshalJSON(tx)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// requestJSON builds a valid sendtx request for tx, then applies edit.
func requestJSON(t *testing.T, tx std.Tx, edit func(*protocol.Request)) []byte {
	t.Helper()
	r := protocol.Request{
		Kind:      protocol.KindTx,
		Mode:      protocol.ModeSendTx,
		ChainID:   "test-chain",
		RPC:       "http://127.0.0.1:26657",
		Signer:    signerAddr.String(),
		Tx:        txJSON(t, tx),
		Requester: &protocol.Requester{Name: "Gnokey Mobile", For: "game.example"},
	}
	if edit != nil {
		edit(&r)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func signTx(t *testing.T, tx std.Tx, key crypto.PrivKey, chainID string, accountNumber, sequence uint64) std.Tx {
	t.Helper()
	tx, err := signWith(tx, key, chainID, accountNumber, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

// signWith appends key's signature, as gnokey sign does.
func signWith(tx std.Tx, key crypto.PrivKey, chainID string, accountNumber, sequence uint64) (std.Tx, error) {
	bz, err := tx.GetSignBytes(chainID, accountNumber, sequence)
	if err != nil {
		return tx, err
	}
	sig, err := key.Sign(bz)
	if err != nil {
		return tx, err
	}
	tx.Signatures = append(tx.Signatures, std.Signature{PubKey: key.PubKey(), Signature: sig})
	return tx, nil
}
