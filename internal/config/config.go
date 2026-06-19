package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// DefaultPickerMaxDepth is how many levels deep the folder picker scans by default.
const DefaultPickerMaxDepth = 3

// Config holds user configuration for eme.
type Config struct {
	Agent  Agent  `toml:"agent"`
	Picker Picker `toml:"picker"`
}

// Agent configures agent execution.
type Agent struct {
	Command string `toml:"command"`
}

// Picker configures the folder picker scan.
type Picker struct {
	// MaxDepth is how many directory levels deep to scan from each root.
	MaxDepth int `toml:"max_depth"`
	// Roots are extra directories to scan in addition to the auto-discovered
	// ones. A leading "~" is expanded to the user's home directory.
	Roots []string `toml:"roots"`
}

// Default returns a config with sensible defaults.
func Default() *Config {
	return &Config{
		Agent:  Agent{Command: "opencode"},
		Picker: Picker{MaxDepth: DefaultPickerMaxDepth},
	}
}

// DefaultPath returns the default config file path.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return filepath.Join(home, ".config", "eme", "config.toml")
}

// Load reads config from path, returning defaults if the file does not exist.
func Load(path string) (*Config, error) {
	cfg := Default()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if cfg.Agent.Command == "" {
		cfg.Agent.Command = "opencode"
	}
	if cfg.Picker.MaxDepth <= 0 {
		cfg.Picker.MaxDepth = DefaultPickerMaxDepth
	}
	return cfg, nil
}

// Save writes the config file with default content if missing.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(c)
}
