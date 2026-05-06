// valkeyimport reads a tm2 priv_validator_key.json and imports the ed25519
// private key into a fresh gnokey keybase so that an unmodified
// `gnokms gnokey <name> --home <keybase>` can sign with the validator's key.
//
// This is a scenario-only convenience: it lets gnokms-backed signer scenarios
// reuse the validator's existing key without modifying gnokms upstream.
package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/gnolang/gno/tm2/pkg/bft/privval/signer/local"
	"github.com/gnolang/gno/tm2/pkg/crypto/keys"
	osm "github.com/gnolang/gno/tm2/pkg/os"
)

func main() {
	var (
		keyFile     = flag.String("priv-validator-key", "", "path to priv_validator_key.json")
		keybaseDir  = flag.String("keybase-dir", "", "output gnokey keybase directory")
		keyName     = flag.String("key-name", "validator", "name to give the imported key in the keybase")
		keyPassword = flag.String("password", "", "passphrase used to encrypt the key in the keybase")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	required := []struct {
		label, value string
	}{
		{"--priv-validator-key", *keyFile},
		{"--keybase-dir", *keybaseDir},
		{"--password", *keyPassword},
	}
	for _, r := range required {
		if r.value == "" {
			logger.Error(r.label + " is required")
			os.Exit(1)
		}
	}

	fk, err := local.LoadFileKey(*keyFile)
	if err != nil {
		logger.Error("unable to load priv_validator_key.json", "err", err)
		os.Exit(1)
	}

	if err := osm.EnsureDir(*keybaseDir, 0o700); err != nil {
		logger.Error("unable to create keybase dir", "err", err)
		os.Exit(1)
	}

	kb, err := keys.NewKeyBaseFromDir(*keybaseDir)
	if err != nil {
		logger.Error("unable to open keybase", "err", err)
		os.Exit(1)
	}

	if err := kb.ImportPrivKey(*keyName, fk.PrivKey, *keyPassword); err != nil {
		logger.Error("unable to import priv key into keybase", "err", err)
		os.Exit(1)
	}

	logger.Info("imported validator key into gnokey keybase",
		"keybase_dir", *keybaseDir,
		"key_name", *keyName,
		"address", fk.Address.String(),
	)
}
