package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/importer"
)

// rulesFile is a config with settings, comments of the person's own and three rules, the
// middle one with a comment of its own above it and a note after it.
const rulesFile = `# my settings
keep_backups = 3

# ---- rules ----

# the supermarket
[[rule]]
contains = "REWE"
category = "Expenses:Groceries"

# bakeries: the spelling varies
[[rule]]
regex = "b(ä|ae)ckerei"
category = "Expenses:Groceries"

# end of the rules above
[[rule]]
contains = "Gehalt"
category = "Income:Salary"
`

func Test_DeleteRule(t *testing.T) {
	tcs := []struct {
		name      string
		existing  string
		index     int
		want      string
		wantRules []importer.Rule
		wantErr   string
	}{
		{
			name:     "the first rule goes with its comment and the blank line after it; the heading stays",
			existing: rulesFile,
			index:    0,
			want:     strings.Replace(rulesFile, "# the supermarket\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n\n", "", 1),
			wantRules: []importer.Rule{
				{Regex: "b(ä|ae)ckerei", Category: "Expenses:Groceries"},
				{Contains: "Gehalt", Category: "Income:Salary"},
			},
		},
		{
			name:     "the middle rule",
			existing: rulesFile,
			index:    1,
			want:     strings.Replace(rulesFile, "# bakeries: the spelling varies\n[[rule]]\nregex = \"b(ä|ae)ckerei\"\ncategory = \"Expenses:Groceries\"\n\n", "", 1),
			wantRules: []importer.Rule{
				{Contains: "REWE", Category: "Expenses:Groceries"},
				{Contains: "Gehalt", Category: "Income:Salary"},
			},
		},
		{
			name:     "the last rule leaves one line end, not a trail of blank lines",
			existing: rulesFile,
			index:    2,
			want:     strings.Replace(rulesFile, "\n# end of the rules above\n[[rule]]\ncontains = \"Gehalt\"\ncategory = \"Income:Salary\"\n", "", 1),
			wantRules: []importer.Rule{
				{Contains: "REWE", Category: "Expenses:Groceries"},
				{Regex: "b(ä|ae)ckerei", Category: "Expenses:Groceries"},
			},
		},
		{
			name:     "the only rule",
			existing: "keep_backups = 3\n\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n",
			index:    0,
			want:     "keep_backups = 3\n",
		},
		{
			name:     "a rule followed by another table: the table stays",
			existing: "[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n\n[features]\n",
			index:    0,
			want:     "[features]\n",
		},
		{
			name:     "a rule that is not there",
			existing: rulesFile,
			index:    3,
			want:     rulesFile,
			wantErr:  "rule_not_found(Rule=4)",
			wantRules: []importer.Rule{
				{Contains: "REWE", Category: "Expenses:Groceries"},
				{Regex: "b(ä|ae)ckerei", Category: "Expenses:Groceries"},
				{Contains: "Gehalt", Category: "Income:Salary"},
			},
		},
		{
			name:     "rules written as a list are refused and the file is left alone",
			existing: "rule = [{contains = \"REWE\", category = \"Expenses:Groceries\"}]\n",
			index:    0,
			want:     "rule = [{contains = \"REWE\", category = \"Expenses:Groceries\"}]\n",
			wantErr:  "rules_layout(Path=PATH)",
			wantRules: []importer.Rule{
				{Contains: "REWE", Category: "Expenses:Groceries"},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "config.toml")
			writeFile(t, path, tc.existing)

			// when
			_, err := DeleteRule(path, tc.index)

			// then
			checkRulesFile(t, path, err, tc.wantErr, tc.want, tc.wantRules)
		})
	}
}

