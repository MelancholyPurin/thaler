package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/melancholypurin/thaler/internal/importer"
)

func errText(err error, path string) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), path, "{path}")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func Test_Load(t *testing.T) {
	tcs := []struct {
		name      string
		file      *string // nil: there is no file
		mustExist bool
		want      Config
		wantErr   string // {path} stands for the file
	}{
		{
			name: "the import folder and a rule",
			file: ptr("import_dir = \"~/Documents/bank\"\n\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n"),
			want: Config{ImportDir: "~/Documents/bank", Rules: []importer.Rule{{Contains: "REWE", Category: "Expenses:Groceries"}}},
		},
		{
			name: "a regex rule and a bank profile",
			file: ptr(`[[rule]]
regex = "b(ä|ae)ckerei"
category = "Expenses:Bakery"

[[profile]]
name = "mybank"
delimiter = ","
[profile.columns]
date = ["Date"]
amount = ["Amount"]
`),
			want: Config{
				Rules:    []importer.Rule{{Regex: "b(ä|ae)ckerei", Category: "Expenses:Bakery"}},
				Profiles: []importer.Profile{{Name: "mybank", Delimiter: ",", Columns: importer.Columns{Date: []string{"Date"}, Amount: []string{"Amount"}}}},
			},
		},
		{name: "a language", file: ptr("language = \"de\"\n"), want: Config{Language: "de"}},
		{name: "an empty language is the environment's", file: ptr("language = \"\"\n")},
		{name: "a language Thaler does not speak is refused", file: ptr("language = \"fr\"\n"), wantErr: "{path}: language is \"fr\": use one of en, es, de"},
		{name: "a backup reminder interval", file: ptr("backup_reminder_days = 14\n"), want: Config{BackupReminderDays: intPtr(14)}},
		{name: "0 switches the reminder off", file: ptr("backup_reminder_days = 0\n"), want: Config{BackupReminderDays: intPtr(0)}},
		{name: "a negative interval is refused", file: ptr("backup_reminder_days = -1\n"), wantErr: "{path}: backup_reminder_days is -1: use a number of days, or 0 for no reminder"},
		{name: "a text is not a number of days", file: ptr("backup_reminder_days = \"weekly\"\n"), wantErr: "reading {path}: toml: line 1 (last key \"backup_reminder_days\"): incompatible types: TOML value has type string; destination has type integer"},
		{name: "how many backups to keep", file: ptr("keep_backups = 3\n"), want: Config{KeepBackups: intPtr(3)}},
		{name: "0 keeps them all", file: ptr("keep_backups = 0\n"), want: Config{KeepBackups: intPtr(0)}},
		{name: "a negative count is refused", file: ptr("keep_backups = -2\n"), wantErr: "{path}: keep_backups is -2: use a number of backups to keep, or 0 to keep them all"},
		{name: "an empty file", file: ptr("")},
		{name: "no file, and none is needed", file: nil},
		{name: "no file, but one was asked for", file: nil, mustExist: true, wantErr: "reading {path}: open {path}: no such file or directory"},
		{name: "a typo in a key is not ignored", file: ptr("import_folder = \"/tmp\"\n"), wantErr: "{path}: unknown key(s): [import_folder]"},
		{name: "a syntax error", file: ptr("rule = ["), wantErr: "reading {path}: toml: line 1 (last key \"rule\"): unexpected EOF; expected value"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "config.toml")
			if tc.file != nil {
				writeFile(t, path, *tc.file)
			}

			// when
			got, err := Load(path, tc.mustExist)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err, path)); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, *got); diff != "" {
				t.Errorf("config mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func intPtr(n int) *int { return &n }

func Test_Config_KeepCount(t *testing.T) {
	tcs := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "no config", cfg: nil, want: 10},
		{name: "not set", cfg: &Config{}, want: 10},
		{name: "set", cfg: &Config{KeepBackups: intPtr(3)}, want: 3},
		{name: "keep all", cfg: &Config{KeepBackups: intPtr(0)}, want: 0},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := tc.cfg.KeepCount()

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Config_ReminderDays(t *testing.T) {
	tcs := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "no config", cfg: nil, want: 7},
		{name: "not set", cfg: &Config{}, want: 7},
		{name: "set", cfg: &Config{BackupReminderDays: intPtr(30)}, want: 30},
		{name: "off", cfg: &Config{BackupReminderDays: intPtr(0)}, want: 0},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := tc.cfg.ReminderDays()

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Ensure(t *testing.T) {
	const header = "# Created by Thaler. The sample below is switched off: remove the leading # from a line to use it.\n#\n"
	tcs := []struct {
		name     string
		existing *string // the file before; nil: none, and its folder does not exist either
		example  string
		want     string // the file after
		wantErr  string
	}{
		{
			name:    "creates the folder and the file, with the sample switched off",
			example: "# sample\n[[rule]]\ncontains = \"X\"\ncategory = \"Expenses:X\"\n",
			want:    header + "# sample\n# [[rule]]\n# contains = \"X\"\n# category = \"Expenses:X\"\n",
		},
		{
			name:    "an empty example still gets the header",
			example: "",
			want:    header,
		},
		{
			name:     "an existing file is never touched",
			existing: ptr("import_dir = \"/x\"\n"),
			example:  "[[rule]]\n",
			want:     "import_dir = \"/x\"\n",
		},
		{
			name:     "an existing empty file is left empty",
			existing: ptr(""),
			example:  "[[rule]]\n",
			want:     "",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "thaler", "config.toml")
			if tc.existing != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, *tc.existing)
			}

			// when
			err := Ensure(path, tc.example)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err, path)); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, readFile(t, path)); diff != "" {
				t.Errorf("file mismatch (-want +got):\n%s", diff)
			}
			if tc.existing == nil {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(fs.FileMode(0o600), info.Mode().Perm()); diff != "" {
					t.Errorf("a private file is expected (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// The example that ships with Thaler must be inert once Ensure has written it:
// a new user's config may not change anything until they switch a line on.
func Test_Ensure_withTheShippedExample(t *testing.T) {
	type observed struct {
		LoadError string
		Config    Config
		Samples   []string // which of the example's active lines survive as comments
	}
	tcs := []struct {
		name string
		want observed
	}{
		{
			name: "written and read back it changes nothing, and keeps the samples as comments",
			want: observed{Samples: []string{`# contains = "REWE"`, `# category = "Expenses:Groceries"`, `# [[rule]]`}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			example, err := os.ReadFile("../../config.example.toml")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.toml")

			// when
			if err := Ensure(path, string(example)); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(path, true)

			// then
			got := observed{LoadError: errText(err, path)}
			if loaded != nil {
				got.Config = *loaded
			}
			body := readFile(t, path)
			for _, want := range tc.want.Samples {
				if strings.Contains(body, want) {
					got.Samples = append(got.Samples, want)
				}
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_commentOut(t *testing.T) {
	tcs := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "comments and blank lines stay as they are", in: "# a\n\n  # b\n", want: "# a\n\n  # b\n"},
		{name: "active lines are switched off", in: "key = 1\n[[rule]]\n", want: "# key = 1\n# [[rule]]\n"},
		{name: "indentation is kept after the marker", in: "  key = 1\n", want: "#   key = 1\n"},
		{name: "a mix", in: "# sample\n[[rule]]\n\ncontains = \"x\"\n", want: "# sample\n# [[rule]]\n\n# contains = \"x\"\n"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			got := commentOut(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_tomlString(t *testing.T) {
	tcs := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "REWE", want: `"REWE"`},
		{name: "empty", in: "", want: `""`},
		{name: "quotes", in: `O'Reilly "Books"`, want: `"O'Reilly \"Books\""`},
		{name: "backslashes", in: `C:\Temp\x`, want: `"C:\\Temp\\x"`},
		{name: "a trailing backslash", in: `ends\`, want: `"ends\\"`},
		{name: "umlauts and dashes are kept as they are", in: "Bäckerei Müller – Straße", want: `"Bäckerei Müller – Straße"`},
		{name: "a tab", in: "a\tb", want: `"a\tb"`},
		{name: "a newline", in: "a\nb", want: `"a\nb"`},
		{name: "a control character", in: "a\x01b", want: `"a\u0001b"`},
		{name: "a delete character", in: "a\x7fb", want: `"a\u007Fb"`},
		{name: "text that looks like a table header", in: "[[rule]] fake", want: `"[[rule]] fake"`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			got := tomlString(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_AppendRule(t *testing.T) {
	groceries := func(contains string) importer.Rule {
		return importer.Rule{Contains: contains, Category: "Expenses:Groceries"}
	}
	tcs := []struct {
		name      string
		existing  *string // nil: no file yet
		example   string
		rule      importer.Rule
		want      string // the file after
		wantRules []importer.Rule
		wantErr   string
	}{
		{
			name:      "appended after text that does not end in a newline, leaving it as it was",
			existing:  ptr("# mine\n[[rule]]\ncontains = \"REWE\"   # the big one\ncategory = \"Expenses:Groceries\""),
			rule:      groceries("  Lidl "),
			want:      "# mine\n[[rule]]\ncontains = \"REWE\"   # the big one\ncategory = \"Expenses:Groceries\"\n\n[[rule]]\ncontains = \"Lidl\"\ncategory = \"Expenses:Groceries\"\n",
			wantRules: []importer.Rule{groceries("REWE"), groceries("Lidl")},
		},
		{
			name:      "appended after text that ends in a newline",
			existing:  ptr("import_dir = \"/x\"\n"),
			rule:      groceries("Aldi"),
			want:      "import_dir = \"/x\"\n\n[[rule]]\ncontains = \"Aldi\"\ncategory = \"Expenses:Groceries\"\n",
			wantRules: []importer.Rule{groceries("Aldi")},
		},
		{
			name:      "an empty file",
			existing:  ptr(""),
			rule:      groceries("Aldi"),
			want:      "\n[[rule]]\ncontains = \"Aldi\"\ncategory = \"Expenses:Groceries\"\n",
			wantRules: []importer.Rule{groceries("Aldi")},
		},
		{
			name:      "no file yet: it starts from the example, switched off",
			example:   "# sample\n[[rule]]\ncontains = \"X\"\ncategory = \"Expenses:X\"\n",
			rule:      groceries("Edeka"),
			want:      "# Created by Thaler. The sample below is switched off: remove the leading # from a line to use it.\n#\n# sample\n# [[rule]]\n# contains = \"X\"\n# category = \"Expenses:X\"\n\n[[rule]]\ncontains = \"Edeka\"\ncategory = \"Expenses:Groceries\"\n",
			wantRules: []importer.Rule{groceries("Edeka")},
		},
		{
			name:      "text the bank sends is escaped and read back unchanged",
			existing:  ptr(""),
			rule:      importer.Rule{Contains: `O'Reilly "Books" & Co\`, Category: "Expenses:Misc"},
			want:      "\n[[rule]]\ncontains = \"O'Reilly \\\"Books\\\" & Co\\\\\"\ncategory = \"Expenses:Misc\"\n",
			wantRules: []importer.Rule{{Contains: `O'Reilly "Books" & Co\`, Category: "Expenses:Misc"}},
		},
		{
			name:      "umlauts, tabs and control characters survive",
			existing:  ptr(""),
			rule:      importer.Rule{Contains: "Bäckerei\tMüller\x01", Category: "Expenses:Misc"},
			want:      "\n[[rule]]\ncontains = \"Bäckerei\\tMüller\\u0001\"\ncategory = \"Expenses:Misc\"\n",
			wantRules: []importer.Rule{{Contains: "Bäckerei\tMüller\x01", Category: "Expenses:Misc"}},
		},
		{
			name:      "text that looks like a table header is just text",
			existing:  ptr(""),
			rule:      groceries("[[rule]] fake"),
			want:      "\n[[rule]]\ncontains = \"[[rule]] fake\"\ncategory = \"Expenses:Groceries\"\n",
			wantRules: []importer.Rule{groceries("[[rule]] fake")},
		},
		{
			name:     "no text to look for",
			existing: ptr("# mine\n"),
			rule:     importer.Rule{Category: "Expenses:X"},
			want:     "# mine\n",
			wantErr:  "a rule needs text to look for and a category",
		},
		{
			name:     "blank text to look for",
			existing: ptr("# mine\n"),
			rule:     importer.Rule{Contains: "  ", Category: "Expenses:X"},
			want:     "# mine\n",
			wantErr:  "a rule needs text to look for and a category",
		},
		{
			name:     "no category",
			existing: ptr("# mine\n"),
			rule:     importer.Rule{Contains: "x"},
			want:     "# mine\n",
			wantErr:  "a rule needs text to look for and a category",
		},
		{
			name:     "a regex rule is not made this way",
			existing: ptr("# mine\n"),
			rule:     importer.Rule{Regex: "x", Category: "Expenses:X"},
			want:     "# mine\n",
			wantErr:  "a rule needs text to look for and a category",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "thaler", "config.toml")
			if tc.existing != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, *tc.existing)
			}

			// when
			err := AppendRule(path, tc.example, tc.rule)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err, path)); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			if tc.existing == nil && tc.wantErr != "" {
				return // a refused rule creates nothing
			}
			if diff := cmp.Diff(tc.want, readFile(t, path)); diff != "" {
				t.Errorf("file mismatch (-want +got):\n%s", diff)
			}
			if tc.wantErr != "" {
				return
			}
			loaded, err := Load(path, true)
			if err != nil {
				t.Fatalf("the result must still load: %v", err)
			}
			if diff := cmp.Diff(tc.wantRules, loaded.Rules); diff != "" {
				t.Errorf("rules read back (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_BackupDir(t *testing.T) {
	tcs := []struct {
		name string
		db   string
		want string
	}{
		{name: "next to the database", db: "/data/thaler/thaler.db", want: "/data/thaler/backups"},
		{name: "wherever the database was put", db: "/tmp/test/my.db", want: "/tmp/test/backups"},
		{name: "a relative path", db: "thaler.db", want: "backups"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := BackupDir(tc.db)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The folders Thaler writes its own copies to lie inside the backups folder, but
// apart from it, so that the interface does not count them as backups you made.
func Test_BackupSubfolders(t *testing.T) {
	tcs := []struct {
		name   string
		folder func(string) string
		db     string
		want   string
	}{
		{name: "replaced, next to the database", folder: ReplacedDir, db: "/data/thaler/thaler.db", want: "/data/thaler/backups/replaced"},
		{name: "replaced, wherever the database was put", folder: ReplacedDir, db: "/tmp/test/my.db", want: "/tmp/test/backups/replaced"},
		{name: "before an upgrade, next to the database", folder: PreMigrationDir, db: "/data/thaler/thaler.db", want: "/data/thaler/backups/pre-migration"},
		{name: "before an upgrade, wherever the database was put", folder: PreMigrationDir, db: "/tmp/test/my.db", want: "/tmp/test/backups/pre-migration"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := tc.folder(tc.db)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(BackupDir(tc.db), filepath.Dir(got)); diff != "" {
				t.Errorf("not inside the backups folder (-want +got):\n%s", diff)
			}
			// a backup in the folder is not found by listing the backups folder
			if matches, _ := filepath.Match(filepath.Join(BackupDir(tc.db), BackupGlobs[0]), filepath.Join(got, "thaler-20261101-120000-before-x.db")); matches {
				t.Errorf("%s would be listed as a backup", got)
			}
		})
	}
}

func Test_BackupName(t *testing.T) {
	tcs := []struct {
		name string
		when time.Time
		want string
	}{
		{name: "a timestamp that sorts by time", when: time.Date(2026, 10, 22, 9, 5, 7, 0, time.UTC), want: "thaler-20261022-090507.db"},
		{name: "the start of a year", when: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), want: "thaler-20270101-000000.db"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := BackupName(tc.when)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			// and every name it makes, plain or encrypted, is one the globs find again
			for _, name := range []string{got, EncryptedName(got)} {
				found := false
				for _, glob := range BackupGlobs {
					if ok, _ := filepath.Match(glob, name); ok {
						found = true
					}
				}
				if !found {
					t.Errorf("%q matches none of %q", name, BackupGlobs)
				}
			}
		})
	}
}

func Test_EncryptedName(t *testing.T) {
	tcs := []struct {
		name string
		dest string
		want string
	}{
		{name: "gets the suffix", dest: "/b/thaler-20261022-090507.db", want: "/b/thaler-20261022-090507.db.age"},
		{name: "keeps it when it has it", dest: "/b/mine.age", want: "/b/mine.age"},
		{name: "any name", dest: "backup", want: "backup.age"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := EncryptedName(tc.dest)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
