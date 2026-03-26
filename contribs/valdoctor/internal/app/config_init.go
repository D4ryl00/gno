package app

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/gnolang/gno/tm2/pkg/commands"
)

// defaultConfigTemplate is written by `config init`. The commented TOML format
// is more readable than a raw marshal and survives a first `config get`.
const defaultConfigTemplate = `# valdoctor configuration
# Run 'valdoctor config init --force' to reset to defaults.

# Maximum number of findings rendered in text output (0 = unlimited).
max_findings = 20

# Maximum number of node sections shown in the health summary (0 = unlimited).
# This limit is lifted automatically when --verbose is active.
max_health = 5

# Output format: "text" or "json".
format = "text"

# Show verbose output including low-severity findings and per-event details
# in the health summary (timeouts with path:line).
verbose = false
`

type configInitCfg struct {
	configBaseCfg
	forceOverwrite bool
}

func newConfigInitCmd(io commands.IO) *commands.Command {
	cfg := &configInitCfg{}

	return commands.NewCommand(
		commands.Metadata{
			Name:       "init",
			ShortUsage: "config init [flags]",
			ShortHelp:  "initialize the valdoctor configuration with default values",
		},
		cfg,
		func(_ context.Context, _ []string) error {
			return execConfigInit(cfg, io)
		},
	)
}

func (c *configInitCfg) RegisterFlags(fs *flag.FlagSet) {
	c.configBaseCfg.RegisterFlags(fs)
	fs.BoolVar(&c.forceOverwrite, "force", false, "overwrite existing config file")
}

func execConfigInit(cfg *configInitCfg, io commands.IO) error {
	if cfg.configPath == "" {
		return fmt.Errorf("config path is empty")
	}
	if _, err := os.Stat(cfg.configPath); err == nil && !cfg.forceOverwrite {
		return errOverwriteNotEnabled
	}
	if err := os.MkdirAll(dirOf(cfg.configPath), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(cfg.configPath, []byte(defaultConfigTemplate), 0o644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	io.Printfln("Default configuration initialized at %s", cfg.configPath)
	return nil
}
