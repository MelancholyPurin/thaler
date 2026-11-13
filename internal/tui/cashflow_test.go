package tui

import (
	"golang.org/x/text/language"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_gauge(t *testing.T) {
	tcs := []struct {
		name          string
		spent, income int64
		width         int
		want          string
	}{
		{name: "a quarter spent", spent: 25, income: 100, width: 8, want: "██░░░░░░"},
		{name: "half", spent: 50, income: 100, width: 8, want: "████░░░░"},
		{name: "to the eighth of a cell", spent: 55, income: 100, width: 8, want: "████▍░░░"},
		{name: "a little is still seen", spent: 1, income: 100000, width: 8, want: "▏░░░░░░░"},
		{name: "almost all is not all", spent: 99999, income: 100000, width: 8, want: "███████▉"},
		{name: "exactly what came in is full, without a mark", spent: 100, income: 100, width: 8, want: "████████"},
		{name: "more than came in is full and marked", spent: 150, income: 100, width: 8, want: "███████!"},
		{name: "spending with no income is marked", spent: 5, income: 0, width: 8, want: "███████!"},
		{name: "nothing spent is all kept", spent: 0, income: 100, width: 8, want: "░░░░░░░░"},
		{name: "a refund is all kept", spent: -20, income: 100, width: 8, want: "░░░░░░░░"},
		{name: "income below zero, after refunds: nothing spent is empty", spent: 0, income: -50, width: 8, want: ""},
		{name: "income below zero, spending: marked", spent: 5, income: -50, width: 8, want: "███████!"},
		{name: "nothing at all is empty", spent: 0, income: 0, width: 8, want: ""},
		{name: "no room", spent: 5, income: 10, width: 0, want: ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := gauge(tc.spent, tc.income, tc.width)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("gauge mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_percent(t *testing.T) {
	tcs := []struct {
		name string
		rate core.SavingsRate
		want string
	}{
		{name: "none", rate: core.SavingsRate{}, want: "–"},
		{name: "a quarter", rate: core.SavingsRate{BasisPoints: 2500, Known: true}, want: "25.0%"},
		{name: "to a tenth", rate: core.SavingsRate{BasisPoints: 6138, Known: true}, want: "61.4%"},
		{name: "a half of a tenth goes up", rate: core.SavingsRate{BasisPoints: 6135, Known: true}, want: "61.4%"},
		{name: "just under goes down", rate: core.SavingsRate{BasisPoints: 6134, Known: true}, want: "61.3%"},
		{name: "none kept", rate: core.SavingsRate{BasisPoints: 0, Known: true}, want: "0.0%"},
		{name: "all kept", rate: core.SavingsRate{BasisPoints: 10000, Known: true}, want: "100.0%"},
		{name: "overspent", rate: core.SavingsRate{BasisPoints: -4000, Known: true}, want: "-40.0%"},
		{name: "a negative half goes away from zero", rate: core.SavingsRate{BasisPoints: -6135, Known: true}, want: "-61.4%"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			got := m.percent(tc.rate)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("percent mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// cashFlowLedger is newModel's ledger with a salary: 100.00 in September and 30.00 in
// October, against the 30.00 and 42.00 spent in those months.
func cashFlowLedger(t *testing.T, st *core.Local) {
	t.Helper()
	book(t, st, "2026-09-01", "ACME", "Assets:Bank", "Income:Salary", 10000)
	book(t, st, "2026-10-01", "ACME", "Assets:Bank", "Income:Salary", 3000)
}

func Test_cashFlowScreen(t *testing.T) {
	tcs := []struct {
		name  string
		setup func(t *testing.T, st *core.Local)
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "tab goes to the income report",
			setup: cashFlowLedger,
			keys:  "R tab",
			shows: []string{"Income against spending", "Nov 2025 – Oct 2026 · EUR", "Income", "Spent", "Kept", "Saved", "Total", "tab report", "↑/↓ scroll"},
			hides: []string{"Spending by category", "Per month"},
		},
		{
			name:  "a month with income: what came in and went out, what was kept, the share",
			setup: cashFlowLedger,
			keys:  "R tab",
			shows: []string{"Sep 2026", "100.00", "30.00", "+70.00", "70.0%"},
			hides: []string{"+0.00"},
		},
		{
			name:  "a month that spent more than it earned: negative, marked",
			setup: cashFlowLedger,
			keys:  "R tab",
			shows: []string{"Oct 2026", "-12.00", "-40.0%", "!"},
		},
		{
			name:  "the total",
			setup: cashFlowLedger,
			keys:  "R tab",
			shows: []string{"130.00", "72.00", "+58.00", "44.6%"},
		},
		{
			name:  "the bar says what part of the income was spent",
			setup: cashFlowLedger,
			keys:  "R tab",
			shows: []string{"░", "█", "Bar: the part of the income that was spent"},
		},
		{
			name:  "tab three times goes back to the categories",
			setup: cashFlowLedger,
			keys:  "R tab tab tab",
			shows: []string{"Spending by category", "All spending"},
			hides: []string{"Income against spending", "Payees"},
		},
		{
			name:  "the months move with the period",
			setup: cashFlowLedger,
			keys:  "R tab [",
			shows: []string{"Oct 2025 – Sep 2026 · EUR", "+70.00", "70.0%"},
			hides: []string{"Oct 2026", "-12.00"},
		},
		{
			name:  "no income at all: no share, and the spending is marked",
			keys:  "R tab",
			shows: []string{"Oct 2026", "42.00", "–", "!"},
			hides: []string{"%"},
		},
		{
			name: "spending in a currency without a rate is said",
			setup: func(t *testing.T, st *core.Local) {
				openAccounts(t, st, "Assets:Dollars USD")
				if _, err := st.AddTransaction(t.Context(), core.NewTransaction{Date: startOn("2026-10-12"), Payee: "x", Postings: []ledger.Posting{
					{Account: "Assets:Dollars", Amount: ledger.Money{Minor: 900, Currency: "USD"}}, {Account: "Income:Gift", Amount: ledger.Money{Minor: -900, Currency: "USD"}}}}); err != nil {
					t.Fatal(err)
				}
			},
			keys:  "R tab",
			shows: []string{"no exchange rate USD → EUR"},
		},
		{
			name:  "the oldest months are at the top, g goes there",
			setup: cashFlowLedger,
			keys:  "R tab g",
			shows: []string{"Nov 2025", "1 of 12"},
			hides: []string{"-12.00"},
		},
		{
			name:  "and the newest again through G",
			setup: cashFlowLedger,
			keys:  "R tab g G",
			shows: []string{"Oct 2026", "12 of 12"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			if tc.setup != nil {
				tc.setup(t, st)
			}
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_Update_cashFlowKeys(t *testing.T) {
	type observed struct {
		Report reportKind
		Back   int
		Cursor int // the category selected in the other report
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "the categories first", keys: "R", want: observed{}},
		{name: "tab", keys: "R tab", want: observed{Report: reportCashFlow}},
		{name: "tab twice is the payees", keys: "R tab tab", want: observed{Report: reportPayees}},
		{name: "tab three times", keys: "R tab tab tab", want: observed{}},
		{name: "up goes back a month", keys: "R tab up", want: observed{Report: reportCashFlow, Back: 1}},
		{name: "k and j", keys: "R tab k k j", want: observed{Report: reportCashFlow, Back: 1}},
		{name: "down stops at the newest", keys: "R tab down", want: observed{Report: reportCashFlow}},
		{name: "g goes to the oldest, held inside the months", keys: "R tab g", want: observed{Report: reportCashFlow, Back: 1}},
		{name: "G comes back", keys: "R tab g G", want: observed{Report: reportCashFlow}},
		{name: "the selection of the categories is kept", keys: "R down tab tab tab", want: observed{Cursor: 1}},
		{name: "keys of the categories do not move the months", keys: "R down down tab", want: observed{Report: reportCashFlow, Cursor: 2}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			cashFlowLedger(t, st)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)
			m.View() // holds the scroll inside the months

			// then
			got := observed{Report: m.report, Back: m.cfBack, Cursor: m.repList.cursor}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_cashFlowScreen_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		shows []string
		hides []string
	}{
		{name: "Spanish: figures and shares as the language writes them", tag: language.Spanish, shows: []string{"sep 2026", "100,00", "+70,00", "70,0 %", "-40,0 %"}, hides: []string{"70.0%", "+70.00"}},
		{name: "German", tag: language.German, shows: []string{"Sep 2026", "100,00", "+70,00", "70,0 %", "-40,0 %"}, hides: []string{"70.0%", "+70.00"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-09-01", "ACME", "Assets:Bank", "Income:Salary", 10000)
			book(t, st, "2026-09-10", "x", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-01", "ACME", "Assets:Bank", "Income:Salary", 3000)
			book(t, st, "2026-10-10", "x", "Assets:Bank", "Expenses:Groceries", -4200)
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, "R tab")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_cashFlowScreen_width(t *testing.T) {
	tcs := []struct {
		name          string
		width, height int
		shows         []string
		hides         []string
	}{
		{name: "no room for the bar: it is left out", width: 49, height: 20, shows: []string{"Sep 2026", "+70.00", "70.0%", "-12.00"}, hides: []string{"░", "█", "Bar:"}},
		{name: "room for a bar", width: 60, height: 20, shows: []string{"Sep 2026", "70.0%", "░", "█"}},
		{name: "a wide terminal does not stretch it", width: 200, height: 20, shows: []string{strings.Repeat("█", 39) + "!"}, hides: []string{strings.Repeat("█", 40)}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			cashFlowLedger(t, st)
			book(t, st, "2026-09-10", "x", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-10", "x", "Assets:Bank", "Expenses:Groceries", -4200)
			m := modelOn(t, st, tc.width, tc.height)

			// when
			play(m, "R tab")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
