package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

var ctx = context.Background()

// equalEmpty treats nil and empty slices alike.
var equalEmpty = cmpopts.EquateEmpty()

func testConfig() (*config.Config, error) {
	return &config.Config{Rules: []importer.Rule{{Contains: "rewe", Category: "Expenses:Groceries"}}}, nil
}

// fixedNow is the clock of every test model: 22 October 2026.
func fixedNow() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }

func openLedger(t *testing.T) *core.Local {
	t.Helper()
	st, err := core.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func modelOn(t *testing.T, st *core.Local, width, height int) *Model {
	t.Helper()
	m, err := newWithClock(ctx, st, Options{LoadConfig: testConfig}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

func eur(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "EUR"} }

// book stores a transaction between an account and a category. A negative
// amount leaves the account.
func book(t *testing.T, st *core.Local, date, payee, account, category string, minor int64) int64 {
	t.Helper()
	d, err := ledger.ParseDate(date)
	if err != nil {
		t.Fatal(err)
	}
	res, err := st.AddTransaction(ctx, core.NewTransaction{
		Date: d, Payee: payee,
		Postings: []ledger.Posting{{Account: account, Amount: eur(minor)}, {Account: category, Amount: eur(-minor)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.ID
}

func openAccounts(t *testing.T, st *core.Local, specs ...string) {
	t.Helper()
	for _, spec := range specs {
		fields := strings.Fields(spec)
		cur := ledger.Currency("")
		if len(fields) > 1 {
			cur = ledger.Currency(fields[1])
		}
		if _, err := st.AddAccount(ctx, fields[0], cur); err != nil {
			t.Fatal(err)
		}
	}
}

// newModel builds a model on an in-memory store with a few transactions: two
// uncategorized spendings in October, categorized ones in September and
// October, and a transfer. The clock is fixed at 2026-10-22 and the terminal is
// 100x20.
func newModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	st := openLedger(t)
	openAccounts(t, st, "Assets:Bank EUR", "Assets:Savings EUR")
	book(t, st, "2026-09-15", "Edeka", "Assets:Bank", "Expenses:Groceries", -3000)
	book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
	book(t, st, "2026-10-02", "REWE", "Assets:Bank", ledger.UncategorizedExpense, -2000)
	book(t, st, "2026-10-03", "Kino", "Assets:Bank", ledger.UncategorizedExpense, -1200)
	book(t, st, "2026-10-04", "to savings", "Assets:Bank", "Assets:Savings", -500)
	return modelOn(t, st, 100, 20), st
}

// emptyModel is a model on a ledger that has one account and nothing else.
func emptyModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	st := openLedger(t)
	openAccounts(t, st, "Assets:Bank EUR")
	return modelOn(t, st, 100, 20), st
}

// noAccountsModel is a model on a ledger with nothing in it, on a 100x24 terminal.
func noAccountsModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	st := openLedger(t)
	return modelOn(t, st, 100, 24), st
}

// ---- driving the model ---------------------------------------------------------

func key(s string) tea.KeyMsg {
	named := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "down": tea.KeyDown, "up": tea.KeyUp, "left": tea.KeyLeft, "right": tea.KeyRight,
		"tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab, "backspace": tea.KeyBackspace, "ctrl+s": tea.KeyCtrlS, "ctrl+u": tea.KeyCtrlU, "ctrl+r": tea.KeyCtrlR,
		"f1": tea.KeyF1, "f2": tea.KeyF2, "f3": tea.KeyF3, "f4": tea.KeyF4,
	}
	if k, ok := named[s]; ok {
		return tea.KeyMsg{Type: k}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press sends keys one after the other. A key is a name like "enter" or "ctrl+s",
// or the text of a single character.
func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(key(k))
	}
}

func typeText(m *Model, text string) {
	for _, r := range text {
		press(m, string(r))
	}
}

// keys splits "3 u down" into the keys to press.
func keys(s string) []string { return strings.Fields(s) }

// play runs a script: keys separated by spaces, where text:abc types the letters
// a, b and c, and an underscore inside typed text is a space ("text:Muster_GmbH").
// raw:abc types exactly what follows, underscores included, for paths.
func play(m *Model, script string) {
	for _, tok := range strings.Fields(script) {
		switch {
		case strings.HasPrefix(tok, "text:"):
			typeText(m, strings.ReplaceAll(strings.TrimPrefix(tok, "text:"), "_", " "))
		case strings.HasPrefix(tok, "raw:"):
			typeText(m, strings.TrimPrefix(tok, "raw:"))
		default:
			press(m, tok)
		}
	}
}

// delta says what changed between two listings of the store: the lines that
// are new, and the lines that are gone.
func delta(before, after []string) (added, removed []string) {
	count := map[string]int{}
	for _, l := range before {
		count[l]++
	}
	for _, l := range after {
		if count[l] > 0 {
			count[l]--
			continue
		}
		added = append(added, l)
	}
	count = map[string]int{}
	for _, l := range after {
		count[l]++
	}
	for _, l := range before {
		if count[l] > 0 {
			count[l]--
			continue
		}
		removed = append(removed, l)
	}
	return added, removed
}

// ---- observing it -------------------------------------------------------------

var paneNames = map[pane]string{paneHome: "home", paneAccounts: "accounts", paneTransactions: "transactions"}
var screenNames2 = map[screen]string{screenLedger: "ledger", screenBudget: "budget", screenForecast: "forecast", screenReports: "reports"}
var modeNames = map[mode]string{
	modeList: "list", modeFilter: "filter", modePick: "pick", modeHelp: "help", modeForm: "form", modeConfirm: "confirm",
	modeImport: "import", modeAccount: "account", modeTransfer: "transfer", modeBackup: "backup", modeRestore: "restore", modePrune: "prune",
}

// visibleRows are the transactions the list shows, newest first.
func visibleRows(m *Model) []string {
	var out []string
	for _, i := range m.view {
		r := m.rows[i]
		out = append(out, r.tx.Date+" "+r.tx.Payee+" "+m.listAmount(r))
	}
	return out
}

// storedLines lists what is stored, oldest first: "date payee | account amount | ...".
func storedLines(t *testing.T, st core.Service) []string {
	t.Helper()
	txs, err := st.ListTransactions(ctx, core.TxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tx := range txs {
		line := tx.Date + " " + tx.Payee
		for _, p := range tx.Postings {
			line += " | " + p.Account + " " + p.Amount.Decimal()
		}
		out = append(out, line)
	}
	return out
}

// missingFrom returns the texts the view does not contain, so that a test says
// which are absent.
func missingFrom(view string, wants []string) []string {
	var out []string
	for _, w := range wants {
		if !strings.Contains(view, w) {
			out = append(out, w)
		}
	}
	return out
}

// presentIn returns the texts the view contains, for things that must not be there.
func presentIn(view string, unwanted []string) []string {
	var out []string
	for _, w := range unwanted {
		if strings.Contains(view, w) {
			out = append(out, w)
		}
	}
	return out
}

// checkView compares what the view must and must not show.
func checkView(t *testing.T, view string, shows, hides []string) {
	t.Helper()
	if diff := cmp.Diff([]string(nil), missingFrom(view, shows)); diff != "" {
		t.Errorf("the view lacks these (-want +got):\n%s\nview:\n%s", diff, view)
	}
	if diff := cmp.Diff([]string(nil), presentIn(view, hides)); diff != "" {
		t.Errorf("the view shows what it should not (-want +got):\n%s\nview:\n%s", diff, view)
	}
}

// txByPayee finds the newest transaction of a payee.
func txByPayee(t *testing.T, st core.Service, payee string) core.TxView {
	t.Helper()
	txs, err := st.ListTransactions(ctx, core.TxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(txs) - 1; i >= 0; i-- {
		if txs[i].Payee == payee {
			return txs[i]
		}
	}
	t.Fatalf("no transaction from %q", payee)
	return core.TxView{}
}

// categoryOf is the category a payee's newest transaction is booked to.
func categoryOf(t *testing.T, st core.Service, payee string) string {
	t.Helper()
	_, cat, ok := txByPayee(t, st, payee).Legs()
	if !ok {
		t.Fatalf("%q is not a simple transaction", payee)
	}
	return cat.Account
}

// selectPayee moves the transaction cursor onto the transaction of payee.
func selectPayee(t *testing.T, m *Model, payee string) {
	t.Helper()
	press(m, "3")
	for i := range m.view {
		if m.rows[m.view[i]].tx.Payee == payee {
			m.txs.cursor = i
			return
		}
	}
	t.Fatalf("%q is not listed", payee)
}

// checkConsistent fails if the ledger's own check finds anything wrong.
func checkConsistent(t *testing.T, st core.Service) {
	t.Helper()
	problems, err := st.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string(nil), problems, equalEmpty); diff != "" {
		t.Errorf("Check found problems (-want +got):\n%s", diff)
	}
}

func txCount(t *testing.T, st core.Service) int {
	t.Helper()
	txs, err := st.ListTransactions(ctx, core.TxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return len(txs)
}

// ---- the helpers themselves ----------------------------------------------------

func Test_fuzzyScore(t *testing.T) {
	tcs := []struct {
		name          string
		pattern, text string
		want          bool
	}{
		{name: "scattered letters", pattern: "grc", text: "Expenses:Groceries", want: true},
		{name: "no match", pattern: "xyz", text: "Expenses:Groceries", want: false},
		{name: "case does not matter", pattern: "GRO", text: "expenses:groceries", want: true},
		{name: "an empty pattern matches anything", pattern: "", text: "Expenses:Groceries", want: true},
		{name: "letters in the wrong order", pattern: "cgr", text: "Expenses:Groceries", want: false},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.pattern and tc.text

			// when
			_, got := fuzzyScore(tc.pattern, tc.text)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("match mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_fuzzyScore_ranking(t *testing.T) {
	tcs := []struct {
		name          string
		pattern       string
		better, worse string
	}{
		{name: "a run at a word start outranks scattered letters", pattern: "gro", better: "Expenses:Groceries", worse: "Expenses:Gift-Rooms-Misc"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.pattern

			// when
			better, _ := fuzzyScore(tc.pattern, tc.better)
			worse, _ := fuzzyScore(tc.pattern, tc.worse)

			// then
			if diff := cmp.Diff(true, better > worse); diff != "" {
				t.Errorf("%q should score above %q: %d vs %d (-want +got):\n%s", tc.better, tc.worse, better, worse, diff)
			}
		})
	}
}

func Test_matchAll(t *testing.T) {
	tcs := []struct {
		name        string
		query, text string
		want        bool
	}{
		{name: "every term is there", query: "rewe 23", text: "2026-10-01 REWE Markt -23,47", want: true},
		{name: "one term is missing", query: "rewe lidl", text: "REWE Markt", want: false},
		{name: "an empty query", query: "", text: "anything", want: true},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.query and tc.text

			// when
			got := matchAll(tc.query, tc.text)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_View_home(t *testing.T) {
	tcs := []struct {
		name      string
		wantFocus string
		shows     []string
	}{
		{
			name:      "the home tile is where it starts",
			wantFocus: "home",
			shows:     []string{bannerCompact[0], "Net worth", "-72.00 EUR", "Uncategorized", "[1] Home", "[2] Accounts", "[3] Transactions"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			view := m.View()

			// then
			if diff := cmp.Diff(tc.wantFocus, paneNames[m.focus]); diff != "" {
				t.Errorf("focus mismatch (-want +got):\n%s", diff)
			}
			checkView(t, view, tc.shows, nil)
		})
	}
}

func Test_Update_focusKeys(t *testing.T) {
	tcs := []struct {
		name string
		keys string
		want string
	}{
		{name: "1 is home", keys: "3 1", want: "home"},
		{name: "2 is accounts", keys: "2", want: "accounts"},
		{name: "3 is transactions", keys: "3", want: "transactions"},
		{name: "tab from the last panel wraps to home", keys: "3 tab", want: "home"},
		{name: "shift+tab goes back", keys: "3 tab shift+tab", want: "transactions"},
		{name: "enter on accounts opens the transactions", keys: "2 enter", want: "transactions"},
		{name: "enter on home opens the transactions", keys: "enter", want: "transactions"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			press(m, keys(tc.keys)...)

			// then
			if diff := cmp.Diff(tc.want, paneNames[m.focus]); diff != "" {
				t.Errorf("focus mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_accountScope(t *testing.T) {
	type observed struct {
		Scope string
		Rows  []string
	}
	all := []string{"2026-10-04 to savings -5.00", "2026-10-03 Kino -12.00", "2026-10-02 REWE -20.00", "2026-10-01 REWE -10.00"}
	tcs := []struct {
		name  string
		keys  string
		want  observed
		shows []string
	}{
		{name: "all accounts to begin with", want: observed{"", all}},
		{name: "the bank touches everything", keys: "2 down", want: observed{"Assets:Bank", all}},
		{
			name: "savings only has the transfer", keys: "2 down down",
			want:  observed{"Assets:Savings", []string{"2026-10-04 to savings -5.00"}},
			shows: []string{"Savings", "Balance"}, // the detail pane describes the account
		},
		{name: "g goes back to all accounts", keys: "2 down down g", want: observed{"", all}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			press(m, keys(tc.keys)...)

			// then
			if diff := cmp.Diff(tc.want, observed{m.scopeAccount(), visibleRows(m)}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

func Test_Update_uncategorizedToggle(t *testing.T) {
	type observed struct {
		Rows     []string
		Selected string
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{
			name: "u shows only what needs a category, the newest selected", keys: "3 u",
			want: observed{[]string{"2026-10-03 Kino -12.00", "2026-10-02 REWE -20.00"}, "Kino"},
		},
		{
			name: "u again shows everything", keys: "3 u u",
			want: observed{[]string{"2026-10-04 to savings -5.00", "2026-10-03 Kino -12.00", "2026-10-02 REWE -20.00", "2026-10-01 REWE -10.00"}, "Kino"}, // the cursor stays on what it was on
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			press(m, keys(tc.keys)...)

			// then
			selected := ""
			if r, ok := m.selected(); ok {
				selected = r.tx.Payee
			}
			if diff := cmp.Diff(tc.want, observed{visibleRows(m), selected}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_search(t *testing.T) {
	type observed struct {
		Mode  string
		Focus string
		Rows  int
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "/ opens the search on the transactions", keys: "/", want: observed{"filter", "transactions", 4}},
		{name: "typing filters as it goes", keys: "/ r e w", want: observed{"filter", "transactions", 2}},
		{name: "enter keeps the filter and leaves the box", keys: "/ r e w enter", want: observed{"list", "transactions", 2}},
		{name: "esc clears it", keys: "/ r e w enter esc", want: observed{"list", "transactions", 4}},
		{name: "esc in the box clears and leaves it", keys: "/ r e w esc", want: observed{"list", "transactions", 4}},
		{name: "every word has to match", keys: "/ r e w e space k i n o", want: observed{"filter", "transactions", 0}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			for _, k := range keys(tc.keys) {
				if k == "space" {
					m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
					continue
				}
				press(m, k)
			}

			// then
			if diff := cmp.Diff(tc.want, observed{modeNames[m.mode], paneNames[m.focus], len(m.view)}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_screenKeys(t *testing.T) {
	tcs := []struct {
		name string
		keys string
		want string
	}{
		{name: "B is the budget", keys: "B", want: "budget"},
		{name: "F is the forecast", keys: "F", want: "forecast"},
		{name: "R is the reports", keys: "R", want: "reports"},
		{name: "L is the ledger", keys: "B L", want: "ledger"},
		{name: "F1 is the ledger", keys: "R f1", want: "ledger"},
		{name: "F2 is the budget", keys: "f2", want: "budget"},
		{name: "F3 is the forecast", keys: "f3", want: "forecast"},
		{name: "F4 is the reports", keys: "f4", want: "reports"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			press(m, keys(tc.keys)...)

			// then
			if diff := cmp.Diff(tc.want, screenNames2[m.screen]); diff != "" {
				t.Errorf("screen mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("screens that are not built say so", func(t *testing.T) {
		// given
		m, _ := newModel(t)

		// when
		press(m, "F")

		// then
		checkView(t, m.View(), []string{"Not built yet", "Forecast"}, nil)
	})
}

func Test_Update_periodKeys(t *testing.T) {
	type observed struct {
		Period string
		Rows   int
	}
	tcs := []struct {
		name  string
		keys  string
		want  observed
		shows []string
	}{
		{name: "the current month to begin with", want: observed{"2026-10", 4}},
		{name: "[ is the month before", keys: "[", want: observed{"2026-09", 1}, shows: []string{"Sep 2026"}},
		{name: "] from september twice is november, which is empty", keys: "[ ] ]", want: observed{"2026-11", 0}, shows: []string{"No transactions in Nov 2026"}},
		{name: "T is all time", keys: "T", want: observed{"", 5}},
		{name: "[ from all time lands on the current month", keys: "T [", want: observed{"2026-10", 4}},
		{name: "t returns to the current month", keys: "[ [ t", want: observed{"2026-10", 4}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			press(m, keys(tc.keys)...)

			// then
			if diff := cmp.Diff(tc.want, observed{m.period, len(m.view)}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

func Test_budgetScreen(t *testing.T) {
	type observed struct {
		Period        string
		Spent         int64
		ExpenseLines  int
		RowsInLedger  int // after going back to the ledger
		LedgerPeriod  string
		ShownInBudget bool
	}
	tcs := []struct {
		name  string
		keys  string
		want  observed
		shows []string
	}{
		{
			name: "october", keys: "B",
			want:  observed{Period: "2026-10", Spent: 4200, ExpenseLines: 2, RowsInLedger: 4, LedgerPeriod: "2026-10"},
			shows: []string{"[1] Months", "Oct 2026", "Spent", "42.00 EUR", "Groceries", "Uncategorized"},
		},
		{name: "down is the month before", keys: "B down", want: observed{Period: "2026-09", Spent: 3000, ExpenseLines: 1, RowsInLedger: 1, LedgerPeriod: "2026-09"}},
		{name: "g is all time", keys: "B g", want: observed{Period: "", Spent: 7200, ExpenseLines: 2, RowsInLedger: 5, LedgerPeriod: ""}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			press(m, keys(tc.keys)...)
			view := m.View()
			got := observed{Period: m.period, Spent: m.budget.ExpenseTotal.Minor, ExpenseLines: len(m.budget.Expenses)}
			press(m, "L") // the period is shared with the ledger
			got.RowsInLedger, got.LedgerPeriod = len(m.view), m.period

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, view, tc.shows, nil)
		})
	}
}

// Whatever is on screen, in any terminal size, no line is wider than the
// terminal and there are no more lines than it is tall.
func Test_View_fitsTerminal(t *testing.T) {
	sizes := [][2]int{{120, 36}, {100, 20}, {80, 14}, {60, 24}, {40, 8}, {200, 60}}
	tcs := []struct {
		name string
		keys string
	}{
		{name: "the ledger", keys: "L"},
		{name: "the budget", keys: "B"},
		{name: "the forecast", keys: "F"},
		{name: "the reports", keys: "R"},
		{name: "the home tile focused", keys: "1"},
		{name: "the accounts focused", keys: "2"},
		{name: "the transactions focused", keys: "3"},
		{name: "the category picker", keys: "3 c"},
		{name: "the help", keys: "?"},
		{name: "the add form", keys: "a"},
		{name: "the delete confirmation", keys: "3 d"},
		{name: "the search", keys: "/ r"},
		{name: "the import popup", keys: "i"},
		{name: "the new account popup", keys: "n"},
		{name: "the balance popup", keys: "2 down b"},
		{name: "the transfer popup", keys: "x"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range sizes {
				// given
				m, _ := newModel(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

				// when
				press(m, keys(tc.keys)...)
				lines := strings.Split(m.View(), "\n")

				// then
				var problems []string
				if len(lines) > size[1] {
					problems = append(problems, fmt.Sprintf("%dx%d: %d lines", size[0], size[1], len(lines)))
				}
				for _, line := range lines {
					if w := lipgloss.Width(line); w > size[0] {
						problems = append(problems, fmt.Sprintf("%dx%d: a line %d cells wide: %q", size[0], size[1], w, line))
					}
				}
				if diff := cmp.Diff([]string(nil), problems); diff != "" {
					t.Errorf("the view does not fit (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// Flow is what a sequence of keys did: where it left the interface, what it
// said, and what it changed in the store.
type Flow struct {
	Mode          string
	Err           string // the message in the popup that is open, if any
	Status        string
	StatusIsError bool
	Added         []string
	Removed       []string
}

// observe describes the interface and the store after a flow; before is the
// store's listing from before it.
func observe(t *testing.T, m *Model, st core.Service, before []string) Flow {
	t.Helper()
	f := Flow{Mode: modeNames[m.mode], Status: m.status, StatusIsError: m.statusIsError}
	switch {
	case m.form != nil:
		f.Err = m.form.err
	case m.acct != nil:
		f.Err = m.acct.err
	case m.imp != nil:
		f.Err = m.imp.err
	case m.xfer != nil:
		f.Err = m.xfer.err
	case m.bkp != nil:
		f.Err = m.bkp.err
	}
	f.Added, f.Removed = delta(before, storedLines(t, st))
	return f
}
