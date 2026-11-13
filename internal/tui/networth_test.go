package tui

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// newModel's ledger is worth -30.00 at the end of September and -72.00 at the end of
// October 2026 (the 5.00 to savings stays in the net worth); with cashFlowLedger's
// salary, 70.00 and 58.00.

func Test_netWorthScreen(t *testing.T) {
	tcs := []struct {
		name  string
		setup func(t *testing.T, st *core.Local)
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "the third tab is the net worth, month by month",
			keys:  "R tab tab tab",
			shows: []string{"Net worth", "Nov 2025 – Oct 2026 · EUR", "Change", "Nov 2025", "Sep 2026", "-30.00", "Oct 2026", "-72.00", "tab report", "↑/↓ scroll"},
			hides: []string{"Spending by category", "Payees", "Income against spending"},
		},
		{
			name:  "the change from the month before; the first month has none in view",
			keys:  "R tab tab tab",
			shows: []string{"-42.00"},
			hides: []string{"+0.00"},
		},
		{
			name:  "a month that rose has a plus, one that fell a minus",
			setup: cashFlowLedger,
			keys:  "R tab tab tab",
			shows: []string{"70.00", "+70.00", "58.00", "-12.00"},
		},
		{
			name:  "the bar is against the highest month, below zero is ▒",
			keys:  "R tab tab tab",
			shows: []string{"▒", "Bar: net worth against the highest month; ▒ is below zero."},
			hides: []string{"█"},
		},
		{
			name:  "a month above zero has a bar",
			setup: cashFlowLedger,
			keys:  "R tab tab tab",
			shows: []string{"█"},
		},
		{
			name:  "the months move with the period",
			keys:  "R tab tab tab [",
			shows: []string{"Oct 2025 – Sep 2026 · EUR", "-30.00"},
			hides: []string{"Oct 2026", "-72.00"},
		},
		{
			name:  "a chosen range is marked, and covers its months",
			keys:  "R tab tab tab r text:2026-09 enter text:2026-10 enter",
			shows: []string{"Sep 2026 – Oct 2026 · EUR · chosen range", "-30.00", "-72.00"},
			hides: []string{"Nov 2025", "Aug 2026"},
		},
		{
			name:  "the oldest months are at the top, g goes there",
			keys:  "R tab tab tab r text:2024-01 enter text:2026-10 enter g",
			shows: []string{"Jan 2024", "Jan 2025", "13 of 34"},
			hides: []string{"Sep 2026", "-72.00"},
		},
		{
			name:  "and the newest again through G",
			keys:  "R tab tab tab r text:2024-01 enter text:2026-10 enter g G",
			shows: []string{"Oct 2026", "-72.00", "34 of 34"},
			hides: []string{"Feb 2024"},
		},
		{
			name: "an account in a currency without a rate is said",
			setup: func(t *testing.T, st *core.Local) {
				openAccounts(t, st, "Assets:Dollars USD")
				if _, err := st.AddTransaction(t.Context(), core.NewTransaction{Date: startOn("2026-10-12"), Payee: "x", Postings: []ledger.Posting{
					{Account: "Assets:Dollars", Amount: ledger.Money{Minor: 900, Currency: "USD"}}, {Account: "Income:Gift", Amount: ledger.Money{Minor: -900, Currency: "USD"}}}}); err != nil {
					t.Fatal(err)
				}
			},
			keys:  "R tab tab tab",
			shows: []string{"no exchange rate USD → EUR", "-72.00"},
		},
		{
			name:  "a rate entered later does not move a month that is past",
			setup: dollarsBeforeRate,
			keys:  "R tab tab tab",
			shows: []string{"Sep 2026", "-21.00", "Oct 2026", "-52.00"},
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

// dollarsBeforeRate holds 10.00 USD from September, at 0.90 EUR a dollar from 2026-09-15
// and 2.00 from 2026-10-20: 9.00 at the end of September and 20.00 at the end of
// October, whatever rate is entered afterwards. It is added to newModel's ledger, whose
// euros are -30.00 and -72.00 at those dates, so the months come to -21.00 and -52.00.
func dollarsBeforeRate(t *testing.T, st *core.Local) {
	t.Helper()
	openAccounts(t, st, "Assets:Dollars USD")
	if _, err := st.AddTransaction(t.Context(), core.NewTransaction{Date: startOn("2026-09-01"), Payee: "x", Postings: []ledger.Posting{
		{Account: "Assets:Dollars", Amount: ledger.Money{Minor: 1000, Currency: "USD"}}, {Account: "Income:Gift", Amount: ledger.Money{Minor: -1000, Currency: "USD"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ date, rate string }{{"2026-09-15", "0.90"}, {"2026-10-20", "2"}} {
		if err := st.SetRate(t.Context(), r.date, "USD", "EUR", r.rate); err != nil {
			t.Fatal(err)
		}
	}
}

func Test_Update_netWorthKeys(t *testing.T) {
	type observed struct {
		Report reportKind
		Back   int
	}
	const longRange = " r text:2024-01 enter text:2026-10 enter" // 34 months, more than fit
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "tab three times", keys: "R tab tab tab", want: observed{Report: reportNetWorth}},
		{name: "up goes back a month", keys: "R tab tab tab" + longRange + " up", want: observed{Report: reportNetWorth, Back: 1}},
		{name: "k and j", keys: "R tab tab tab" + longRange + " k k j", want: observed{Report: reportNetWorth, Back: 1}},
		{name: "down stops at the newest", keys: "R tab tab tab" + longRange + " down", want: observed{Report: reportNetWorth}},
		{name: "a page is the lines shown less one", keys: "R tab tab tab" + longRange + " pgup", want: observed{Report: reportNetWorth, Back: 12}},
		{name: "ctrl+u and ctrl+d", keys: "R tab tab tab" + longRange + " ctrl+u ctrl+u ctrl+d", want: observed{Report: reportNetWorth, Back: 12}},
		{name: "g goes to the oldest, held inside the months", keys: "R tab tab tab" + longRange + " g", want: observed{Report: reportNetWorth, Back: 34 - 13}},
		{name: "G comes back", keys: "R tab tab tab" + longRange + " g G", want: observed{Report: reportNetWorth}},
		{name: "with all the months in view there is nothing to scroll", keys: "R tab tab tab up up", want: observed{Report: reportNetWorth}},
		{name: "a new range starts at the newest again", keys: "R tab tab tab" + longRange + " up up" + " r text:2023-01 enter enter", want: observed{Report: reportNetWorth}},
		{name: "the other reports do not move it", keys: "R tab tab" + " down down", want: observed{Report: reportPayees}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			play(m, tc.keys)
			m.View() // holds the scroll inside the months

			// then
			got := observed{Report: m.report, Back: m.nwBack}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_netWorthScreen_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		shows []string
		hides []string
	}{
		{name: "Spanish: figures as the language writes them", tag: language.Spanish, shows: []string{"Patrimonio neto", "Cambio", "sep 2026", "70,00", "+70,00", "-12,00", "Barra: patrimonio neto"}, hides: []string{"70.00", "Net worth"}},
		{name: "German", tag: language.German, shows: []string{"Nettovermögen", "Änderung", "Sep 2026", "70,00", "+70,00", "-12,00", "Balken: Nettovermögen"}, hides: []string{"70.00", "Net worth"}},
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
			play(m, "R tab tab tab")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_netWorthScreen_width(t *testing.T) {
	tcs := []struct {
		name          string
		width, height int
		shows         []string
		hides         []string
	}{
		{name: "no room for the bar: it is left out", width: 36, height: 20, shows: []string{"Sep 2026", "70.00", "+70.00"}, hides: []string{"█", "Bar:"}},
		{name: "room for a bar", width: 60, height: 20, shows: []string{"Sep 2026", "█", "Bar:"}},
		{name: "one month too many for the screen: the position is said", width: 100, height: 18, shows: []string{"│ Dec 2025  ", "12 of 12"}, hides: []string{"│ Nov 2025  "}},
		{name: "all the months fit: no position", width: 100, height: 19, shows: []string{"│ Nov 2025  ", "│ Dec 2025  "}, hides: []string{" of 12"}},
		{name: "a wide terminal does not stretch it", width: 200, height: 20, shows: []string{strings.Repeat("█", 40)}, hides: []string{strings.Repeat("█", 41)}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-09-01", "ACME", "Assets:Bank", "Income:Salary", 10000)
			book(t, st, "2026-09-10", "x", "Assets:Bank", "Expenses:Groceries", -3000)
			m := modelOn(t, st, tc.width, tc.height)

			// when
			play(m, "R tab tab tab")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// Each month's line: its figure, what it changed by from the month before (a dash for the
// first in view), and the bar.
func Test_netWorthScreen_lines(t *testing.T) {
	tcs := []struct {
		name  string
		setup func(t *testing.T, st *core.Local)
		month string
		want  []string
	}{
		{name: "the first month has no change", month: "Nov 2025", want: []string{"Nov", "2025", "0.00", "–"}},
		{name: "the second one changed by nothing", month: "Dec 2025", want: []string{"Dec", "2025", "0.00", "0.00"}},
		{name: "a fall", month: "Sep 2026", want: []string{"Sep", "2026", "-30.00", "-30.00", "▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒"}},
		{name: "a fall again, the bar the longest", month: "Oct 2026", want: []string{"Oct", "2026", "-72.00", "-42.00", strings.Repeat("▒", 40)}},
		{name: "a rise", setup: cashFlowLedger, month: "Sep 2026", want: []string{"Sep", "2026", "70.00", "+70.00", strings.Repeat("█", 40)}},
		{name: "then a fall from it", setup: cashFlowLedger, month: "Oct 2026", want: []string{"Oct", "2026", "58.00", "-12.00", strings.Repeat("█", 33) + "▏"}},
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
			play(m, "R tab tab tab")
			line := lineWith(t, m.View(), "│ "+tc.month+"  ")

			// then
			got := strings.Fields(strings.Trim(line, "│ "))
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("line mismatch (-want +got):\n%s\n%s", diff, line)
			}
		})
	}
}
