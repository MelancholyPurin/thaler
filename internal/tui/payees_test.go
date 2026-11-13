package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_shareBasisPoints(t *testing.T) {
	type result struct {
		BasisPoints int64
		Known       bool
	}
	tcs := []struct {
		name        string
		part, whole int64
		want        result
	}{
		{name: "a quarter", part: 25, whole: 100, want: result{2500, true}},
		{name: "all of it", part: 100, whole: 100, want: result{10000, true}},
		{name: "none of it", part: 0, whole: 100, want: result{0, true}},
		{name: "a third rounds down", part: 1, whole: 3, want: result{3333, true}},
		{name: "two thirds round up", part: 2, whole: 3, want: result{6667, true}},
		{name: "a half goes up", part: 1, whole: 32, want: result{313, true}},
		{name: "just under a half goes down", part: 1, whole: 33, want: result{303, true}},
		{name: "a refund is a negative share", part: -25, whole: 100, want: result{-2500, true}},
		{name: "a negative half goes away from zero", part: -1, whole: 32, want: result{-313, true}},
		{name: "more than the whole, as when others were refunded", part: 150, whole: 100, want: result{15000, true}},
		{name: "amounts too big to multiply in 64 bits", part: 1_000_000_000_000_000, whole: 2_000_000_000_000_000, want: result{5000, true}},
		{name: "nothing to take a share of", part: 5, whole: 0, want: result{}},
		{name: "refunds overall: no share", part: 5, whole: -10, want: result{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			bp, known := shareBasisPoints(tc.part, tc.whole)

			// then
			if diff := cmp.Diff(tc.want, result{bp, known}); diff != "" {
				t.Errorf("shareBasisPoints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// lineWith is the first line of the view that has text in it.
func lineWith(t *testing.T, view, text string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	t.Fatalf("no line has %q:\n%s", text, view)
	return ""
}

// manyPayees books 1.00 to each of n payees, P000 and up, in October.
func manyPayees(n int) func(t *testing.T, st *core.Local) {
	return func(t *testing.T, st *core.Local) {
		for i := range n {
			book(t, st, "2026-10-05", fmt.Sprintf("P%03d", i), "Assets:Bank", "Expenses:Fun", -100)
		}
	}
}

func Test_payeesScreen(t *testing.T) {
	tcs := []struct {
		name  string
		setup func(t *testing.T, st *core.Local)
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "tab twice goes to the payees",
			keys:  "R tab tab",
			shows: []string{"Payees", "Nov 2025 – Oct 2026 · EUR", "Amount", "Share", "Count", "Total", "tab report", "↑/↓ scroll"},
			hides: []string{"Spending by category", "Income against spending", "Per month"},
		},
		{
			name:  "each payee with what it took, its share and its transactions, the biggest first",
			keys:  "R tab tab",
			shows: []string{"Edeka", "REWE", "Kino", "30.00", "12.00", "41.7%", "16.7%", "1 │"}, // a space before the border
			hides: []string{"to savings"},                                                       // a transfer is not spending
		},
		{
			name: "the order is the service's: the biggest first, equal amounts by name",
			keys: "R tab tab",
			// Edeka and REWE both took 30.00, then Kino
			shows: []string{"Edeka", "REWE", "Kino"},
		},
		{
			name:  "the total, and how many transactions it is",
			keys:  "R tab tab",
			shows: []string{"72.00", "4"},
		},
		{
			name: "a payee with no name is said",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-10-06", "", "Assets:Bank", "Expenses:Fun", -9000)
			},
			keys:  "R tab tab",
			shows: []string{"(no payee)", "90.00", "55.6%"},
		},
		{
			name: "a refund counts against its payee: negative, drawn with ▒",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-10-06", "Zoo", "Assets:Bank", "Expenses:Fun", 500)
			},
			keys:  "R tab tab",
			shows: []string{"Zoo", "-5.00", "▒"},
		},
		{
			name:  "the payees move with the period",
			keys:  "R tab tab [",
			shows: []string{"Oct 2025 – Sep 2026 · EUR", "Edeka", "30.00", "100.0%"},
			hides: []string{"Kino", "REWE"},
		},
		{
			name:  "nothing was spent",
			keys:  "R tab tab [ [ [ [ [ [ [ [ [ [ [ [ [",
			shows: []string{"Payees", "Nothing was spent in these months."},
			hides: []string{"Share", "Total"},
		},
		{
			name: "spending in a currency without a rate is said",
			setup: func(t *testing.T, st *core.Local) {
				openAccounts(t, st, "Assets:Dollars USD")
				if _, err := st.AddTransaction(t.Context(), core.NewTransaction{Date: startOn("2026-10-12"), Payee: "Diner", Postings: []ledger.Posting{
					{Account: "Assets:Dollars", Amount: ledger.Money{Minor: -900, Currency: "USD"}}, {Account: "Expenses:Fun", Amount: ledger.Money{Minor: 900, Currency: "USD"}}}}); err != nil {
					t.Fatal(err)
				}
			},
			keys:  "R tab tab",
			shows: []string{"no exchange rate USD → EUR", "Edeka"},
			hides: []string{"Diner"},
		},
		{
			name:  "the payees past the limit are one line, at the end",
			setup: manyPayees(99), // with the three of the ledger, 102 payees
			keys:  "R tab tab G",
			shows: []string{"2 other payees", "P096", "2.00", "Total", "103 │"}, // the transactions of all of them
			hides: []string{"P097", "P098", "Edeka"},
		},
		{
			name:  "a single payee left out is said in the singular",
			setup: manyPayees(98),
			keys:  "R tab tab G",
			shows: []string{"1 other payee"},
			hides: []string{"1 other payees"},
		},
		{
			name:  "the top of a long list",
			setup: manyPayees(99),
			keys:  "R tab tab",
			shows: []string{"Edeka", "12 of 101"},
			hides: []string{"2 other payees"},
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

func Test_payeesScreen_barsAgainstARefund(t *testing.T) {
	tcs := []struct {
		name string
		want string
	}{
		{name: "a refund bigger than any payee's spending sets the scale (30 of 40 of 28 cells)", want: strings.Repeat("█", 21)},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			book(t, st, "2026-10-06", "Zoo", "Assets:Bank", "Expenses:Fun", 4000)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, "R tab tab")
			line := lineWith(t, m.View(), "Edeka")

			// then
			if diff := cmp.Diff(true, strings.Contains(line, "  "+tc.want+" ")); diff != "" {
				t.Errorf("the line of Edeka lacks the bar %q (-want +got):\n%s\n%s", tc.want, diff, line)
			}
		})
	}
}

func Test_payeesScreen_bars(t *testing.T) {
	tcs := []struct {
		name  string
		payee string
		want  string
	}{
		{name: "the biggest is full (what is left of the line, 29 cells)", payee: "Edeka", want: strings.Repeat("█", 29)},
		{name: "another against the biggest, to the eighth of a cell", payee: "Kino", want: strings.Repeat("█", 11) + "▌"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			play(m, "R tab tab")
			line := lineWith(t, m.View(), tc.payee)

			// then: the bar, then the padding up to the figures
			if diff := cmp.Diff(true, strings.Contains(line, "  "+tc.want+" ")); diff != "" {
				t.Errorf("the line of %s lacks the bar %q (-want +got):\n%s\n%s", tc.payee, tc.want, diff, line)
			}
		})
	}
}

func Test_Update_payeeKeys(t *testing.T) {
	type observed struct {
		Report reportKind
		Top    int
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "the first line", keys: "R tab tab", want: observed{Report: reportPayees}},
		{name: "down", keys: "R tab tab down", want: observed{Report: reportPayees, Top: 1}},
		{name: "j and k", keys: "R tab tab j j k", want: observed{Report: reportPayees, Top: 1}},
		{name: "up stops at the top", keys: "R tab tab up", want: observed{Report: reportPayees}},
		{name: "a page", keys: "R tab tab pgdown", want: observed{Report: reportPayees, Top: 11}},
		{name: "ctrl+d and ctrl+u", keys: "R tab tab ctrl+d ctrl+d ctrl+u", want: observed{Report: reportPayees, Top: 11}},
		{name: "G goes to the end, held inside the list", keys: "R tab tab G", want: observed{Report: reportPayees, Top: 101 - 12}},
		{name: "g comes back", keys: "R tab tab G g", want: observed{Report: reportPayees}},
		{name: "keys of the categories do not scroll the payees", keys: "R down down tab tab", want: observed{Report: reportPayees}},
		{name: "the place is kept when the report is left", keys: "R tab tab down tab", want: observed{Top: 1}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			manyPayees(102)(t, st)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)
			m.View() // holds the scroll inside the list

			// then
			got := observed{Report: m.report, Top: m.payTop}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_payeesScreen_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		shows []string
		hides []string
	}{
		{name: "Spanish: figures and shares as the language writes them", tag: language.Spanish, shows: []string{"Beneficiarios", "Parte", "Núm.", "(sin beneficiario)", "30,00", "62,5 %", "37,5 %"}, hides: []string{"Payees", "(no payee)", "62.5%", "30.00"}},
		{name: "German", tag: language.German, shows: []string{"Empfänger", "Anteil", "Anzahl", "(ohne Empfänger)", "30,00", "62,5 %", "37,5 %"}, hides: []string{"Payees", "(no payee)", "62.5%", "30.00"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "Edeka", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-02", "", "Assets:Bank", "Expenses:Groceries", -1800)
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, "R tab tab")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_payeesScreen_width(t *testing.T) {
	tcs := []struct {
		name          string
		width, height int
		shows         []string
		hides         []string
	}{
		{name: "no room for the bar: it is left out", width: 36, height: 20, shows: []string{"Edeka", "30.00", "41.7%"}, hides: []string{"█"}},
		{name: "room for a bar", width: 60, height: 20, shows: []string{"Edeka", "41.7%", "█"}},
		{name: "a wide terminal does not stretch it", width: 200, height: 20, shows: []string{strings.Repeat("█", 30)}, hides: []string{strings.Repeat("█", 31)}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "Edeka", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-02", "REWE", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-03", "Kino", "Assets:Bank", "Expenses:Fun", -1200)
			m := modelOn(t, st, tc.width, tc.height)

			// when
			play(m, "R tab tab")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