// checkRulesFile compares the error, the text of the file and the rules it loads as.
func checkRulesFile(t *testing.T, path string, err error, wantErr, want string, wantRules []importer.Rule) {
	t.Helper()
	gotErr := ""
	if err != nil {
		gotErr = strings.ReplaceAll(err.Error(), path, "PATH")
		if strings.HasPrefix(gotErr, wantErr+": ") { // the words of the library that failed are not ours to compare
			gotErr = wantErr
		}
	}
	if diff := cmp.Diff(wantErr, gotErr); diff != "" {
		t.Fatalf("error mismatch (-want +got):\n%s", diff)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, string(body)); diff != "" {
		t.Errorf("file mismatch (-want +got):\n%s", diff)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatalf("the file no longer loads: %v", err)
	}
	if diff := cmp.Diff(wantRules, cfg.Rules); diff != "" {
		t.Errorf("rules mismatch (-want +got):\n%s", diff)
	}
}

func Test_MoveRule(t *testing.T) {
	const swapped = `# my settings
keep_backups = 3

# ---- rules ----

# bakeries: the spelling varies
[[rule]]
regex = "b(ä|ae)ckerei"
category = "Expenses:Groceries"

# the supermarket
[[rule]]
contains = "REWE"
category = "Expenses:Groceries"

# end of the rules above
[[rule]]
contains = "Gehalt"
category = "Income:Salary"
`
	rewe := importer.Rule{Contains: "REWE", Category: "Expenses:Groceries"}
	bakery := importer.Rule{Regex: "b(ä|ae)ckerei", Category: "Expenses:Groceries"}
	salary := importer.Rule{Contains: "Gehalt", Category: "Income:Salary"}
	tcs := []struct {
		name      string
		existing  string
		index     int
		delta     int
		want      string
		wantRules []importer.Rule
		wantErr   string
	}{
		{
			name:      "the first goes down: each takes its comment along",
			existing:  rulesFile,
			index:     0,
			delta:     1,
			want:      swapped,
			wantRules: []importer.Rule{bakery, rewe, salary},
		},
		{
			name:      "the second goes up: the same swap",
			existing:  rulesFile,
			index:     1,
			delta:     -1,
			want:      swapped,
			wantRules: []importer.Rule{bakery, rewe, salary},
		},
		{
			name:      "the last goes up",
			existing:  rulesFile,
			index:     2,
			delta:     -1,
			want:      strings.Replace(strings.Replace(rulesFile, "# bakeries: the spelling varies\n[[rule]]\nregex = \"b(ä|ae)ckerei\"\ncategory = \"Expenses:Groceries\"\n\n# end of the rules above\n[[rule]]\ncontains = \"Gehalt\"\ncategory = \"Income:Salary\"\n", "@@", 1), "@@", "# end of the rules above\n[[rule]]\ncontains = \"Gehalt\"\ncategory = \"Income:Salary\"\n\n# bakeries: the spelling varies\n[[rule]]\nregex = \"b(ä|ae)ckerei\"\ncategory = \"Expenses:Groceries\"\n", 1),
			wantRules: []importer.Rule{rewe, salary, bakery},
		},
		{
			name:      "text between two rules stays between them",
			existing:  "[[rule]]\ncontains = \"A1\"\ncategory = \"Expenses:A\"\n\n# a note about both\n\n[[rule]]\ncontains = \"B2\"\ncategory = \"Expenses:B\"\n",
			index:     0,
			delta:     1,
			want:      "[[rule]]\ncontains = \"B2\"\ncategory = \"Expenses:B\"\n\n# a note about both\n\n[[rule]]\ncontains = \"A1\"\ncategory = \"Expenses:A\"\n",
			wantRules: []importer.Rule{{Contains: "B2", Category: "Expenses:B"}, {Contains: "A1", Category: "Expenses:A"}},
		},
		{
			name:      "the first cannot go up",
			existing:  rulesFile,
			index:     0,
			delta:     -1,
			want:      rulesFile,
			wantErr:   "rule_not_found(Rule=1)",
			wantRules: []importer.Rule{rewe, bakery, salary},
		},
		{
			name:      "the last cannot go down",
			existing:  rulesFile,
			index:     2,
			delta:     1,
			want:      rulesFile,
			wantErr:   "rule_not_found(Rule=3)",
			wantRules: []importer.Rule{rewe, bakery, salary},
		},
		{
			name:      "only a step of one is a move",
			existing:  rulesFile,
			index:     0,
			delta:     2,
			want:      rulesFile,
			wantErr:   "rule_not_found(Rule=1)",
			wantRules: []importer.Rule{rewe, bakery, salary},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "config.toml")
			writeFile(t, path, tc.existing)

			// when
			_, err := MoveRule(path, tc.index, tc.delta)

			// then
			checkRulesFile(t, path, err, tc.wantErr, tc.want, tc.wantRules)
		})
	}
}

