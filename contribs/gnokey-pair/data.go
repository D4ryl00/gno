package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/crypto/secp256k1"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// A data request asks the identity to sign a text — a service's challenge —
// to prove it controls the address. gnokey sign-data signs it; nothing goes
// on chain, so no node is needed.

// dataRequest is a data request that passed validation.
type dataRequest struct {
	protocol.Request
	signer crypto.Address
}

func parseDataRequest(raw []byte) (*dataRequest, error) {
	invalid := func(format string, args ...any) error {
		return failf(protocol.CodeInvalidRequest, format, args...)
	}
	var r dataRequest
	if err := json.Unmarshal(raw, &r.Request); err != nil {
		return nil, invalid("malformed request: %v", err)
	}
	switch {
	case r.Kind != protocol.KindData:
		return nil, invalid("unsupported kind %q", r.Kind)
	case r.ChainID == "":
		return nil, invalid("missing chainid")
	case len(r.Request.Tx) > 0 && string(r.Request.Tx) != "null":
		return nil, invalid("a data request carries a transaction")
	case r.Offline:
		return nil, invalid("a data request is never offline: it needs no network anyway")
	case r.Data == "":
		return nil, invalid("no text to sign")
	case len(r.Data) > protocol.MaxDataSize:
		return nil, invalid("a text of %d bytes exceeds %d", len(r.Data), protocol.MaxDataSize)
	case !utf8.ValidString(r.Data):
		return nil, invalid("the text is not UTF-8")
	case strings.IndexFunc(r.Data, unicode.IsControl) >= 0:
		return nil, invalid("the text has a control character")
	}
	signer, err := crypto.AddressFromBech32(r.Signer)
	if err != nil {
		return nil, invalid("signer: %v", err)
	}
	r.signer = signer
	return &r, nil
}

// kindOf reads a request's kind before it is parsed for that kind.
func kindOf(raw []byte) string {
	var k struct{ Kind string }
	json.Unmarshal(raw, &k)
	return k.Kind
}

func (p *pairing) processData(ctx context.Context, raw []byte, words string) (*protocol.Result, error) {
	req, err := parseDataRequest(raw)
	if err != nil {
		return nil, err
	}
	key, err := p.findKey(ctx, req.signer)
	if err != nil {
		return nil, err
	}
	p.printf("\n")
	p.renderData(req, key, words)

	question := "Sign? [y/N] "
	if p.cfg.manual {
		question = "Approve and print the gnokey command? [y/N] "
	}
	ok, err := p.ask(ctx, question)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errCancelled
	}

	dir, err := os.MkdirTemp("", "gnokey-pair-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	dataPath, outPath := filepath.Join(dir, "data.txt"), filepath.Join(dir, "proof.json")
	if err := os.WriteFile(dataPath, []byte(req.Data), 0o600); err != nil {
		return nil, err
	}

	var proof []byte
	if p.cfg.manual {
		cmd := p.gnokeyCommand("sign-data")
		if p.cfg.home != "" {
			cmd = append(cmd, "-home", p.cfg.home)
		}
		cmd = append(cmd, "-chainid", req.ChainID, "-data-path", dataPath, "-output-document", outPath, req.signer.String())
		p.printf("\nRun this in another terminal; gnokey-pair then sends the proof to your phone.\n\n%s\n\nWaiting for the proof (at most %s, Ctrl-C to cancel)...\n", shellJoin(cmd), p.cfg.timeout)
		if proof, err = waitProof(ctx, outPath, p.cfg.timeout); err != nil {
			return nil, err
		}
	} else {
		if key != nil && key.Type == "ledger" {
			p.printf("Confirm on the Ledger.\n")
		}
		err = p.keys.SignData(ctx, dataPath, req.ChainID, req.signer.String(), outPath)
		switch {
		case errors.Is(err, errKeyNotFound):
			return nil, failf(protocol.CodeSignerUnavailable, "%v", err)
		case err != nil:
			return nil, failf(protocol.CodeTxFailed, "%v", err)
		}
		if proof, err = os.ReadFile(outPath); err != nil {
			return nil, err
		}
	}
	if err := verifyProof(proof, req); err != nil {
		return nil, failf(protocol.CodeTxFailed, "gnokey produced something else than what you approved, so nothing is sent: %v", err)
	}
	return &protocol.Result{Status: protocol.StatusSuccess, Proof: json.RawMessage(proof)}, nil
}

