package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
	"github.com/muesli/termenv"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// testTarget is a target a test starts from.
type testTarget struct {
	month, category string
	minor           int64
}

// targetModel is a model on a ledger with three spending categories: Groceries (10.00
// in October), Rent (800.00 in September, nothing in October) and the uncategorized
// 32.00 of October. The targets are those given.
func targetModel(t *testing.T, targets ...testTarget) (*Model, *core.Local) {
	t.Helper()
	m, st := newModel(t)
	book(t, st, "2026-09-20", "Landlord", "Assets:Bank", "Expenses:Rent", -80000)
	for _, tg := range targets {
		if err := st.SetBudgetTarget(t.Context(), tg.month, tg.category, eur(tg.minor)); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	return m, st
}

func Test_Update_target(t *testing.T) {
	type observed struct {
		Mode          string
		Status        string
		StatusIsError bool
		Err           string // the message in the popup, when it is open
		Amount        string // what is in the amount, when it is open
		October       []core.BudgetTarget
		September     []core.BudgetTarget
	}
	food, rent := "Expenses:Groceries", "Expenses:Rent"
	tcs := []struct {
		name    string
		targets []testTarget
		keys    string
		want    observed
	}{
		{
			name: "e opens the popup on the first category, with nothing in the amount",
			keys: "B e",
			want: observed{Mode: "target"},
		},
		{
			name: "a target for the category under the cursor",
			keys: "B e text:300 enter",
			want: observed{Mode: "list", Status: "Target for Groceries in Oct 2026: 300.00 EUR · z undoes",
				October: []core.BudgetTarget{{Category: food, Amount: eur(30000)}}},
		},
		{
			name: "down moves to the next category",
			keys: "B e down text:900.50 enter",
			want: observed{Mode: "list", Status: "Target for Rent in Oct 2026: 900.50 EUR · z undoes",
				October: []core.BudgetTarget{{Category: rent, Amount: eur(90050)}}},
		},
		{
			name: "the month on show is the month of the target",
			keys: "B down e text:100 enter",
			want: observed{Mode: "list", Status: "Target for Groceries in Sep 2026: 100.00 EUR · z undoes",
				September: []core.BudgetTarget{{Category: food, Amount: eur(10000)}}},
		},
		{
			name:    "the amount starts as the target there is, and typing replaces it",
			targets: []testTarget{{"2026-10", food, 30000}},
			keys:    "B e ctrl+u text:350 enter",
			want: observed{Mode: "list", Status: "Target for Groceries in Oct 2026: 350.00 EUR · z undoes",
				October: []core.BudgetTarget{{Category: food, Amount: eur(35000)}}},
		},
		{
			name:    "the amount follows the cursor",
			targets: []testTarget{{"2026-10", food, 30000}, {"2026-10", rent, 90000}},
			keys:    "B e down",
			want: observed{Mode: "target", Amount: "900.00",
				October: []core.BudgetTarget{{Category: food, Amount: eur(30000)}, {Category: rent, Amount: eur(90000)}}},
		},
		{
			name:    "an empty amount clears the target",
			targets: []testTarget{{"2026-10", food, 30000}, {"2026-10", rent, 90000}},
			keys:    "B e ctrl+u enter",
			want: observed{Mode: "list", Status: "Target for Groceries in Oct 2026 cleared · z undoes",
				October: []core.BudgetTarget{{Category: rent, Amount: eur(90000)}}},
		},
		{
			name: "an empty amount with no target to clear says so, and the popup stays",
			keys: "B e enter",
			want: observed{Mode: "target", Err: "type an amount: there is no target to clear"},
		},
		{
			name: "an amount that is not one",
			keys: "B e text:abc enter",
			want: observed{Mode: "target", Amount: "abc", Err: `amount: invalid amount "abc"`},
		},
		{
			name: "zero is not a target",
			keys: "B e text:0 enter",
			want: observed{Mode: "target", Amount: "0", Err: "amount must be greater than zero"},
		},
		{
			name: "esc leaves nothing behind",
			keys: "B e text:300 esc",
			want: observed{Mode: "list"},
		},
		{
			name: "all time has no targets to set",
			keys: "B g e",
			want: observed{Mode: "list", Status: "choose a month first: a target is for one month", StatusIsError: true},
		},
		{
			name: "z takes a new target back",
			keys: "B e text:300 enter z",
			want: observed{Mode: "list", Status: "Target for Groceries in Oct 2026 taken back"},
		},
		{
			name:    "z puts the old target back",
			targets: []testTarget{{"2026-10", food, 30000}},
			keys:    "B e ctrl+u text:350 enter z",
			want: observed{Mode: "list", Status: "Target for Groceries in Oct 2026 taken back",
				October: []core.BudgetTarget{{Category: food, Amount: eur(30000)}}},
		},
		{
			name:    "z puts a cleared target back",
			targets: []testTarget{{"2026-10", food, 30000}},
			keys:    "B e ctrl+u enter z",
			want: observed{Mode: "list", Status: "Target for Groceries in Oct 2026 taken back",
				October: []core.BudgetTarget{{Category: food, Amount: eur(30000)}}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := targetModel(t, tc.targets...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, StatusIsError: m.statusIsError}
			if m.tgt != nil {
				got.Err, got.Amount = m.tgt.err, m.tgt.amount.Value()
			}
			var err error
			if got.October, err = st.BudgetTargets(t.Context(), "2026-10"); err != nil {
				t.Fatal(err)
			}
			if got.September, err = st.BudgetTargets(t.Context(), "2026-09"); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The budget of a month shows each category with a target against it: what is spent,
// the target, and what is left, or by how much it was passed.
func Test_budgetTargets(t *testing.T) {
	food, rent := "Expenses:Groceries", "Expenses:Rent"
	tcs := []struct {
		name    string
		targets []testTarget
		keys    string
		shows   []string
		hides   []string
	}{
		{
			name:  "no targets: the spending as before, and how to set one",
			keys:  "B",
			shows: []string{"Spending", "Groceries", "Uncategorized", "Press e to set a target"},
			hides: []string{"Left", "No target", "Targets"},
		},
		{
			name:    "a target not reached: spent, target and what is left",
			targets: []testTarget{{"2026-10", food, 30000}},
			keys:    "B",
			shows:   []string{"Target", "300.00 EUR", "Left", "290.00 EUR", "Targets", "Groceries", "of 300.00 · 290.00 left", "No target", "Uncategorized"},
			hides:   []string{"Press e", "Spending"},
		},
		{
			name:    "a target passed: by how much",
			targets: []testTarget{{"2026-10", food, 500}},
			keys:    "B",
			shows:   []string{"Left", "-5.00 EUR", "of 5.00 · 5.00 over"},
			hides:   []string{"5.00 left"},
		},
		{
			name:    "a target for a category nothing was spent on",
			targets: []testTarget{{"2026-10", rent, 90000}},
			keys:    "B",
			shows:   []string{"Rent", "of 900.00 · 900.00 left"},
		},
		{
			name:    "the targets of another month do not count",
			targets: []testTarget{{"2026-09", rent, 90000}},
			keys:    "B",
			shows:   []string{"Spending", "Press e to set a target"},
			hides:   []string{"Rent", "of 900.00"},
		},
		{
			name:    "that month shows its own",
			targets: []testTarget{{"2026-09", rent, 70000}},
			keys:    "B down",
			shows:   []string{"Rent", "of 700.00 · 100.00 over"},
		},
		{
			name:    "all time has no targets",
			targets: []testTarget{{"2026-10", food, 30000}},
			keys:    "B g",
			shows:   []string{"Spending", "Groceries"},
			hides:   []string{"Left", "of 300.00", "Press e"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := targetModel(t, tc.targets...)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			// when
			play(m, tc.keys)
			view := m.View()

			// then
			checkView(t, view, tc.shows, tc.hides)
		})
	}
}

func Test_targetPopup_view(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
	}{
		{
			name:  "the categories with their targets, and the amount under them",
			keys:  "B e",
			shows: []string{"Targets for Oct 2026", "Groceries", "Rent", "Target", "In EUR, the base currency", "enter save"},
		},
		{
			name:  "a target shows beside its category",
			keys:  "B e down",
			shows: []string{"Rent", "900.00"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := targetModel(t, testTarget{"2026-10", "Expenses:Rent", 90000})

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

// withColor makes lipgloss write colours, as a terminal would, for one test.
func withColor(t *testing.T) {
	t.Helper()
	was := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(was) })
}

// A category that passed its target is in red: a full red bar and the amount over. One
// that did not is not.
func Test_budgetTargets_red(t *testing.T) {
	tcs := []struct {
		name   string
		target int64
		want   bool
	}{
		{name: "passed", target: 500, want: true},
		{name: "exactly reached", target: 1000, want: false},
		{name: "not reached", target: 30000, want: false},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			withColor(t)
			m, _ := targetModel(t, testTarget{"2026-10", "Expenses:Groceries", tc.target})
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			// when
			press(m, "B")
			view := m.View()

			// then
			redBar := warn.Render(strings.Repeat("█", 10))
			if got := strings.Contains(view, redBar); got != tc.want {
				t.Errorf("a full red bar: got %v, want %v\n%s", got, tc.want, view)
			}
		})
	}
}

func Test_Update_copyTargets(t *testing.T) {
	type observed struct {
		Mode          string
		Status        string
		StatusIsError bool
		October       []core.BudgetTarget
		September     []core.BudgetTarget
	}
	food, rent := "Expenses:Groceries", "Expenses:Rent"
	both := []testTarget{{"2026-09", food, 10000}, {"2026-09", rent, 90000}}
	septemberKept := []core.BudgetTarget{{Category: food, Amount: eur(10000)}, {Category: rent, Amount: eur(90000)}}
	tcs := []struct {
		name    string
		targets []testTarget
		keys    string
		want    observed
	}{
		{
			name: "the targets of last month into this one", targets: both, keys: "B c",
			want: observed{Mode: "list", Status: "2 targets copied from Sep 2026 into Oct 2026 · z undoes", October: septemberKept, September: septemberKept},
		},
		{
			name: "one target says so in the singular", targets: both[:1], keys: "B c",
			want: observed{Mode: "list", Status: "1 target copied from Sep 2026 into Oct 2026 · z undoes",
				October: septemberKept[:1], September: septemberKept[:1]},
		},
		{
			name: "a target this month has is kept", targets: append([]testTarget{{"2026-10", food, 5000}}, both...), keys: "B c",
			want: observed{Mode: "list", Status: "1 target copied from Sep 2026 into Oct 2026 · 1 was already set, kept · z undoes",
				October:   []core.BudgetTarget{{Category: food, Amount: eur(5000)}, {Category: rent, Amount: eur(90000)}},
				September: septemberKept},
		},
		{
			name: "every target already set", targets: append([]testTarget{{"2026-10", food, 5000}, {"2026-10", rent, 7000}}, both...), keys: "B c",
			want: observed{Mode: "list", Status: "nothing to copy: every target of Sep 2026 is already set in Oct 2026", StatusIsError: true,
				October:   []core.BudgetTarget{{Category: food, Amount: eur(5000)}, {Category: rent, Amount: eur(7000)}},
				September: septemberKept},
		},
		{
			name: "a month before with no targets", keys: "B c",
			want: observed{Mode: "list", Status: "Sep 2026 has no targets to copy", StatusIsError: true},
		},
		{
			name: "from the month before the one on show", targets: both, keys: "B down c",
			want: observed{Mode: "list", Status: "Aug 2026 has no targets to copy", StatusIsError: true, September: septemberKept},
		},
		{
			name: "all time has no targets", targets: both, keys: "B g c",
			want: observed{Mode: "list", Status: "choose a month first: a target is for one month", StatusIsError: true, September: septemberKept},
		},
		{
			name: "z takes back what was copied", targets: both, keys: "B c z",
			want: observed{Mode: "list", Status: "Targets copied into Oct 2026 taken back", September: septemberKept},
		},
		{
			name: "z leaves the targets the month had", targets: append([]testTarget{{"2026-10", food, 5000}}, both...), keys: "B c z",
			want: observed{Mode: "list", Status: "Targets copied into Oct 2026 taken back",
				October: []core.BudgetTarget{{Category: food, Amount: eur(5000)}}, September: septemberKept},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := targetModel(t, tc.targets...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, StatusIsError: m.statusIsError}
			var err error
			if got.October, err = st.BudgetTargets(t.Context(), "2026-10"); err != nil {
				t.Fatal(err)
			}
			if got.September, err = st.BudgetTargets(t.Context(), "2026-09"); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// rolloverModel is a targetModel where some categories carry over.
func rolloverModel(t *testing.T, targets []testTarget, rollovers ...core.BudgetRollover) (*Model, *core.Local) {
	t.Helper()
	m, st := targetModel(t, targets...)
	for _, r := range rollovers {
		if err := st.SetBudgetRollover(t.Context(), r.Category, r.From); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	return m, st
}

func Test_Update_targetRollover(t *testing.T) {
	type observed struct {
		Mode          string
		Status        string
		Pending       bool // the carry-over the popup will save, when it is open
		Rollovers     []core.BudgetRollover
		OctoberTarget []core.BudgetTarget
	}
	food := "Expenses:Groceries"
	on := []core.BudgetRollover{{Category: food, From: "2026-10"}}
	tcs := []struct {
		name      string
		targets   []testTarget
		rollovers []core.BudgetRollover
		keys      string
		want      observed
	}{
		{
			name: "tab switches it on in the popup, and nothing is saved yet",
			keys: "B e tab",
			want: observed{Mode: "target", Pending: true},
		},
		{
			name: "enter saves it from the month on show",
			keys: "B e tab enter",
			want: observed{Mode: "list", Status: "Carry-over on for Groceries from Oct 2026 · z undoes", Rollovers: on},
		},
		{
			name: "tab twice is no change, and an empty amount still has nothing to clear",
			keys: "B e tab tab enter",
			want: observed{Mode: "target"},
		},
		{
			name:      "a category that carries over starts switched on, and tab switches it off",
			rollovers: on,
			keys:      "B e tab enter",
			want:      observed{Mode: "list", Status: "Carry-over off for Groceries · z undoes"},
		},
		{
			name:      "the popup shows it on",
			rollovers: on,
			keys:      "B e",
			want:      observed{Mode: "target", Pending: true, Rollovers: on},
		},
		{
			name: "a target and the carry-over together",
			keys: "B e text:300 tab enter",
			want: observed{Mode: "list", Status: "Groceries: target 300.00 EUR · carry-over on from Oct 2026 · z undoes", Rollovers: on,
				OctoberTarget: []core.BudgetTarget{{Category: food, Amount: eur(30000)}}},
		},
		{
			name:      "a carry-over that starts later is moved to the month on show",
			rollovers: []core.BudgetRollover{{Category: food, From: "2026-11"}},
			keys:      "B e tab enter",
			want:      observed{Mode: "list", Status: "Carry-over on for Groceries from Oct 2026 · z undoes", Rollovers: on},
		},
		{
			name:      "z puts the start back",
			rollovers: []core.BudgetRollover{{Category: food, From: "2026-11"}},
			keys:      "B e tab enter z",
			want:      observed{Mode: "list", Status: "Carry-over for Groceries taken back", Rollovers: []core.BudgetRollover{{Category: food, From: "2026-11"}}},
		},
		{
			name: "z takes a new carry-over back",
			keys: "B e tab enter z",
			want: observed{Mode: "list", Status: "Carry-over for Groceries taken back"},
		},
		{
			name:      "z puts a carry-over switched off back",
			rollovers: on,
			keys:      "B e tab enter z",
			want:      observed{Mode: "list", Status: "Carry-over for Groceries taken back", Rollovers: on},
		},
		{
			name: "after a target and a carry-over, z takes back the carry-over first",
			keys: "B e text:300 tab enter z",
			want: observed{Mode: "list", Status: "Carry-over for Groceries taken back",
				OctoberTarget: []core.BudgetTarget{{Category: food, Amount: eur(30000)}}},
		},
		{
			name: "and a second z the target",
			keys: "B e text:300 tab enter z z",
			want: observed{Mode: "list", Status: "Target for Groceries in Oct 2026 taken back"},
		},
		{
			name:      "enter on what is there changes nothing and says nothing",
			targets:   []testTarget{{"2026-10", food, 30000}},
			rollovers: on,
			keys:      "B e enter",
			want:      observed{Mode: "list", Rollovers: on, OctoberTarget: []core.BudgetTarget{{Category: food, Amount: eur(30000)}}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := rolloverModel(t, tc.targets, tc.rollovers...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status}
			if m.tgt != nil {
				got.Pending = m.tgt.rollover
			}
			var err error
			if got.Rollovers, err = st.BudgetRollovers(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got.OctoberTarget, err = st.BudgetTargets(t.Context(), "2026-10"); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// What the month shows of a category that carries over.
func Test_budgetRollover_view(t *testing.T) {
	food := "Expenses:Groceries"
	from := core.BudgetRollover{Category: food, From: "2026-09"}
	tcs := []struct {
		name    string
		targets []testTarget
		keys    string
		shows   []string
		hides   []string
	}{
		{
			name:    "what was left comes in",
			targets: []testTarget{{"2026-09", food, 10000}, {"2026-10", food, 5000}},
			keys:    "B",
			shows:   []string{"Carried in", "+70.00 EUR", "of 50.00 · +70.00 carried · 110.00 left", "Left", "110.00 EUR"},
		},
		{
			name:    "what was overspent comes in as a negative",
			targets: []testTarget{{"2026-09", food, 2000}, {"2026-10", food, 5000}},
			keys:    "B",
			shows:   []string{"Carried in", "-10.00 EUR", "of 50.00 · -10.00 carried · 30.00 left"},
		},
		{
			name:    "a month with no target still has what came in",
			targets: []testTarget{{"2026-09", food, 10000}},
			keys:    "B",
			shows:   []string{"Targets", "Groceries", "+70.00 carried · 60.00 left"},
			hides:   []string{"of 100", "of 50"},
		},
		{
			name:    "the first month carries nothing in",
			targets: []testTarget{{"2026-09", food, 10000}, {"2026-10", food, 5000}},
			keys:    "B down",
			shows:   []string{"of 100.00 · 70.00 left"},
			hides:   []string{"carried", "Carried in"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := rolloverModel(t, tc.targets, from)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// The categories of the Budget can be walked, and each opens the transactions behind its
// number.
func Test_Update_budgetCategories(t *testing.T) {
	type observed struct {
		Screen       string
		Period       string
		CatsFocused  bool
		Selected     string
		OnlyCategory string
		Rows         []string
		Status       string
	}
	uncat, food := ledger.UncategorizedExpense, "Expenses:Groceries"
	tcs := []struct {
		name    string
		targets []testTarget
		width   int
		keys    string
		shrunk  int    // a width the terminal then shrinks to
		then    string // and the keys pressed after that
		want    observed
	}{
		{
			name: "tab moves into the categories, on the largest",
			keys: "B tab",
			want: observed{Screen: "budget", CatsFocused: true, Selected: uncat},
		},
		{
			name: "down moves to the next",
			keys: "B tab down",
			want: observed{Screen: "budget", CatsFocused: true, Selected: food},
		},
		{
			name: "up and down stay among the categories, not the months",
			keys: "B tab down up",
			want: observed{Screen: "budget", CatsFocused: true, Selected: uncat},
		},
		{
			name: "tab again goes back to the months",
			keys: "B tab tab",
			want: observed{Screen: "budget"},
		},
		{
			name: "esc goes back to the months",
			keys: "B tab esc",
			want: observed{Screen: "budget"},
		},
		{
			name: "enter shows the transactions behind the category, in the month",
			keys: "B tab down enter",
			want: observed{Screen: "ledger", OnlyCategory: food, Rows: []string{"2026-10-01 REWE -10.00"}, Status: "1 transaction in Groceries · esc shows all"},
		},
		{
			name: "a category with several, newest first",
			keys: "B tab enter",
			want: observed{Screen: "ledger", OnlyCategory: uncat,
				Rows:   []string{"2026-10-03 Kino -12.00", "2026-10-02 REWE -20.00"},
				Status: "2 transactions in Uncategorized · esc shows all"},
		},
		{
			name: "another month has its own",
			keys: "B down tab down enter",
			want: observed{Screen: "ledger", Period: "2026-09", OnlyCategory: food, Rows: []string{"2026-09-15 Edeka -30.00"}, Status: "1 transaction in Groceries · esc shows all"},
		},
		{
			name: "all time has all of them",
			keys: "B g tab down enter",
			want: observed{Screen: "ledger", OnlyCategory: food, Rows: []string{"2026-10-01 REWE -10.00", "2026-09-15 Edeka -30.00"}, Status: "2 transactions in Groceries · esc shows all"},
		},
		{
			name: "esc in the ledger shows everything again",
			keys: "B tab down enter esc",
			want: observed{Screen: "ledger", Rows: []string{"2026-10-04 to savings -5.00", "2026-10-03 Kino -12.00", "2026-10-02 REWE -20.00", "2026-10-01 REWE -10.00"}},
		},
		{
			name:    "the categories with money set aside come first",
			targets: []testTarget{{"2026-10", food, 5000}},
			keys:    "B tab",
			want:    observed{Screen: "budget", CatsFocused: true, Selected: food},
		},
		{
			name: "an account that was selected does not narrow the category",
			keys: "L 2 down down B tab down enter",
			want: observed{Screen: "ledger", OnlyCategory: food, Rows: []string{"2026-10-01 REWE -10.00"}, Status: "1 transaction in Groceries · esc shows all"},
		},
		{
			name:   "if the terminal shrinks to one column the months have the focus again",
			keys:   "B tab",
			shrunk: 60,
			then:   "down",
			want:   observed{Screen: "budget", Period: "2026-09"},
		},
		{
			name:  "with one column there are no categories to move into",
			width: 60,
			keys:  "B tab",
			want:  observed{Screen: "budget"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := targetModel(t, tc.targets...)
			if tc.width > 0 {
				m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 20})
			}

			// when
			play(m, tc.keys)
			if tc.shrunk > 0 {
				m.Update(tea.WindowSizeMsg{Width: tc.shrunk, Height: 20})
				play(m, tc.then)
			}

			// then
			if tc.want.Period == "" && !strings.Contains(tc.keys, "B g") { // October unless the case says otherwise
				tc.want.Period = "2026-10"
			}
			got := observed{Screen: screenNames2[m.screen], Period: m.period, OnlyCategory: m.onlyCategory, Status: m.status}
			if m.screen == screenBudget && m.catsFocused() {
				got.CatsFocused, got.Selected = true, m.budgetSelected()
			}
			if m.onlyCategory != "" || strings.HasSuffix(tc.keys, "enter esc") {
				got.Rows = visibleRows(m)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A long list of categories scrolls to keep the selected one in view.
func Test_budgetCategories_scroll(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{name: "at first the largest shows, marked", keys: "B tab", shows: []string{"▸ C01"}, hides: []string{"C24"}},
		{name: "at the end the last shows, marked, and the first is gone", keys: "B tab G", shows: []string{"▸ C24"}, hides: []string{"C01"}},
		{name: "in the middle the selected shows", keys: "B tab down down down down down down down down down down", shows: []string{"▸ C11"}},
		{name: "without the focus nothing is marked", keys: "B", hides: []string{"▸"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			for i := 1; i <= 24; i++ {
				book(t, st, "2026-10-02", "shop", "Assets:Bank", fmt.Sprintf("Expenses:C%02d", i), -int64((30-i)*100))
			}
			m := modelOn(t, st, 100, 20)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// The Home says what is left of this month's plan, whichever month the Budget shows.
func Test_homeLeftThisMonth(t *testing.T) {
	food := "Expenses:Groceries"
	tcs := []struct {
		name      string
		targets   []testTarget
		rollovers []core.BudgetRollover
		keys      string
		shows     []string
		hides     []string
	}{
		{
			name:    "a target not reached: what is left",
			targets: []testTarget{{"2026-10", food, 5000}},
			keys:    "L",
			shows:   []string{"Left this month", "40.00 EUR"},
		},
		{
			name:    "a target passed: a negative amount",
			targets: []testTarget{{"2026-10", food, 500}},
			keys:    "L",
			shows:   []string{"Left this month", "-5.00 EUR"},
		},
		{
			name:    "it adds up the targets of all the categories",
			targets: []testTarget{{"2026-10", food, 5000}, {"2026-10", "Expenses:Rent", 90000}},
			keys:    "L",
			shows:   []string{"Left this month", "940.00 EUR"},
		},
		{
			name:      "what was carried in counts",
			targets:   []testTarget{{"2026-09", food, 10000}},
			rollovers: []core.BudgetRollover{{Category: food, From: "2026-09"}},
			keys:      "L",
			shows:     []string{"Left this month", "60.00 EUR"},
		},
		{
			name:    "the current month, not the one the Budget shows",
			targets: []testTarget{{"2026-10", food, 5000}, {"2026-09", food, 100}},
			keys:    "B down L",
			shows:   []string{"Left this month", "40.00 EUR"},
		},
		{
			name:    "still the current month after the data is loaded again",
			targets: []testTarget{{"2026-10", food, 5000}, {"2026-09", food, 100}},
			keys:    "B down ctrl+l enter L",
			shows:   []string{"Left this month", "40.00 EUR"},
		},
		{
			name:    "no plan for this month: nothing is said",
			targets: []testTarget{{"2026-09", food, 5000}},
			keys:    "L",
			hides:   []string{"Left this month"},
		},
		{
			name:  "no targets at all: nothing is said",
			keys:  "L",
			hides: []string{"Left this month"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := rolloverModel(t, tc.targets, tc.rollovers...)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// What is left is in red when it is below zero.
func Test_homeLeftThisMonth_red(t *testing.T) {
	tcs := []struct {
		name   string
		target int64
		want   bool
	}{
		{name: "below zero", target: 500, want: true},
		{name: "exactly nothing", target: 1000, want: false},
		{name: "some left", target: 5000, want: false},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			withColor(t)
			m, _ := rolloverModel(t, []testTarget{{"2026-10", "Expenses:Groceries", tc.target}})
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			// when
			press(m, "L")
			view := m.View()

			// then
			if got := strings.Contains(view, bold.Render(warn.Render(m.loc.Money(m.thisMonth.LeftTotal)))); got != tc.want {
				t.Errorf("the amount in red: got %v, want %v\n%s", got, tc.want, view)
			}
		})
	}
}
