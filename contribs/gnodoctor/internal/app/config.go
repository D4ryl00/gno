package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml"
)

// userConfig holds the persisted defaults for the inspect command.
// CLI flags always take precedence over these values.
type userConfig struct {
	MaxFindings int    `toml:"max_findings" json:"max_findings" comment:"maximum number of findings rendered in text output (0 = unlimited)"`
	MaxHealth   int    `toml:"max_health"   json:"max_health"   comment:"maximum number of node sections in health summary (0 = unlimited)"`
	Format      string `toml:"format"       json:"format"       comment:"report format: text or json"`
	Verbose     bool   `toml:"verbose"      json:"verbose"      comment:"show low-severity findings and per-event details in health summary"`
}

// defaultUserConfig returns a userConfig with sensible defaults.
func defaultUserConfig() userConfig {
	return userConfig{
		MaxFindings: 20,
		MaxHealth:   5,
		Format:      "text",
		Verbose:     false,
	}
}

// defaultConfigPath returns the XDG-compliant path for the user config file:
//
//	$XDG_CONFIG_HOME/gnodoctor/config.toml  (if XDG_CONFIG_HOME is set)
//	~/.config/gnodoctor/config.toml         (otherwise)
func defaultConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "gnodoctor", "config.toml")
}

// loadConfig reads and merges configuration for the inspect command.
//
// The default XDG config is loaded first. If explicitPath is non-empty it is
// loaded on top, overriding any value set by the default file. A missing
// default config is silently ignored; a missing explicit config is an error.
func loadConfig(explicitPath string) (userConfig, error) {
	var cfg userConfig

	if defaultPath := defaultConfigPath(); defaultPath != "" {
		if data, err := os.ReadFile(defaultPath); err == nil {
			if err := toml.Unmarshal(data, &cfg); err != nil {
				return cfg, fmt.Errorf("parsing config %s: %w", defaultPath, err)
			}
		}
	}

	if explicitPath != "" {
		data, err := os.ReadFile(explicitPath)
		if err != nil {
			return cfg, fmt.Errorf("reading config %s: %w", explicitPath, err)
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parsing config %s: %w", explicitPath, err)
		}
	}

	return cfg, nil
}

// readConfigFile loads a config file from the given path. Returns an error if
// the file does not exist (unlike loadConfig which silently ignores the default).
func readConfigFile(path string) (userConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return userConfig{}, fmt.Errorf("unable to load config; try running `gnodoctor config init`: %w", err)
	}
	var cfg userConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return userConfig{}, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return cfg, nil
}

// writeUserConfig marshals cfg to TOML and writes it to path,
// creating parent directories as needed.
func writeUserConfig(path string, cfg userConfig) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// validateUserConfig returns an error if any config field holds an invalid value.
func validateUserConfig(cfg userConfig) error {
	if cfg.MaxFindings < 0 {
		return fmt.Errorf("max_findings must be >= 0, got %d", cfg.MaxFindings)
	}
	if cfg.MaxHealth < 0 {
		return fmt.Errorf("max_health must be >= 0, got %d", cfg.MaxHealth)
	}
	switch cfg.Format {
	case "", "text", "json":
	default:
		return fmt.Errorf("format must be \"text\" or \"json\", got %q", cfg.Format)
	}
	return nil
}
