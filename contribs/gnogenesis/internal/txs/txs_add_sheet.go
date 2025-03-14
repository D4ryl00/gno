package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/gnolang/gno/gno.land/pkg/gnoland"
	"github.com/gnolang/gno/tm2/pkg/bft/types"
	"github.com/gnolang/gno/tm2/pkg/commands"
)

var errNoTxsFileSpecified = errors.New("no txs file specified")

// newTxsAddSheetCmd creates the genesis txs add sheet subcommand
func newTxsAddSheetCmd(txsCfg *txsCfg, io commands.IO) *commands.Command {
	return commands.NewCommand(
		commands.Metadata{
			Name:       "sheets",
			ShortUsage: "txs add sheets <sheet-path ...>",
			ShortHelp:  "imports transactions from the given sheets into the genesis.json",
			LongHelp:   "Imports the transactions from a given transactions sheet to the genesis.json",
		},
		commands.NewEmptyConfig(),
		func(ctx context.Context, args []string) error {
			return execTxsAddSheet(ctx, txsCfg, io, args)
		},
	)
}

func execTxsAddSheet(
	ctx context.Context,
	cfg *txsCfg,
	io commands.IO,
	args []string,
) error {
	// Load the genesis
	genesis, loadErr := types.GenesisDocFromFile(cfg.GenesisPath)
	if loadErr != nil {
		return fmt.Errorf("unable to load genesis, %w", loadErr)
	}

	// Open the transactions files
	if len(args) == 0 {
		return errNoTxsFileSpecified
	}

	parsedTxs := make([]gnoland.TxWithMetadata, 0)
	for _, file := range args {
		txs, err := gnoland.ReadGenesisTxs(ctx, file)
		if err != nil {
			return fmt.Errorf("unable to parse file, %w", err)
		}

		parsedTxs = append(parsedTxs, txs...)
	}

	// Save the txs to the genesis.json
	if err := appendGenesisTxs(genesis, parsedTxs); err != nil {
		return fmt.Errorf("unable to append genesis transactions, %w", err)
	}

	// Save the updated genesis
	if err := genesis.SaveAs(cfg.GenesisPath); err != nil {
		return fmt.Errorf("unable to save genesis.json, %w", err)
	}

	io.Printfln(
		"Saved %d transactions to genesis.json",
		len(parsedTxs),
	)

	return nil
}
