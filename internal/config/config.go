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

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/i18n"
	"github.com/melancholypurin/thaler/internal/importer"
)

// Config is the content of config.toml.
type Config struct {
	// ImportDir is the folder the TUI lists bank exports from; default ~/Downloads.
	ImportDir string `toml:"import_dir"`
	// Language is the language of the messages: en, es or de. Empty means that the
	// environment decides (see i18n.Detect).
	Language string `toml:"language"`
	// BackupReminderDays is how many days may pass since the last backup before
	// the TUI reminds you; 0 switches the reminder off. Nil means not set: see
	// ReminderDays.
	BackupReminderDays *int `toml:"backup_reminder_days"`
	// KeepBackups is how many of the newest backups in each backups folder are
	// kept when old ones are pruned; 0 means keep everything. Nil means not set:
	// see KeepCount.
	KeepBackups *int `toml:"keep_backups"`
	// Features switches on the stories that are merged but not finished; see
	// Feature. Only the names in knownFeatures are accepted.
	Features map[string]bool    `toml:"features"`
	Profiles []importer.Profile `toml:"profile"`
	Rules    []importer.Rule    `toml:"rule"`
}

// DefaultBackupReminderDays is the reminder interval when config.toml does not
// set one.
const DefaultBackupReminderDays = 7

// DefaultKeepBackups is how many backups are kept in each folder when config.toml
// does not say.
const DefaultKeepBackups = 10

// KeepCount is how many of the newest backups in each folder to keep, or 0 to
// keep them all. Nothing is ever removed without asking, whatever this is.
func (c *Config) KeepCount() int {
	if c == nil || c.KeepBackups == nil {
		return DefaultKeepBackups
	}
	return *c.KeepBackups
}

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
		return nil, fmt.Errorf("%w: %w", coded.New("reading_file", map[string]any{"Path": path}), err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		return nil, coded.New("config_unknown_keys", map[string]any{"Path": path, "Count": len(unknown), "Keys": joinKeys(unknown)})
	}
	if _, ok := i18n.Parse(c.Language); c.Language != "" && !ok {
		return nil, coded.New("config_language", map[string]any{"Path": path, "Value": c.Language, "Languages": strings.Join(i18n.Codes(), ", ")})
	}
	if d := c.BackupReminderDays; d != nil && *d < 0 {
		return nil, coded.New("config_reminder_days", map[string]any{"Path": path, "Value": *d})
	}
	if n := c.KeepBackups; n != nil && *n < 0 {
		return nil, coded.New("config_keep_backups", map[string]any{"Path": path, "Value": *n})
	}
	if err := checkFeatures(c.Features, knownFeatures); err != nil {
		return nil, fmt.Errorf("%w: %w", coded.New("error_in_file", map[string]any{"Path": path}), err)
	}
	return &c, nil
}

const newFileHeader = "# Created by Thaler. The sample below is switched off: remove the leading # from a line to use it.\n#\n"

// joinKeys writes the keys of the file that Thaler does not know the way the file
// does, a.b, separated by commas.
func joinKeys(keys []toml.Key) string {
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.String()
	}
	return strings.Join(names, ", ")
}

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
		return coded.New("rule_needs_text", nil)
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

// ReplacedDir is where a restore keeps the database it replaced: a folder of its
// own inside the backups folder, so that it is not mistaken for a backup you made.
func ReplacedDir(dbPath string) string { return filepath.Join(BackupDir(dbPath), "replaced") }

// PreMigrationDir is where the copies made before an upgrade go: a folder of its
// own inside the backups folder, for the same reason.
func PreMigrationDir(dbPath string) string { return filepath.Join(BackupDir(dbPath), "pre-migration") }

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
