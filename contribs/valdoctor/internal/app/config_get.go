package app

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/gnolang/gno/tm2/pkg/commands"
)

var errInvalidConfigGetArgs = errors.New("invalid number of arguments: config get accepts at most one key")

type configGetCfg struct {
	configBaseCfg
	raw bool
}

func newConfigGetCmd(io commands.IO) *commands.Command {
	cfg := &configGetCfg{}

	cmd := commands.NewCommand(
		commands.Metadata{
			Name:       "get",
			ShortUsage: "config get [flags] [<key>]",
			ShortHelp:  "show valdoctor configuration values",
			LongHelp:   "Show all valdoctor configuration values, or a single value by key name.",
		},
		cfg,
		func(_ context.Context, args []string) error {
			return execConfigGet(cfg, io, args)
		},
	)

	// Generate per-field subcommands for tab completion.
	gen := commands.FieldsGenerator{
		MetaUpdate: func(meta *commands.Metadata, inputType string) {
			meta.ShortUsage = fmt.Sprintf("config get %s", meta.Name)
		},
		TagNameSelector: "toml",
	}
	cmd.AddSubCommands(gen.GenerateFrom(userConfig{}, func(_ context.Context, args []string) error {
		return execConfigGet(cfg, io, args)
	})...)

	return cmd
}

func (c *configGetCfg) RegisterFlags(fs *flag.FlagSet) {
	c.configBaseCfg.RegisterFlags(fs)
	fs.BoolVar(&c.raw, "raw", false, "print raw value instead of JSON")
}

func execConfigGet(cfg *configGetCfg, io commands.IO, args []string) error {
	if len(args) > 1 {
		return errInvalidConfigGetArgs
	}

	loaded, err := readConfigFile(cfg.configPath)
	if err != nil {
		return err
	}

	if err := printKeyValue(&loaded, cfg.raw, io, args...); err != nil {
		return fmt.Errorf("unable to get config field: %w", err)
	}

	return nil
}
