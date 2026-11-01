package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_Update_categorize(t *testing.T) {
	type observed struct {
		Flow
		First    pickItem // what the picker suggests first, while it is open
		Last     pickItem
		Selected string
	}
	const (
		kino    = "2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Uncategorized 12.00"
		rewe    = "2026-10-02 REWE | Assets:Bank -20.00 | Expenses:Uncategorized 20.00"
		decide  = "decide later"
		uncateg = ledger.UncategorizedExpense
	)
	tcs := []struct {
		name   string
		script string
		want   observed
		shows  []string
	}{
		{
			name: "the picker suggests the category the payee went to last time", script: "3 u down c",
			want:  observed{Flow: Flow{Mode: "pick"}, First: pickItem{name: "Expenses:Groceries", hint: "same payee"}, Last: pickItem{name: uncateg, hint: decide}},
			shows: []string{"same payee"},
		},
		{
			name: "taking the suggestion", script: "3 u down c enter",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "#3 → Expenses:Groceries",
					Added: []string{"2026-10-02 REWE | Assets:Bank -20.00 | Expenses:Groceries 20.00"}, Removed: []string{rewe},
				},
				Selected: "Kino",
			},
		},
		{
			name: "a full name that does not exist is offered as a new category", script: "3 u c text:Expenses:Fun",
			want: observed{Flow: Flow{Mode: "pick"}, First: pickItem{name: "Expenses:Fun", hint: "new category", create: true}, Last: pickItem{name: "Expenses:Fun", hint: "new category", create: true}},
		},
		{
			name: "choosing it creates the category", script: "3 u c text:Expenses:Fun down enter",
			want: observed{
				Flow:     Flow{Mode: "list", Status: "#4 → Expenses:Fun", Added: []string{"2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Fun 12.00"}, Removed: []string{kino}},
				Selected: "REWE", // Kino has left the list of what needs a category
			},
		},
		{
			name: "decide later keeps it uncategorized", script: "3 u c down down down down down enter",
			want: observed{Flow: Flow{Mode: "list", Status: "#4 → " + uncateg}, Selected: "Kino"},
		},
		{
			name: "esc leaves it as it was", script: "3 u c text:Expenses:Fun esc",
			want: observed{Flow: Flow{Mode: "list"}, Selected: "Kino"},
		},
		{
			name: "a transfer cannot be recategorized", script: "3 g c",
			want: observed{Flow: Flow{Mode: "list", Status: "transfers and split transactions cannot be recategorized here", StatusIsError: true}, Selected: "to savings"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before)}
			if m.mode == modePick {
				got.First, got.Last = m.pickItems[0], m.pickItems[len(m.pickItems)-1]
			}
			if r, ok := m.selected(); ok {
				got.Selected = r.tx.Payee
			}
			if tc.want.Selected == "" {
				got.Selected = ""
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty, cmp.AllowUnexported(pickItem{})); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

