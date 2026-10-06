package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/gnolang/gno/tm2/pkg/amino"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// verifySigned checks the transaction gnokey wrote before it is used, so
// that gnokey-pair never returns or broadcasts anything other than what the
// user approved, whatever gnokey version is installed.
func verifySigned(signed, reviewed std.Tx, signer crypto.Address, chainID string, accountNumber, sequence uint64) error {
	unsigned := func(tx std.Tx) ([]byte, error) {
		tx.Signatures = nil
		return amino.MarshalJSON(tx)
	}
	want, err := unsigned(reviewed)
	if err != nil {
		return err
	}
	got, err := unsigned(signed)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("the signed transaction differs from the reviewed one")
	}
	if len(signed.Signatures) != 1 {
		return fmt.Errorf("%d signatures instead of one", len(signed.Signatures))
	}
	sig := signed.Signatures[0]
	switch {
	case sig.PubKey == nil:
		return errors.New("the signature carries no public key")
	case sig.PubKey.Address() != signer:
		return fmt.Errorf("signed by %s instead of %s", sig.PubKey.Address(), signer)
	case !sig.SessionAddr.IsZero():
		return errors.New("signed by a session key")
	}
	rendering, err := std.VerifySignaturePayload(sig.PubKey, signed.SignDoc(chainID, accountNumber, sequence), sig.Signature)
	if err != nil {
		return err
	}
	if rendering == std.PayloadRenderingNone {
		return errors.New("the signature does not verify for this chain, account number and sequence")
	}
	return nil
}
