package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// categoryState is everything the categories touch, as lines of text, for comparing what
// a call left behind with what it should.
type categoryState struct {
	Categories   []string // the Income and Expenses accounts
	Transactions []string
	Targets      []string
	Rollovers    []string
	Schedules    []string
}

func categoryStateOf(t *testing.T, st *Local) categoryState {
	t.Helper()
	ctx := t.Context()
	var s categoryState
	accs, err := st.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accs {
		if a.Type == ledger.Income || a.Type == ledger.Expenses {
			s.Categories = append(s.Categories, a.Name)
		}
	}
	postings := func(ps []PostingView) string {
		var parts []string
		for _, p := range ps {
			parts = append(parts, p.Account+" "+p.Amount.String())
		}
		return strings.Join(parts, ", ")
	}
	txs, err := st.ListTransactions(ctx, TxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range txs {
		s.Transactions = append(s.Transactions, fmt.Sprintf("%d %s %s: %s", tx.ID, tx.Date, tx.Payee, postings(tx.Postings)))
	}
	for _, month := range []string{"2026-09", "2026-10", "2026-11"} {
		targets, err := st.BudgetTargets(ctx, month)
		if err != nil {
			t.Fatal(err)
		}
		for _, tg := range targets {
			s.Targets = append(s.Targets, month+" "+tg.Category+" "+tg.Amount.String())
		}
	}
	rolls, err := st.BudgetRollovers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rolls {
		s.Rollovers = append(s.Rollovers, r.Category+" from "+r.From)
	}
	schedules, err := st.Schedules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range schedules {
		s.Schedules = append(s.Schedules, sc.Name+": "+postings(sc.Postings))
	}
	return s
}

// lunch is a schedule that books to Expenses:Food.
func lunch() given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.CreateSchedule(t.Context(), NewSchedule{
			Name: "Lunch", Every: 1, Unit: EveryMonth, Start: day("2026-10-15"),
			Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Food", Amount: eur(1000)}},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// foodWorld is a ledger with Food and Groceries, both with targets (for October both),
// Food carrying over, a schedule that books to Food, and two Income categories.
func foodWorld() []given {
	return []given{
		accounts("Assets:Bank EUR", "Assets:Dollars USD"),
		spending("2026-09-05", "Expenses:Food", 1200), spending("2026-10-06", "Expenses:Food", 800),
		spending("2026-10-07", "Expenses:Groceries", 3000), spending("2026-10-08", "Expenses:Rent", 90000),
		earning("2026-10-01", "Income:Salary", 250000), earning("2026-10-02", "Income:Gift", 1000),
		target("2026-09", "Expenses:Food", eur(10000)), target("2026-10", "Expenses:Food", eur(20000)), target("2026-10", "Expenses:Groceries", eur(5000)),
		target("2026-10", "Income:Salary", eur(260000)),
		rollover("Expenses:Food", "2026-09"), lunch(),
	}
}

// the state of foodWorld, as the cases below start from it
var foodState = categoryState{
	Categories: []string{"Expenses:Food", "Expenses:Groceries", "Expenses:Rent", "Income:Gift", "Income:Salary"},
	Transactions: []string{
		"1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:Food 12.00 EUR",
		"5 2026-10-01 employer: Assets:Bank 2500.00 EUR, Income:Salary -2500.00 EUR",
		"6 2026-10-02 employer: Assets:Bank 10.00 EUR, Income:Gift -10.00 EUR",
		"2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Food 8.00 EUR",
		"3 2026-10-07 shop: Assets:Bank -30.00 EUR, Expenses:Groceries 30.00 EUR",
		"4 2026-10-08 shop: Assets:Bank -900.00 EUR, Expenses:Rent 900.00 EUR",
	},
	Targets:   []string{"2026-09 Expenses:Food 100.00 EUR", "2026-10 Expenses:Food 200.00 EUR", "2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"},
	Rollovers: []string{"Expenses:Food from 2026-09"},
	Schedules: []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Food 10.00 EUR"},
}

func Test_foodWorld(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		want  categoryState
	}{{name: "the ledger the cases start from", setup: foodWorld(), want: foodState}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got := categoryStateOf(t, st)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// with is foodState with changes made by f.
func with(f func(*categoryState)) categoryState {
	s := foodState
	s.Categories = append([]string(nil), s.Categories...)
	s.Transactions = append([]string(nil), s.Transactions...)
	s.Targets = append([]string(nil), s.Targets...)
	s.Rollovers = append([]string(nil), s.Rollovers...)
	s.Schedules = append([]string(nil), s.Schedules...)
	f(&s)
	return s
}

func Test_RenameCategory(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		from  string
		to    string
		want  CategoryRename
		state categoryState
	}{
		{
			name: "the transactions, targets, carry-over and schedules follow the name",
			from: "Expenses:Food", to: "Expenses:Dining", setup: foodWorld(),
			want: CategoryRename{Renamed: []string{"Expenses:Dining"}},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Dining", "Expenses:Groceries", "Expenses:Rent", "Income:Gift", "Income:Salary"}
				s.Transactions[0] = "1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:Dining 12.00 EUR"
				s.Transactions[3] = "2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Dining 8.00 EUR"
				s.Targets = []string{"2026-09 Expenses:Dining 100.00 EUR", "2026-10 Expenses:Dining 200.00 EUR", "2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
				s.Rollovers = []string{"Expenses:Dining from 2026-09"}
				s.Schedules = []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Dining 10.00 EUR"}
			}),
		},
		{
			name: "an income category",
			from: "Income:Salary", to: "Income:Pay", setup: foodWorld(),
			want: CategoryRename{Renamed: []string{"Income:Pay"}},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Food", "Expenses:Groceries", "Expenses:Rent", "Income:Gift", "Income:Pay"}
				s.Transactions[1] = "5 2026-10-01 employer: Assets:Bank 2500.00 EUR, Income:Pay -2500.00 EUR"
				s.Targets[3] = "2026-10 Income:Pay 2600.00 EUR"
			}),
		},
		{
			name: "the categories below it come along, the one asked for first",
			from: "Expenses:Food", to: "Expenses:Eating",
			setup: append(foodWorld(), accounts("Expenses:Food:Out", "Expenses:Food:Out:Late", "Expenses:Foodie"), spending("2026-10-09", "Expenses:Food:Out", 500)),
			want:  CategoryRename{Renamed: []string{"Expenses:Eating", "Expenses:Eating:Out", "Expenses:Eating:Out:Late"}},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Eating", "Expenses:Eating:Out", "Expenses:Eating:Out:Late", "Expenses:Foodie", "Expenses:Groceries", "Expenses:Rent", "Income:Gift", "Income:Salary"}
				s.Transactions[0] = "1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:Eating 12.00 EUR"
				s.Transactions[3] = "2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Eating 8.00 EUR"
				s.Transactions = append(s.Transactions, "7 2026-10-09 shop: Assets:Bank -5.00 EUR, Expenses:Eating:Out 5.00 EUR")
				s.Targets = []string{"2026-09 Expenses:Eating 100.00 EUR", "2026-10 Expenses:Eating 200.00 EUR", "2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
				s.Rollovers = []string{"Expenses:Eating from 2026-09"}
				s.Schedules = []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Eating 10.00 EUR"}
			}),
		},
		{
			name: "a category with nothing on it",
			from: "Expenses:Empty", to: "Expenses:Void", setup: append(foodWorld(), accounts("Expenses:Empty")),
			want: CategoryRename{Renamed: []string{"Expenses:Void"}},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Food", "Expenses:Groceries", "Expenses:Rent", "Expenses:Void", "Income:Gift", "Income:Salary"}
			}),
		},
		{
			name: "only the case changes",
			from: "Expenses:Food", to: "Expenses:FOOD", setup: foodWorld(),
			want: CategoryRename{Renamed: []string{"Expenses:FOOD"}},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:FOOD", "Expenses:Groceries", "Expenses:Rent", "Income:Gift", "Income:Salary"}
				s.Transactions[0] = "1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:FOOD 12.00 EUR"
				s.Transactions[3] = "2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:FOOD 8.00 EUR"
				s.Targets = []string{"2026-09 Expenses:FOOD 100.00 EUR", "2026-10 Expenses:FOOD 200.00 EUR", "2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
				s.Rollovers = []string{"Expenses:FOOD from 2026-09"}
				s.Schedules = []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:FOOD 10.00 EUR"}
			}),
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.RenameCategory(t.Context(), tc.from, tc.to)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("RenameCategory mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.state, categoryStateOf(t, st)); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

// A call that is refused changes nothing.
func Test_RenameCategory_refused(t *testing.T) {
	tcs := []struct {
		name     string
		setup    []given
		from, to string
		wantErr  string
	}{
		{name: "no such category", from: "Expenses:Nope", to: "Expenses:New", wantErr: "account does not exist: Expenses:Nope"},
		{name: "an account of your own is not a category", from: "Assets:Bank", to: "Expenses:New", wantErr: "Assets:Bank is not an Income or Expenses category"},
		{name: "Equity neither", from: "Equity:Transfers", to: "Expenses:New", setup: []given{accounts("Equity:Transfers")}, wantErr: "Equity:Transfers is not an Income or Expenses category"},
		{name: "Uncategorized stays", from: ledger.UncategorizedExpense, to: "Expenses:New", setup: []given{accounts(ledger.UncategorizedExpense)}, wantErr: "Expenses:Uncategorized is the category Thaler keeps for what has none: it cannot be renamed, merged away, deleted or given as a new name"},
		{name: "nor is it a new name", from: "Expenses:Food", to: ledger.UncategorizedExpense, wantErr: "Expenses:Uncategorized is the category Thaler keeps for what has none: it cannot be renamed, merged away, deleted or given as a new name"},
		{name: "the income Uncategorized is not a new name either", from: "Income:Gift", to: ledger.UncategorizedIncome, wantErr: "Income:Uncategorized is the category Thaler keeps for what has none: it cannot be renamed, merged away, deleted or given as a new name"},
		{name: "the type stays", from: "Expenses:Food", to: "Income:Food", wantErr: "cannot rename Expenses:Food to Income:Food: a category stays Expenses, so the new name starts with Expenses:"},
		{name: "not into an account of your own", from: "Expenses:Food", to: "Assets:Food", wantErr: "cannot rename Expenses:Food to Assets:Food: a category stays Expenses, so the new name starts with Expenses:"},
		{name: "the same name", from: "Expenses:Food", to: "Expenses:Food", wantErr: "Expenses:Food already has that name"},
		{name: "a name that is taken", from: "Expenses:Food", to: "Expenses:Groceries", wantErr: "account already exists: Expenses:Groceries"},
		{name: "a name that an account below would take", from: "Expenses:Food", to: "Expenses:Rent", setup: []given{accounts("Expenses:Food:Rent")}, wantErr: "account already exists: Expenses:Rent"},
		{name: "a name a category below would need", from: "Expenses:Food", to: "Expenses:Fun", setup: []given{accounts("Expenses:Food:Out", "Expenses:Fun:Out")}, wantErr: "account already exists: Expenses:Fun:Out"},
		{name: "a name below one of its own is taken after the change", from: "Expenses:Food", to: "Expenses:Food:Out", setup: []given{accounts("Expenses:Food:Out")}, wantErr: "account already exists: Expenses:Food:Out"},
		{name: "not a name", from: "Expenses:Food", to: "Expenses:Food Out", wantErr: `account "Expenses:Food Out": invalid character " " (use letters, digits, '-' and '_')`},
		{name: "no type", from: "Expenses:Food", to: "Food", wantErr: `account "Food" must start with one of Assets, Liabilities, Income, Expenses, Equity`},
		{name: "an empty segment", from: "Expenses:Food", to: "Expenses::Food", wantErr: `account "Expenses::Food" has an empty segment`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(foodWorld(), tc.setup...)...)
			before := categoryStateOf(t, st)

			// when
			_, err := st.RenameCategory(t.Context(), tc.from, tc.to)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(before, categoryStateOf(t, st)); diff != "" {
				t.Errorf("a refused call changed something (-before +after):\n%s", diff)
			}
		})
	}
}