func (p *pairing) renderData(req *dataRequest, key *keyInfo, words string) {
	var b strings.Builder
	b.WriteString("gnokey-pair: request from your phone\n")
	fmt.Fprintf(&b, "Check words:  %s      (compare with the phone; stop if they differ)\n\n", strings.Join(strings.Fields(words), "  "))
	switch rq := req.Requester; {
	case rq == nil || rq.Name == "" && rq.For == "":
		line(&b, topColumn, "Requester", "not stated")
	case rq.For == "":
		line(&b, topColumn, "Requester", "%s   (claimed, not verified)", quote(rq.Name))
	default:
		line(&b, topColumn, "Requester", "%s for %s   (claimed, not verified)", quote(rq.Name), quote(rq.For))
	}
	line(&b, topColumn, "Network", "%s", esc(req.ChainID))
	signer := req.signer.String()
	if key != nil {
		signer += "  " + quote(key.Name)
		if key.Type == "ledger" {
			signer += " (Ledger: confirm on the device)"
		}
	}
	line(&b, topColumn, "Signer", "%s", signer)
	line(&b, topColumn, "Action", "sign this text to prove the address is yours; nothing goes on chain")
	b.WriteString("\n")
	line(&b, topColumn, "Text", "%s", quote(req.Data))
	b.WriteString("\n")
	p.printf("%s", b.String())
}

// proofOutput is gnokey sign-data's output (Adena's SignArbitrary answer).
type proofOutput struct {
	Signed    json.RawMessage `json:"signed"`
	Signature struct {
		PubKey struct {
			TypeURL string `json:"typeUrl"`
			Value   string `json:"value"`
		} `json:"pubKey"`
		Signature string `json:"signature"`
	} `json:"signature"`
}

// verifyProof checks what gnokey wrote: the request's signer signed the
// request's text on the request's chain.
func verifyProof(proof []byte, req *dataRequest) error {
	var o proofOutput
	if err := json.Unmarshal(proof, &o); err != nil {
		return fmt.Errorf("unreadable proof: %w", err)
	}
	if o.Signature.PubKey.TypeURL != "/tm.PubKeySecp256k1" {
		return fmt.Errorf("a %q key, not secp256k1", o.Signature.PubKey.TypeURL)
	}
	raw, err := base64.StdEncoding.DecodeString(o.Signature.PubKey.Value)
	if err != nil || len(raw) != secp256k1.PubKeySecp256k1Size {
		return errors.New("an unreadable public key")
	}
	var pub secp256k1.PubKeySecp256k1
	copy(pub[:], raw)
	sig, err := base64.StdEncoding.DecodeString(o.Signature.Signature)
	if err != nil {
		return errors.New("an unreadable signature")
	}
	signed, err := std.VerifySignData(o.Signed, pub, sig)
	switch {
	case err != nil:
		return err
	case signed.Signer != req.signer:
		return fmt.Errorf("signed by %s instead of %s", signed.Signer, req.signer)
	case signed.ChainID != req.ChainID:
		return fmt.Errorf("signed for %s instead of %s", signed.ChainID, req.ChainID)
	case signed.Data != req.Data:
		return errors.New("another text was signed")
	}
	return nil
}

// waitProof waits for path to hold a proof. A half-written file does not
// decode, so it is read again at the next poll.
func waitProof(ctx context.Context, path string, timeout time.Duration) ([]byte, error) {
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(signedPoll)
	defer tick.Stop()
	for {
		var o proofOutput
		if bz, err := os.ReadFile(path); err == nil && json.Unmarshal(bz, &o) == nil && len(o.Signed) > 0 {
			return bz, nil
		}
		select {
		case <-wctx.Done():
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, failf(protocol.CodeTxFailed, "no proof in %s after %s", path, timeout)
		case <-tick.C:
		}
	}
}
