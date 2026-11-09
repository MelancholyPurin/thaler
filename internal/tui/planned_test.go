package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
)

// plannedModel is a model on newModel's ledger with 2900.00 of salary received on 2
// October, so that there is an Income category to plan. The targets are those given.
func plannedModel(t *testing.T, targets ...testTarget) (*Model, *core.Local) {
	t.Helper()
	m, st := newModel(t)
	book(t, st, "2026-10-02", "Employer", "Assets:Bank", "Income:Salary", 290000)
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

func Test_Update_plannedIncome(t *testing.T) {
	type observed struct {
		Mode      string
		Status    string
		Amount    string // what is in the amount, when the popup is open
		October   []core.BudgetTarget
		Rollovers []core.BudgetRollover
	}
	food, salary := "Expenses:Groceries", "Income:Salary"
	tcs := []struct {
		name    string
		targets []testTarget
		keys    string
		want    observed
	}{
		{
			name: "income comes after the spending categories",
			keys: "B e down",
			want: observed{Mode: "target"},
		},
		{
			name: "a plan for an income category",
			keys: "B e down text:3000 enter",
			want: observed{Mode: "list", Status: "Target for Salary in Oct 2026: 3,000.00 EUR · z undoes",
				October: []core.BudgetTarget{{Category: salary, Amount: eur(300000)}}},
		},
		{
			name:    "the amount is the plan there is",
			targets: []testTarget{{"2026-10", salary, 300000}},
			keys:    "B e down",
			want: observed{Mode: "target", Amount: "3,000.00",
				October: []core.BudgetTarget{{Category: salary, Amount: eur(300000)}}},
		},
		{
			name:    "an empty amount clears the plan",
			targets: []testTarget{{"2026-10", salary, 300000}},
			keys:    "B e down ctrl+u enter",
			want:    observed{Mode: "list", Status: "Target for Salary in Oct 2026 cleared · z undoes"},
		},
		{
			name:    "tab changes nothing for income: there is nothing to carry",
			targets: []testTarget{{"2026-10", salary, 300000}},
			keys:    "B e down tab ctrl+u text:2500 enter",
			want: observed{Mode: "list", Status: "Target for Salary in Oct 2026: 2,500.00 EUR · z undoes",
				October: []core.BudgetTarget{{Category: salary, Amount: eur(250000)}}},
		},
		{
			name:    "z puts the old plan back",
			targets: []testTarget{{"2026-10", salary, 300000}},
			keys:    "B e down ctrl+u text:2500 enter z",
			want: observed{Mode: "list", Status: "Target for Salary in Oct 2026 taken back",
				October: []core.BudgetTarget{{Category: salary, Amount: eur(300000)}}},
		},
		{
			name:    "a spending category still carries over, beside the plan",
			targets: []testTarget{{"2026-10", salary, 300000}},
			keys:    "B e tab text:300 enter",
			want: observed{Mode: "list", Status: "Groceries: target 300.00 EUR · carry-over on from Oct 2026 · z undoes",
				October:   []core.BudgetTarget{{Category: food, Amount: eur(30000)}, {Category: salary, Amount: eur(300000)}},
				Rollovers: []core.BudgetRollover{{Category: food, From: "2026-10"}}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := plannedModel(t, tc.targets...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status}
			if m.tgt != nil {
				got.Amount = m.tgt.amount.Value()
			}
			var err error
			if got.October, err = st.BudgetTargets(t.Context(), "2026-10"); err != nil {
				t.Fatal(err)
			}
			if got.Rollovers, err = st.BudgetRollovers(t.Context()); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_targetPopup_income(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "a spending category: the target and the carry-over, no plan",
			keys:  "B e",
			shows: []string{"Groceries", "Salary +", "Target", "Carry over"},
			hides: []string{"Planned"},
		},
		{
			name:  "an income category is marked +, is planned rather than targeted, and carries nothing",
			keys:  "B e down",
			shows: []string{"Groceries", "Salary +", "Planned"},
			hides: []string{"Carry over"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := plannedModel(t)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// The budget of a month with planned income shows each plan against what came in, and
// what of the income is not yet given to a target.
func Test_budgetPlannedIncome(t *testing.T) {
	food, salary := "Expenses:Groceries", "Income:Salary"
	tcs := []struct {
		name    string
		targets []testTarget
		keys    string
		shows   []string
		hides   []string
	}{
		{
			name:    "the plan, the targets and what is unassigned",
			targets: []testTarget{{"2026-10", salary, 300000}, {"2026-10", food, 30000}},
			keys:    "B",
			shows:   []string{"Planned income", "3,000.00 EUR", "Unassigned", "2,700.00 EUR", "Salary", "of 3,000.00 · 100.00 still to come"},
			hides:   []string{"above plan"},
		},
		{
			name:    "income past the plan says by how much",
			targets: []testTarget{{"2026-10", salary, 250000}},
			keys:    "B",
			shows:   []string{"Planned income", "2,500.00 EUR", "Unassigned", "of 2,500.00 · 400.00 above plan"},
			hides:   []string{"still to come"},
		},
		{
			name:    "targets over the plan leave a negative amount unassigned",
			targets: []testTarget{{"2026-10", salary, 20000}, {"2026-10", food, 30000}},
			keys:    "B",
			shows:   []string{"Planned income", "200.00 EUR", "Unassigned", "-100.00 EUR"},
		},
		{
			name:  "no plan: the income as before, and nothing unassigned",
			keys:  "B",
			shows: []string{"Income", "Salary"},
			hides: []string{"Planned income", "Unassigned"},
		},
		{
			name:    "the plan of another month does not count",
			targets: []testTarget{{"2026-09", salary, 300000}},
			keys:    "B",
			shows:   []string{"Income", "Salary"},
			hides:   []string{"Planned income", "Unassigned"},
		},
		{
			name:    "all time has no plan",
			targets: []testTarget{{"2026-10", salary, 300000}},
			keys:    "B g",
			shows:   []string{"Income", "Salary"},
			hides:   []string{"Planned income", "Unassigned"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := plannedModel(t, tc.targets...)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// What is unassigned is in red when the targets add up to more than the plan, and
// only then.
func Test_budgetPlannedIncome_red(t *testing.T) {
	tcs := []struct {
		name string
		plan int64
		want bool
	}{
		{name: "targets over the plan", plan: 20000, want: true},
		{name: "exactly used", plan: 30000, want: false},
		{name: "something left", plan: 300000, want: false},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			withColor(t)
			m, _ := plannedModel(t, testTarget{"2026-10", "Income:Salary", tc.plan}, testTarget{"2026-10", "Expenses:Groceries", 30000})
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			// when
			press(m, "B")
			view := m.View()

			// then
			unassigned := warn.Render(m.loc.Money(m.monthly.Unassigned))
			if got := strings.Contains(view, unassigned); got != tc.want {
				t.Errorf("unassigned in red: got %v, want %v\n%s", got, tc.want, view)
			}
		})
	}
}
