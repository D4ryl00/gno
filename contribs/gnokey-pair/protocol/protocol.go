// Package protocol defines the gnokey-pair messages exchanged over the
// wormhole channel: the phone sends a Request (phase "0") then Received
// (phase "1"); gnokey-pair answers with a Result (phase "0").
package protocol

import "encoding/json"

// VersionsKey names this protocol in the wormhole app_versions.
const VersionsKey = "gnokey-pair"

// Versions is one side's entry under VersionsKey. Unknown fields are ignored.
type Versions struct {
	V        int      `json:"v"`
	Kinds    []string `json:"kinds"`
	Modes    []string `json:"modes,omitempty"`
	Features []string `json:"features,omitempty"`
}

// AppVersions wraps v for wormhole.Config.Versions.
func AppVersions(v Versions) map[string]Versions { return map[string]Versions{VersionsKey: v} }

const (
	KindTx = "tx" // an unsigned std.Tx, amino JSON

	ModeSendTx = "sendtx" // sign, simulate, broadcast
	ModeSignTx = "signtx" // sign, return the signed bytes

	// FeatureOffline: gnokey-pair accepts a signtx Request marked Offline.
	FeatureOffline = "offline"
)

// MaxTxSize bounds Request.Tx.
const MaxTxSize = 64 << 10

type Request struct {
	Kind      string          `json:"kind"`
	Mode      string          `json:"mode"`
	ChainID   string          `json:"chainid"`
	RPC       string          `json:"rpc,omitempty"` // advisory
	Signer    string          `json:"signer"`        // bech32 address of the identity
	Tx        json.RawMessage `json:"tx"`
	Requester *Requester      `json:"requester,omitempty"` // display only, never verified

	// Offline asks gnokey-pair to sign with no network operation, from
	// Account; only with ModeSignTx, and only to a peer listing FeatureOffline.
	Offline bool     `json:"offline,omitempty"`
	Account *Account `json:"account,omitempty"`
}

// Account is the signer's account number and sequence, as amino JSON writes
// uint64: decimal strings.
type Account struct {
	Number   string `json:"number"`
	Sequence string `json:"sequence"`
}

type Requester struct {
	Name string `json:"name,omitempty"`
	For  string `json:"for,omitempty"` // the GnoConnect producer the phone acts for
}

// Statuses and error codes keep GnoConnect's closed vocabulary.
const (
	StatusSuccess   = "success"
	StatusCancelled = "cancelled"
	StatusError     = "error"

	CodeInvalidRequest    = "invalid_request"
	CodeNetworkDeclined   = "network_declined"
	CodeSignerUnavailable = "signer_unavailable"
	CodeTxFailed          = "tx_failed"
)

type Result struct {
	Status   string `json:"status"`
	Code     string `json:"code,omitempty"`
	Detail   string `json:"detail,omitempty"`   // for display only; never branch on it
	Hash     string `json:"hash,omitempty"`     // sendtx: base64, as gnokey prints it
	Height   int64  `json:"height,omitempty"`   // sendtx
	SignedTx string `json:"signedtx,omitempty"` // signtx: base64 amino-binary, broadcast unmodified
	Manual   bool   `json:"manual,omitempty"`   // sendtx: the user runs the gnokey command; watch the chain
}

// Received tells gnokey-pair that the phone has read the Result.
type Received struct {
	Received bool `json:"received"`
}