func Test_MergeCategory(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		from  string
		into  string
		want  CategoryMerge
		state categoryState
	}{
		{
			name: "transactions, schedules, targets and carry-over go to the other category, its own targets stay",
			from: "Expenses:Food", into: "Expenses:Groceries", setup: foodWorld(),
			want: CategoryMerge{Transactions: 2, Schedules: 1, Targets: 1, TargetsDropped: 1},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Groceries", "Expenses:Rent", "Income:Gift", "Income:Salary"}
				s.Transactions[0] = "1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:Groceries 12.00 EUR"
				s.Transactions[3] = "2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Groceries 8.00 EUR"
				s.Targets = []string{"2026-09 Expenses:Groceries 100.00 EUR", "2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
				s.Rollovers = []string{"Expenses:Groceries from 2026-09"}
				s.Schedules = []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Groceries 10.00 EUR"}
			}),
		},
		{
			name: "the other category's own carry-over is kept",
			from: "Expenses:Food", into: "Expenses:Groceries", setup: append(foodWorld(), rollover("Expenses:Groceries", "2026-10")),
			want: CategoryMerge{Transactions: 2, Schedules: 1, Targets: 1, TargetsDropped: 1},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Groceries", "Expenses:Rent", "Income:Gift", "Income:Salary"}
				s.Transactions[0] = "1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:Groceries 12.00 EUR"
				s.Transactions[3] = "2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Groceries 8.00 EUR"
				s.Targets = []string{"2026-09 Expenses:Groceries 100.00 EUR", "2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
				s.Rollovers = []string{"Expenses:Groceries from 2026-10"}
				s.Schedules = []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Groceries 10.00 EUR"}
			}),
		},
		{
			name: "a category with no carry-over adds none",
			from: "Expenses:Groceries", into: "Expenses:Food", setup: foodWorld(),
			want: CategoryMerge{Transactions: 1, Targets: 0, TargetsDropped: 1},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Food", "Expenses:Rent", "Income:Gift", "Income:Salary"}
				s.Transactions[4] = "3 2026-10-07 shop: Assets:Bank -30.00 EUR, Expenses:Food 30.00 EUR"
				s.Targets = []string{"2026-09 Expenses:Food 100.00 EUR", "2026-10 Expenses:Food 200.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
			}),
		},
		{
			name: "into Uncategorized: the transactions go, the targets and carry-over are dropped",
			from: "Expenses:Food", into: ledger.UncategorizedExpense, setup: append(foodWorld(), accounts(ledger.UncategorizedExpense)),
			want: CategoryMerge{Transactions: 2, Schedules: 1, Targets: 0, TargetsDropped: 2},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Groceries", "Expenses:Rent", "Expenses:Uncategorized", "Income:Gift", "Income:Salary"}
				s.Transactions[0] = "1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:Uncategorized 12.00 EUR"
				s.Transactions[3] = "2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Uncategorized 8.00 EUR"
				s.Targets = []string{"2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
				s.Rollovers = nil
				s.Schedules = []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Uncategorized 10.00 EUR"}
			}),
		},
		{
			name: "income into income",
			from: "Income:Gift", into: "Income:Salary", setup: foodWorld(),
			want: CategoryMerge{Transactions: 1},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Food", "Expenses:Groceries", "Expenses:Rent", "Income:Salary"}
				s.Transactions[2] = "6 2026-10-02 employer: Assets:Bank 10.00 EUR, Income:Salary -10.00 EUR"
			}),
		},
		{
			name: "income with a plan into one without: the plan moves",
			from: "Income:Salary", into: "Income:Gift", setup: foodWorld(),
			want: CategoryMerge{Transactions: 1, Targets: 1},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Food", "Expenses:Groceries", "Expenses:Rent", "Income:Gift"}
				s.Transactions[1] = "5 2026-10-01 employer: Assets:Bank 2500.00 EUR, Income:Gift -2500.00 EUR"
				s.Targets = []string{"2026-09 Expenses:Food 100.00 EUR", "2026-10 Expenses:Food 200.00 EUR", "2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Gift 2600.00 EUR"}
			}),
		},
		{
			name: "a category with nothing on it",
			from: "Expenses:Empty", into: "Expenses:Groceries", setup: append(foodWorld(), accounts("Expenses:Empty")),
			want:  CategoryMerge{},
			state: foodState,
		},
		{
			name: "a transaction that was split between the two ends up with both lines on one category",
			from: "Expenses:Food", into: "Expenses:Groceries",
			setup: []given{
				accounts("Assets:Bank EUR"),
				stored(NewTransaction{Date: day("2026-10-05"), Payee: "market", Postings: []ledger.Posting{
					{Account: "Assets:Bank", Amount: eur(-3000)}, {Account: "Expenses:Food", Amount: eur(2000)}, {Account: "Expenses:Groceries", Amount: eur(1000)}}}),
			},
			want: CategoryMerge{Transactions: 1},
			state: categoryState{
				Categories:   []string{"Expenses:Groceries"},
				Transactions: []string{"1 2026-10-05 market: Assets:Bank -30.00 EUR, Expenses:Groceries 20.00 EUR, Expenses:Groceries 10.00 EUR"},
			},
		},
		{
			name: "two lines of one transaction on the category count as one transaction",
			from: "Expenses:Food", into: "Expenses:Groceries",
			setup: []given{
				accounts("Assets:Bank EUR", "Expenses:Groceries"),
				stored(NewTransaction{Date: day("2026-10-05"), Payee: "market", Postings: []ledger.Posting{
					{Account: "Assets:Bank", Amount: eur(-3000)}, {Account: "Expenses:Food", Amount: eur(2000)}, {Account: "Expenses:Food", Amount: eur(1000)}}}),
			},
			want: CategoryMerge{Transactions: 1},
			state: categoryState{
				Categories:   []string{"Expenses:Groceries"},
				Transactions: []string{"1 2026-10-05 market: Assets:Bank -30.00 EUR, Expenses:Groceries 20.00 EUR, Expenses:Groceries 10.00 EUR"},
			},
		},
		{
			name: "two lines of one schedule on the category count as one schedule",
			from: "Expenses:Food", into: "Expenses:Groceries",
			setup: []given{
				accounts("Assets:Bank EUR", "Expenses:Groceries"),
				func(t *testing.T, st *Local) {
					t.Helper()
					if _, err := st.CreateSchedule(t.Context(), NewSchedule{
						Name: "Market", Every: 1, Unit: EveryMonth, Start: day("2026-10-15"),
						Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-3000)}, {Account: "Expenses:Food", Amount: eur(2000)}, {Account: "Expenses:Food", Amount: eur(1000)}},
					}); err != nil {
						t.Fatal(err)
					}
				},
			},
			want: CategoryMerge{Schedules: 1},
			state: categoryState{
				Categories: []string{"Expenses:Groceries"},
				Schedules:  []string{"Market: Assets:Bank -30.00 EUR, Expenses:Groceries 20.00 EUR, Expenses:Groceries 10.00 EUR"},
			},
		},
		{
			name: "a category that takes one currency, and what is in it is that",
			from: "Expenses:Food", into: "Expenses:Euros",
			setup: []given{
				func(t *testing.T, st *Local) {
					t.Helper()
					if _, err := st.AddAccount(t.Context(), "Expenses:Euros", "EUR"); err != nil {
						t.Fatal(err)
					}
				},
				accounts("Assets:Bank EUR"), spending("2026-10-05", "Expenses:Food", 400),
			},
			want: CategoryMerge{Transactions: 1},
			state: categoryState{
				Categories:   []string{"Expenses:Euros"},
				Transactions: []string{"1 2026-10-05 shop: Assets:Bank -4.00 EUR, Expenses:Euros 4.00 EUR"},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.MergeCategory(t.Context(), tc.from, tc.into)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("MergeCategory mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.state, categoryStateOf(t, st), equalEmpty); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_MergeCategory_refused(t *testing.T) {
	protected := "Expenses:Uncategorized is the category Thaler keeps for what has none: it cannot be renamed, merged away, deleted or given as a new name"
	tcs := []struct {
		name       string
		setup      []given
		from, into string
		wantErr    string
	}{
		{name: "no such category", from: "Expenses:Nope", into: "Expenses:Food", wantErr: "account does not exist: Expenses:Nope"},
		{name: "nor to merge into", from: "Expenses:Food", into: "Expenses:Nope", wantErr: "account does not exist: Expenses:Nope"},
		{name: "itself", from: "Expenses:Food", into: "Expenses:Food", wantErr: "cannot merge Expenses:Food into itself"},
		{name: "an account of your own", from: "Assets:Bank", into: "Expenses:Food", wantErr: "Assets:Bank is not an Income or Expenses category"},
		{name: "nor to merge into one", from: "Expenses:Food", into: "Assets:Bank", wantErr: "Assets:Bank is not an Income or Expenses category"},
		{name: "Uncategorized is not merged away", from: ledger.UncategorizedExpense, into: "Expenses:Food", setup: []given{accounts(ledger.UncategorizedExpense)}, wantErr: protected},
		{name: "income into spending", from: "Income:Gift", into: "Expenses:Food", wantErr: "cannot merge Income:Gift (Income) into Expenses:Food (Expenses): categories merge only into one of their own type"},
		{name: "spending into income", from: "Expenses:Food", into: "Income:Gift", wantErr: "cannot merge Expenses:Food (Expenses) into Income:Gift (Income): categories merge only into one of their own type"},
		{name: "a category with categories below it", from: "Expenses:Food", into: "Expenses:Groceries", setup: []given{accounts("Expenses:Food:Out")}, wantErr: "Expenses:Food has categories below it, such as Expenses:Food:Out: merge or delete those first"},
		{
			name: "a category that takes one currency, and what is in it is another",
			from: "Expenses:Food", into: "Expenses:Euros",
			setup: []given{
				func(t *testing.T, st *Local) {
					t.Helper()
					if _, err := st.AddAccount(t.Context(), "Expenses:Euros", "EUR"); err != nil {
						t.Fatal(err)
					}
				},
				stored(NewTransaction{Date: day("2026-10-09"), Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(-500)}, {Account: "Expenses:Food", Amount: usd(500)}}}),
			},
			wantErr: "Expenses:Euros only takes EUR, and Expenses:Food has transactions in another currency",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(foodWorld(), tc.setup...)...)
			before := categoryStateOf(t, st)

			// when
			_, err := st.MergeCategory(t.Context(), tc.from, tc.into)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(before, categoryStateOf(t, st)); diff != "" {
				t.Errorf("a refused call changed something (-before +after):\n%s", diff)
			}
		})
	}
}

