package wormhole

import (
	"fmt"
	"strings"

	"github.com/psanford/wormhole-william/wordlist"
)

// codeWords is the number of words after the nameplate: 16 bits, one online
// guess per code.
const codeWords = 2

func newCode(nameplate string) string {
	return nameplate + "-" + wordlist.ChooseWords(codeWords)
}

// parseCode returns the nameplate of a "<nameplate>-<word>-..." code.
func parseCode(code string) (string, error) {
	nameplate, words, ok := strings.Cut(code, "-")
	if !ok || nameplate == "" || words == "" || strings.Trim(nameplate, "0123456789") != "" {
		return "", fmt.Errorf("%w: %q", ErrBadCode, code)
	}
	return nameplate, nil
}

// checkWords renders the first two verifier bytes with the PGP word list: the
// even list for byte 0, the odd list for byte 1.
func checkWords(v []byte) string {
	return wordlist.RawWords[v[0]].Even + " " + wordlist.RawWords[v[1]].Odd
}
