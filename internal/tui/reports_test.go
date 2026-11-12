package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_sparkline(t *testing.T) {
	tcs := []struct {
		name   string
		values []int64
		want   string
	}{
		{name: "nothing", values: nil, want: ""},
		{name: "no spending at all is dots", values: []int64{0, 0, 0}, want: "···"},
		{name: "the largest is full, the others against it", values: []int64{0, 100, 50, 25}, want: "·█▄▂"},
		{name: "a little is still seen", values: []int64{1, 1000}, want: "▁█"},
		{name: "the rounding is up", values: []int64{126, 1000}, want: "▂█"},
		{name: "exactly an eighth", values: []int64{125, 1000}, want: "▁█"},
		{name: "a refund is a dash, not a small spending", values: []int64{-500, 1000, 0}, want: "-█·"},
		{name: "only refunds", values: []int64{-500, -100}, want: "--"},
		{name: "a refund and nothing else", values: []int64{-500, 0}, want: "-·"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := sparkline(tc.values)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("sparkline mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_bar(t *testing.T) {
	tcs := []struct {
		name  string
		v     int64
		top   int64
		width int
		want  string
	}{
		{name: "the largest fills the width", v: 100, top: 100, width: 10, want: "██████████"},
		{name: "half", v: 50, top: 100, width: 10, want: "█████"},
		{name: "to the eighth of a cell", v: 55, top: 100, width: 10, want: "█████▌"},
		{name: "an eighth", v: 13, top: 1000, width: 10, want: "▏"},
		{name: "a hair is still seen", v: 1, top: 100000, width: 10, want: "▏"},
		{name: "nothing", v: 0, top: 100, width: 10, want: ""},
		{name: "no scale", v: 5, top: 0, width: 10, want: ""},
		{name: "no room", v: 5, top: 10, width: 0, want: ""},
		{name: "a refund in whole cells", v: -50, top: 100, width: 10, want: "▒▒▒▒▒"},
		{name: "a small refund is one cell", v: -1, top: 100, width: 10, want: "▒"},
		{name: "the largest refund fills the width", v: -100, top: 100, width: 10, want: "▒▒▒▒▒▒▒▒▒▒"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := bar(tc.v, tc.top, tc.width)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("bar mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_reportsScreen(t *testing.T) {
	tcs := []struct {
		name  string
		setup func(t *testing.T, st *core.Local)
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "the categories with their months and totals",
			keys:  "R",
			shows: []string{"Spending by category", "Nov 2025 – Oct 2026 · EUR", "All spending", "72.00", "Groceries", "40.00", "Uncategorized", "32.00", "1 of 3", "↑/↓ category"},
			hides: []string{"Not built yet"},
		},
		{
			name:  "all spending as bars: every month, the total and the average",
			keys:  "R",
			shows: []string{"Nov 2025", "Sep 2026", "30.00", "Oct 2026", "42.00", "█", "Total", "72.00 EUR", "Per month", "6.00 EUR"},
		},
		{
			name:  "a category's months",
			keys:  "R down",
			shows: []string{"2 of 3", "Groceries", "Sep 2026", "30.00", "Oct 2026", "10.00", "40.00 EUR", "3.33 EUR"},
			hides: []string{"72.00 EUR", "42.00"},
		},
		{
			name:  "the next category",
			keys:  "R down down",
			shows: []string{"3 of 3", "Uncategorized", "32.00 EUR", "2.66 EUR"},
			hides: []string{"Sep 2026 █"},
		},
		{
			name:  "the 12 months end where the period does",
			keys:  "R [",
			shows: []string{"Oct 2025 – Sep 2026 · EUR", "30.00 EUR", "Sep 2026"},
			hides: []string{"Oct 2026"},
		},
		{
			name:  "a month later",
			keys:  "R ] ]",
			shows: []string{"Jan 2026 – Dec 2026 · EUR", "72.00 EUR"},
		},
		{
			name:  "all time ends this month",
			keys:  "R T",
			shows: []string{"Nov 2025 – Oct 2026 · EUR", "72.00 EUR"},
		},
		{
			name:  "back to this month",
			keys:  "R [ [ t",
			shows: []string{"Nov 2025 – Oct 2026 · EUR", "72.00 EUR"},
		},
		{
			name: "a refund is a negative month, drawn apart from spending",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-10-10", "Refund", "Assets:Bank", "Expenses:Groceries", 2000)
			},
			keys:  "R down down",
			shows: []string{"Groceries", "-10.00", "▒", "20.00 EUR", "█-"},
		},
		{
			name: "bars are scaled to the largest month, a refund bigger than any spending included",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-09-05", "x", "Assets:Bank", "Expenses:Fun", -1000)
				book(t, st, "2026-10-05", "x", "Assets:Bank", "Expenses:Fun", 3000)
			},
			keys:  "R down down down",
			shows: []string{"Fun", "Sep 2026 " + "███████████" + " ", "Oct 2026 " + "▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒", "-20.00 EUR"},
		},
		{
			name: "spending in a currency without a rate is left out, and said",
			setup: func(t *testing.T, st *core.Local) {
				openAccounts(t, st, "Assets:Dollars USD")
				if _, err := st.AddTransaction(t.Context(), core.NewTransaction{Date: startOn("2026-10-12"), Payee: "shop", Postings: []ledger.Posting{
					{Account: "Assets:Dollars", Amount: ledger.Money{Minor: -900, Currency: "USD"}}, {Account: "Expenses:Travel", Amount: ledger.Money{Minor: 900, Currency: "USD"}}}}); err != nil {
					t.Fatal(err)
				}
			},
			keys:  "R",
			shows: []string{"no exchange rate USD → EUR", "72.00 EUR"},
			hides: []string{"Travel"},
		},
		{
			name: "a category with a long name",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-10-11", "x", "Assets:Bank", "Expenses:Reading-Lamps-And-Other-Furniture", -1000)
			},
			keys:  "R",
			shows: []string{"Reading-Lamps", "10.00"},
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

func Test_reportsScreen_nothing(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{name: "nothing spent", keys: "R", shows: []string{"Spending by category", "Nov 2025 – Oct 2026 · EUR", "Nothing was spent in these months."}, hides: []string{"All spending", "Total"}},
		{name: "keys do nothing", keys: "R down G up", shows: []string{"Nothing was spent in these months."}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := emptyModel(t)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_Update_reportsKeys(t *testing.T) {
	tcs := []struct {
		name string
		keys string
		want int // the entry selected
	}{
		{name: "all spending first", keys: "R", want: 0},
		{name: "down", keys: "R down", want: 1},
		{name: "j and k", keys: "R j j k", want: 1},
		{name: "up stops at the first", keys: "R up", want: 0},
		{name: "down stops at the last", keys: "R down down down down", want: 2},
		{name: "G goes to the last", keys: "R G", want: 2},
		{name: "g goes back", keys: "R G g", want: 0},
		{name: "the selection stays when the months move", keys: "R down [", want: 1},
		{name: "and when fewer categories are left it holds inside them", keys: "R G [ [ [ [ [ [ [ [ [ [ [ [", want: 0},
		{name: "other screens keep their keys", keys: "R down B F R", want: 1},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			play(m, tc.keys)
			m.View()

			// then
			if diff := cmp.Diff(tc.want, m.repList.cursor); diff != "" {
				t.Errorf("cursor mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_reportsScreen_narrow(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "only the list when there is no room for two panes",
			keys:  "R",
			shows: []string{"Spending by category", "All spending", "Groceries", "72.00", "1 of 3"},
			hides: []string{"Per month", "Sep 2026"},
		},
		{name: "it still moves", keys: "R down", shows: []string{"2 of 3"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-09-15", "Edeka", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-03", "Kino", "Assets:Bank", ledger.UncategorizedExpense, -1200)
			book(t, st, "2026-10-04", "REWE", "Assets:Bank", "Expenses:Groceries", -3000)
			m := modelOn(t, st, 60, 20)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
