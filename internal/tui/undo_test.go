package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_Update_delete(t *testing.T) {
	type observed struct {
		Flow
		Rows int
	}
	const kino = "2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Uncategorized 12.00"
	tcs := []struct {
		name  string
		keys  string
		want  observed
		shows []string
	}{
		{name: "d asks first", keys: "3 u d", want: observed{Flow{Mode: "confirm"}, 2}, shows: []string{"Delete"}},
		{name: "n keeps it", keys: "3 u d n", want: observed{Flow{Mode: "list"}, 2}},
		{name: "esc keeps it", keys: "3 u d esc", want: observed{Flow{Mode: "list"}, 2}},
		{
			name: "y deletes it and the list follows", keys: "3 u d y",
			want: observed{Flow{Mode: "list", Status: "Deleted #4  (z to undo)", Removed: []string{kino}}, 1},
		},
		{name: "z brings it back", keys: "3 u d y z", want: observed{Flow{Mode: "list", Status: "Undid deleting #4"}, 2}},
		{
			name: "with nothing left to undo it says so", keys: "3 u d y z z",
			want: observed{Flow{Mode: "list", Status: "nothing to undo", StatusIsError: true}, 2},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			before := storedLines(t, st)

			// when
			press(m, keys(tc.keys)...)

			// then
			got := observed{observe(t, m, st, before), len(m.view)}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

// z undoes the most recent change, then the one before it.
func Test_Update_undo(t *testing.T) {
	const (
		kinoBefore = "2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Uncategorized 12.00"
		kinoAfter  = "2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Groceries 12.00"
		added      = "2026-10-22  | Assets:Bank -2.00 | Expenses:Groceries 2.00"
	)
	tcs := []struct {
		name string
		keys string
		want Flow
	}{
		{
			name: "a categorization and then an add", keys: "3 u c enter a text:2 ctrl+s",
			want: Flow{Mode: "list", Status: "Added #6    -2.00 EUR → Expenses:Groceries  (not shown: Oct 2026, filtered)", Added: []string{kinoAfter, added}, Removed: []string{kinoBefore}},
		},
		{
			name: "z undoes the add", keys: "3 u c enter a text:2 ctrl+s z",
			want: Flow{Mode: "list", Status: "Undid adding #6", Added: []string{kinoAfter}, Removed: []string{kinoBefore}},
		},
		{name: "z again undoes the categorization", keys: "3 u c enter a text:2 ctrl+s z z", want: Flow{Mode: "list", Status: "Undid categorizing #4"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			before := storedLines(t, st)

			// when
			play(m, tc.keys)

			// then
			if diff := cmp.Diff(tc.want, observe(t, m, st, before), equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// After a change that shrinks the ledger the list still pointed at the old rows;
// that must not crash. Undoing an import that was all there was, and deleting the
// last transaction, are the two ways to get there.
func Test_selected_afterTheLedgerShrinks(t *testing.T) {
	type observed struct {
		Flow
		Rows     int
		Selected bool
	}
	tcs := []struct {
		name   string
		script string
		want   observed
	}{
		{
			name:   "undoing an import that empties the ledger",
			script: "i enter enter z",
			want:   observed{Flow: Flow{Mode: "list", Status: "Undid the import of 3 transactions"}},
		},
		{
			name:   "deleting the only transaction",
			script: "i enter enter z u a text:5 enter text:Bakery enter enter 3 d y",
			want:   observed{Flow: Flow{Mode: "list", Status: "Deleted #1  (z to undo)"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := emptyModel(t)
			writeExport(t, m, "umsaetze.csv", exportCSV)
			before := storedLines(t, st)

			// when
			play(m, tc.script)
			_ = m.View() // drawing it is what used to panic

			// then
			_, hasSelection := m.selected()
			got := observed{Flow: observe(t, m, st, before), Rows: len(m.view), Selected: hasSelection}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
