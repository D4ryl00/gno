package protocol

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// DefaultRelay is the gno.land mailbox server, the default of both ends.
const DefaultRelay = "wss://gnokey-pair.berty.io/v1"

// URIScheme starts what gnokey-pair's QR holds: gnopair:<code>?relay=<url>.
const URIScheme = "gnopair"

// PairingURI is what the QR holds. The relay is in it because both sides
// must use the same server.
func PairingURI(code, relay string) string {
	return URIScheme + ":" + code + "?relay=" + url.QueryEscape(relay)
}

// ParsePairingURI reads a PairingURI back. relay is empty when the URI names
// none; it is a ws:// or wss:// URL otherwise.
func ParsePairingURI(uri string) (code, relay string, err error) {
	rest, ok := strings.CutPrefix(uri, URIScheme+":")
	if !ok {
		return "", "", fmt.Errorf("not a %s: URI", URIScheme)
	}
	code, query, _ := strings.Cut(rest, "?")
	if code == "" {
		return "", "", errors.New("no code in the URI")
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return "", "", fmt.Errorf("URI query: %w", err)
	}
	relay = q.Get("relay")
	if relay != "" {
		u, err := url.Parse(relay)
		if err != nil || u.Scheme != "ws" && u.Scheme != "wss" || u.Host == "" {
			return "", "", fmt.Errorf("relay %q is not a ws:// or wss:// URL", relay)
		}
	}
	return code, relay, nil
}
