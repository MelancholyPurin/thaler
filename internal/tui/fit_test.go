package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
)

// fitWidth and fitHeight are the smallest terminal Thaler promises to fit in.
const (
	fitWidth  = 80
	fitHeight = 24
)

// fitModel is a model for the fit tests: a terminal of 80 by 24, short paths so that
// they are not cut for being long, backups to list, and a bank export to import.
func fitModel(t *testing.T, tag language.Tag) *Model {
	t.Helper()
	cheapScrypt(t)
	dir, err := os.MkdirTemp("/tmp", "thaler-fit-") // short: a long path would be cut, and that is not what is tested
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(filepath.Join(backups, "replaced"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"thaler-20261021-090000.db", "thaler-20261020-090000.db.age", "thaler-20261019-090000.db",
		"replaced/thaler-20261018-090000-replaced.db",
	} {
		when := fixedNow().AddDate(0, 0, -3)
		path := filepath.Join(backups, name)
		if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	downloads := filepath.Join(dir, "dl")
	if err := os.Mkdir(downloads, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(downloads, "umsaetze.csv"), []byte(exportCSV), 0o600); err != nil {
		t.Fatal(err)
	}

	st := openLedger(t)
	openAccounts(t, st, "Assets:Bank EUR", "Assets:USD USD")
	book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Uncategorized", -1000)
	book(t, st, "2026-10-02", "EDEKA", "Assets:Bank", "Expenses:Groceries", -2000)
	book(t, st, "2026-10-03", "ACME", "Assets:Bank", "Income:Salary", 5000)
	testSchedules(t, st, "Assets:USD")
	keep := 1
	m, err := newWithClock(t.Context(), st, Options{
		LoadConfig:     func() (*config.Config, error) { return &config.Config{KeepBackups: &keep}, nil },
		DatabasePath:   filepath.Join(dir, "thaler.db"),
		BackupDir:      backups,
		ConfigPath:     filepath.Join(dir, "config.toml"),
		ConfigTemplate: "# sample\n",
		Restore: func(context.Context, string, string) (core.Service, core.RestoreResult, error) {
			return nil, core.RestoreResult{}, errors.New("not tried here")
		},
		Loc: i18n.New(tag),
	}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	m.downloads = downloads
	m.Update(tea.WindowSizeMsg{Width: fitWidth, Height: fitHeight})
	return m
}

// cutIn lists the lines of a screen that are wider than the terminal, or that hold the
// mark that Thaler leaves where it cut a text short.
func cutIn(view string) []string {
	var bad []string
	lines := strings.Split(view, "\n")
	if len(lines) > fitHeight {
		bad = append(bad, "taller than the terminal: "+strings.Repeat("|", len(lines)))
	}
	for _, l := range lines {
		l = strings.ReplaceAll(l, "→ …", "→ ") // a path cut from the left on purpose, to keep the end that tells files apart
		switch {
		case lipgloss.Width(l) > fitWidth:
			bad = append(bad, "wider than the terminal: "+l)
		case strings.Contains(l, "…"):
			bad = append(bad, strings.TrimRight(l, " "))
		}
	}
	return bad
}

// Every screen and popup fits a terminal of 80 by 24 in every language: nothing is
// cut short (Thaler marks a cut with …), and nothing is wider or taller than the
// terminal. The data is short on purpose, so what is cut is Thaler's own text.
func Test_View_fits(t *testing.T) {
	screens := []struct {
		name string
		keys string
	}{
		{name: "home", keys: ""},
		{name: "home, left this month", keys: "B e text:30 enter L"},
		{name: "home, left this month, passed", keys: "B e text:5 enter L"},
		{name: "accounts", keys: "tab"},
		{name: "one account", keys: "tab down"},
		{name: "transactions", keys: "tab tab"},
		{name: "one transaction", keys: "tab tab down"},
		{name: "an uncategorized transaction", keys: "tab tab down down"},
		{name: "add form", keys: "a"},
		{name: "add form, payee", keys: "a tab"},
		{name: "add form, category", keys: "a tab tab"},
		{name: "add form, date", keys: "a tab tab tab"},
		{name: "add form, description", keys: "a tab tab tab tab"},
		{name: "add form, error", keys: "a ctrl+s"},
		{name: "transfer", keys: "x"},
		{name: "transfer across currencies", keys: "x tab text:10 tab text:11"},
		{name: "transfer, date", keys: "x tab tab"},
		{name: "transfer, accounts", keys: "x tab tab tab"},
		{name: "import, files", keys: "i"},
		{name: "import, review", keys: "i enter"},
		{name: "new account", keys: "n"},
		{name: "new account, type", keys: "n tab"},
		{name: "new account, currency", keys: "n tab tab"},
		{name: "new account, balance", keys: "n tab tab tab"},
		{name: "new account, date", keys: "n tab tab tab tab"},
		{name: "set balance", keys: "tab down b"},
		{name: "backup", keys: "S"},
		{name: "backup, passphrase", keys: "S tab text:secret"},
		{name: "restore", keys: "ctrl+r"},
		{name: "restore, passphrase", keys: "ctrl+r down tab"},
		{name: "restore, sure?", keys: "ctrl+r enter"},
		{name: "remove old backups", keys: "ctrl+p"},
		{name: "help", keys: "?"},
		{name: "delete", keys: "tab tab down d"},
		{name: "categorize", keys: "tab tab down down c"},
		{name: "categorize, rule", keys: "tab tab down down c tab text:rewe"},
		{name: "language", keys: "ctrl+l"},
		{name: "budget", keys: "B"},
		{name: "budget with a target", keys: "B e text:30 enter"},
		{name: "budget with a target passed", keys: "B e text:5 enter"},
		{name: "budget, categories", keys: "B tab"},
		{name: "budget, categories with a target", keys: "B e text:30 enter tab"},
		{name: "budget, a category's transactions", keys: "B tab enter"},
		{name: "budget, a category's transactions, nothing left after a move", keys: "B tab enter esc"},
		{name: "target", keys: "B e"},
		{name: "target, with a target", keys: "B e text:30"},
		{name: "target, carry-over on", keys: "B e tab"},
		{name: "budget, planned income", keys: "B e down text:80 enter"},
		{name: "budget, planned income passed", keys: "B e down text:20 enter"},
		{name: "budget, planned income, categories", keys: "B e down text:80 enter tab tab"},
		{name: "target, income", keys: "B e down"},
		{name: "target, income with a plan", keys: "B e down text:80"},
		{name: "forecast", keys: "F"},
		{name: "forecast, the last schedule", keys: "F G"},
		{name: "forecast, one account", keys: "F a"},
		{name: "forecast, a second account", keys: "F a a"},
		{name: "forecast, one account, the balances", keys: "F a tab"},
		{name: "forecast, one account, nothing due", keys: "F a a enter"},
		{name: "balances", keys: "F tab"},
		{name: "balances, the end", keys: "F tab G"},
		{name: "balances, under a minimum", keys: "F m text:5000 enter tab"},
		{name: "balances, the minimum", keys: "F tab m text:5000"},
		{name: "schedule, new", keys: "F n"},
		{name: "schedule, the amount", keys: "F n tab"},
		{name: "schedule, what it goes to", keys: "F n tab tab tab"},
		{name: "schedule, a new category", keys: "F n tab tab tab text:Expenses:Gifts"},
		{name: "schedule, a transfer", keys: "F n tab tab tab text:Sav tab"},
		{name: "schedule, a transfer between currencies", keys: "F n tab tab tab text:USD tab"},
		{name: "schedule, the period", keys: "F n tab tab tab tab tab tab"},
		{name: "schedule, the tolerance", keys: "F n tab tab tab text:Gro tab tab tab tab tab tab"},
		{name: "schedule, an error", keys: "F n ctrl+s"},
		{name: "schedule, edited", keys: "F e"},
		{name: "schedule, saved and undone", keys: "F e ctrl+s z"},
		{name: "schedule, paused", keys: "F p"},
		{name: "due", keys: "F enter"},
		{name: "due, the amount", keys: "F enter e"},
		{name: "due, an error", keys: "F enter e ctrl+u text:0 enter"},
		{name: "due, booked", keys: "F enter enter"},
		{name: "due, skipped", keys: "F enter s"},
		{name: "due, nothing", keys: "F enter enter z esc"},
		{name: "minimum", keys: "F m"},
		{name: "minimum, set", keys: "F m text:500"},
		{name: "minimum, a second account", keys: "F m down text:100"},
		{name: "minimum, the screen under it", keys: "F m text:-50 enter"},
		{name: "minimum, already under", keys: "F m text:500 enter"},
		{name: "minimum, an error", keys: "F m enter"},
		{name: "minimum, set and undone", keys: "F m text:500 enter z"},
		{name: "reports", keys: "R"},
		{name: "reports, a category", keys: "R down"},
		{name: "reports, an earlier month", keys: "R [ [ ["},
		{name: "reports, income against spending", keys: "R tab"},
		{name: "reports, income against spending, the oldest", keys: "R tab g"},
		{name: "reports, income against spending, an earlier month", keys: "R tab [ [ ["},
		{name: "reports, payees", keys: "R tab tab"},
		{name: "reports, payees, the last", keys: "R tab tab G"},
		{name: "reports, payees, an earlier month", keys: "R tab tab [ [ ["},
		{name: "reports, range", keys: "R r"},
		{name: "reports, range, typed", keys: "R r text:2026-09 enter text:2026-10"},
		{name: "reports, range, not a month", keys: "R r text:soon enter enter"},
		{name: "reports, range, backwards", keys: "R r text:2026-10 enter text:2026-09 enter"},
		{name: "reports, range, too long", keys: "R r text:2000-01 enter enter"},
		{name: "reports, a chosen range", keys: "R r text:2025-01 enter enter"},
		{name: "reports, a chosen range, the second month", keys: "R r text:2026-09 enter text:2026-10 enter"},
		{name: "reports, a long range", keys: "R r text:2020-01 enter enter"},
		{name: "reports, a long range, income against spending", keys: "R tab r text:2020-01 enter enter"},
		{name: "reports, a chosen range, payees", keys: "R tab tab r text:2025-01 enter enter"},
		{name: "reports, back to the categories", keys: "R tab tab tab"},
		{name: "search", keys: "tab tab /"},

		// What the bottom bar says after something was done.
		{name: "added", keys: "a text:5.50 tab tab tab ctrl+u text:2026-09-03 ctrl+s"},
		{name: "added and undone", keys: "a text:5.50 tab tab tab ctrl+u text:2026-10-03 ctrl+s z"},
		{name: "deleted", keys: "tab tab down d y"},
		{name: "deleted and undone", keys: "tab tab down d y z"},
		{name: "transferred", keys: "x text:10 enter ctrl+s"},
		{name: "transferred across currencies", keys: "x text:10 tab text:11 ctrl+s"},
		{name: "transferred and undone", keys: "x text:10 tab text:11 ctrl+s z"},
		{name: "imported", keys: "i enter enter"},
		{name: "imported and undone", keys: "i enter enter z"},
		{name: "account created", keys: "n text:Cash tab tab tab text:100 ctrl+s"},
		{name: "balance set", keys: "tab down b text:100 ctrl+s"},
		{name: "categorized", keys: "tab tab down down c down enter"},
		{name: "categorized with a rule", keys: "tab tab down down c down tab text:rewe enter"},
		{name: "backed up", keys: "S enter"},
		{name: "backed up, encrypted", keys: "S tab text:secret tab text:secret enter"},
		{name: "pruned", keys: "ctrl+p enter"},
		{name: "language", keys: "ctrl+l down enter"},
		{name: "target set", keys: "B e text:30 enter"},
		{name: "planned income set", keys: "B e down text:80 enter"},
		{name: "planned income cleared", keys: "B e down text:80 enter e down ctrl+u enter"},
		{name: "targets copied", keys: "B [ e text:30 enter ] c"},
		{name: "targets copied and undone", keys: "B [ e text:30 enter ] c z"},
		{name: "nothing to copy", keys: "B [ e text:30 enter ] e text:5 enter c"},
		{name: "carry-over on", keys: "B e tab enter"},
		{name: "carry-over on with a target", keys: "B e text:30 tab enter"},
		{name: "carry-over off", keys: "B e tab enter e tab enter"},
		{name: "carry-over undone", keys: "B e tab enter z"},
		{name: "budget with something carried in", keys: "B [ e text:30 tab enter ] e text:20 enter"},
		{name: "budget with a debt carried in", keys: "B [ e text:5 tab enter ] e text:20 enter"},
		{name: "no targets to copy", keys: "B c"},
		{name: "target cleared", keys: "B e text:30 enter e ctrl+u enter"},
		{name: "target set and undone", keys: "B e text:30 enter z"},
		{name: "nothing to undo", keys: "z"},

		// What the popups say when something is wrong.
		{name: "transfer, error", keys: "x ctrl+s"},
		{name: "new account, error", keys: "n ctrl+s"},
		{name: "backup, passphrases differ", keys: "S tab text:a tab text:b enter"},
		{name: "backup, repeat without passphrase", keys: "S tab tab text:b enter"},
		{name: "import, no file", keys: "i tab tab tab text:nothing"},
		{name: "import, no such file", keys: "i raw:/no/such/file.csv enter"},
		{name: "restore, encrypted needs a passphrase", keys: "ctrl+r down enter"},
		{name: "categorize, rule with no text", keys: "tab tab down down c tab text:x"},
		{name: "target, nothing to clear", keys: "B e enter"},
		{name: "target, not an amount", keys: "B e text:abc enter"},
		{name: "target, zero", keys: "B e text:0 enter"},
		{name: "target, all time", keys: "B g e"},
	}
	for _, l := range i18n.Languages {
		for _, sc := range screens {
			t.Run(l.Code+", "+sc.name, func(t *testing.T) {
				// given
				m := fitModel(t, l.Tag)

				// when
				play(m, sc.keys)
				view := m.View()

				// then
				if diff := cmp.Diff([]string(nil), cutIn(view)); diff != "" {
					t.Errorf("cut short or too big (-want +got):\n%s\n\nthe screen:\n%s", diff, view)
				}
			})
		}
	}
}
