package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/gno.land/pkg/sdk/vm"
	"github.com/gnolang/gno/tm2/pkg/amino"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/sdk/auth"
	"github.com/gnolang/gno/tm2/pkg/sdk/bank"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// failure is an error Result; detail is shown to the user and sent along.
type failure struct {
	code, detail string
}

func (f *failure) Error() string { return f.code + ": " + esc(f.detail) }

func failf(code, format string, args ...any) *failure {
	return &failure{code: code, detail: fmt.Sprintf(format, args...)}
}

// txRequest is a request that passed validation.
type txRequest struct {
	protocol.Request
	tx     std.Tx
	signer crypto.Address
}

// parseRequest decodes and validates a request before anything is shown:
// what is not rendered is not reviewed, so it is not signed.
func parseRequest(raw []byte, now time.Time) (*txRequest, error) {
	invalid := func(format string, args ...any) error {
		return failf(protocol.CodeInvalidRequest, format, args...)
	}
	var r txRequest
	if err := json.Unmarshal(raw, &r.Request); err != nil {
		return nil, invalid("malformed request: %v", err)
	}
	if r.Kind != protocol.KindTx {
		return nil, invalid("unsupported kind %q", r.Kind)
	}
	if r.Mode != protocol.ModeSendTx && r.Mode != protocol.ModeSignTx {
		return nil, invalid("unsupported mode %q", r.Mode)
	}
	if r.ChainID == "" {
		return nil, invalid("missing chainid")
	}
	signer, err := crypto.AddressFromBech32(r.Signer)
	if err != nil {
		return nil, invalid("signer: %v", err)
	}
	r.signer = signer
	switch {
	case len(r.Request.Tx) == 0 || string(r.Request.Tx) == "null":
		return nil, invalid("missing tx")
	case len(r.Request.Tx) > protocol.MaxTxSize:
		return nil, invalid("tx of %d bytes exceeds %d", len(r.Request.Tx), protocol.MaxTxSize)
	}
	if err := amino.UnmarshalJSON(r.Request.Tx, &r.tx); err != nil {
		return nil, invalid("tx: %v", err)
	}
	if len(r.tx.Signatures) > 0 {
		return nil, invalid("tx is already signed")
	}
	if len(r.tx.Msgs) == 0 {
		return nil, invalid("tx has no messages")
	}
	if !r.tx.Fee.GasFee.IsValid() || r.tx.Fee.GasWanted <= 0 {
		return nil, invalid("invalid fee %s, gas %d", r.tx.Fee.GasFee, r.tx.Fee.GasWanted)
	}
	for i, msg := range r.tx.Msgs {
		// None of these needs a verified signature to simulate, which the
		// review relies on (simulateUnsigned). vm/run and vm/add_package do.
		switch m := msg.(type) {
		case vm.MsgCall, bank.MsgSend, bank.MsgMultiSend, auth.MsgRevokeSession, auth.MsgRevokeAllSessions:
		case auth.MsgCreateSession:
			if m.ExpiresAt != 0 && m.ExpiresAt <= now.Unix() {
				return nil, invalid("message %d: the session expired at %s", i+1, formatTime(m.ExpiresAt))
			}
		default:
			return nil, invalid("message %d: gnokey-pair cannot review %s/%s", i+1, msg.Route(), msg.Type())
		}
		if err := msg.ValidateBasic(); err != nil {
			return nil, invalid("message %d: %v", i+1, err)
		}
	}
	if s := r.tx.GetSigners(); len(s) != 1 || s[0] != signer {
		return nil, invalid("the messages must be signed by %s alone, not %v", signer, s)
	}
	return &r, nil
}
