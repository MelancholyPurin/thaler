package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/i18n"
)

// rulesConfig is a config with settings, a comment of the person's own and three rules.
const rulesConfig = `# mine
keep_backups = 3

# the supermarket
[[rule]]
contains = "REWE"
category = "Expenses:Groceries"

[[rule]]
regex = "b(ä|ae)ckerei"
category = "Expenses:Groceries"

[[rule]]
contains = "Gehalt"
category = "Income:Salary"
`

func Test_rulesScreen(t *testing.T) {
	tcs := []struct {
		name    string
		initial string
		keys    string
		shows   []string
		hides   []string
	}{
		{
			name:    "M lists the rules in the order they are checked, a regular expression between slashes",
			initial: rulesConfig,
			keys:    "M",
			shows:   []string{"Rules", `1 "REWE"`, "→ Expenses:Groceries", "2 /b(ä|ae)ckerei/", `3 "Gehalt"`, "→ Income:Salary", "The first rule that matches wins", "enter edit · esc close"},
		},
		{
			name:    "e shows what the rule looks for and its category",
			initial: rulesConfig,
			keys:    "M e",
			shows:   []string{"▸ Contains   REWE", "Category   Expenses:Groceries", "tab goes to the other field", "tab field · enter save · esc back"},
			hides:   []string{"The first rule that matches wins"},
		},
		{
			name:    "a regular expression is called so",
			initial: rulesConfig,
			keys:    "M down e",
			shows:   []string{"▸ Regex      b(ä|ae)ckerei"},
		},
		{
			name:    "tab goes to the category",
			initial: rulesConfig,
			keys:    "M e tab",
			shows:   []string{"Contains   REWE", "▸ Category   Expenses:Groceries"},
		},
		{
			name:    "d asks before deleting",
			initial: rulesConfig,
			keys:    "M down d",
			shows:   []string{"Delete rule 2, /b(ä|ae)ckerei/ → Expenses:Groceries?", "it categorized stay as they are", "enter delete · esc back"},
		},
		{
			name:    "no rules: it says how to make one",
			initial: "keep_backups = 3\n",
			keys:    "M",
			shows:   []string{"No rules yet: make one while categorizing (c)"},
			hides:   []string{"The first rule"},
		},
		{
			name:    "the key help lists it",
			initial: rulesConfig,
			keys:    "? pgdown pgdown pgdown",
			shows:   []string{"Rules: ↑ ↓ choose; e edits its text and category"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _, _ := ruleModel(t, tc.initial)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_rules_actions(t *testing.T) {
	type observed struct {
		Mode   string
		Status string
		Err    string // what the popup says is wrong
		Note   string // what it says was done
		Cursor int
		Step   ruleStep // where in the popup it is
		Undo   int      // changes that z could take back
		File   string
	}
	const (
		rewe    = "[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n"
		bakery  = "[[rule]]\nregex = \"b(ä|ae)ckerei\"\ncategory = \"Expenses:Groceries\"\n"
		salary  = "[[rule]]\ncontains = \"Gehalt\"\ncategory = \"Income:Salary\"\n"
		head    = "# mine\nkeep_backups = 3\n\n"
		comment = "# the supermarket\n"
	)
	tcs := []struct {
		name    string
		initial string
		keys    string
		between func(t *testing.T, path string) // what the person does to the file while the popup is open
		after   string                          // more keys, after that
		want    observed
	}{
		{
			name:    "edit: the text changes where the rule stands, with its comment",
			initial: rulesConfig,
			keys:    "M e ctrl+u text:REWE_Markt enter",
			want: observed{
				Mode: "rules", Note: "Rule 1 changed · z undoes", Undo: 1,
				File: head + comment + "[[rule]]\ncontains = \"REWE Markt\"\ncategory = \"Expenses:Groceries\"\n\n" + bakery + "\n" + salary,
			},
		},
		{
			name:    "edit: the category",
			initial: rulesConfig,
			keys:    "M e tab ctrl+u raw:Expenses:Food enter",
			want: observed{
				Mode: "rules", Note: "Rule 1 changed · z undoes", Undo: 1,
				File: head + comment + "[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Food\"\n\n" + bakery + "\n" + salary,
			},
		},
		{
			name:    "edit: a regular expression stays one, and what is typed is written so that it reads back the same",
			initial: rulesConfig,
			keys:    `M down e ctrl+u raw:rewe\s+markt enter`,
			want: observed{
				Mode: "rules", Cursor: 1, Note: "Rule 2 changed · z undoes", Undo: 1,
				File: head + comment + rewe + "\n[[rule]]\nregex = \"rewe\\\\s+markt\"\ncategory = \"Expenses:Groceries\"\n\n" + salary,
			},
		},
		{
			name:    "edit: nothing changed, nothing written, nothing to undo",
			initial: rulesConfig,
			keys:    "M e enter",
			want:    observed{Mode: "rules", Undo: 0, File: rulesConfig},
		},
		{
			name:    "edit: no text is said, and the file is left alone",
			initial: rulesConfig,
			keys:    "M e ctrl+u enter",
			want:    observed{Mode: "rules", Step: ruleEdit, Err: "rule 1: set exactly one of contains and regex", File: rulesConfig},
		},
		{
			name:    "edit: text that is too short",
			initial: rulesConfig,
			keys:    "M e ctrl+u text:a enter",
			want:    observed{Mode: "rules", Step: ruleEdit, Err: "a rule needs at least two characters to look for", File: rulesConfig},
		},
		{
			name:    "edit: an expression that does not compile",
			initial: rulesConfig,
			keys:    "M down e ctrl+u raw:( enter",
			want:    observed{Mode: "rules", Step: ruleEdit, Cursor: 1, Err: "rule 2: regex: error parsing regexp: missing closing ): `(?i)(`", File: rulesConfig},
		},
		{
			name:    "edit: a category that is not a name",
			initial: rulesConfig,
			keys:    "M e tab ctrl+u raw:Groceries enter",
			want:    observed{Mode: "rules", Step: ruleEdit, Err: "rule 1: category: account \"Groceries\" must start with one of Assets, Liabilities, Income, Expenses, Equity", File: rulesConfig},
		},
		{
			name:    "edit: not Uncategorized",
			initial: rulesConfig,
			keys:    "M e tab ctrl+u raw:Expenses:Uncategorized enter",
			want:    observed{Mode: "rules", Step: ruleEdit, Err: `pick a real category for a rule, not "decide later"`, File: rulesConfig},
		},
		{
			name:    "edit: esc goes back to the list without writing",
			initial: rulesConfig,
			keys:    "M e ctrl+u text:XX esc",
			want:    observed{Mode: "rules", File: rulesConfig},
		},
		{
			name:    "delete: asks, then the rule goes with its comment",
			initial: rulesConfig,
			keys:    "M d enter",
			want: observed{
				Mode: "rules", Note: "Rule 1 deleted · z undoes", Undo: 1,
				File: head + bakery + "\n" + salary,
			},
		},
		{
			name:    "delete: y does as enter",
			initial: rulesConfig,
			keys:    "M down d y",
			want: observed{
				Mode: "rules", Cursor: 1, Note: "Rule 2 deleted · z undoes", Undo: 1,
				File: head + comment + rewe + "\n" + salary,
			},
		},
		{
			name:    "delete: the last rule leaves the cursor on the one before",
			initial: rulesConfig,
			keys:    "M G d enter",
			want: observed{
				Mode: "rules", Cursor: 1, Note: "Rule 3 deleted · z undoes", Undo: 1,
				File: head + comment + rewe + "\n" + bakery,
			},
		},
		{
			name:    "delete: esc is no",
			initial: rulesConfig,
			keys:    "M d esc",
			want:    observed{Mode: "rules", File: rulesConfig},
		},
		{
			name:    "delete: n is no",
			initial: rulesConfig,
			keys:    "M d n",
			want:    observed{Mode: "rules", File: rulesConfig},
		},
		{
			name:    "delete: other keys are not an answer",
			initial: rulesConfig,
			keys:    "M d x",
			want:    observed{Mode: "rules", Step: ruleDelete, File: rulesConfig},
		},
		{
			name:    "delete: the only rule closes the popup",
			initial: "[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n",
			keys:    "M d enter",
			want:    observed{Mode: "list", Status: "No rules yet: make one while categorizing (c)", Undo: 1, File: ""},
		},
		{
			name:    "move: down, and the cursor goes with the rule",
			initial: rulesConfig,
			keys:    "M >",
			want: observed{
				Mode: "rules", Cursor: 1, Note: "Rule 1 moved down to place 2 · z undoes", Undo: 1,
				File: head + bakery + "\n" + comment + rewe + "\n" + salary,
			},
		},
		{
			name:    "move: up",
			initial: rulesConfig,
			keys:    "M G <",
			want: observed{
				Mode: "rules", Cursor: 1, Note: "Rule 3 moved up to place 2 · z undoes", Undo: 1,
				File: head + comment + rewe + "\n" + salary + "\n" + bakery,
			},
		},
		{
			name:    "move: the first cannot go up and the last cannot go down",
			initial: rulesConfig,
			keys:    "M < G >",
			want:    observed{Mode: "rules", Cursor: 2, File: rulesConfig},
		},
		{
			name:    "undo: z in the popup takes back the move",
			initial: rulesConfig,
			keys:    "M > z",
			want:    observed{Mode: "rules", Cursor: 1, Note: "Undid the change to the rules", File: rulesConfig},
		},
		{
			name:    "undo: a second z takes back the one before",
			initial: rulesConfig,
			keys:    "M > d enter z z",
			want:    observed{Mode: "rules", Cursor: 1, Note: "Undid the change to the rules", File: rulesConfig},
		},
		{
			name:    "undo: z after closing the popup does the same",
			initial: rulesConfig,
			keys:    "M d enter esc z",
			want:    observed{Mode: "list", Status: "Undid the change to the rules", File: rulesConfig},
		},
		{
			name:    "undo: nothing to undo",
			initial: rulesConfig,
			keys:    "M z",
			want:    observed{Mode: "rules", Err: "nothing to undo", File: rulesConfig},
		},
		{
			name:    "undo: a change that is not to the rules is not undone from here",
			initial: rulesConfig,
			keys:    "tab tab down d y M z",
			want:    observed{Mode: "rules", Err: "nothing to undo", Undo: 1, File: rulesConfig},
		},
		{
			name:    "undo: a file the person changed since is not overwritten",
			initial: rulesConfig,
			keys:    "M >",
			between: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("# changed by hand\n"+rulesConfig), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			after: "z",
			want: observed{
				Mode: "rules", Cursor: 1, Err: "undo failed: PATH has been changed since, so Thaler leaves it as it is", Undo: 0,
				File: "# changed by hand\n" + rulesConfig,
			},
		},
		{
			name:    "a file with the rules written some other way is refused, and left alone",
			initial: "rule = [{contains = \"REWE\", category = \"Expenses:Groceries\"}]\n",
			keys:    "M e ctrl+u text:REWE_Markt enter",
			want: observed{
				Mode: "rules", Step: ruleEdit, Err: "the rules in PATH are not written as [[rule]] tables, one after the other, so Thaler will not rewrite them: change them in the file (C)",
				File: "rule = [{contains = \"REWE\", category = \"Expenses:Groceries\"}]\n",
			},
		},
		{
			name:    "esc closes the popup",
			initial: rulesConfig,
			keys:    "M esc",
			want:    observed{Mode: "list", File: rulesConfig},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _, path := ruleModel(t, tc.initial)
			m.undo = nil

			// when
			play(m, tc.keys)
			if tc.between != nil {
				tc.between(t, path)
				play(m, tc.after)
			}

			// then
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := observed{Mode: modeNames[m.mode], Status: m.status, Undo: len(m.undo), File: string(body)}
			if m.ruleForm != nil {
				got.Err, got.Note, got.Cursor, got.Step = m.ruleForm.err, m.ruleForm.note, m.ruleForm.list.cursor, m.ruleForm.step
				if cfg, err := config.Load(path, true); err == nil { // the popup shows what the file holds
					if diff := cmp.Diff(cfg.Rules, m.ruleForm.rules); diff != "" {
						t.Errorf("rules shown mismatch (-file +popup):\n%s", diff)
					}
				}
			}
			got.Err = strings.ReplaceAll(got.Err, path, "PATH")
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Without a config file or a way to read it there is nothing to show.
func Test_rules_noConfig(t *testing.T) {
	tcs := []struct {
		name string
		path string
		load bool
		want string
	}{
		{name: "no config location", path: "", load: true, want: "no config file to read the rules from"},
		{name: "no way to read it", path: "/somewhere/config.toml", load: false, want: "no config file to read the rules from"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _, _ := ruleModel(t, rulesConfig)
			m.opts.ConfigPath = tc.path
			if !tc.load {
				m.loadConfig = nil
			}

			// when
			play(m, "M")

			// then
			got := [2]any{modeNames[m.mode], m.status}
			if diff := cmp.Diff([2]any{"list", tc.want}, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The popup speaks the language of the interface, and says what was done in it too.
func Test_rules_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		keys  string
		shows []string
		hides []string
	}{
		{name: "Spanish, the list", tag: language.Spanish, keys: "M", shows: []string{"Reglas", "Gana la primera regla que coincide", "enter editar · esc cerrar"}, hides: []string{"The first rule", "Rules"}},
		{name: "German, the list", tag: language.German, keys: "M", shows: []string{"Regeln", "Es gilt die erste passende Regel", "enter ändern · esc schließen"}, hides: []string{"The first rule", "Rules"}},
		{name: "Spanish, the form", tag: language.Spanish, keys: "M e", shows: []string{"Contiene", "Categoría", "tab campo · enter guardar · esc volver"}, hides: []string{"Contains"}},
		{name: "German, the question", tag: language.German, keys: "M d", shows: []string{"Regel 1, \"REWE\" → Expenses:Groceries löschen?", "enter löschen · esc zurück"}, hides: []string{"Delete rule"}},
		{name: "Spanish, moved", tag: language.Spanish, keys: "M >", shows: []string{"La regla 1 bajó al puesto 2 · z deshace"}, hides: []string{"moved down"}},
		{name: "German, deleted and undone", tag: language.German, keys: "M d enter z", shows: []string{"Änderung an den Regeln rückgängig gemacht"}, hides: []string{"Undid"}},
		{name: "Spanish, an error", tag: language.Spanish, keys: "M e ctrl+u enter", shows: []string{"regla 1"}, hides: []string{"rule 1"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _, _ := ruleModel(t, rulesConfig)
			m.loc = i18n.New(tc.tag)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
