package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"
)

func Test_bucketed(t *testing.T) {
	tcs := []struct {
		name   string
		values []int64
		cells  int
		want   []int64
	}{
		{name: "fewer than the cells: as they are", values: []int64{1, 2, 3}, cells: 12, want: []int64{1, 2, 3}},
		{name: "as many as the cells: as they are", values: []int64{1, 2, 3}, cells: 3, want: []int64{1, 2, 3}},
		{name: "twice as many: the mean of each pair", values: []int64{2, 4, 6, 8}, cells: 2, want: []int64{3, 7}},
		{name: "a mean that is not whole goes up", values: []int64{1, 2, 3, 4}, cells: 2, want: []int64{2, 4}},
		{name: "something is never nothing", values: []int64{0, 1, 0, 0}, cells: 2, want: []int64{1, 0}},
		{name: "a refund goes away from zero", values: []int64{0, -1, 0, 0}, cells: 2, want: []int64{-1, 0}},
		{name: "a refund and spending", values: []int64{-10, 4, 5, 5}, cells: 2, want: []int64{-3, 5}},
		{name: "uneven groups: 5 in 2", values: []int64{1, 1, 5, 5, 5}, cells: 2, want: []int64{1, 5}},
		{name: "13 months in 12 cells: one cell of two", values: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}, cells: 12, want: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13}},
		{name: "nothing", values: nil, cells: 12, want: nil},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := bucketed(tc.values, tc.cells)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("bucketed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_rangeScreen(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "r asks for the range, with the months on show as placeholders",
			keys:  "R r",
			shows: []string{"Range of the reports", "From", "Until", "2025-11", "2026-10", "Months as YYYY-MM, at most 120", "enter next · ctrl+s apply · esc cancel"},
		},
		{
			name:  "a range of two months: the reports cover it, and say so",
			keys:  "R r text:2026-09 enter text:2026-10 enter",
			shows: []string{"Sep 2026 – Oct 2026 · EUR · chosen range", "Groceries"},
			hides: []string{"Range of the reports", "Nov 2025"},
		},
		{
			name:  "the cash flow covers it too",
			keys:  "R tab r text:2026-09 enter text:2026-10 enter",
			shows: []string{"Income against spending", "Sep 2026 – Oct 2026 · EUR · chosen range", "Sep 2026", "Oct 2026", "72.00"},
			hides: []string{"Nov 2025"},
		},
		{
			name:  "so do the payees: a payee outside it is gone",
			keys:  "R tab tab r text:2026-10 enter text:2026-10 enter",
			shows: []string{"Payees", "Oct 2026 – Oct 2026 · EUR · chosen range", "REWE", "Kino", "42.00"},
			hides: []string{"Edeka"},
		},
		{
			name:  "an empty Until is this month",
			keys:  "R r text:2026-09 enter enter",
			shows: []string{"Sep 2026 – Oct 2026 · EUR · chosen range"},
		},
		{
			name:  "an empty From is 11 months before Until",
			keys:  "R r enter text:2026-03 enter",
			shows: []string{"Apr 2025 – Mar 2026 · EUR · chosen range"},
		},
		{
			name:  "this month is the clock's, not the month on show",
			keys:  "R [ r text:2026-09 enter enter",
			shows: []string{"Sep 2026 – Oct 2026 · EUR · chosen range", "[ Sep 2026 ]"},
		},
		{
			name:  "a chosen range stays where it is when the month on show moves",
			keys:  "R r text:2026-09 enter text:2026-10 enter [ [",
			shows: []string{"Sep 2026 – Oct 2026 · EUR · chosen range", "[ Aug 2026 ]"},
			hides: []string{"Jul 2026"},
		},
		{
			name:  "both empty: back to the 12 months up to the month on show",
			keys:  "R r text:2026-09 enter text:2026-10 enter r ctrl+u enter ctrl+u enter",
			shows: []string{"Nov 2025 – Oct 2026 · EUR"},
			hides: []string{"chosen range"},
		},
		{
			name:  "and they move with the month on show again",
			keys:  "R r text:2026-09 enter text:2026-10 enter r ctrl+u enter ctrl+u enter [",
			shows: []string{"Oct 2025 – Sep 2026 · EUR"},
		},
		{
			name:  "the popup shows what was chosen, and keeps the months as placeholders",
			keys:  "R r text:2026-09 enter text:2026-10 enter r",
			shows: []string{"Range of the reports", "2026-09", "2026-10"},
		},
		{
			name:  "esc keeps the range as it was",
			keys:  "R r text:2026-09 esc",
			shows: []string{"Nov 2025 – Oct 2026 · EUR"},
			hides: []string{"Range of the reports", "chosen range"},
		},
		{
			name:  "not a month: said, and the popup stays",
			keys:  "R r text:soon enter enter",
			shows: []string{"Range of the reports", `invalid month "soon": use YYYY-MM`},
		},
		{
			name:  "typing takes the message away",
			keys:  "R r text:soon enter enter text:1",
			shows: []string{"Range of the reports"},
			hides: []string{"invalid month"},
		},
		{
			name:  "an end before the start",
			keys:  "R r text:2026-10 enter text:2026-09 enter",
			shows: []string{"Range of the reports", "the report cannot start in 2026-10: that is after it", "(2026-09)"},
		},
		{
			name:  "more than ten years",
			keys:  "R r text:2000-01 enter enter",
			shows: []string{"Range of the reports", "a report covers at most 120 months"},
		},
		{
			name:  "a range refused is not kept: esc, and the reports are as they were",
			keys:  "R r text:2026-10 enter text:2026-09 enter esc",
			shows: []string{"Nov 2025 – Oct 2026 · EUR", "Groceries"},
			hides: []string{"chosen range", "the report cannot start"},
		},
		{
			name:  "a range refused leaves a good one in place",
			keys:  "R r text:2026-09 enter text:2026-10 enter r ctrl+u text:soon enter enter esc",
			shows: []string{"Sep 2026 – Oct 2026 · EUR · chosen range"},
		},
		{
			name:  "the hint in the bottom bar",
			keys:  "R",
			shows: []string{"r range"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_Update_rangeKeys(t *testing.T) {
	type observed struct {
		Mode        string
		OnTo        bool
		From, To    string // what is typed
		RepFrom     string // what is chosen
		RepTo       string
		CfBack      int
		PayTop      int
		Report      reportKind
		Placeholder string
		Cursor      int // the category selected
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "closed", keys: "R", want: observed{Mode: "list"}},
		{name: "r opens it on From", keys: "R r", want: observed{Mode: "range", Placeholder: "2025-11"}},
		{name: "enter goes to Until", keys: "R r enter", want: observed{Mode: "range", OnTo: true, Placeholder: "2025-11"}},
		{name: "tab goes back and forth", keys: "R r tab", want: observed{Mode: "range", OnTo: true, Placeholder: "2025-11"}},
		{name: "tab twice", keys: "R r tab tab", want: observed{Mode: "range", Placeholder: "2025-11"}},
		{name: "down and up as well", keys: "R r down up", want: observed{Mode: "range", Placeholder: "2025-11"}},
		{name: "typing goes in the field", keys: "R r text:2026-0", want: observed{Mode: "range", From: "2026-0", Placeholder: "2025-11"}},
		{name: "a month is 7 characters at most", keys: "R r text:2026-09-15", want: observed{Mode: "range", From: "2026-09", Placeholder: "2025-11"}},
		{name: "typing in Until", keys: "R r enter text:2026-0", want: observed{Mode: "range", OnTo: true, To: "2026-0", Placeholder: "2025-11"}},
		{name: "enter on Until applies", keys: "R r text:2026-09 enter enter", want: observed{Mode: "list", RepFrom: "2026-09"}},
		{name: "ctrl+s applies from either field", keys: "R r text:2026-09 ctrl+s", want: observed{Mode: "list", RepFrom: "2026-09"}},
		{name: "esc closes it", keys: "R r text:2026-09 esc", want: observed{Mode: "list"}},
		{name: "the scrolling goes back to the top", keys: "R tab k r text:2026-09 enter enter", want: observed{Mode: "list", RepFrom: "2026-09", Report: reportCashFlow}},
		{name: "the places in the lists go back to the top", keys: "R down down tab k tab G r text:2026-09 enter enter", want: observed{Mode: "list", RepFrom: "2026-09", Report: reportPayees}},
		{name: "what was chosen is in the fields when it is opened again", keys: "R r text:2026-09 enter text:2026-10 enter r", want: observed{Mode: "range", From: "2026-09", To: "2026-10", RepFrom: "2026-09", RepTo: "2026-10", Placeholder: "2026-09"}},
		{name: "it opens on the report on show", keys: "R tab tab r", want: observed{Mode: "range", Report: reportPayees, Placeholder: "2025-11"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			manyPayees(30)(t, st) // more payees than the screen holds
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)

			// then
			got := observed{Cursor: m.repList.cursor, Mode: modeNames[m.mode], RepFrom: m.repFrom, RepTo: m.repTo, CfBack: m.cfBack, PayTop: m.payTop, Report: m.report}
			if m.rng != nil {
				got.OnTo, got.From, got.To, got.Placeholder = m.rng.onTo, m.rng.from.Value(), m.rng.to.Value(), m.rng.from.Placeholder
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A range longer than 12 months is drawn in the pane that holds 12.
func Test_rangeScreen_long(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "the categories: the newest months of the selected one, what is left out, and the total",
			keys:  "R r text:2024-01 enter text:2026-10 enter",
			shows: []string{"Jan 2024 – Oct 2026 · EUR · chosen range", "earlier months not shown", "Oct 2026", "Total", "Per month", "72.00", "2.11"},
			hides: []string{"Jan 2024  ", "more lines"},
		},
		{
			name:  "the cash flow scrolls through the months",
			keys:  "R tab r text:2024-01 enter text:2026-10 enter",
			shows: []string{"Oct 2026", "Total", "72.00", "34 of 34"},
			hides: []string{"Nov 2025"},
		},
		{
			name:  "the cash flow, at the oldest",
			keys:  "R tab r text:2024-01 enter text:2026-10 enter g",
			shows: []string{"Jan 2024", "Total", "1 of 34"},
		},
		{
			name:  "a range of one month",
			keys:  "R r text:2026-10 enter text:2026-10 enter",
			shows: []string{"Oct 2026 – Oct 2026 · EUR · chosen range", "42.00", "Per month"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_rangeScreen_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		keys  string
		shows []string
		hides []string
	}{
		{name: "Spanish: the popup", tag: language.Spanish, keys: "R r", shows: []string{"Período de los informes", "Desde", "Hasta", "Meses como AAAA-MM", "enter siguiente · ctrl+s aplicar · esc cancelar"}, hides: []string{"Range of the reports", "Until"}},
		{name: "German: the popup", tag: language.German, keys: "R r", shows: []string{"Zeitraum der Berichte", "Von", "Bis", "Monate als JJJJ-MM", "enter weiter · ctrl+s anwenden · esc abbrechen"}, hides: []string{"Range of the reports", "Until"}},
		{name: "Spanish: the range chosen", tag: language.Spanish, keys: "R tab r text:2026-09 enter enter", shows: []string{"sep 2026 – oct 2026 · EUR · período elegido"}, hides: []string{"chosen range"}},
		{name: "German: the range chosen", tag: language.German, keys: "R tab r text:2026-09 enter enter", shows: []string{"Sep 2026 – Okt 2026 · EUR · gewählter Zeitraum"}, hides: []string{"chosen range"}},
		{name: "Spanish: a month that is none", tag: language.Spanish, keys: "R r text:soon enter enter", shows: []string{"Período de los informes"}, hides: []string{"invalid month"}},
		{name: "Spanish: months left out", tag: language.Spanish, keys: "R r text:2024-01 enter text:2026-10 enter", shows: []string{"meses anteriores sin mostrar"}, hides: []string{"earlier months not shown"}},
		{name: "German: months left out", tag: language.German, keys: "R r text:2024-01 enter text:2026-10 enter", shows: []string{"frühere Monate nicht gezeigt"}, hides: []string{"earlier months not shown"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-09-10", "x", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-10", "x", "Assets:Bank", "Expenses:Groceries", -4200)
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// On a terminal too short for any month, the total of a long range is still in view.
func Test_rangeScreen_short(t *testing.T) {
	tcs := []struct {
		name   string
		height int
		shows  []string
		hides  []string
	}{
		{name: "room for the total and what is left out, not for a month", height: 8, shows: []string{"34 earlier months not shown", "Total", "Per month", "42.00"}, hides: []string{"more lines", "Oct 2026 █"}},
		{name: "room for a month", height: 9, shows: []string{"33 earlier months not shown", "Oct 2026 █", "Total", "Per month"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-10", "x", "Assets:Bank", "Expenses:Groceries", -4200)
			m := modelOn(t, st, 100, tc.height)

			// when
			play(m, "R r text:2024-01 enter text:2026-10 enter")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
