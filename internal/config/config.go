// Package config loads thaler's optional TOML configuration (categorisation
// rules and custom CSV import profiles) and resolves default file locations.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/melancholypurin/thaler/internal/importer"
)

// Config is the content of config.toml.
type Config struct {
	// ImportDir is the folder the TUI lists bank exports from; default ~/Downloads.
	ImportDir string `toml:"import_dir"`
	// BackupReminderDays is how many days may pass since the last backup before
	// the TUI reminds you; 0 switches the reminder off. Nil means not set: see
	// ReminderDays.
	BackupReminderDays *int               `toml:"backup_reminder_days"`
	Profiles           []importer.Profile `toml:"profile"`
	Rules              []importer.Rule    `toml:"rule"`
}

// DefaultBackupReminderDays is the reminder interval when config.toml does not
// set one.
const DefaultBackupReminderDays = 7

// ReminderDays is the number of days after which an old backup is reminded of,
// or 0 for never.
func (c *Config) ReminderDays() int {
	if c == nil || c.BackupReminderDays == nil {
		return DefaultBackupReminderDays
	}
	return *c.BackupReminderDays
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
	if d := c.BackupReminderDays; d != nil && *d < 0 {
		return nil, fmt.Errorf("%s: backup_reminder_days is %d: use a number of days, or 0 for no reminder", path, *d)
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

// AppendRule adds a "contains" rule to the end of the config file, creating the
// file from the example first if there is none. The file is only appended to,
// so the user's comments and the order of their rules are untouched; and since
// the first matching rule wins, a rule added here is the last resort.
func AppendRule(path, example string, r importer.Rule) error {
	if strings.TrimSpace(r.Contains) == "" || r.Regex != "" || r.Category == "" {
		return errors.New("a rule needs text to look for and a category")
	}
	if err := Ensure(path, example); err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var add strings.Builder
	if len(body) > 0 && body[len(body)-1] != '\n' {
		add.WriteString("\n")
	}
	add.WriteString("\n[[rule]]\ncontains = " + tomlString(strings.TrimSpace(r.Contains)) + "\ncategory = " + tomlString(r.Category) + "\n")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(add.String()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// tomlString quotes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
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

// EncryptedSuffix ends the name of an encrypted backup: an age file.
const EncryptedSuffix = ".age"

// BackupGlobs match the file names BackupName produces, plain and encrypted.
var BackupGlobs = []string{"thaler-*.db", "thaler-*.db" + EncryptedSuffix}

// EncryptedName is dest with the suffix of an encrypted backup, unless it has it.
func EncryptedName(dest string) string {
	if strings.HasSuffix(dest, EncryptedSuffix) {
		return dest
	}
	return dest + EncryptedSuffix
}

// BackupDir is where backups go unless told otherwise: a backups folder next to
// the database.
func BackupDir(dbPath string) string { return filepath.Join(filepath.Dir(dbPath), "backups") }

// BackupName is the file name of a backup made at t.
func BackupName(t time.Time) string { return "thaler-" + t.Format("20060102-150405") + ".db" }

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
