package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/melancholypurin/thaler/internal/importer"
)

// fuzzConfigs are config files to start from: the sample of the repository's rules tests,
// settings of each kind, and texts a person might leave half written.
var fuzzConfigs = []string{
	rulesFile,
	"",
	"language = \"de\"\ntheme = \"dark\"\nkeep_backups = 5\nbackup_reminder_days = 0\nimport_dir = \"~/x\"\n",
	"[[rule]]\ncontains = \"a\"\ncategory = \"Expenses:A\"\n[[rule]] # two\nregex = \"(b\"\ncategory = \"Expenses:B\"\n",
	"rule = [{contains = \"a\", category = \"Expenses:A\"}]\n",
	"[[rule]]\ncontains = \"\"\"\nREWE\n\"\"\"\ncategory = \"Expenses:A\"\n",
	"[[profile]]\nname = \"x\"\nformat = \"qif\"\n[profile.columns]\ndate = [\"D\"]\n",
	"[features]\nsomething = true\n",
	"keep_backups = -1\n",
	"[[rule]\n",
}

// Fuzz_Load feeds Load any text as config.toml. It reads it or says what is wrong, and
// never panics; what it reads, it reads the same way again.
//
// make fuzz runs it for longer; go test runs the seeds.
func Fuzz_Load(f *testing.F) {
	for _, c := range fuzzConfigs {
		f.Add(c)
	}
	f.Fuzz(func(t *testing.T, text string) {
		// given
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}

		// when
		got, err := Load(path, true)
		again, errAgain := Load(path, true)

		// then
		if (err == nil) != (errAgain == nil) {
			t.Fatalf("the same file read once with %v and once with %v", err, errAgain)
		}
		if diff := cmp.Diff(got, again); diff != "" {
			t.Fatalf("the same file read differently (-first +again):\n%s", diff)
		}
	})
}

// Fuzz_rules deletes, moves and rewrites a rule in any config file. Whatever the file
// holds, a change that is refused leaves it as it was; one that is made gives exactly the
// rules meant, leaves every other setting as it was, and Revert puts back the file byte
// for byte.
//
// make fuzz runs it for longer; go test runs the seeds.
func Fuzz_rules(f *testing.F) {
	for i, c := range fuzzConfigs {
		for op := range uint8(4) {
			f.Add(c, op, uint8(i%3), "Lidl", "Expenses:Groceries")
		}
	}
	f.Fuzz(func(t *testing.T, text string, op, index uint8, contains, category string) {
		// given
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		before, errBefore := decodeRules([]byte(text))
		i := int(index % 4)
		edited := importer.Rule{Contains: contains, Category: category}
		written := importer.Rule{Contains: strings.TrimSpace(contains), Category: category} // EditRule trims what to look for

		// when
		var change Change
		var err error
		var want []importer.Rule
		switch op % 4 {
		case 0:
			change, err = DeleteRule(path, i)
			if i < len(before) {
				want = slices.Delete(slices.Clone(before), i, i+1)
			}
		case 1, 2:
			delta := 1
			if op%4 == 2 {
				delta = -1
			}
			change, err = MoveRule(path, i, delta)
			if j := i + delta; i < len(before) && j >= 0 && j < len(before) {
				want = slices.Clone(before)
				want[i], want[j] = want[j], want[i]
			}
		case 3:
			change, err = EditRule(path, i, edited)
			if i < len(before) {
				want = slices.Clone(before)
				want[i] = written
			}
		}

		// then
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err != nil {
			if string(after) != text {
				t.Fatalf("a refused change (%v) changed the file:\n%q\nbecame\n%q", err, text, after)
			}
			return
		}
		if errBefore != nil {
			t.Fatalf("a change was made to a file that does not read as TOML: %v", errBefore)
		}
		got, err := decodeRules(after)
		if err != nil {
			t.Fatalf("the changed file does not read as TOML: %v\n%q", err, after)
		}
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Fatalf("the rules are not what was meant (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(otherSettings(t, []byte(text)), otherSettings(t, after)); diff != "" {
			t.Fatalf("a setting other than the rules changed (-before +after):\n%s", diff)
		}
		if err := Revert(path, change); err != nil {
			t.Fatalf("Revert: %v", err)
		}
		if reverted, _ := os.ReadFile(path); string(reverted) != text {
			t.Fatalf("Revert did not put the file back:\n%q\nis\n%q", text, reverted)
		}
	})
}

// otherSettings is everything in the config text but its rules.
func otherSettings(t *testing.T, text []byte) map[string]any {
	t.Helper()
	var all map[string]any
	if _, err := toml.Decode(string(text), &all); err != nil {
		t.Fatalf("reading the settings: %v", err)
	}
	delete(all, "rule")
	return all
}