func Test_DeleteCategory(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		do    string
		want  CategoryMerge
		state categoryState
	}{
		{
			name: "the transactions go to Uncategorized, made for it; targets and carry-over are dropped",
			do:   "Expenses:Food", setup: foodWorld(),
			want: CategoryMerge{Transactions: 2, Schedules: 1, TargetsDropped: 2},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Groceries", "Expenses:Rent", "Expenses:Uncategorized", "Income:Gift", "Income:Salary"}
				s.Transactions[0] = "1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:Uncategorized 12.00 EUR"
				s.Transactions[3] = "2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Uncategorized 8.00 EUR"
				s.Targets = []string{"2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
				s.Rollovers = nil
				s.Schedules = []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Uncategorized 10.00 EUR"}
			}),
		},
		{
			name: "an Uncategorized that is there is used",
			do:   "Expenses:Food", setup: append(foodWorld(), spending("2026-10-20", ledger.UncategorizedExpense, 100)),
			want: CategoryMerge{Transactions: 2, Schedules: 1, TargetsDropped: 2},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Groceries", "Expenses:Rent", "Expenses:Uncategorized", "Income:Gift", "Income:Salary"}
				s.Transactions[0] = "1 2026-09-05 shop: Assets:Bank -12.00 EUR, Expenses:Uncategorized 12.00 EUR"
				s.Transactions[3] = "2 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Uncategorized 8.00 EUR"
				s.Transactions = append(s.Transactions, "7 2026-10-20 shop: Assets:Bank -1.00 EUR, Expenses:Uncategorized 1.00 EUR")
				s.Targets = []string{"2026-10 Expenses:Groceries 50.00 EUR", "2026-10 Income:Salary 2600.00 EUR"}
				s.Rollovers = nil
				s.Schedules = []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Uncategorized 10.00 EUR"}
			}),
		},
		{
			name: "an income category goes to the Uncategorized income",
			do:   "Income:Salary", setup: foodWorld(),
			want: CategoryMerge{Transactions: 1, TargetsDropped: 1},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Food", "Expenses:Groceries", "Expenses:Rent", "Income:Gift", "Income:Uncategorized"}
				s.Transactions[1] = "5 2026-10-01 employer: Assets:Bank 2500.00 EUR, Income:Uncategorized -2500.00 EUR"
				s.Targets = []string{"2026-09 Expenses:Food 100.00 EUR", "2026-10 Expenses:Food 200.00 EUR", "2026-10 Expenses:Groceries 50.00 EUR"}
			}),
		},
		{
			name: "a category with nothing on it goes too, and Uncategorized is made as for any other",
			do:   "Expenses:Empty", setup: append(foodWorld(), accounts("Expenses:Empty")),
			want: CategoryMerge{},
			state: with(func(s *categoryState) {
				s.Categories = []string{"Expenses:Food", "Expenses:Groceries", "Expenses:Rent", "Expenses:Uncategorized", "Income:Gift", "Income:Salary"}
			}),
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.DeleteCategory(t.Context(), tc.do)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("DeleteCategory mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.state, categoryStateOf(t, st), equalEmpty); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_DeleteCategory_refused(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		do      string
		wantErr string
	}{
		{name: "no such category", do: "Expenses:Nope", wantErr: "account does not exist: Expenses:Nope"},
		{name: "an account of your own", do: "Assets:Bank", wantErr: "Assets:Bank is not an Income or Expenses category"},
		{name: "Uncategorized stays", do: ledger.UncategorizedIncome, setup: []given{accounts(ledger.UncategorizedIncome)}, wantErr: "Income:Uncategorized is the category Thaler keeps for what has none: it cannot be renamed, merged away, deleted or given as a new name"},
		{name: "a category with categories below it", do: "Expenses:Food", setup: []given{accounts("Expenses:Food:Out")}, wantErr: "Expenses:Food has categories below it, such as Expenses:Food:Out: merge or delete those first"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(foodWorld(), tc.setup...)...)
			before := categoryStateOf(t, st)

			// when
			_, err := st.DeleteCategory(t.Context(), tc.do)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(before, categoryStateOf(t, st)); diff != "" {
				t.Errorf("a refused call changed something (-before +after):\n%s", diff)
			}
		})
	}
}

