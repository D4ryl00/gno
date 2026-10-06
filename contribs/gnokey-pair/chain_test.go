package main

import (
	"testing"

	"github.com/gnolang/gno/tm2/pkg/sdk"
	"github.com/gnolang/gno/tm2/pkg/std"
)

func TestReason(t *testing.T) {
	t.Parallel()
	r := sdk.ABCIResultFromError(std.ErrInsufficientCoins("insufficient account funds; 1ugnot < 2ugnot"))
	if got, want := reason(r.ResponseBase), "insufficient coins error: insufficient account funds; 1ugnot < 2ugnot"; got != want {
		t.Fatalf("%q", got)
	}
}
