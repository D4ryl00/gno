package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gnolang/gno/gnovm/pkg/doc"
	"github.com/gnolang/gno/tm2/pkg/amino"
	abci "github.com/gnolang/gno/tm2/pkg/bft/abci/types"
	rpcclient "github.com/gnolang/gno/tm2/pkg/bft/rpc/client"
	ctypes "github.com/gnolang/gno/tm2/pkg/bft/rpc/core/types"
	"github.com/gnolang/gno/tm2/pkg/crypto"
	"github.com/gnolang/gno/tm2/pkg/std"
)

// node is what gnokey-pair needs from the chain. A lying node can at worst
// produce a stale account, a wrong simulation or a dropped broadcast: it
// cannot change what is signed.
type node interface {
	ChainID(ctx context.Context) (string, error)
	// Account returns nil when the chain has no account at addr.
	Account(ctx context.Context, addr crypto.Address) (*std.BaseAccount, error)
	GasPrice(ctx context.Context) (std.GasPrice, error)
	// FuncParams returns the parameter names of a realm's functions, without
	// the realm parameter of crossing functions.
	FuncParams(ctx context.Context, pkgPath string) (map[string][]string, error)
	Simulate(ctx context.Context, tx std.Tx) (abci.ResponseDeliverTx, error)
	Broadcast(ctx context.Context, tx std.Tx) (*ctypes.ResultBroadcastTxCommit, error)
}

type rpcNode struct{ c *rpcclient.RPCClient }

func dialNode(remote string) (node, error) {
	c, err := rpcclient.NewHTTPClient(remote)
	if err != nil {
		return nil, err
	}
	return rpcNode{c}, nil
}

func (n rpcNode) ChainID(ctx context.Context) (string, error) {
	st, err := n.c.Status(ctx, nil)
	if err != nil {
		return "", err
	}
	return st.NodeInfo.Network, nil
}

func (n rpcNode) query(ctx context.Context, path string, data []byte) (abci.ResponseQuery, error) {
	res, err := n.c.ABCIQuery(ctx, path, data)
	if err != nil {
		return abci.ResponseQuery{}, fmt.Errorf("query %s: %w", path, err)
	}
	if res.Response.Error != nil {
		return abci.ResponseQuery{}, fmt.Errorf("query %s: %s", path, reason(res.Response.ResponseBase))
	}
	return res.Response, nil
}

func (n rpcNode) Account(ctx context.Context, addr crypto.Address) (*std.BaseAccount, error) {
	res, err := n.query(ctx, "auth/accounts/"+addr.String(), nil)
	if err != nil {
		return nil, err
	}
	// Only BaseAccount is needed; encoding/json skips the fields the chain
	// adds around it, which amino would refuse.
	var acc struct{ BaseAccount json.RawMessage }
	if err := json.Unmarshal(res.Data, &acc); err != nil {
		return nil, fmt.Errorf("account: %w", err)
	}
	if len(acc.BaseAccount) == 0 || string(acc.BaseAccount) == "null" {
		return nil, nil
	}
	var base std.BaseAccount
	if err := amino.UnmarshalJSON(acc.BaseAccount, &base); err != nil {
		return nil, fmt.Errorf("account: %w", err)
	}
	return &base, nil
}

func (n rpcNode) GasPrice(ctx context.Context) (std.GasPrice, error) {
	var gp std.GasPrice
	res, err := n.query(ctx, "auth/gasprice", nil)
	if err == nil {
		err = amino.UnmarshalJSON(res.Data, &gp)
	}
	return gp, err
}

func (n rpcNode) FuncParams(ctx context.Context, pkgPath string) (map[string][]string, error) {
	res, err := n.query(ctx, "vm/qdoc", []byte(pkgPath))
	if err != nil {
		return nil, err
	}
	var d doc.JSONDocumentation
	if err := json.Unmarshal(res.Data, &d); err != nil {
		return nil, fmt.Errorf("qdoc: %w", err)
	}
	funcs := map[string][]string{}
	for _, f := range d.Funcs {
		if f.Type != "" { // a method
			continue
		}
		params := f.Params
		if f.Crossing && len(params) > 0 {
			params = params[1:]
		}
		names := make([]string, len(params))
		for i, p := range params {
			names[i] = p.Name
		}
		funcs[f.Name] = names
	}
	return funcs, nil
}

func (n rpcNode) Simulate(ctx context.Context, tx std.Tx) (abci.ResponseDeliverTx, error) {
	var res abci.ResponseDeliverTx
	bz, err := amino.Marshal(tx)
	if err != nil {
		return res, err
	}
	q, err := n.query(ctx, ".app/simulate", bz)
	if err == nil {
		err = amino.Unmarshal(q.Value, &res)
	}
	return res, err
}

func (n rpcNode) Broadcast(ctx context.Context, tx std.Tx) (*ctypes.ResultBroadcastTxCommit, error) {
	bz, err := amino.Marshal(tx)
	if err != nil {
		return nil, err
	}
	return n.c.BroadcastTxCommit(ctx, bz)
}

// traceMsg is a "Msg Traces" line of a tm2 error log: "    0  file.go:108 - msg".
var traceMsg = regexp.MustCompile(`(?m)^\s+\d+\s+\S+ - (.+)$`)

// reason condenses a failed result: the error kind and its messages, without
// the stack trace the log carries.
func reason(res abci.ResponseBase) string {
	if res.Error == nil {
		return res.Log
	}
	parts := []string{res.Error.Error()}
	for _, m := range traceMsg.FindAllStringSubmatch(res.Log, -1) {
		parts = append(parts, m[1])
	}
	return strings.Join(parts, ": ")
}
