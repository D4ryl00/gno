package protocol

import "testing"

func TestPairingURI(t *testing.T) {
	t.Parallel()
	uri := PairingURI("7-guitarist-revenge", DefaultRelay)
	if want := "gnopair:7-guitarist-revenge?relay=wss%3A%2F%2Fgnokey-pair.berty.io%2Fv1"; uri != want {
		t.Fatalf("got %s, want %s", uri, want)
	}
	code, relay, err := ParsePairingURI(uri)
	if err != nil || code != "7-guitarist-revenge" || relay != DefaultRelay {
		t.Fatalf("got %q %q %v", code, relay, err)
	}

	if code, relay, err := ParsePairingURI("gnopair:12-a-b"); err != nil || code != "12-a-b" || relay != "" {
		t.Fatalf("no relay: got %q %q %v", code, relay, err)
	}
	for _, bad := range []string{
		"7-guitarist-revenge",
		"gnopair:",
		"gnopair:?relay=wss%3A%2F%2Fx",
		"gnopair:7-a-b?relay=https%3A%2F%2Fx",
		"gnopair:7-a-b?relay=wss%3A",
		"gnopair:7-a-b?relay=%zz",
	} {
		if _, _, err := ParsePairingURI(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}
