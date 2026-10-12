package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gnolang/gno/contribs/gnokey-pair/protocol"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/crypto/secp256k1"
	"github.com/gnolang/gno/tm2/pkg/std"
)

const testPhrase = "example.com: prove this wallet is yours · 9UQV & +"

func dataRequestJSON(t *testing.T, edit func(*protocol.Request)) []byte {
	t.Helper()
	r := protocol.Request{
		Kind: protocol.KindData, ChainID: "test-chain", Signer: signerAddr.String(), Data: testPhrase,
		Requester: &protocol.Requester{Name: "Gnokey Mobile", For: "example.com"},
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

// proofJSON is what gnokey sign-data writes for data, signed by key.
func proofJSON(t *testing.T, key crypto.PrivKey, chainID string, signer crypto.Address, data string) []byte {
	t.Helper()
	out, err := signDataProof(key, chainID, signer, data)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// signDataProof plays gnokey sign-data: Adena's SignArbitrary answer.
func signDataProof(key crypto.PrivKey, chainID string, signer crypto.Address, data string) ([]byte, error) {
	payload, err := std.SignDataPayload(chainID, signer, data)
	if err != nil {
		return nil, err
	}
	sig, err := key.Sign(payload)
	if err != nil {
		return nil, err
	}
	pub := key.PubKey().(secp256k1.PubKeySecp256k1)
	return json.Marshal(map[string]any{
		"signed": json.RawMessage(payload),
		"signature": map[string]any{
			"pubKey":    map[string]string{"typeUrl": "/tm.PubKeySecp256k1", "value": base64.StdEncoding.EncodeToString(pub[:])},
			"signature": base64.StdEncoding.EncodeToString(sig),
		},
	})
}

func TestParseDataRequest(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		edit func(*protocol.Request)
		want string // substring of the detail; empty means valid
	}{
		{"valid", nil, ""},
		{"no chain", func(r *protocol.Request) { r.ChainID = "" }, "missing chainid"},
		{"bad signer", func(r *protocol.Request) { r.Signer = "g1nope" }, "signer"},
		{"no data", func(r *protocol.Request) { r.Data = "" }, "no text to sign"},
		{"too long", func(r *protocol.Request) { r.Data = strings.Repeat("a", protocol.MaxDataSize+1) }, "exceeds"},
		{"control character", func(r *protocol.Request) { r.Data = "a\nb" }, "control character"},
		{"a tx too", func(r *protocol.Request) { r.Tx = json.RawMessage(`{}`) }, "carries a transaction"},
		{"offline", func(r *protocol.Request) { r.Offline = true }, "offline"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseDataRequest(dataRequestJSON(t, c.edit))
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
}

func TestVerifyProof(t *testing.T) {
	t.Parallel()
	req, err := parseDataRequest(dataRequestJSON(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyProof(proofJSON(t, signerKey, "test-chain", signerAddr, testPhrase), req); err != nil {
		t.Fatalf("a good proof: %v", err)
	}
	for name, proof := range map[string][]byte{
		"another key":    proofJSON(t, otherKey, "test-chain", otherAddr, testPhrase),
		"another chain":  proofJSON(t, signerKey, "other-chain", signerAddr, testPhrase),
		"another phrase": proofJSON(t, signerKey, "test-chain", signerAddr, "something else"),
		"not JSON":       []byte("{"),
	} {
		if verifyProof(proof, req) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPipelineData(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		edit   func(*pipelineCase)
		status string
		code   string
	}{
		{name: "signed", status: protocol.StatusSuccess},
		{name: "declined", edit: func(c *pipelineCase) { c.answers = "\n" }, status: protocol.StatusCancelled},
		{
			name: "gnokey signs with another key", edit: func(c *pipelineCase) { c.keys.key = otherKey },
			status: protocol.StatusError, code: protocol.CodeTxFailed,
		},
		{
			name: "signer not in keybase", edit: func(c *pipelineCase) { c.keys.keys = nil },
			status: protocol.StatusError, code: protocol.CodeSignerUnavailable,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pc := newPipelineCase()
			pc.node.chainID, pc.node.acc = "never-asked", nil // a data request touches no node
			if c.edit != nil {
				c.edit(pc)
			}
			res, out := pc.serve(t, context.Background(), dataRequestJSON(t, nil))
			if res.Status != c.status || res.Code != c.code {
				t.Fatalf("got %s/%s, want %s/%s\n%s", res.Status, res.Code, c.status, c.code, out)
			}
			if c.status != protocol.StatusSuccess {
				return
			}
			if !strings.Contains(out, quote(testPhrase)) || !strings.Contains(out, "nothing goes on chain") {
				t.Errorf("the review does not show the phrase:\n%s", out)
			}
			req, _ := parseDataRequest(dataRequestJSON(t, nil))
			if err := verifyProof(res.Proof, req); err != nil {
				t.Errorf("the returned proof: %v", err)
			}
		})
	}
}

func TestPipelineDataByHand(t *testing.T) {
	t.Parallel()
	pc := newPipelineCase()
	var out lockedBuffer
	p := pc.pairing(strings.NewReader("y\n"), &out)
	p.cfg.manual = true
	p.cfg.timeout = 3 * time.Second
	done := make(chan protocol.Result, 1)
	go func() {
		res, _ := p.serve(context.Background(), dataRequestJSON(t, nil), "aardvark adroitness")
		done <- res
	}()
	path := waitForOutputPath(t, &out) // the -output-document of the printed command
	if err := writeFile(path, proofJSON(t, signerKey, "test-chain", signerAddr, testPhrase)); err != nil {
		t.Fatal(err)
	}
	res := <-done
	if res.Status != protocol.StatusSuccess || len(res.Proof) == 0 {
		t.Fatalf("got %+v\n%s", res, out.String())
	}
	if !bytes.Contains([]byte(out.String()), []byte("gnokey sign-data -chainid test-chain -data-path ")) {
		t.Errorf("no gnokey sign-data command:\n%s", out.String())
	}
}
