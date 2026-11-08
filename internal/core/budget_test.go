package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// target sets a budget target as the service is asked to.
func target(month, category string, amount ledger.Money) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if err := st.SetBudgetTarget(t.Context(), month, category, amount); err != nil {
			t.Fatalf("SetBudgetTarget(%s, %s): %v", month, category, err)
		}
	}
}

// baseCurrency changes the currency reports and targets are in.
func baseCurrency(c ledger.Currency) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if err := st.SetBaseCurrency(t.Context(), c); err != nil {
			t.Fatal(err)
		}
	}
}

func targetLedger() []given {
	return []given{accounts("Assets:Bank EUR", "Expenses:Food", "Expenses:Rent", "Income:Salary", ledger.UncategorizedExpense)}
}

func Test_SetBudgetTarget(t *testing.T) {
	tcs := []struct {
		name     string
		setup    []given
		month    string
		category string
		amount   ledger.Money
		want     []BudgetTarget // the targets of 2026-10 afterwards
		wantErr  string
	}{
		{
			name:  "a target for a category",
			month: "2026-10", category: "Expenses:Food", amount: eur(40000),
			want: []BudgetTarget{{"Expenses:Food", eur(40000)}},
		},
		{
			name:  "a second target replaces the first",
			setup: []given{target("2026-10", "Expenses:Food", eur(40000))},
			month: "2026-10", category: "Expenses:Food", amount: eur(45050),
			want: []BudgetTarget{{"Expenses:Food", eur(45050)}},
		},
		{
			name:  "categories are listed by name",
			setup: []given{target("2026-10", "Expenses:Rent", eur(90000))},
			month: "2026-10", category: "Expenses:Food", amount: eur(40000),
			want: []BudgetTarget{{"Expenses:Food", eur(40000)}, {"Expenses:Rent", eur(90000)}},
		},
		{
			name:  "another month keeps its own target",
			setup: []given{target("2026-09", "Expenses:Food", eur(30000))},
			month: "2026-10", category: "Expenses:Food", amount: eur(40000),
			want: []BudgetTarget{{"Expenses:Food", eur(40000)}},
		},
		{
			name:  "in a new base currency",
			setup: []given{baseCurrency("USD")},
			month: "2026-10", category: "Expenses:Food", amount: usd(40000),
			want: []BudgetTarget{{"Expenses:Food", usd(40000)}},
		},
		{
			name:  "a category that does not exist",
			month: "2026-10", category: "Expenses:Nope", amount: eur(40000),
			wantErr: "account does not exist: Expenses:Nope",
		},
		{
			name:  "an account that is not a category",
			month: "2026-10", category: "Assets:Bank", amount: eur(40000),
			wantErr: "Assets:Bank is not a spending category: only an Expenses category, other than Uncategorized, can have a target",
		},
		{
			name:  "income has no target",
			month: "2026-10", category: "Income:Salary", amount: eur(40000),
			wantErr: "Income:Salary is not a spending category: only an Expenses category, other than Uncategorized, can have a target",
		},
		{
			name:  "uncategorized has no target",
			month: "2026-10", category: ledger.UncategorizedExpense, amount: eur(40000),
			wantErr: "Expenses:Uncategorized is not a spending category: only an Expenses category, other than Uncategorized, can have a target",
		},
		{
			name:  "zero",
			month: "2026-10", category: "Expenses:Food", amount: eur(0),
			wantErr: "amount must be greater than zero",
		},
		{
			name:  "negative",
			setup: []given{target("2026-10", "Expenses:Food", eur(40000))},
			month: "2026-10", category: "Expenses:Food", amount: eur(-1),
			want:    []BudgetTarget{{"Expenses:Food", eur(40000)}}, // the old target stays
			wantErr: "amount must be greater than zero",
		},
		{
			name:  "not in the base currency",
			month: "2026-10", category: "Expenses:Food", amount: usd(40000),
			wantErr: "a target is in the base currency (EUR), not in USD",
		},
		{
			name:  "a month with one digit",
			month: "2026-1", category: "Expenses:Food", amount: eur(40000),
			wantErr: `invalid month "2026-1": use YYYY-MM`,
		},
		{
			name:  "month thirteen",
			month: "2026-13", category: "Expenses:Food", amount: eur(40000),
			wantErr: `invalid month "2026-13": use YYYY-MM`,
		},
		{
			name:  "a day is not a month",
			month: "2026-10-01", category: "Expenses:Food", amount: eur(40000),
			wantErr: `invalid month "2026-10-01": use YYYY-MM`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(targetLedger(), tc.setup...)...)

			// when
			err := st.SetBudgetTarget(t.Context(), tc.month, tc.category, tc.amount)

			// then
			checkErr(t, err, tc.wantErr, nil)
			got, err := st.BudgetTargets(t.Context(), "2026-10")
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("targets mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ClearBudgetTarget(t *testing.T) {
	both := []given{target("2026-10", "Expenses:Food", eur(40000)), target("2026-10", "Expenses:Rent", eur(90000)), target("2026-09", "Expenses:Food", eur(30000))}
	tcs := []struct {
		name      string
		setup     []given
		month     string
		category  string
		wantMonth []BudgetTarget // the targets of the month afterwards
		wantSep   []BudgetTarget // the targets of September afterwards
		wantErr   string
	}{
		{
			name:  "one target goes, the others stay",
			setup: both, month: "2026-10", category: "Expenses:Food",
			wantMonth: []BudgetTarget{{"Expenses:Rent", eur(90000)}},
			wantSep:   []BudgetTarget{{"Expenses:Food", eur(30000)}},
		},
		{
			name:  "a category with no target is left as it is",
			setup: both, month: "2026-08", category: "Expenses:Food",
			wantSep:   []BudgetTarget{{"Expenses:Food", eur(30000)}},
			wantMonth: []BudgetTarget{{"Expenses:Food", eur(40000)}, {"Expenses:Rent", eur(90000)}},
		},
		{
			name:  "a category that does not exist",
			setup: both, month: "2026-10", category: "Expenses:Nope",
			wantMonth: []BudgetTarget{{"Expenses:Food", eur(40000)}, {"Expenses:Rent", eur(90000)}},
			wantSep:   []BudgetTarget{{"Expenses:Food", eur(30000)}},
			wantErr:   "account does not exist: Expenses:Nope",
		},
		{
			name:  "a month that is none",
			setup: both, month: "October", category: "Expenses:Food",
			wantMonth: []BudgetTarget{{"Expenses:Food", eur(40000)}, {"Expenses:Rent", eur(90000)}},
			wantSep:   []BudgetTarget{{"Expenses:Food", eur(30000)}},
			wantErr:   `invalid month "October": use YYYY-MM`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(targetLedger(), tc.setup...)...)

			// when
			err := st.ClearBudgetTarget(t.Context(), tc.month, tc.category)

			// then
			checkErr(t, err, tc.wantErr, nil)
			gotOct, errOct := st.BudgetTargets(t.Context(), "2026-10")
			gotSep, errSep := st.BudgetTargets(t.Context(), "2026-09")
			if errOct != nil || errSep != nil {
				t.Fatal(errOct, errSep)
			}
			if diff := cmp.Diff(tc.wantMonth, gotOct, equalEmpty); diff != "" {
				t.Errorf("October mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantSep, gotSep, equalEmpty); diff != "" {
				t.Errorf("September mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_BudgetTargets(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		month   string
		want    []BudgetTarget
		wantErr string
	}{
		{name: "no targets", month: "2026-10"},
		{
			name:  "only the month asked for",
			setup: []given{target("2026-10", "Expenses:Rent", eur(90000)), target("2026-11", "Expenses:Food", eur(1)), target("2026-10", "Expenses:Food", eur(40000))},
			month: "2026-10",
			want:  []BudgetTarget{{"Expenses:Food", eur(40000)}, {"Expenses:Rent", eur(90000)}},
		},
		{name: "a month that is none", month: "2026-00", wantErr: `invalid month "2026-00": use YYYY-MM`},
		{name: "no month at all", wantErr: `invalid month "": use YYYY-MM`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(targetLedger(), tc.setup...)...)

			// when
			got, err := st.BudgetTargets(t.Context(), tc.month)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("targets mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
