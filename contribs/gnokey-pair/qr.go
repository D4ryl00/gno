package main

import (
	"net/url"
	"strings"

	"rsc.io/qr"
)

// pairingURI is what the QR holds. The relay is in it because both sides
// must use the same server.
func pairingURI(code, relay string) string {
	return "gnopair:" + code + "?relay=" + url.QueryEscape(relay)
}

// renderQR draws text as a QR code with half blocks, two modules per
// character row. Colors are explicit (black on white), so it scans on dark
// and light terminals alike.
func renderQR(text string) (string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", err
	}
	const quiet = 2
	dark := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	var b strings.Builder
	size := code.Size + 2*quiet
	for y := 0; y < size; y += 2 {
		b.WriteString("\x1b[30;107m") // black foreground, bright white background
		for x := range size {
			top, bottom := dark(x, y), dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}
