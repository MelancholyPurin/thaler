// Package config loads thaler's optional TOML configuration (categorisation
// rules and custom CSV import profiles) and resolves default file locations.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/melancholypurin/thaler/internal/importer"
)

// Config is the content of config.toml.
type Config struct {
	Profiles []importer.Profile `toml:"profile"`
	Rules    []importer.Rule    `toml:"rule"`
}

// Load reads the config file at path. A missing file is not an error unless
// mustExist is set (i.e. the user passed --config explicitly). Unknown keys are
// rejected so that typos do not silently disable a rule.
func Load(path string, mustExist bool) (*Config, error) {
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !mustExist {
			return &c, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		return nil, fmt.Errorf("%s: unknown key(s): %v", path, unknown)
	}
	return &c, nil
}

// DefaultDBPath is $THALER_DB if set, else <data dir>/thaler.db.
func DefaultDBPath() string {
	if p := os.Getenv("THALER_DB"); p != "" {
		return p
	}
	return filepath.Join(dataDir(), "thaler.db")
}

// DefaultConfigPath is $XDG_CONFIG_HOME/thaler/config.toml, falling back to
// ~/.config/thaler/config.toml.
func DefaultConfigPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "thaler", "config.toml")
	}
	return filepath.Join(homeDir(), ".config", "thaler", "config.toml")
}

func dataDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "thaler")
	}
	return filepath.Join(homeDir(), ".local", "share", "thaler")
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}
