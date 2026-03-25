package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"reflect"
	"strings"

	"github.com/gnolang/gno/tm2/pkg/commands"
)

var errOverwriteNotEnabled = errors.New("config file already exists; use --force to overwrite")

type configBaseCfg struct {
	configPath string
}

func (c *configBaseCfg) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(
		&c.configPath,
		"config-path",
		defaultConfigPath(),
		"path to the gnodoctor config.toml",
	)
}

// newConfigCmd creates the config root command.
func newConfigCmd(io commands.IO) *commands.Command {
	cmd := commands.NewCommand(
		commands.Metadata{
			Name:       "config",
			ShortUsage: "config <subcommand> [flags]",
			ShortHelp:  "gnodoctor configuration management",
		},
		commands.NewEmptyConfig(),
		commands.HelpExec,
	)

	cmd.AddSubCommands(
		newConfigInitCmd(io),
		newConfigGetCmd(io),
		newConfigSetCmd(io),
	)

	return cmd
}

// printKeyValue prints the config value for the given key (or all values) as JSON.
// If raw is true, string values are printed without JSON quoting.
func printKeyValue(cfg *userConfig, raw bool, io commands.IO, key ...string) error {
	prepareOutput := func(v any) (string, error) {
		encoded, err := json.MarshalIndent(v, "", "    ")
		if err != nil {
			return "", fmt.Errorf("unable to marshal JSON: %w", err)
		}
		output := string(encoded)
		if raw {
			if err := json.Unmarshal(encoded, &output); err != nil {
				// Not a plain string — fall back to raw JSON
				return string(encoded), nil
			}
		}
		return output, nil
	}

	if len(key) == 0 {
		output, err := prepareOutput(cfg)
		if err != nil {
			return err
		}
		io.Println(output)
		return nil
	}

	cfgValue := reflect.ValueOf(cfg).Elem()
	field, err := commands.GetFieldByPath(cfgValue, "toml", strings.Split(key[0], "."))
	if err != nil {
		return err
	}

	output, err := prepareOutput(field.Interface())
	if err != nil {
		return err
	}
	io.Println(output)
	return nil
}
