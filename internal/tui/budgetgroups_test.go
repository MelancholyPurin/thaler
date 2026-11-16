package tui

import (
	"sort"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/i18n"
)

// groupModel is newModel with spending in October in groups: Housing (Rent and Power with
// targets, Water without, and some on Housing itself), Food (no targets) and Fun (a target
// and no group).
func groupModel(t *testing.T) *Model {
	t.Helper()
	m, st := newModel(t)
	book(t, st, "2026-10-01", "Landlord", "Assets:Bank", "Expenses:Housing:Rent", -80000)
	book(t, st, "2026-10-02", "Power Co", "Assets:Bank", "Expenses:Housing:Power", -12000)
	book(t, st, "2026-10-03", "Water Co", "Assets:Bank", "Expenses:Housing:Water", -2000)
	book(t, st, "2026-10-04", "Hardware", "Assets:Bank", "Expenses:Housing", -1000)
	book(t, st, "2026-10-05", "Diner", "Assets:Bank", "Expenses:Food:Out", -4500)
	book(t, st, "2026-10-06", "Market", "Assets:Bank", "Expenses:Food:Shop", -3000)
	book(t, st, "2026-10-07", "Cinema", "Assets:Bank", "Expenses:Fun", -3000)
	for cat, minor := range map[string]int64{"Expenses:Housing:Rent": 90000, "Expenses:Housing:Power": 10000, "Expenses:Fun": 5000} {
		if err := st.SetBudgetTarget(t.Context(), "2026-10", cat, eur(minor)); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	return m
}

func Test_Update_budgetGroups(t *testing.T) {
	type observed struct {
		Selected     string
		Entries      []string
		Folded       []string
		Screen       string
		OnlyCategory string
		OnlyGroup    bool
		Rows         []string // of the Ledger, when the category is shown there
		Stored       string   // the selection as it is kept, when a case asks
		CatsOn       string   // the category the categories popup is on, when it is open
	}
	const (
		housing = "#Expenses:Housing"
		rent    = "Expenses:Housing:Rent"
		power   = "Expenses:Housing:Power"
		water   = "Expenses:Housing:Water"
		direct  = "Expenses:Housing"
		fun     = "Expenses:Fun"
		food    = "#Expenses:Food"
		out     = "Expenses:Food:Out"
		shop    = "Expenses:Food:Shop"
		uncat   = "Expenses:Uncategorized"
		groc    = "Expenses:Groceries"
	)
	all := []string{housing, rent, power, water, direct, fun, food, out, shop, uncat, groc}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "groups are rows, with their categories under them", keys: "B tab", want: observed{Selected: housing, Entries: all, Screen: "budget"}},
		{name: "a group is folded with space", keys: "B tab space", want: observed{Selected: housing, Entries: []string{housing, fun, food, out, shop, uncat, groc}, Folded: []string{"Expenses:Housing"}, Screen: "budget"}},
		{name: "and unfolded again", keys: "B tab space space", want: observed{Selected: housing, Entries: all, Screen: "budget"}},
		{name: "space on a category of a group folds it and selects the group", keys: "B tab down down space", want: observed{Selected: housing, Entries: []string{housing, fun, food, out, shop, uncat, groc}, Folded: []string{"Expenses:Housing"}, Screen: "budget"}},
		{name: "folding from a category keeps the group selected", keys: "B tab down down space", want: observed{Selected: housing, Stored: housing, Entries: []string{housing, fun, food, out, shop, uncat, groc}, Folded: []string{"Expenses:Housing"}, Screen: "budget"}},
		{name: "the categories popup opens on the category a group is named after", keys: "B tab K", want: observed{Selected: housing, Entries: all, Screen: "budget", CatsOn: direct}},
		{name: "← on a category goes to its group", keys: "B tab down down left", want: observed{Selected: housing, Entries: all, Screen: "budget"}},
		{name: "← on a group folds it", keys: "B tab left", want: observed{Selected: housing, Entries: []string{housing, fun, food, out, shop, uncat, groc}, Folded: []string{"Expenses:Housing"}, Screen: "budget"}},
		{name: "→ unfolds it", keys: "B tab left right", want: observed{Selected: housing, Entries: all, Screen: "budget"}},
		{name: "→ on an unfolded group changes nothing", keys: "B tab right", want: observed{Selected: housing, Entries: all, Screen: "budget"}},
		{name: "space on a category that is in no group does nothing", keys: "B tab down down down down down space", want: observed{Selected: fun, Entries: all, Screen: "budget"}},
		{name: "a group without a target is folded too", keys: "B tab down down down down down down space", want: observed{Selected: food, Entries: []string{housing, rent, power, water, direct, fun, food, uncat, groc}, Folded: []string{"Expenses:Food"}, Screen: "budget"}},
		{name: "a folded group is stepped over", keys: "B tab space down", want: observed{Selected: fun, Entries: []string{housing, fun, food, out, shop, uncat, groc}, Folded: []string{"Expenses:Housing"}, Screen: "budget"}},
		{name: "both folded", keys: "B tab space down down space", want: observed{Selected: food, Entries: []string{housing, fun, food, uncat, groc}, Folded: []string{"Expenses:Food", "Expenses:Housing"}, Screen: "budget"}},
		{
			name: "enter on a group shows the transactions of all its categories",
			keys: "B tab enter",
			want: observed{Selected: housing, Entries: all, Screen: "ledger", OnlyCategory: "Expenses:Housing", OnlyGroup: true,
				Rows: []string{"2026-10-04 Hardware -10.00", "2026-10-03 Water Co -20.00", "2026-10-02 Power Co -120.00", "2026-10-01 Landlord -800.00"}},
		},
		{
			name: "enter on a category shows its own",
			keys: "B tab down enter",
			want: observed{Selected: rent, Entries: all, Screen: "ledger", OnlyCategory: rent, Rows: []string{"2026-10-01 Landlord -800.00"}},
		},
		{
			name: "enter on the category a group is named after shows only what is booked to it",
			keys: "B tab down down down down enter",
			want: observed{Selected: direct, Entries: all, Screen: "ledger", OnlyCategory: direct, Rows: []string{"2026-10-04 Hardware -10.00"}},
		},
		{
			name: "esc in the Ledger lets go of the group",
			keys: "B tab enter esc",
			want: observed{Selected: housing, Entries: all, Screen: "ledger", Rows: []string{"2026-10-07 Cinema -30.00", "2026-10-06 Market -30.00", "2026-10-05 Diner -45.00", "2026-10-04 Hardware -10.00", "2026-10-04 to savings -5.00", "2026-10-03 Water Co -20.00", "2026-10-03 Kino -12.00", "2026-10-02 Power Co -120.00", "2026-10-02 REWE -20.00", "2026-10-01 Landlord -800.00", "2026-10-01 REWE -10.00"}},
		},
		{name: "all time groups them as well", keys: "B g tab", want: observed{Selected: "#Expenses:Housing", Entries: []string{"#Expenses:Housing", rent, power, water, direct, "#Expenses:Food", out, shop, groc, uncat, fun}, Screen: "budget"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := groupModel(t)

			// when
			play(m, tc.keys)

			// then
			got := observed{Selected: m.budgetSelected(), Entries: m.budgetEntries(), Screen: screenNames2[m.screen], OnlyCategory: m.onlyCategory, OnlyGroup: m.onlyGroup}
			for g, on := range m.folds {
				if on {
					got.Folded = append(got.Folded, g)
				}
			}
			sort.Strings(got.Folded)
			if tc.want.Stored != "" {
				got.Stored = m.budgetSel
			}
			if m.mode == modeCategories {
				got.CatsOn = m.catForm.rows[m.catForm.list.cursor].name
			}
			if m.screen == screenLedger {
				got.Rows = visibleRows(m)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_budgetGroups_screen(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		lang  language.Tag
		shows []string
		hides []string
	}{
		{name: "a group has a row that adds up its categories", keys: "B tab", shows: []string{"- Housing", "Rent", "Power", "Water", "Housing (directly)", "of 1,000.00 · 80.00 left", "No target"}, hides: []string{"Expenses:Housing:Rent"}},
		{name: "the categories of a group that has none set aside are under it too", keys: "B tab", shows: []string{"- Food", "Out", "Shop"}},
		{name: "folded", keys: "B tab space", shows: []string{"+ Housing"}, hides: []string{"Water", "Housing (directly)"}},
		{name: "the group is selected", keys: "B tab", shows: []string{"▸ - Housing"}},
		{name: "the hint", keys: "B tab", shows: []string{"space fold"}},
		{name: "no hint when nothing is grouped", keys: "B", hides: []string{"space fold"}},
		{name: "the transactions of a group", keys: "B tab enter", shows: []string{"Transactions · Housing", "Landlord", "Hardware"}, hides: []string{"Cinema"}},
		{name: "the key help lists it", keys: "? pgdown", shows: []string{"fold or unfold the group of the selected category"}},
		{name: "Spanish", lang: language.Spanish, keys: "B tab", shows: []string{"- Housing", "Housing (directamente)", "espacio plegar"}},
		{name: "German", lang: language.German, keys: "B tab", shows: []string{"- Housing", "Housing (direkt)", "Leertaste falten"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := groupModel(t)
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 50}) // room for all of the categories
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

// A rename of a category, or of the group, is followed by what the Budget has selected.
func Test_followCategory_group(t *testing.T) {
	tcs := []struct {
		name string
		sel  string
		from string
		to   string
		want string
	}{
		{name: "the group", sel: "#Expenses:Housing", from: "Expenses:Housing", to: "Expenses:Home", want: "#Expenses:Home"},
		{name: "a category", sel: "Expenses:Housing:Rent", from: "Expenses:Housing", to: "Expenses:Home", want: "Expenses:Home:Rent"},
		{name: "another group", sel: "#Expenses:Food", from: "Expenses:Housing", to: "Expenses:Home", want: "#Expenses:Food"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := groupModel(t)
			m.budgetSel = tc.sel

			// when
			m.followCategory(tc.from, tc.to)

			// then
			if diff := cmp.Diff(tc.want, m.budgetSel); diff != "" {
				t.Errorf("selection mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
