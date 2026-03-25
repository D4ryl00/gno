package app

import (
	"context"
	"os"

	"github.com/gnolang/gno/tm2/pkg/commands"
)

func NewRootCmd(io commands.IO) *commands.Command {
	cmd := commands.NewCommand(
		commands.Metadata{
			Name:       "gnodoctor",
			ShortUsage: "<subcommand> [flags]",
			ShortHelp:  "inspect Gnoland and TM2 incidents from genesis and logs",
		},
		commands.NewEmptyConfig(),
		commands.HelpExec,
	)

	cmd.AddSubCommands(newInspectCmd(io))

	return cmd
}

func background() context.Context {
	return context.Background()
}

func args() []string {
	return os.Args[1:]
}
