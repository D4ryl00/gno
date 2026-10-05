package wormhole

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"golang.org/x/crypto/nacl/secretbox"
	"salsa.debian.org/vasudev/gospake2"
)

// Key schedule of the magic-wormhole generic protocol. Any change here breaks
// interoperability with the reference client.

const nonceSize = 24

var errDecrypt = errors.New("wormhole: message does not decrypt")

func derive(key []byte, purpose string) []byte {
	out, err := hkdf.Key(sha256.New, key, nil, purpose, 32)
	if err != nil {
		panic(err) // only for an oversized key length
	}
	return out
}

func phaseKey(key []byte, side, phase string) *[32]byte {
	s, p := sha256.Sum256([]byte(side)), sha256.Sum256([]byte(phase))
	var k [32]byte
	copy(k[:], derive(key, "wormhole:phase:"+string(s[:])+string(p[:])))
	return &k
}

func verifier(key []byte) []byte { return derive(key, "wormhole:verifier") }

// seal encrypts a message for (side, phase) and hex-encodes it as a mailbox body.
func seal(key []byte, side, phase string, plain []byte) string {
	var nonce [nonceSize]byte
	rand.Read(nonce[:]) // never fails since Go 1.24
	return hex.EncodeToString(secretbox.Seal(nonce[:], plain, &nonce, phaseKey(key, side, phase)))
}

func unseal(key []byte, side, phase, body string) ([]byte, error) {
	b, err := hex.DecodeString(body)
	if err != nil || len(b) < nonceSize+secretbox.Overhead {
		return nil, errDecrypt
	}
	var nonce [nonceSize]byte
	copy(nonce[:], b)
	plain, ok := secretbox.Open(nil, b[nonceSize:], &nonce, phaseKey(key, side, phase))
	if !ok {
		return nil, errDecrypt
	}
	return plain, nil
}

// newPake starts SPAKE2 on the code and returns the "pake" phase body,
// which is not encrypted.
func newPake(appID, code string) (gospake2.SPAKE2, string) {
	s := gospake2.SPAKE2Symmetric(gospake2.NewPassword(code), gospake2.NewIdentityS(appID))
	body, _ := json.Marshal(map[string]string{"pake_v1": hex.EncodeToString(s.Start())})
	return s, hex.EncodeToString(body)
}

func finishPake(s gospake2.SPAKE2, peerBody string) ([]byte, error) {
	raw, err := hex.DecodeString(peerBody)
	if err != nil {
		return nil, err
	}
	var m struct {
		Pake string `json:"pake_v1"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	msg, err := hex.DecodeString(m.Pake)
	if err != nil {
		return nil, err
	}
	return s.Finish(msg)
}
