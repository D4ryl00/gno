package main

import "testing"

func TestParseList(t *testing.T) {
	t.Parallel()
	keys, err := parseList("0. a (ledger) - addr: g1x pub: gpub1y, path: 44'/118'/0'/0/0\n\n1. b c (local) - addr: g1z pub: gpub1w, path: <nil>\n")
	if err != nil || len(keys) != 2 || keys[0].Type != "ledger" || keys[1].Name != "b c" {
		t.Fatalf("%+v, %v", keys, err)
	}
	if _, err := parseList("Key: a\n"); err == nil {
		t.Fatal("unrecognized output parsed")
	}
}
