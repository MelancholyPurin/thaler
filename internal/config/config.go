// Package config loads thaler's optional TOML configuration (categorisation
// rules and custom CSV import profiles) and resolves default file locations.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/melancholypurin/thaler/internal/importer"
)

// Config is the content of config.toml.
type Config struct {
	// ImportDir is the folder the TUI lists bank exports from; default ~/Downloads.
	ImportDir string             `toml:"import_dir"`
	Profiles  []importer.Profile `toml:"profile"`
	Rules     []importer.Rule    `toml:"rule"`
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

const newFileHeader = "# Created by Thaler. The sample below is switched off: remove the leading # from a line to use it.\n#\n"

// Ensure creates the config file from the example text if it does not exist,
// with every active line commented out so that it changes nothing until the
// user switches lines on.
func Ensure(path, example string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(newFileHeader + commentOut(example)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// commentOut puts "# " in front of every line that is not already a comment or
// blank, so a template full of sample rules starts out switched off.
func commentOut(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			lines[i] = "# " + l
		}
	}
	return strings.Join(lines, "\n")
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
