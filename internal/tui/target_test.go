package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
	"github.com/muesli/termenv"

	"github.com/melancholypurin/thaler/internal/core"
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
			shows: []string{"Spending", "Groceries", "Uncategorized", "Press e to set a monthly target"},
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
			shows:   []string{"Spending", "Press e to set a monthly target"},
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