func Test_EditRule(t *testing.T) {
	rewe := importer.Rule{Contains: "REWE", Category: "Expenses:Groceries"}
	bakery := importer.Rule{Regex: "b(ä|ae)ckerei", Category: "Expenses:Groceries"}
	salary := importer.Rule{Contains: "Gehalt", Category: "Income:Salary"}
	tcs := []struct {
		name      string
		index     int
		rule      importer.Rule
		want      string
		wantRules []importer.Rule
		wantErr   string
	}{
		{
			name:      "the text and the category change; the comment and the place stay",
			index:     0,
			rule:      importer.Rule{Contains: "REWE Markt", Category: "Expenses:Food"},
			want:      strings.Replace(rulesFile, "contains = \"REWE\"\ncategory = \"Expenses:Groceries\"", "contains = \"REWE Markt\"\ncategory = \"Expenses:Food\"", 1),
			wantRules: []importer.Rule{{Contains: "REWE Markt", Category: "Expenses:Food"}, bakery, salary},
		},
		{
			name:      "a regular expression with a backslash is written so that it reads back the same",
			index:     1,
			rule:      importer.Rule{Regex: `rewe\s+markt|"edeka"`, Category: "Expenses:Groceries"},
			want:      strings.Replace(rulesFile, "regex = \"b(ä|ae)ckerei\"", `regex = "rewe\\s+markt|\"edeka\""`, 1),
			wantRules: []importer.Rule{rewe, {Regex: `rewe\s+markt|"edeka"`, Category: "Expenses:Groceries"}, salary},
		},
		{
			name:      "spaces around what is typed are not part of it",
			index:     0,
			rule:      importer.Rule{Contains: "  REWE Markt  ", Category: "Expenses:Groceries"},
			want:      strings.Replace(rulesFile, "contains = \"REWE\"", "contains = \"REWE Markt\"", 1),
			wantRules: []importer.Rule{{Contains: "REWE Markt", Category: "Expenses:Groceries"}, bakery, salary},
		},
		{
			name:      "a comment on a line of the rule goes with the line",
			index:     2,
			rule:      importer.Rule{Contains: "Lohn", Category: "Income:Salary"},
			want:      strings.Replace(rulesFile, "contains = \"Gehalt\"", "contains = \"Lohn\"", 1),
			wantRules: []importer.Rule{rewe, bakery, {Contains: "Lohn", Category: "Income:Salary"}},
		},
		{
			name:      "a rule that would have both is refused",
			index:     0,
			rule:      importer.Rule{Contains: "REWE", Regex: "x", Category: "Expenses:Groceries"},
			want:      rulesFile,
			wantErr:   "rule_set_one(Rule=1)",
			wantRules: []importer.Rule{rewe, bakery, salary},
		},
		{
			name:      "a rule with no text is refused",
			index:     1,
			rule:      importer.Rule{Contains: "  ", Category: "Expenses:Groceries"},
			want:      rulesFile,
			wantErr:   "rule_set_one(Rule=2)",
			wantRules: []importer.Rule{rewe, bakery, salary},
		},
		{
			name:      "a category that is not a name is refused",
			index:     0,
			rule:      importer.Rule{Contains: "REWE", Category: "Groceries"},
			want:      rulesFile,
			wantErr:   "rule_category(Rule=1): invalid_account_type(Name=Groceries)",
			wantRules: []importer.Rule{rewe, bakery, salary},
		},
		{
			name:      "an expression that does not compile is refused",
			index:     0,
			rule:      importer.Rule{Regex: "(", Category: "Expenses:Groceries"},
			want:      rulesFile,
			wantErr:   "rule_regex(Rule=1)",
			wantRules: []importer.Rule{rewe, bakery, salary},
		},
		{
			name:      "a rule that is not there",
			index:     -1,
			rule:      rewe,
			want:      rulesFile,
			wantErr:   "rule_not_found(Rule=0)",
			wantRules: []importer.Rule{rewe, bakery, salary},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "config.toml")
			writeFile(t, path, rulesFile)

			// when
			_, err := EditRule(path, tc.index, tc.rule)

			// then
			checkRulesFile(t, path, err, tc.wantErr, tc.want, tc.wantRules)
		})
	}
}

