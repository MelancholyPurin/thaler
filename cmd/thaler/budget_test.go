package main

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

var (
	spendGroceries = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-120", "-t", "Expenses:Groceries", "-d", "2026-10-03"}
	spendRent      = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-800", "-t", "Expenses:Rent", "-d", "2026-10-01"}
	spendKiosk     = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-32.50", "-d", "2026-10-09"}
	spendSeptember = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-30", "-t", "Expenses:Groceries", "-d", "2026-09-12"}
	targetFood     = []string{"cli", "budget", "set", "Expenses:Groceries", "400", "--month", "2026-10"}
	targetRent     = []string{"cli", "budget", "set", "Expenses:Rent", "900", "--month", "2026-10"}
)

func Test_budget(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "nothing yet",
			setup: [][]string{addBank},
			args:  []string{"cli", "budget", "--month", "2026-10"},
			want:  "Nothing spent and no targets in 2026-10. Set one with: thaler cli budget set Expenses:Groceries 400 --month 2026-10\n",
		},
		{
			name:  "spending and no targets",
			setup: [][]string{addBank, spendGroceries, spendRent},
			args:  []string{"cli", "budget", "--month", "2026-10"},
			want:  "Budget for 2026-10, in EUR\n\nCategory             Spent\nExpenses:Rent       800.00\nExpenses:Groceries  120.00\nTotal               920.00\n",
		},
		{
			name:  "targets: spent, target and what is left, and the others apart",
			setup: [][]string{addBank, spendGroceries, spendRent, spendKiosk, targetFood, targetRent},
			args:  []string{"cli", "budget", "--month", "2026-10"},
			want: "Budget for 2026-10, in EUR\n\n" +
				"Category                 Spent   Target    Left\n" +
				"Expenses:Rent           800.00   900.00  100.00\n" +
				"Expenses:Groceries      120.00   400.00  280.00\n" +
				"Expenses:Uncategorized   32.50\n" +
				"Total                   952.50  1300.00  380.00\n" +
				"\nLeft counts the categories with a target or something carried in; 32.50 was spent on the others.\n",
		},
		{
			name:  "a target passed is a negative amount left",
			setup: [][]string{addBank, spendGroceries, {"cli", "budget", "set", "Expenses:Groceries", "100", "--month", "2026-10"}},
			args:  []string{"cli", "budget", "-m", "2026-10"},
			want: "Budget for 2026-10, in EUR\n\n" +
				"Category             Spent  Target    Left\n" +
				"Expenses:Groceries  120.00  100.00  -20.00\n" +
				"Total               120.00  100.00  -20.00\n",
		},
		{
			name:  "this month when none is given",
			setup: [][]string{addBank, spendGroceries, targetFood},
			args:  []string{"cli", "budget"},
			want: "Budget for 2026-10, in EUR\n\n" +
				"Category             Spent  Target    Left\n" +
				"Expenses:Groceries  120.00  400.00  280.00\n" +
				"Total               120.00  400.00  280.00\n",
		},
		{
			name:  "another month has its own spending and targets",
			setup: [][]string{addBank, spendGroceries, spendSeptember, targetFood},
			args:  []string{"cli", "budget", "--month", "2026-09"},
			want: "Budget for 2026-09, in EUR\n\n" +
				"Category            Spent\n" +
				"Expenses:Groceries  30.00\n" +
				"Total               30.00\n",
		},
		{
			name:  "spending in dollars with no rate is named and left out",
			setup: [][]string{addBank, addDollars, spendGroceries, {"cli", "add", "-a", "Assets:Dollars", "--amount=-10", "-t", "Expenses:Groceries", "-d", "2026-10-04"}},
			args:  []string{"cli", "budget", "--month", "2026-10"},
			want: "Budget for 2026-10, in EUR\n\n" +
				"Category             Spent\n" +
				"Expenses:Groceries  120.00\n" +
				"Total               120.00\n" +
				"warning: no exchange rate USD -> EUR, so USD is left out; add one with: thaler cli rate set USD EUR <rate>\n",
		},
		{name: "a month that is none", setup: [][]string{addBank}, args: []string{"cli", "budget", "--month", "October"}, wantErr: `invalid month "October": use YYYY-MM`},
		{name: "arguments are not expected", setup: [][]string{addBank}, args: []string{"cli", "budget", "Expenses:Food"}, wantErr: `unknown command "Expenses:Food" for "thaler cli budget"`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_budget_set(t *testing.T) {
	shown := []string{"cli", "budget", "--month", "2026-10"}
	tcs := []struct {
		name    string
		setup   [][]string
		args    []string
		want    string
		wantErr string
		after   string // what the month shows afterwards, when it matters
	}{
		{
			name:  "a target",
			setup: [][]string{addBank, spendGroceries},
			args:  targetFood,
			want:  "Target for Expenses:Groceries in 2026-10: 400.00 EUR\n",
			after: "Budget for 2026-10, in EUR\n\nCategory             Spent  Target    Left\nExpenses:Groceries  120.00  400.00  280.00\nTotal               120.00  400.00  280.00\n",
		},
		{
			name:  "a target with a decimal comma, replacing the one there",
			setup: [][]string{addBank, spendGroceries, targetFood},
			args:  []string{"cli", "budget", "set", "Expenses:Groceries", "250,50", "--month", "2026-10"},
			want:  "Target for Expenses:Groceries in 2026-10: 250.50 EUR\n",
			after: "Budget for 2026-10, in EUR\n\nCategory             Spent  Target    Left\nExpenses:Groceries  120.00  250.50  130.50\nTotal               120.00  250.50  130.50\n",
		},
		{
			name:  "this month when none is given",
			setup: [][]string{addBank, spendGroceries},
			args:  []string{"cli", "budget", "set", "Expenses:Groceries", "400"},
			want:  "Target for Expenses:Groceries in 2026-10: 400.00 EUR\n",
		},
		{
			name:  "an income category is planned to bring something in",
			setup: [][]string{addBank, {"cli", "add", "-a", "Assets:Bank", "--amount=2900", "-t", "Income:Salary", "-d", "2026-09-28"}},
			args:  []string{"cli", "budget", "set", "Income:Salary", "3000", "--month", "2026-10"},
			want:  "Target for Income:Salary in 2026-10: 3000.00 EUR\n",
		},
		{
			name:    "a category that does not exist",
			setup:   [][]string{addBank},
			args:    []string{"cli", "budget", "set", "Expenses:Nope", "400"},
			wantErr: "account does not exist: Expenses:Nope",
		},
		{
			name:    "an account that is not a category",
			setup:   [][]string{addBank},
			args:    []string{"cli", "budget", "set", "Assets:Bank", "400"},
			wantErr: "Assets:Bank is not an Income or Expenses category: only those, other than Uncategorized, can have a target",
		},
		{
			name:    "zero",
			setup:   [][]string{addBank, spendGroceries},
			args:    []string{"cli", "budget", "set", "Expenses:Groceries", "0"},
			wantErr: "amount must be greater than zero",
		},
		{
			name:    "not an amount",
			setup:   [][]string{addBank, spendGroceries},
			args:    []string{"cli", "budget", "set", "Expenses:Groceries", "lots"},
			wantErr: `invalid amount "lots"`,
		},
		{
			name:    "a month that is none",
			setup:   [][]string{addBank, spendGroceries},
			args:    []string{"cli", "budget", "set", "Expenses:Groceries", "400", "--month", "2026-13"},
			wantErr: `invalid month "2026-13": use YYYY-MM`,
		},
		{
			name:    "one argument is not enough",
			setup:   [][]string{addBank},
			args:    []string{"cli", "budget", "set", "Expenses:Groceries"},
			wantErr: "accepts 2 arg(s), received 1",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, nil, tc.setup...)

			// when
			got, err := e.run(tc.args...)

			// then
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			if tc.after != "" {
				after, err := e.run(shown...)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(tc.after, after); diff != "" {
					t.Errorf("the month afterwards (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func Test_budget_clear(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "a target goes",
			setup: [][]string{addBank, spendGroceries, targetFood},
			args:  []string{"cli", "budget", "clear", "Expenses:Groceries", "--month", "2026-10"},
			want:  "Cleared the target for Expenses:Groceries in 2026-10\n",
		},
		{
			name:  "a category with no target is left as it is",
			setup: [][]string{addBank, spendGroceries},
			args:  []string{"cli", "budget", "clear", "Expenses:Groceries", "--month", "2026-10"},
			want:  "Expenses:Groceries has no target in 2026-10\n",
		},
		{
			name:  "a target of another category does not count",
			setup: [][]string{addBank, spendGroceries, spendRent, targetRent},
			args:  []string{"cli", "budget", "clear", "Expenses:Groceries", "--month", "2026-10"},
			want:  "Expenses:Groceries has no target in 2026-10\n",
		},
		{
			name:  "the target of another month is not touched",
			setup: [][]string{addBank, spendGroceries, targetFood},
			args:  []string{"cli", "budget", "clear", "Expenses:Groceries", "--month", "2026-09"},
			want:  "Expenses:Groceries has no target in 2026-09\n",
		},
		{
			name:    "a category that does not exist",
			setup:   [][]string{addBank},
			args:    []string{"cli", "budget", "clear", "Expenses:Nope"},
			wantErr: "account does not exist: Expenses:Nope",
		},
		{
			name:    "a month that is none",
			setup:   [][]string{addBank, spendGroceries},
			args:    []string{"cli", "budget", "clear", "Expenses:Groceries", "--month", "soon"},
			wantErr: `invalid month "soon": use YYYY-MM`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

// A cleared target is gone from the month, and the month goes back to its spending.
func Test_budget_clear_then_show(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "after clearing",
			setup: [][]string{addBank, spendGroceries, targetFood, {"cli", "budget", "clear", "Expenses:Groceries", "--month", "2026-10"}},
			args:  []string{"cli", "budget", "--month", "2026-10"},
			want:  "Budget for 2026-10, in EUR\n\nCategory             Spent\nExpenses:Groceries  120.00\nTotal               120.00\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

// What the table says of money carried in, a category past its target and spending nobody
// planned for; the command cannot switch carrying on, so this takes the month as it is.
func Test_printMonthBudget(t *testing.T) {
	eur := func(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "EUR"} }
	tcs := []struct {
		name string
		in   core.MonthBudget
		want string
	}{
		{
			name: "a carried amount gets its column",
			in: core.MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines: []core.MonthBudgetLine{
					{Name: "Expenses:Food", Spent: eur(10000), HasTarget: true, Target: eur(40000), Rollover: true, Carried: eur(25000), Left: eur(55000)},
					{Name: "Expenses:Fun", Spent: eur(500), HasTarget: false, Target: eur(0), Rollover: true, Carried: eur(-2000), Left: eur(-2500)},
				},
				SpentTotal: eur(10500), TargetTotal: eur(40000), CarriedTotal: eur(23000), LeftTotal: eur(52500), UnbudgetedTotal: eur(0),
			},
			want: "Budget for 2026-10, in EUR\n\n" +
				"Category        Spent  Target  Carried    Left\n" +
				"Expenses:Food  100.00  400.00   250.00  550.00\n" +
				"Expenses:Fun     5.00           -20.00  -25.00\n" +
				"Total          105.00  400.00   230.00  525.00\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			var out bytes.Buffer

			// when
			printMonthBudget(&out, tc.in)

			// then
			if diff := cmp.Diff(tc.want, out.String()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
