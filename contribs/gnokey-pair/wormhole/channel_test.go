package wormhole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPair(t *testing.T) {
	needRelay(t)
	t.Parallel()
	ctx := testContext(t, 20*time.Second)
	desk, phone := pair(t, ctx, relayURL)

	if v := desk.Verifier(); v == nil || !bytes.Equal(v, phone.Verifier()) {
		t.Fatalf("verifiers differ: %x, %x", v, phone.Verifier())
	}
	words := desk.CheckWords()
	if len(strings.Fields(words)) != 2 || words != phone.CheckWords() {
		t.Fatalf("check words: %q, %q", words, phone.CheckWords())
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal(desk.PeerVersions(), &v); err != nil || v["gnokey-pair"] == nil {
		t.Fatalf("peer versions %s: %v", desk.PeerVersions(), err)
	}

	send(t, phone, "request")
	expect(t, ctx, desk, "request")
	send(t, desk, "result")
	expect(t, ctx, phone, "result")
	send(t, phone, "received")
	expect(t, ctx, desk, "received")

	closeAll(t, ctx, phone, desk)
	if _, err := desk.Recv(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("Recv after Close: %v", err)
	}
}

func TestMessagesInOrder(t *testing.T) {
	needRelay(t)
	t.Parallel()
	ctx := testContext(t, 20*time.Second)
	desk, phone := pair(t, ctx, relayURL)
	for _, m := range []string{"a", "b", "c"} {
		send(t, phone, m)
	}
	for _, m := range []string{"a", "b", "c"} {
		expect(t, ctx, desk, m)
	}
	expectNothing(t, desk)
}

func TestWrongCode(t *testing.T) {
	needRelay(t)
	t.Parallel()
	ctx := testContext(t, 20*time.Second)
	desk := allocate(t, ctx, testConfig(relayURL))
	phone := join(t, ctx, testConfig(relayURL), desk.Code()+"x")
	errA, errB := handshake(ctx, desk, phone)
	if !errors.Is(errA, ErrWrongCode) || !errors.Is(errB, ErrWrongCode) {
		t.Fatalf("desktop %v, phone %v", errA, errB)
	}
	if err := desk.Send([]byte("x")); err == nil {
		t.Fatal("Send after a failed handshake")
	}
	if err := desk.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestHandshakeTimeout(t *testing.T) {
	needRelay(t)
	t.Parallel()
	desk := allocate(t, testContext(t, 10*time.Second), testConfig(relayURL))
	err := desk.Handshake(testContext(t, 300*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := desk.Close(testContext(t, 5*time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestThirdSideRefused(t *testing.T) {
	needRelay(t)
	t.Parallel()
	ctx := testContext(t, 20*time.Second)
	desk := allocate(t, ctx, testConfig(relayURL))
	phone := join(t, ctx, testConfig(relayURL), desk.Code())
	if ch, err := Join(ctx, testConfig(relayURL), desk.Code()); !errors.Is(err, ErrCrowded) {
		if ch != nil {
			closeOnCleanup(t, ch)
		}
		t.Fatalf("third side: %v", err)
	}
	mustHandshake(t, ctx, desk, phone)
	// The refused side's claim stays on the server; closing must still work.
	closeAll(t, ctx, phone, desk)
}

func TestNotReady(t *testing.T) {
	needRelay(t)
	t.Parallel()
	ctx := testContext(t, 10*time.Second)
	desk := allocate(t, ctx, testConfig(relayURL))
	if err := desk.Send([]byte("x")); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	if _, err := desk.Recv(ctx); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	if desk.Verifier() != nil || desk.CheckWords() != "" || desk.PeerVersions() != nil {
		t.Fatal("handshake results before Handshake")
	}
}

func TestConfigErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := Allocate(ctx, Config{}); err == nil {
		t.Fatal("no relay URL accepted")
	}
	if _, err := Allocate(ctx, Config{RelayURL: "ws://x", Versions: func() {}}); err == nil {
		t.Fatal("unmarshalable versions accepted")
	}
	for _, code := range []string{"", "7", "7-", "-a-b", "x7-a-b", "7a-b"} {
		if _, err := Join(ctx, Config{RelayURL: "ws://x"}, code); !errors.Is(err, ErrBadCode) {
			t.Errorf("code %q: %v", code, err)
		}
	}
	if err := (&Channel{}).Send(make([]byte, MaxMessageSize+1)); err == nil {
		t.Fatal("oversized message accepted")
	}
}

func TestCheckWords(t *testing.T) {
	t.Parallel()
	for v, want := range map[[2]byte]string{
		{0x00, 0x00}: "aardvark adroitness",
		{0x00, 0x01}: "aardvark adviser",
		{0xff, 0xfe}: "zulu yesteryear",
	} {
		if got := checkWords(v[:]); got != want {
			t.Errorf("%x: %q, want %q", v, got, want)
		}
	}
}

func TestSeal(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{7}, 32)
	body := seal(key, "side", "0", []byte("hi"))
	if got, err := unseal(key, "side", "0", body); err != nil || string(got) != "hi" {
		t.Fatalf("%q, %v", got, err)
	}
	for _, c := range []struct{ side, phase, body string }{
		{"other", "0", body},
		{"side", "1", body},
		{"side", "0", "zz"},
		{"side", "0", body[:20]},
	} {
		if _, err := unseal(key, c.side, c.phase, c.body); err == nil {
			t.Errorf("%+v opened", c)
		}
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	for attempt, want := range []time.Duration{0, minBackoff, 2 * minBackoff, 4 * minBackoff} {
		if got := backoff(attempt); got != want {
			t.Errorf("attempt %d: %v, want %v", attempt, got, want)
		}
	}
	if backoff(1000) != maxBackoff {
		t.Error("backoff not capped")
	}
}