// What changed has its version raised, for the sync that is to come, and what did not has
// not.
func Test_categories_versions(t *testing.T) {
	versions := func(t *testing.T, st *Local) map[string]int {
		t.Helper()
		out := map[string]int{}
		for table, key := range map[string]string{"accounts": "name", "transactions": "id"} {
			rows, err := st.repo.db.QueryContext(t.Context(), "SELECT CAST("+key+" AS TEXT), version FROM "+table)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var k string
				var v int
				if err := rows.Scan(&k, &v); err != nil {
					t.Fatal(err)
				}
				out[table[:3]+" "+k] = v
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
		}
		return out
	}
	tcs := []struct {
		name  string
		setup []given
		do    func(t *testing.T, st *Local)
		want  map[string]int
	}{
		{
			name: "a rename raises the account, and the ones below it",
			setup: []given{
				accounts("Assets:Bank EUR", "Expenses:Groceries", "Expenses:Food:Out"),
				spending("2026-10-05", "Expenses:Food", 100), spending("2026-10-06", "Expenses:Groceries", 200), spending("2026-10-07", "Expenses:Food:Out", 300),
			},
			do: func(t *testing.T, st *Local) {
				if _, err := st.RenameCategory(t.Context(), "Expenses:Food", "Expenses:Eating"); err != nil {
					t.Fatal(err)
				}
			},
			want: map[string]int{
				"acc Assets:Bank": 1, "acc Expenses:Eating": 2, "acc Expenses:Eating:Out": 2, "acc Expenses:Groceries": 1,
				"tra 1": 1, "tra 2": 1, "tra 3": 1,
			},
		},
		{
			name: "a merge raises the category that took over and the transactions that moved",
			setup: []given{
				accounts("Assets:Bank EUR", "Expenses:Groceries"),
				spending("2026-10-05", "Expenses:Food", 100), spending("2026-10-06", "Expenses:Groceries", 200),
			},
			do: func(t *testing.T, st *Local) {
				if _, err := st.MergeCategory(t.Context(), "Expenses:Food", "Expenses:Groceries"); err != nil {
					t.Fatal(err)
				}
			},
			want: map[string]int{"acc Assets:Bank": 1, "acc Expenses:Groceries": 2, "tra 1": 2, "tra 2": 1},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			tc.do(t, st)

			// then
			got := versions(t, st)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("versions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
