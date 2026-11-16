package tui

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/i18n"
)

// shownWith lists the transactions on show with the category of each, newest first.
func shownWith(m *Model) []string {
	var out []string
	for _, i := range m.view {
		r := m.rows[i]
		out = append(out, r.tx.Payee+"→"+m.loc.ShortName(r.category.Account))
	}
	return out
}

func markedPayees(m *Model) []string {
	var out []string
	for _, r := range m.markedRows() {
		out = append(out, r.tx.Payee)
	}
	return out
}

func Test_Update_marks(t *testing.T) {
	type observed struct {
		Mode   string
		Status string
		Marked []string // payees, in the order of the list
		Held   int      // how many marks there are, shown or not
		Picked string   // what is typed in the picker
		RuleOn bool     // the rule field has the focus
		Income bool     // the picker ranks income categories first
		Cursor int
		Search string
		Shown  []string // payee→category of the list
		Undos  int
	}
	// The month on show, newest first: Hotel, Gehalt, to savings (a transfer), Kino and the
	// two REWE, of which the second one is uncategorized.
	const (
		hotel   = "Hotel→Travel"
		salary  = "Gehalt→Salary"
		savings = "to savings→Bank → Assets:Savings"
		kino    = "Kino→Uncategorized"
		rewe2   = "REWE→Uncategorized"
		rewe1   = "REWE→Groceries"
	)
	all := []string{hotel, salary, savings, kino, rewe2, rewe1}
	except := func(drop ...string) []string {
		var out []string
		for _, s := range all {
			if !slices.Contains(drop, s) {
				out = append(out, s)
			}
		}
		return out
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "nothing is marked to begin with", keys: "3", want: observed{Mode: "list", Shown: all}},
		{name: "space marks the transaction and moves down", keys: "3 space", want: observed{Mode: "list", Marked: []string{"Hotel"}, Cursor: 1, Shown: all}},
		{name: "a run of them", keys: "3 space space space", want: observed{Mode: "list", Marked: []string{"Hotel", "Gehalt", "to savings"}, Cursor: 3, Shown: all}},
		{name: "space on a marked one lets go of it", keys: "3 space k space", want: observed{Mode: "list", Cursor: 1, Shown: all}},
		{name: "space on the last one stays there", keys: "3 G space", want: observed{Mode: "list", Marked: []string{"REWE"}, Cursor: 5, Shown: all}},
		{name: "ctrl+a marks all on show", keys: "3 ctrl+a", want: observed{Mode: "list", Marked: []string{"Hotel", "Gehalt", "to savings", "Kino", "REWE", "REWE"}, Shown: all}},
		{name: "ctrl+a again lets go of them", keys: "3 ctrl+a ctrl+a", want: observed{Mode: "list", Shown: all}},
		{name: "ctrl+a marks the rest when some are marked", keys: "3 space ctrl+a", want: observed{Mode: "list", Marked: []string{"Hotel", "Gehalt", "to savings", "Kino", "REWE", "REWE"}, Cursor: 1, Shown: all}},
		{name: "esc lets go of the marks", keys: "3 space space esc", want: observed{Mode: "list", Cursor: 2, Shown: all}},
		{name: "a search hides marked ones and their marks go", keys: "3 ctrl+a / text:Kino enter", want: observed{Mode: "list", Marked: []string{"Kino"}, Held: 1, Search: "Kino", Shown: []string{kino}}},
		{name: "esc lets go of the marks before the search", keys: "3 ctrl+a / text:Kino enter esc", want: observed{Mode: "list", Search: "Kino", Shown: []string{kino}}},
		{name: "marks hidden by a search do not come back when it is widened", keys: "3 ctrl+a / text:Kino enter / backspace backspace backspace backspace enter", want: observed{Mode: "list", Marked: []string{"Kino"}, Held: 1, Cursor: 3, Shown: all}},
		{name: "a mark does not come back with the search cleared", keys: "3 ctrl+a / text:Kino enter esc esc", want: observed{Mode: "list", Cursor: 3, Shown: all}},
		{name: "another month hides them", keys: "3 ctrl+a [", want: observed{Mode: "list", Shown: []string{"Amazon→Shopping", "Edeka→Groceries"}}},
		{name: "c asks for the category of all of them", keys: "3 j j j space space c", want: observed{Mode: "pick", Marked: []string{"Kino", "REWE"}, Cursor: 5, Shown: all}},
		{
			name: "enter puts them all in the category, and z puts them back",
			keys: "3 j j j space space c text:Expenses:Food enter",
			want: observed{Mode: "list", Status: "2 transactions → Expenses:Food  (z to undo)", Cursor: 5, Undos: 1,
				Shown: []string{hotel, salary, savings, "Kino→Food", "REWE→Food", rewe1}},
		},
		{
			name: "undone",
			keys: "3 j j j space space c text:Expenses:Food enter z",
			want: observed{Mode: "list", Status: "Undid categorizing 2 transactions", Cursor: 5, Shown: all},
		},
		{name: "esc leaves the picker and keeps the marks", keys: "3 j j j space space c esc", want: observed{Mode: "list", Marked: []string{"Kino", "REWE"}, Cursor: 5, Shown: all}},
		{name: "tab does not open the rule field", keys: "3 j j j space space c tab text:x", want: observed{Mode: "pick", Marked: []string{"Kino", "REWE"}, Picked: "x", Cursor: 5, Shown: all}},
		{name: "income and spending together are ranked as spending", keys: "3 space space c", want: observed{Mode: "pick", Marked: []string{"Hotel", "Gehalt"}, Cursor: 2, Shown: all}},
		{name: "income alone is ranked as income", keys: "3 j space c", want: observed{Mode: "pick", Marked: []string{"Gehalt"}, Income: true, Cursor: 2, Shown: all}},
		{
			name: "a transfer among them stops it, and nothing changes",
			keys: "3 j j space space c",
			want: observed{Mode: "list", Status: "some of the marked transactions are transfers or split transactions, which cannot be recategorized here: unmark them first",
				Marked: []string{"to savings", "Kino"}, Cursor: 4, Shown: all},
		},
		{
			name: "one marked one is categorized, wherever the cursor is",
			keys: "3 j j j space j c text:Expenses:Food enter",
			want: observed{Mode: "list", Status: "#4 → Expenses:Food", Cursor: 5, Undos: 1,
				Shown: []string{hotel, salary, savings, "Kino→Food", rewe2, rewe1}},
		},
		{name: "d asks first, naming how many", keys: "3 space space d", want: observed{Mode: "confirm", Marked: []string{"Hotel", "Gehalt"}, Cursor: 2, Shown: all}},
		{name: "n changes nothing", keys: "3 space space d n", want: observed{Mode: "list", Marked: []string{"Hotel", "Gehalt"}, Cursor: 2, Shown: all}},
		{
			name: "y deletes them all",
			keys: "3 space space d y",
			want: observed{Mode: "list", Status: "Deleted 2 transactions  (z to undo)", Cursor: 2, Undos: 1, Shown: except(hotel, salary)},
		},
		{
			name: "z brings them all back",
			keys: "3 space space d y z",
			want: observed{Mode: "list", Status: "Undid deleting 2 transactions", Cursor: 2, Shown: all},
		},
		{
			name: "one marked one is deleted, wherever the cursor is",
			keys: "3 space j j d y",
			want: observed{Mode: "list", Status: "Deleted #8  (z to undo)", Cursor: 3, Undos: 1, Shown: except(hotel)},
		},
		{name: "d on the unmarked one still asks about it alone", keys: "3 d", want: observed{Mode: "confirm", Shown: all}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := filterModel(t)

			want := tc.want
			if want.Held == 0 {
				want.Held = len(want.Marked) // a mark is only ever on a transaction on show
			}

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, Marked: markedPayees(m), Held: len(m.marked), Picked: pickedText(m), RuleOn: m.pickRuleOn, Income: m.mode == modePick && m.pickIsIncome, Cursor: m.txs.cursor, Search: m.filter.Value(), Shown: shownWith(m), Undos: len(m.undo)}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_marks_screen(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		lang  language.Tag
		shows []string
		hides []string
	}{
		{name: "a mark is a check, and the list says how many", keys: "3 space space", shows: []string{"Transactions · 2 marked", "✓ Hotel", "✓ Gehalt", "esc unmark"}, hides: []string{"✓ Kino"}},
		{name: "one", keys: "3 space", shows: []string{"Transactions · 1 marked"}},
		{name: "nothing marked says nothing", keys: "3", hides: []string{"marked", "✓", "esc unmark"}},
		{name: "the picker names how many", keys: "3 j j j space space c", shows: []string{"Categorize 2 transactions", "enter apply · esc cancel", "a rule is made from one payee"}, hides: []string{"tab make a rule"}},
		{name: "the picker for one marked one is the ordinary one", keys: "3 j j j space j c", shows: []string{"Categorize · Kino", "tab make a rule"}},
		{name: "the question lists them", keys: "3 space space d", shows: []string{"Delete 2 transactions", "Hotel", "Gehalt", "Delete these transactions?", "y delete · n cancel"}},
		{name: "and how many more", keys: "3 ctrl+a d", shows: []string{"Delete 6 transactions", "and 1 more", "Delete these transactions?"}, hides: []string{"2026-10-01  REWE"}},
		{name: "the help lists the keys", keys: "? pgdown", shows: []string{"space  ctrl+a", "mark the transaction under the cursor"}},
		{name: "Spanish", lang: language.Spanish, keys: "3 space space", shows: []string{"2 marcadas", "esc desmarcar"}},
		{name: "Spanish, delete", lang: language.Spanish, keys: "3 space space d", shows: []string{"Eliminar 2 transacciones", "¿Eliminar estas transacciones?"}},
		{name: "German", lang: language.German, keys: "3 j j j space space c", shows: []string{"2 Transaktionen kategorisieren", "enter übernehmen · esc abbrechen"}},
		{name: "German, delete", lang: language.German, keys: "3 space space d", shows: []string{"2 Transaktionen löschen", "Diese Transaktionen löschen?"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := filterModel(t)
			for _, l := range i18n.Languages {
				if tc.lang != language.Und && l.Tag == tc.lang {
					m.setLanguage(l)
				}
			}

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// pickedText is what is typed in the picker, while it is open.
func pickedText(m *Model) string {
	if m.mode != modePick {
		return ""
	}
	return m.picker.Value()
}
