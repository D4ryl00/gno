package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/gnolang/gno/tm2/pkg/commands"
)

var errInvalidConfigSetArgs = errors.New("usage: config set <key> <value>")

func newConfigSetCmd(io commands.IO) *commands.Command {
	cfg := &configBaseCfg{}

	cmd := commands.NewCommand(
		commands.Metadata{
			Name:       "set",
			ShortUsage: "config set <key> <value>",
			ShortHelp:  "set a gnodoctor configuration value",
			LongHelp:   "Set a gnodoctor configuration value by key name. The config file is updated in place.",
		},
		cfg,
		func(_ context.Context, args []string) error {
			return execConfigSet(cfg, io, args)
		},
	)

	// Generate per-field subcommands for tab completion.
	gen := commands.FieldsGenerator{
		MetaUpdate: func(meta *commands.Metadata, inputType string) {
			meta.ShortUsage = fmt.Sprintf("config set %s <%s>", meta.Name, inputType)
		},
		TagNameSelector: "toml",
	}
	cmd.AddSubCommands(gen.GenerateFrom(userConfig{}, func(_ context.Context, args []string) error {
		return execConfigSet(cfg, io, args)
	})...)

	return cmd
}

func execConfigSet(cfg *configBaseCfg, io commands.IO, args []string) error {
	if len(args) != 2 {
		return errInvalidConfigSetArgs
	}

	key := args[0]
	value := args[1]

	loaded, err := readConfigFile(cfg.configPath)
	if err != nil {
		return err
	}

	if err := setUserConfigField(&loaded, key, value); err != nil {
		return fmt.Errorf("unable to update config field: %w", err)
	}

	if err := validateUserConfig(loaded); err != nil {
		return fmt.Errorf("invalid config value: %w", err)
	}

	if err := writeUserConfig(cfg.configPath, loaded); err != nil {
		return fmt.Errorf("unable to save config: %w", err)
	}

	io.Printfln("Configuration saved at %s", cfg.configPath)
	return nil
}

// setUserConfigField updates a single field in cfg identified by key (toml tag name).
func setUserConfigField(cfg *userConfig, key, value string) error {
	cfgValue := reflect.ValueOf(cfg).Elem()
	field, err := commands.GetFieldByPath(cfgValue, "toml", strings.Split(key, "."))
	if err != nil {
		return err
	}
	return applyStringToValue(value, *field)
}

// applyStringToValue converts the string value to the field's type and sets it.
func applyStringToValue(value string, dst reflect.Value) error {
	switch dst.Interface().(type) {
	case string:
		dst.Set(reflect.ValueOf(value))
	case int:
		var v int
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("expected integer, got %q", value)
		}
		dst.Set(reflect.ValueOf(v))
	case bool:
		var v bool
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("expected true or false, got %q", value)
		}
		dst.Set(reflect.ValueOf(v))
	default:
		return fmt.Errorf("unsupported field type %s", dst.Type())
	}
	return nil
}

// dirOf returns the directory part of a file path.
func dirOf(path string) string {
	return filepath.Dir(path)
}