// Equity:Transfers is not an income or expense category, so the picker offers it
// separately: last, just before "decide later".
func Test_Update_categorize_transfers(t *testing.T) {
	type observed struct {
		Items    []pickItem
		Category string // what Kino is booked to once the script has run
	}
	tcs := []struct {
		name   string
		script string
		want   observed
	}{
		{
			name: "offered just before 'decide later'", script: "3 u c",
			want: observed{Items: []pickItem{
				{name: "Expenses:Groceries", hint: "2×"},
				{name: ledger.TransferAccount, hint: "between my accounts"},
				{name: ledger.UncategorizedExpense, hint: "decide later"},
			}, Category: ledger.UncategorizedExpense},
		},
		{
			name: "found by typing a few letters, and not offered as new", script: "3 u c text:transf",
			want: observed{Items: []pickItem{{name: ledger.TransferAccount, hint: "between my accounts"}}, Category: ledger.UncategorizedExpense},
		},
		{
			name: "chosen", script: "3 u c text:transf enter",
			want: observed{Category: ledger.TransferAccount},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)

			// when
			play(m, tc.script)

			// then
			got := observed{Category: categoryOf(t, st, "Kino")}
			if m.mode == modePick {
				got.Items = m.pickItems
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty, cmp.AllowUnexported(pickItem{})); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// ruleModel is newModel with a real config file that rules are read from and
// appended to, and a second uncategorized "Kino" so a rule has something to
// catch besides the transaction being categorized. initial is the file's
// content before; "" means there is no file.
func ruleModel(t *testing.T, initial string) (*Model, *core.Local, string) {
	t.Helper()
	m, st := newModel(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if initial != "" {
		if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	load := func() (*config.Config, error) { return config.Load(path, false) }
	m.loadConfig = load
	m.opts = Options{LoadConfig: load, ConfigPath: path, ConfigTemplate: "# sample\n[[rule]]\ncontains = \"SAMPLE\"\ncategory = \"Expenses:Sample\"\n"}
	book(t, st, "2026-10-10", "Kino Royal", "Assets:Bank", ledger.UncategorizedExpense, -500)
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.applyFilter(false)
	return m, st, path
}

func Test_Update_categorize_makingARule(t *testing.T) {
	type observed struct {
		Flow
		RuleOn       bool   // the rule field has the focus
		RuleText     string // what it looks for
		Note         string // what the popup says about the rule
		NoteIsError  bool
		Config       []importer.Rule // the rules in config.toml
		ConfigExists bool
	}
	const (
		kino      = "2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Uncategorized 12.00"
		kinoRoyal = "2026-10-10 Kino Royal | Assets:Bank -5.00 | Expenses:Uncategorized 5.00"
		rewe      = "2026-10-02 REWE | Assets:Bank -20.00 | Expenses:Uncategorized 20.00"
		existing  = "# mine\n[[rule]]\ncontains = \"rewe\"\ncategory = \"Expenses:Groceries\"\n"
	)
	kinoFun := importer.Rule{Contains: "Kino", Category: "Expenses:Fun"}
	tcs := []struct {
		name    string
		initial string
		payee   string
		script  string
		broken  bool // the config cannot be read
		want    observed
	}{
		{
			name: "tab moves to the rule field and offers the payee", payee: "Kino", script: "c text:Expenses:Fun tab",
			want: observed{Flow: Flow{Mode: "pick"}, RuleOn: true, RuleText: "Kino", Note: "adds a rule to config.toml · also categorizes 1 other"},
		},
		{
			name: "enter categorizes this one, writes the rule and categorizes the others", payee: "Kino", script: "c text:Expenses:Fun tab enter",
			want: observed{
				Flow: Flow{
					Mode:   "list",
					Status: "#4 → Expenses:Fun · rule added to config.toml · 1 more categorized · z undoes · C edits the rules",
					Added: []string{
						"2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Fun 12.00",
						"2026-10-10 Kino Royal | Assets:Bank -5.00 | Expenses:Fun 5.00",
					},
					Removed: []string{kino, kinoRoyal},
				},
				Config: []importer.Rule{kinoFun}, ConfigExists: true,
			},
		},
		{
			name: "z puts both categories back; the rule stays in the file", payee: "Kino", script: "c text:Expenses:Fun tab enter z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid categorizing #4 and 1 more"}, Config: []importer.Rule{kinoFun}, ConfigExists: true},
		},
		{
			name: "the payee can be trimmed to widen the rule", payee: "Kino Royal", script: "c text:Expenses:Fun tab",
			want: observed{Flow: Flow{Mode: "pick"}, RuleOn: true, RuleText: "Kino Royal", Note: "adds a rule to config.toml · no other uncategorized match"},
		},
		{
			name: "...and then it catches the other one", payee: "Kino Royal", script: "c text:Expenses:Fun tab ctrl+u text:Kino",
			want: observed{Flow: Flow{Mode: "pick"}, RuleOn: true, RuleText: "Kino", Note: "adds a rule to config.toml · also categorizes 1 other"},
		},
		{
			name: "an earlier rule that decides otherwise is named, and nothing is written", initial: existing, payee: "REWE", script: "c text:Expenses:Fun tab enter",
			want: observed{
				Flow:   Flow{Mode: "pick"},
				RuleOn: true, RuleText: "REWE", NoteIsError: true,
				Note:   "rule 1 already sends this to Expenses:Groceries and is checked first; change it with C",
				Config: []importer.Rule{{Contains: "rewe", Category: "Expenses:Groceries"}}, ConfigExists: true,
			},
		},
		{
			name: "choosing what that rule says adds nothing to the file", initial: existing, payee: "REWE",
			script: "c text:Expenses:Fun tab tab backspace backspace backspace backspace backspace backspace backspace backspace backspace backspace backspace backspace text:Groceries enter",
			want: observed{
				Flow: Flow{
					Mode:   "list",
					Status: "#3 → Expenses:Groceries · rule 1 already covers this · z undoes · C edits the rules",
					Added:  []string{"2026-10-02 REWE | Assets:Bank -20.00 | Expenses:Groceries 20.00"}, Removed: []string{rewe},
				},
				Config: []importer.Rule{{Contains: "rewe", Category: "Expenses:Groceries"}}, ConfigExists: true,
			},
		},
		{
			name: "text that is not in the transaction", payee: "Kino", script: "c text:Expenses:Fun tab ctrl+u text:zzz enter",
			want: observed{Flow: Flow{Mode: "pick"}, RuleOn: true, RuleText: "zzz", NoteIsError: true, Note: `"zzz" does not appear in this transaction's payee or description`},
		},
		{
			name: "a single character", payee: "Kino", script: "c text:Expenses:Fun tab ctrl+u text:K enter",
			want: observed{Flow: Flow{Mode: "pick"}, RuleOn: true, RuleText: "K", NoteIsError: true, Note: "a rule needs at least two characters to look for"},
		},
		{
			name: "'decide later' is no category for a rule", payee: "Kino", script: "c down down down down down tab enter",
			want: observed{Flow: Flow{Mode: "pick"}, RuleOn: true, RuleText: "Kino", NoteIsError: true, Note: `pick a real category for a rule, not "decide later"`},
		},
		{
			name: "an empty rule field means no rule", payee: "Kino", script: "c text:Expenses:Fun tab ctrl+u enter",
			want: observed{Flow: Flow{
				Mode: "list", Status: "#4 → Expenses:Fun",
				Added: []string{"2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Fun 12.00"}, Removed: []string{kino},
			}},
		},
		{
			name: "esc writes nothing", payee: "Kino", script: "c text:Expenses:Fun tab esc",
			want: observed{Flow: Flow{Mode: "list"}},
		},
		{
			name: "tab twice returns to the category field", payee: "Kino", script: "c tab tab",
			want: observed{Flow: Flow{Mode: "pick"}, RuleText: "Kino", Note: "adds a rule to config.toml · also categorizes 1 other"},
		},
		{
			name: "a config that cannot be read stops the rule but not the categorizing", payee: "Kino", broken: true, script: "c text:Expenses:Fun tab enter",
			want: observed{Flow: Flow{
				Mode: "list", Status: "#4 → Expenses:Fun",
				Added: []string{"2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Fun 12.00"}, Removed: []string{kino},
			}},
		},
		{
			name: "...and says why", payee: "Kino", broken: true, script: "c text:Expenses:Fun tab",
			want: observed{Flow: Flow{Mode: "pick"}, NoteIsError: true, Note: "fix the config first (esc, then C): config.toml: unknown key(s): [rule.typo]"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st, path := ruleModel(t, tc.initial)
			if tc.broken {
				m.loadConfig = func() (*config.Config, error) { return nil, errors.New("config.toml: unknown key(s): [rule.typo]") }
			}
			before := storedLines(t, st)
			selectPayee(t, m, tc.payee)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), RuleOn: m.pickRuleOn, RuleText: m.pickRule.Value(), Note: m.pickNote, NoteIsError: m.pickNoteBad}
			if m.mode != modePick { // the popup's own state is only meaningful while it is open
				got.RuleOn, got.RuleText, got.Note, got.NoteIsError = false, "", "", false
			}
			if _, err := os.Stat(path); err == nil {
				got.ConfigExists = true
				if c, err := config.Load(path, true); err == nil {
					got.Config = c.Rules
				}
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
