package core

import (
	"context"
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
			wantErr: "Assets:Bank is not a spending category: only an Expenses category, other than Uncategorized, can have a target or carry over",
		},
		{
			name:  "income has no target",
			month: "2026-10", category: "Income:Salary", amount: eur(40000),
			wantErr: "Income:Salary is not a spending category: only an Expenses category, other than Uncategorized, can have a target or carry over",
		},
		{
			name:  "uncategorized has no target",
			month: "2026-10", category: ledger.UncategorizedExpense, amount: eur(40000),
			wantErr: "Expenses:Uncategorized is not a spending category: only an Expenses category, other than Uncategorized, can have a target or carry over",
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

// spendingUSD books an expense of amount dollars from a dollar account.
func spendingUSD(date, category string, amount int64) given {
	return stored(NewTransaction{
		Date: day(date), Payee: "shop",
		Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(-amount)}, {Account: category, Amount: usd(amount)}},
	})
}

func Test_MonthBudget(t *testing.T) {
	// more accounts than targetLedger has, then the steps of the case
	with := func(steps ...given) []given {
		return append([]given{accounts("Expenses:Fun", "Assets:Dollars USD")}, steps...)
	}
	tcs := []struct {
		name    string
		setup   []given
		req     MonthBudgetRequest
		want    MonthBudget
		wantErr string
	}{
		{
			name: "spending against targets, a target not touched, and spending nobody planned",
			setup: with(
				target("2026-10", "Expenses:Food", eur(40000)), target("2026-10", "Expenses:Rent", eur(90000)),
				spending("2026-10-03", "Expenses:Food", 12500), spending("2026-10-20", "Expenses:Food", 2500),
				spending("2026-10-09", "Expenses:Fun", 1500)),
			req: MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines: []MonthBudgetLine{
					{"Expenses:Food", eur(15000), true, eur(40000), false, eur(0), eur(25000)},
					{"Expenses:Fun", eur(1500), false, eur(0), false, eur(0), eur(0)},
					{"Expenses:Rent", eur(0), true, eur(90000), false, eur(0), eur(90000)},
				},
				SpentTotal: eur(16500), TargetTotal: eur(130000), CarriedTotal: eur(0), LeftTotal: eur(115000), UnbudgetedTotal: eur(1500),
			},
		},
		{
			name: "overspending is a negative amount left, and takes from the total left",
			setup: with(
				target("2026-10", "Expenses:Food", eur(40000)), target("2026-10", "Expenses:Rent", eur(90000)),
				spending("2026-10-03", "Expenses:Food", 52500)),
			req: MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines: []MonthBudgetLine{
					{"Expenses:Food", eur(52500), true, eur(40000), false, eur(0), eur(-12500)},
					{"Expenses:Rent", eur(0), true, eur(90000), false, eur(0), eur(90000)},
				},
				SpentTotal: eur(52500), TargetTotal: eur(130000), CarriedTotal: eur(0), LeftTotal: eur(77500), UnbudgetedTotal: eur(0),
			},
		},
		{
			name: "only the month asked for: its spending and its targets",
			setup: with(
				target("2026-09", "Expenses:Rent", eur(80000)), target("2026-10", "Expenses:Food", eur(40000)),
				spending("2026-09-30", "Expenses:Food", 9900), spending("2026-10-01", "Expenses:Food", 100),
				spending("2026-10-31", "Expenses:Food", 200), spending("2026-11-01", "Expenses:Food", 7700)),
			req: MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{"Expenses:Food", eur(300), true, eur(40000), false, eur(0), eur(39700)}},
				SpentTotal: eur(300), TargetTotal: eur(40000), CarriedTotal: eur(0), LeftTotal: eur(39700), UnbudgetedTotal: eur(0),
			},
		},
		{
			name:  "no targets: all of the spending is unbudgeted",
			setup: with(spending("2026-10-03", "Expenses:Food", 4000)),
			req:   MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{"Expenses:Food", eur(4000), false, eur(0), false, eur(0), eur(0)}},
				SpentTotal: eur(4000), TargetTotal: eur(0), CarriedTotal: eur(0), LeftTotal: eur(0), UnbudgetedTotal: eur(4000),
			},
		},
		{
			name: "a month with nothing in it",
			req:  MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{Month: "2026-10", Base: "EUR", SpentTotal: eur(0), TargetTotal: eur(0), CarriedTotal: eur(0), LeftTotal: eur(0), UnbudgetedTotal: eur(0)},
		},
		{
			name: "equal spending is listed by name",
			setup: with(
				spending("2026-10-03", "Expenses:Rent", 100), spending("2026-10-03", "Expenses:Food", 100)),
			req: MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{"Expenses:Food", eur(100), false, eur(0), false, eur(0), eur(0)}, {"Expenses:Rent", eur(100), false, eur(0), false, eur(0), eur(0)}},
				SpentTotal: eur(200), TargetTotal: eur(0), CarriedTotal: eur(0), LeftTotal: eur(0), UnbudgetedTotal: eur(200),
			},
		},
		{
			name: "dollars spent count at the rate of the month",
			setup: with(setRate("2026-10-01", "USD", "EUR", "0.92"), target("2026-10", "Expenses:Food", eur(40000)),
				spendingUSD("2026-10-05", "Expenses:Food", 10000)),
			req: MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{"Expenses:Food", eur(9200), true, eur(40000), false, eur(0), eur(30800)}},
				SpentTotal: eur(9200), TargetTotal: eur(40000), CarriedTotal: eur(0), LeftTotal: eur(30800), UnbudgetedTotal: eur(0),
			},
		},
		{
			name: "dollars with no rate are left out, and said so",
			setup: with(target("2026-10", "Expenses:Food", eur(40000)), spending("2026-10-03", "Expenses:Food", 500),
				spendingUSD("2026-10-05", "Expenses:Food", 10000)),
			req: MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{"Expenses:Food", eur(500), true, eur(40000), false, eur(0), eur(39500)}},
				SpentTotal: eur(500), TargetTotal: eur(40000), CarriedTotal: eur(0), LeftTotal: eur(39500), UnbudgetedTotal: eur(0),
				Missing: []ledger.Currency{"USD"},
			},
		},
		{
			name: "a target set before the base currency changed is converted",
			setup: with(target("2026-10", "Expenses:Food", eur(40000)), baseCurrency("USD"),
				setRate("2026-10-01", "EUR", "USD", "1.10"), spendingUSD("2026-10-05", "Expenses:Food", 1000)),
			req: MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "USD",
				Lines:      []MonthBudgetLine{{"Expenses:Food", usd(1000), true, usd(44000), false, usd(0), usd(43000)}},
				SpentTotal: usd(1000), TargetTotal: usd(44000), CarriedTotal: usd(0), LeftTotal: usd(43000), UnbudgetedTotal: usd(0),
			},
		},
		{
			name: "a target in a currency with no rate is left out, and said so",
			setup: with(target("2026-10", "Expenses:Food", eur(40000)), baseCurrency("USD"),
				spendingUSD("2026-10-05", "Expenses:Food", 1000)),
			req: MonthBudgetRequest{Month: "2026-10", RateDate: "2026-10-31"},
			want: MonthBudget{
				Month: "2026-10", Base: "USD",
				Lines:      []MonthBudgetLine{{"Expenses:Food", usd(1000), false, usd(0), false, usd(0), usd(0)}},
				SpentTotal: usd(1000), TargetTotal: usd(0), CarriedTotal: usd(0), LeftTotal: usd(0), UnbudgetedTotal: usd(1000),
				Missing: []ledger.Currency{"EUR"},
			},
		},
		{name: "a month that is none", req: MonthBudgetRequest{Month: "2026-1"}, wantErr: `invalid month "2026-1": use YYYY-MM`},
		{name: "no month", req: MonthBudgetRequest{}, wantErr: `invalid month "": use YYYY-MM`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(targetLedger(), tc.setup...)...)

			// when
			got, err := st.MonthBudget(t.Context(), tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_CopyBudgetTargets(t *testing.T) {
	food9, rent9 := BudgetTarget{"Expenses:Food", eur(40000)}, BudgetTarget{"Expenses:Rent", eur(90000)}
	september := []given{target("2026-09", "Expenses:Food", eur(40000)), target("2026-09", "Expenses:Rent", eur(90000))}
	tcs := []struct {
		name     string
		setup    []given
		from, to string
		want     CopiedTargets
		wantTo   []BudgetTarget
		wantFrom []BudgetTarget // the source month afterwards
		wantErr  string
	}{
		{
			name: "every target into an empty month", setup: september, from: "2026-09", to: "2026-10",
			want:   CopiedTargets{Copied: []BudgetTarget{food9, rent9}},
			wantTo: []BudgetTarget{food9, rent9}, wantFrom: []BudgetTarget{food9, rent9},
		},
		{
			name:  "a category that has a target keeps it",
			setup: append(september, target("2026-10", "Expenses:Food", eur(50000))), from: "2026-09", to: "2026-10",
			want:   CopiedTargets{Copied: []BudgetTarget{rent9}, Kept: 1},
			wantTo: []BudgetTarget{{"Expenses:Food", eur(50000)}, rent9}, wantFrom: []BudgetTarget{food9, rent9},
		},
		{
			name:  "copying again adds nothing",
			setup: append(september, target("2026-10", "Expenses:Food", eur(40000)), target("2026-10", "Expenses:Rent", eur(90000))), from: "2026-09", to: "2026-10",
			want:   CopiedTargets{Kept: 2},
			wantTo: []BudgetTarget{food9, rent9}, wantFrom: []BudgetTarget{food9, rent9},
		},
		{
			name:  "a month with no targets has none to copy",
			setup: []given{target("2026-10", "Expenses:Food", eur(50000))}, from: "2026-09", to: "2026-10",
			wantTo: []BudgetTarget{{"Expenses:Food", eur(50000)}},
		},
		{
			name:  "the targets of other months are not copied",
			setup: append(september, target("2026-08", "Expenses:Food", eur(10000)), target("2026-11", "Expenses:Food", eur(20000))), from: "2026-09", to: "2026-10",
			want:   CopiedTargets{Copied: []BudgetTarget{food9, rent9}},
			wantTo: []BudgetTarget{food9, rent9}, wantFrom: []BudgetTarget{food9, rent9},
		},
		{
			name:  "a target keeps the currency it was set in",
			setup: append([]given{target("2026-09", "Expenses:Food", eur(40000))}, baseCurrency("USD")), from: "2026-09", to: "2026-10",
			want:   CopiedTargets{Copied: []BudgetTarget{food9}},
			wantTo: []BudgetTarget{food9}, wantFrom: []BudgetTarget{food9},
		},
		{
			name: "the same month", setup: september, from: "2026-09", to: "2026-09",
			wantFrom: []BudgetTarget{food9, rent9}, wantErr: "cannot copy the targets of 2026-09 into the same month",
		},
		{
			name: "a source that is no month", setup: september, from: "September", to: "2026-10",
			wantFrom: []BudgetTarget{food9, rent9}, wantErr: `invalid month "September": use YYYY-MM`,
		},
		{
			name: "a destination that is no month", setup: september, from: "2026-09", to: "2026-10-01",
			wantFrom: []BudgetTarget{food9, rent9}, wantErr: `invalid month "2026-10-01": use YYYY-MM`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(targetLedger(), tc.setup...)...)

			// when
			got, err := st.CopyBudgetTargets(t.Context(), tc.from, tc.to)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
			gotTo, errTo := st.BudgetTargets(t.Context(), "2026-10")
			gotFrom, errFrom := st.BudgetTargets(t.Context(), tc.from)
			if errTo != nil {
				t.Fatal(errTo)
			}
			if diff := cmp.Diff(tc.wantTo, gotTo, equalEmpty); diff != "" {
				t.Errorf("destination mismatch (-want +got):\n%s", diff)
			}
			if errFrom == nil { // a source that is no month has no list
				if diff := cmp.Diff(tc.wantFrom, gotFrom, equalEmpty); diff != "" {
					t.Errorf("source mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// rollover switches carrying over on for a category from a month.
func rollover(category, from string) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if err := st.SetBudgetRollover(t.Context(), category, from); err != nil {
			t.Fatalf("SetBudgetRollover(%s, %s): %v", category, from, err)
		}
	}
}

func Test_BudgetRollover(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		call    func(context.Context, *Local) error
		want    []BudgetRollover
		wantErr string
	}{
		{
			name: "a category carries over from a month",
			call: func(ctx context.Context, st *Local) error {
				return st.SetBudgetRollover(ctx, "Expenses:Food", "2026-10")
			},
			want: []BudgetRollover{{"Expenses:Food", "2026-10"}},
		},
		{
			name:  "setting it again moves the start",
			setup: []given{rollover("Expenses:Food", "2026-10")},
			call: func(ctx context.Context, st *Local) error {
				return st.SetBudgetRollover(ctx, "Expenses:Food", "2026-08")
			},
			want: []BudgetRollover{{"Expenses:Food", "2026-08"}},
		},
		{
			name:  "categories are listed by name",
			setup: []given{rollover("Expenses:Rent", "2026-10")},
			call: func(ctx context.Context, st *Local) error {
				return st.SetBudgetRollover(ctx, "Expenses:Food", "2026-09")
			},
			want: []BudgetRollover{{"Expenses:Food", "2026-09"}, {"Expenses:Rent", "2026-10"}},
		},
		{
			name: "a category that does not exist",
			call: func(ctx context.Context, st *Local) error {
				return st.SetBudgetRollover(ctx, "Expenses:Nope", "2026-10")
			},
			wantErr: "account does not exist: Expenses:Nope",
		},
		{
			name: "income does not carry over",
			call: func(ctx context.Context, st *Local) error {
				return st.SetBudgetRollover(ctx, "Income:Salary", "2026-10")
			},
			wantErr: "Income:Salary is not a spending category: only an Expenses category, other than Uncategorized, can have a target or carry over",
		},
		{
			name: "uncategorized does not carry over",
			call: func(ctx context.Context, st *Local) error {
				return st.SetBudgetRollover(ctx, ledger.UncategorizedExpense, "2026-10")
			},
			wantErr: "Expenses:Uncategorized is not a spending category: only an Expenses category, other than Uncategorized, can have a target or carry over",
		},
		{
			name: "a start that is no month",
			call: func(ctx context.Context, st *Local) error {
				return st.SetBudgetRollover(ctx, "Expenses:Food", "2026-1")
			},
			wantErr: `invalid month "2026-1": use YYYY-MM`,
		},
		{
			name:  "clearing stops it, the others go on",
			setup: []given{rollover("Expenses:Food", "2026-10"), rollover("Expenses:Rent", "2026-10")},
			call:  func(ctx context.Context, st *Local) error { return st.ClearBudgetRollover(ctx, "Expenses:Food") },
			want:  []BudgetRollover{{"Expenses:Rent", "2026-10"}},
		},
		{
			name: "clearing a category that does not carry over is left as it is",
			call: func(ctx context.Context, st *Local) error { return st.ClearBudgetRollover(ctx, "Expenses:Food") },
		},
		{
			name:    "clearing a category that does not exist",
			setup:   []given{rollover("Expenses:Food", "2026-10")},
			call:    func(ctx context.Context, st *Local) error { return st.ClearBudgetRollover(ctx, "Expenses:Nope") },
			want:    []BudgetRollover{{"Expenses:Food", "2026-10"}},
			wantErr: "account does not exist: Expenses:Nope",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(targetLedger(), tc.setup...)...)

			// when
			err := tc.call(t.Context(), st)

			// then
			checkErr(t, err, tc.wantErr, nil)
			got, errList := st.BudgetRollovers(t.Context())
			if errList != nil {
				t.Fatal(errList)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("rollovers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// What a category that carries over brings into a month.
func Test_MonthBudget_rollover(t *testing.T) {
	with := func(steps ...given) []given {
		return append([]given{accounts("Assets:Dollars USD")}, steps...)
	}
	food := "Expenses:Food"
	tcs := []struct {
		name  string
		setup []given
		month string
		want  MonthBudget
	}{
		{
			name: "what was left carries into the next month",
			setup: with(rollover(food, "2026-09"), target("2026-09", food, eur(40000)), spending("2026-09-10", food, 15000),
				target("2026-10", food, eur(40000)), spending("2026-10-10", food, 10000)),
			month: "2026-10",
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(10000), true, eur(40000), true, eur(25000), eur(55000)}},
				SpentTotal: eur(10000), TargetTotal: eur(40000), CarriedTotal: eur(25000), LeftTotal: eur(55000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name:  "the first month carries nothing in",
			setup: with(rollover(food, "2026-09"), target("2026-09", food, eur(40000)), spending("2026-09-10", food, 15000)),
			month: "2026-09",
			want: MonthBudget{
				Month: "2026-09", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(15000), true, eur(40000), true, eur(0), eur(25000)}},
				SpentTotal: eur(15000), TargetTotal: eur(40000), CarriedTotal: eur(0), LeftTotal: eur(25000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name: "overspending carries as a negative",
			setup: with(rollover(food, "2026-09"), target("2026-09", food, eur(40000)), spending("2026-09-10", food, 60000),
				target("2026-10", food, eur(40000)), spending("2026-10-10", food, 10000)),
			month: "2026-10",
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(10000), true, eur(40000), true, eur(-20000), eur(10000)}},
				SpentTotal: eur(10000), TargetTotal: eur(40000), CarriedTotal: eur(-20000), LeftTotal: eur(10000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name: "it builds up over several months",
			setup: with(rollover(food, "2026-08"), target("2026-08", food, eur(40000)), spending("2026-08-10", food, 15000),
				target("2026-09", food, eur(40000)), spending("2026-09-10", food, 10000), target("2026-10", food, eur(40000))),
			month: "2026-10",
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(0), true, eur(40000), true, eur(55000), eur(95000)}},
				SpentTotal: eur(0), TargetTotal: eur(40000), CarriedTotal: eur(55000), LeftTotal: eur(95000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name: "a month with no target goes on: it carries what it spent",
			setup: with(rollover(food, "2026-08"), target("2026-08", food, eur(40000)), spending("2026-08-10", food, 15000),
				spending("2026-09-10", food, 10000)),
			month: "2026-10",
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(0), false, eur(0), true, eur(15000), eur(15000)}},
				SpentTotal: eur(0), TargetTotal: eur(0), CarriedTotal: eur(15000), LeftTotal: eur(15000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name: "months before the start do not count",
			setup: with(rollover(food, "2026-10"), target("2026-09", food, eur(40000)), spending("2026-09-10", food, 15000),
				target("2026-10", food, eur(40000)), spending("2026-10-10", food, 10000)),
			month: "2026-11",
			want: MonthBudget{
				Month: "2026-11", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(0), false, eur(0), true, eur(30000), eur(30000)}},
				SpentTotal: eur(0), TargetTotal: eur(0), CarriedTotal: eur(30000), LeftTotal: eur(30000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name:  "a month before the start is not carrying yet",
			setup: with(rollover(food, "2026-10"), target("2026-09", food, eur(40000)), spending("2026-09-10", food, 15000)),
			month: "2026-09",
			want: MonthBudget{
				Month: "2026-09", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(15000), true, eur(40000), false, eur(0), eur(25000)}},
				SpentTotal: eur(15000), TargetTotal: eur(40000), CarriedTotal: eur(0), LeftTotal: eur(25000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name:  "without it nothing carries",
			setup: with(target("2026-09", food, eur(40000)), spending("2026-09-10", food, 15000), target("2026-10", food, eur(40000))),
			month: "2026-10",
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(0), true, eur(40000), false, eur(0), eur(40000)}},
				SpentTotal: eur(0), TargetTotal: eur(40000), CarriedTotal: eur(0), LeftTotal: eur(40000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name: "another category is not touched, and its spending stays unplanned",
			setup: with(rollover(food, "2026-09"), target("2026-09", food, eur(40000)), spending("2026-09-10", "Expenses:Rent", 90000),
				spending("2026-10-10", "Expenses:Rent", 500)),
			month: "2026-10",
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines: []MonthBudgetLine{
					{"Expenses:Rent", eur(500), false, eur(0), false, eur(0), eur(0)},
					{food, eur(0), false, eur(0), true, eur(40000), eur(40000)},
				},
				SpentTotal: eur(500), TargetTotal: eur(0), CarriedTotal: eur(40000), LeftTotal: eur(40000), UnbudgetedTotal: eur(500),
			},
		},
		{
			name: "each month is converted at its own rates",
			setup: with(rollover(food, "2026-09"), target("2026-09", food, eur(40000)), setRate("2026-09-01", "USD", "EUR", "0.90"),
				setRate("2026-10-01", "USD", "EUR", "0.50"), spendingUSD("2026-09-10", food, 10000)),
			month: "2026-10",
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(0), false, eur(0), true, eur(31000), eur(31000)}},
				SpentTotal: eur(0), TargetTotal: eur(0), CarriedTotal: eur(31000), LeftTotal: eur(31000), UnbudgetedTotal: eur(0),
			},
		},
		{
			name:  "dollars with no rate are left out of what is carried, and said so",
			setup: with(rollover(food, "2026-09"), target("2026-09", food, eur(40000)), spendingUSD("2026-09-10", food, 10000)),
			month: "2026-10",
			want: MonthBudget{
				Month: "2026-10", Base: "EUR",
				Lines:      []MonthBudgetLine{{food, eur(0), false, eur(0), true, eur(40000), eur(40000)}},
				SpentTotal: eur(0), TargetTotal: eur(0), CarriedTotal: eur(40000), LeftTotal: eur(40000), UnbudgetedTotal: eur(0),
				Missing: []ledger.Currency{"USD"},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(targetLedger(), tc.setup...)...)

			// when
			got, err := st.MonthBudget(t.Context(), MonthBudgetRequest{Month: tc.month, RateDate: tc.month + "-28"})

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