func Test_Revert(t *testing.T) {
	tcs := []struct {
		name    string
		change  func(t *testing.T, path string)
		want    string
		wantErr string
	}{
		{
			name:   "nothing else touched the file: it is as it was",
			change: func(*testing.T, string) {},
			want:   rulesFile,
		},
		{
			name: "the person edited the file since: it stays as they left it",
			change: func(t *testing.T, path string) {
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, string(body)+"# added by hand\n")
			},
			want:    "# my settings\nkeep_backups = 3\n\n# ---- rules ----\n\n# bakeries: the spelling varies\n[[rule]]\nregex = \"b(ä|ae)ckerei\"\ncategory = \"Expenses:Groceries\"\n\n# the supermarket\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n\n# end of the rules above\n[[rule]]\ncontains = \"Gehalt\"\ncategory = \"Income:Salary\"\n# added by hand\n",
			wantErr: "config_changed(Path=PATH)",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "config.toml")
			writeFile(t, path, rulesFile)
			c, err := MoveRule(path, 0, 1)
			if err != nil {
				t.Fatal(err)
			}
			tc.change(t, path)

			// when
			err = Revert(path, c)

			// then
			gotErr := ""
			if err != nil {
				gotErr = strings.ReplaceAll(err.Error(), path, "PATH")
			}
			if diff := cmp.Diff(tc.wantErr, gotErr); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, string(body)); diff != "" {
				t.Errorf("file mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A rule the lines cannot be cut around, such as a string over several lines with a line that
// looks like a table, is refused by the check on what the new text reads as, and the file is
// left alone.
func Test_rules_unsafeLayout(t *testing.T) {
	const tricky = "[[rule]]\nregex = \"\"\"\n[a-z]+\"\"\"\ncategory = \"Expenses:X\"\n\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n"
	tcs := []struct {
		name string
		do   func(path string) error
	}{
		{name: "delete", do: func(path string) error { _, err := DeleteRule(path, 0); return err }},
		{name: "edit", do: func(path string) error {
			_, err := EditRule(path, 0, importer.Rule{Regex: "[a-z]+", Category: "Expenses:Y"})
			return err
		}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "config.toml")
			writeFile(t, path, tricky)

			// when
			err := tc.do(path)

			// then
			gotErr := ""
			if err != nil {
				gotErr = strings.ReplaceAll(err.Error(), path, "PATH")
			}
			if diff := cmp.Diff("rules_layout(Path=PATH)", gotErr); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tricky, string(body)); diff != "" {
				t.Errorf("file mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The file keeps its permissions, and no stray file is left beside it.
func Test_rules_fileMode(t *testing.T) {
	tcs := []struct {
		name string
		mode os.FileMode
		do   func(path string) error
	}{
		{name: "delete", mode: 0o640, do: func(path string) error { _, err := DeleteRule(path, 0); return err }},
		{name: "move", mode: 0o600, do: func(path string) error { _, err := MoveRule(path, 0, 1); return err }},
		{name: "revert", mode: 0o640, do: func(path string) error {
			c, err := MoveRule(path, 0, 1)
			if err != nil {
				return err
			}
			return Revert(path, c)
		}},
		{name: "edit", mode: 0o644, do: func(path string) error {
			_, err := EditRule(path, 0, importer.Rule{Contains: "Edeka", Category: "Expenses:Groceries"})
			return err
		}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			writeFile(t, path, rulesFile)
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}

			// when
			err := tc.do(path)

			// then
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff([2]any{tc.mode, 1}, [2]any{info.Mode().Perm(), len(entries)}); diff != "" {
				t.Errorf("mode and files mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
