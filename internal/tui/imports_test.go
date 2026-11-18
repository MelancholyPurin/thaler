package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
)

// A second export, to have two imports.
const secondCSV = exportHeader +
	"06.10.26;Einkauf;Baeckerei;-3,00;EUR;Umsatz gebucht\n"

// A statement that leaves the account empty, so that it can be closed.
const nettingCSV = exportHeader +
	"02.10.26;;In;10,00;EUR;Umsatz gebucht\n" +
	"03.10.26;;Out;-10,00;EUR;Umsatz gebucht\n"

func Test_Update_imports(t *testing.T) {
	type observed struct {
		Mode     string
		Confirm  bool
		Err      string
		Status   string
		Removed  []string // what left the ledger during the script
		Left     []int64  // the imports still listed
		Undoable int      // the steps `z` has
	}
	modes := map[mode]string{modeList: "list", modeImports: "imports"}
	const (
		first = "i enter enter" // imports umsaetze.csv into the account that is selected
		gone  = "Took back umsaetze.csv: 3 transactions deleted"
	)
	threeRows := []string{
		"2026-10-02 REWE Markt | Assets:Bank -23.47 | Expenses:Groceries 23.47",
		"2026-10-03 Mystery Shop | Assets:Bank -5.00 | Expenses:Uncategorized 5.00",
		"2026-10-04 Arbeitgeber | Assets:Bank 1000.00 | Income:Uncategorized -1000.00",
	}
	tcs := []struct {
		name   string
		empty  bool   // a ledger with one account and nothing else
		file   string // umsaetze.csv
		setup  string // keys before the ledger is looked at
		second string // another export, imported after setup
		after  func(t *testing.T, st *core.Local)
		script string
		want   observed
		shows  []string
		hides  []string
	}{
		{
			name: "I lists the imports with what each did", file: exportCSV, setup: first, script: "I",
			want:  observed{Mode: "imports", Left: []int64{1}, Undoable: 1},
			shows: []string{"Earlier imports", "2026-10-22", "Bank · umsaetze.csv", "3 new", "profile sparkasse", "enter take back"},
		},
		{
			name: "with no import made it says so", file: exportCSV, script: "I",
			want:  observed{Mode: "imports"},
			shows: []string{"No imports are recorded yet", "esc close"},
			hides: []string{"enter take back"},
		},
		{
			name: "enter with nothing listed does nothing", file: exportCSV, script: "I enter",
			want: observed{Mode: "imports"},
		},
		{
			name: "esc closes it", file: exportCSV, setup: first, script: "I esc",
			want: observed{Mode: "list", Left: []int64{1}, Undoable: 1},
		},
		{
			name: "enter asks before anything goes", file: exportCSV, setup: first, script: "I enter",
			want:  observed{Mode: "imports", Confirm: true, Left: []int64{1}, Undoable: 1},
			shows: []string{"Take this import back?", "umsaetze.csv", "3 transactions it added are deleted", "z does not bring them back", "enter take it back"},
		},
		{
			name: "esc then keeps it", file: exportCSV, setup: first, script: "I enter esc",
			want: observed{Mode: "imports", Left: []int64{1}, Undoable: 1},
		},
		{
			name: "a second enter takes it back", file: exportCSV, setup: first, script: "I enter enter",
			want: observed{Mode: "list", Status: gone, Removed: threeRows},
		},
		{
			name: "y does too", file: exportCSV, setup: first, script: "I enter y",
			want: observed{Mode: "list", Status: gone, Removed: threeRows},
		},
		{
			name: "an import that is not the last can be taken back, and the last stays", file: exportCSV, second: secondCSV,
			setup: first, script: "I down enter enter",
			want: observed{Mode: "list", Status: gone, Removed: threeRows, Left: []int64{2}, Undoable: 1},
		},
		{
			name: "the newest is first", file: exportCSV, second: secondCSV, setup: first, script: "I",
			want:  observed{Mode: "imports", Left: []int64{2, 1}, Undoable: 2},
			shows: []string{"▸ 2026-10-22"},
		},
		{
			name: "z does not look for what the list took back", file: exportCSV, setup: first, script: "I enter enter z",
			want: observed{Mode: "list", Status: "nothing to undo", Removed: threeRows},
		},
		{
			name: "z still takes an import back, and the list forgets it", file: exportCSV, setup: first, script: "z I",
			want:  observed{Mode: "imports", Removed: threeRows},
			shows: []string{"No imports are recorded yet"},
		},
		{
			name: "an account that was closed since refuses, and says why", empty: true, file: nettingCSV, setup: first,
			after: func(t *testing.T, st *core.Local) {
				if _, err := st.CloseAccount(t.Context(), "Assets:Bank"); err != nil {
					t.Fatal(err)
				}
			},
			script: "I enter enter",
			want:   observed{Mode: "imports", Err: "Assets:Bank is closed: reopen it first", Left: []int64{1}, Undoable: 1},
			shows:  []string{"Earlier imports", "Assets:Bank is closed"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			if tc.empty {
				m, st = emptyModel(t)
			}
			writeExport(t, m, "umsaetze.csv", tc.file)
			play(m, tc.setup)
			if tc.second != "" {
				writeExport(t, m, "second.csv", tc.second)
				play(m, first)
			}
			if tc.after != nil {
				tc.after(t, st)
			}
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			left, err := st.ListImports(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var ids []int64
			for _, r := range left {
				ids = append(ids, r.ID)
			}
			_, removed := delta(before, storedLines(t, st))
			got := observed{Mode: modes[m.mode], Status: m.status, Removed: removed, Left: ids, Undoable: len(m.undo)}
			if m.ims != nil {
				got.Confirm, got.Err = m.ims.confirm, m.ims.err
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
