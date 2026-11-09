package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// A salary, some food, and 100.00 EUR turned into 108.50 USD.
func balancesLedger() []given {
	return []given{
		accounts("Assets:Bank EUR", "Assets:Cash-USD USD"),
		earning("2026-10-01", "Income:Salary", 250000),
		spending("2026-10-10", "Expenses:Food", 4000),
		stored(NewTransaction{
			Date: day("2026-10-12"),
			Postings: []ledger.Posting{
				{Account: "Assets:Bank", Amount: eur(-10000)},
				{Account: ledger.ConversionAccount, Amount: eur(10000)},
				{Account: ledger.ConversionAccount, Amount: usd(-10850)},
				{Account: "Assets:Cash-USD", Amount: usd(10850)},
			},
		}),
	}
}

func setRate(date, base, quote, rate string) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if err := st.SetRate(t.Context(), date, ledger.Currency(base), ledger.Currency(quote), rate); err != nil {
			t.Fatal(err)
		}
	}
}

func Test_BalanceReport(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		req     BalanceRequest
		want    BalanceReport
		wantErr string
	}{
		{
			name:  "every account, as of today, with the dollars missing a rate",
			setup: append(balancesLedger(), today("2026-10-21")),
			req:   BalanceRequest{IncludeAll: true},
			want: BalanceReport{
				Date: "2026-10-21",
				Lines: []Balance{
					{"Assets:Bank", ledger.Assets, eur(236000)},
					{"Assets:Cash-USD", ledger.Assets, usd(10850)},
					{ledger.ConversionAccount, ledger.Equity, eur(10000)},
					{ledger.ConversionAccount, ledger.Equity, usd(-10850)},
					{"Expenses:Food", ledger.Expenses, eur(4000)},
					{"Income:Salary", ledger.Income, eur(-250000)},
				},
				NetWorth: NetWorth{Base: "EUR", Total: eur(236000), Missing: []ledger.Currency{"USD"}},
			},
		},
		{
			name:  "only assets and liabilities by default",
			setup: append(balancesLedger(), today("2026-10-21")),
			req:   BalanceRequest{},
			want: BalanceReport{
				Date: "2026-10-21",
				Lines: []Balance{
					{"Assets:Bank", ledger.Assets, eur(236000)},
					{"Assets:Cash-USD", ledger.Assets, usd(10850)},
				},
				NetWorth: NetWorth{Base: "EUR", Total: eur(236000), Missing: []ledger.Currency{"USD"}},
			},
		},
		{
			name:  "with a rate the dollars count",
			setup: append(balancesLedger(), setRate("2026-10-01", "USD", "EUR", "0.92"), today("2026-10-21")),
			req:   BalanceRequest{},
			want: BalanceReport{
				Date: "2026-10-21",
				Lines: []Balance{
					{"Assets:Bank", ledger.Assets, eur(236000)},
					{"Assets:Cash-USD", ledger.Assets, usd(10850)},
				},
				NetWorth: NetWorth{Base: "EUR", Total: eur(245982)}, // 236000 + 10850 * 0.92
			},
		},
		{
			name:  "as of a day before the dollars existed",
			setup: append(balancesLedger(), today("2026-10-21")),
			req:   BalanceRequest{AsOf: "2026-10-09", IncludeAll: true},
			want: BalanceReport{
				Date: "2026-10-09",
				Lines: []Balance{
					{"Assets:Bank", ledger.Assets, eur(250000)},
					{"Income:Salary", ledger.Income, eur(-250000)},
				},
				NetWorth: NetWorth{Base: "EUR", Total: eur(250000)},
			},
		},
		{
			name: "a debt lowers the net worth",
			setup: append(balancesLedger(), accounts("Liabilities:Card EUR"), stored(NewTransaction{
				Date:     day("2026-10-03"),
				Postings: []ledger.Posting{{Account: "Liabilities:Card", Amount: eur(-50000)}, {Account: "Expenses:Food", Amount: eur(50000)}},
			}), today("2026-10-21")),
			req: BalanceRequest{AsOf: "2026-10-09"},
			want: BalanceReport{
				Date: "2026-10-09",
				Lines: []Balance{
					{"Assets:Bank", ledger.Assets, eur(250000)},
					{"Liabilities:Card", ledger.Liabilities, eur(-50000)},
				},
				NetWorth: NetWorth{Base: "EUR", Total: eur(200000)},
			},
		},
		{
			name:  "an empty ledger",
			setup: []given{today("2026-10-21")},
			want:  BalanceReport{Date: "2026-10-21", NetWorth: NetWorth{Base: "EUR", Total: eur(0)}},
		},
		{
			name:    "a date that is not a date",
			setup:   balancesLedger(),
			req:     BalanceRequest{AsOf: "not a date"},
			wantErr: `invalid date "not a date": use YYYY-MM-DD or DD.MM.YYYY`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.BalanceReport(t.Context(), tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("report mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// October has food, rent, fun and a salary in euros, and a gift in dollars;
// September has a little food.
func budgetLedger() []given {
	return []given{
		accounts("Assets:Bank EUR", "Assets:Cash-USD USD"),
		spending("2026-09-30", "Expenses:Food", 300),
		spending("2026-10-01", "Expenses:Food", 500),
		spending("2026-10-20", "Expenses:Rent", 100000),
		spending("2026-10-21", "Expenses:Fun", 500), // ties with Food: sorted by name
		earning("2026-10-02", "Income:Salary", 200000),
		stored(NewTransaction{
			Date:     day("2026-10-03"),
			Postings: []ledger.Posting{{Account: "Assets:Cash-USD", Amount: usd(1000)}, {Account: "Income:Gift", Amount: usd(-1000)}},
		}),
	}
}

func Test_BudgetReport(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		req   BudgetRequest
		want  BudgetReport
	}{
		{
			name:  "october, with the dollar gift missing a rate",
			setup: budgetLedger(),
			req:   BudgetRequest{From: "2026-10-01", To: "2026-10-31", RateDate: "2026-10-22"},
			want: BudgetReport{
				Base:         "EUR",
				Income:       []BudgetLine{{"Income:Salary", eur(200000)}},
				Expenses:     []BudgetLine{{"Expenses:Rent", eur(100000)}, {"Expenses:Food", eur(500)}, {"Expenses:Fun", eur(500)}}, // largest first, ties by name
				IncomeTotal:  eur(200000),
				ExpenseTotal: eur(101000),
				Missing:      []ledger.Currency{"USD"},
			},
		},
		{
			name:  "a past month uses the rate of that month, not a later one",
			setup: append(budgetLedger(), setRate("2026-10-01", "USD", "EUR", "0.90"), setRate("2026-11-15", "USD", "EUR", "2"), today("2026-12-01")),
			req:   BudgetRequest{From: "2026-10-01", To: "2026-10-31"}, // RateDate defaults to today, capped at To
			want: BudgetReport{
				Base:         "EUR",
				Income:       []BudgetLine{{"Income:Salary", eur(200000)}, {"Income:Gift", eur(900)}},
				Expenses:     []BudgetLine{{"Expenses:Rent", eur(100000)}, {"Expenses:Food", eur(500)}, {"Expenses:Fun", eur(500)}},
				IncomeTotal:  eur(200900),
				ExpenseTotal: eur(101000),
			},
		},
		{
			name:  "all time uses the rate of each day, not today's",
			setup: append(budgetLedger(), setRate("2026-10-01", "USD", "EUR", "0.90"), setRate("2026-11-15", "USD", "EUR", "2"), today("2026-12-01")),
			req:   BudgetRequest{},
			want: BudgetReport{
				Base:         "EUR",
				Income:       []BudgetLine{{"Income:Salary", eur(200000)}, {"Income:Gift", eur(900)}},
				Expenses:     []BudgetLine{{"Expenses:Rent", eur(100000)}, {"Expenses:Food", eur(800)}, {"Expenses:Fun", eur(500)}},
				IncomeTotal:  eur(200900),
				ExpenseTotal: eur(101300),
			},
		},
		{
			name:  "a rate entered after the day counts only from its own day on",
			setup: append(budgetLedger(), setRate("2026-10-02", "USD", "EUR", "0.90"), setRate("2026-10-10", "USD", "EUR", "2")),
			req:   BudgetRequest{From: "2026-10-01", To: "2026-10-31", RateDate: "2026-10-31"},
			want: BudgetReport{
				Base:         "EUR",
				Income:       []BudgetLine{{"Income:Salary", eur(200000)}, {"Income:Gift", eur(900)}}, // 3 October: 0.90, not 2
				Expenses:     []BudgetLine{{"Expenses:Rent", eur(100000)}, {"Expenses:Food", eur(500)}, {"Expenses:Fun", eur(500)}},
				IncomeTotal:  eur(200900),
				ExpenseTotal: eur(101000),
			},
		},
		{
			name:  "a day before the first rate takes the rate at the end of the period",
			setup: append(budgetLedger(), setRate("2026-10-15", "USD", "EUR", "0.80")),
			req:   BudgetRequest{From: "2026-10-01", To: "2026-10-31", RateDate: "2026-10-31"},
			want: BudgetReport{
				Base:         "EUR",
				Income:       []BudgetLine{{"Income:Salary", eur(200000)}, {"Income:Gift", eur(800)}},
				Expenses:     []BudgetLine{{"Expenses:Rent", eur(100000)}, {"Expenses:Food", eur(500)}, {"Expenses:Fun", eur(500)}},
				IncomeTotal:  eur(200800),
				ExpenseTotal: eur(101000),
			},
		},
		{
			name:  "a month with nothing in it",
			setup: budgetLedger(),
			req:   BudgetRequest{From: "2026-07-01", To: "2026-07-31"},
			want:  BudgetReport{Base: "EUR", IncomeTotal: eur(0), ExpenseTotal: eur(0)},
		},
		{
			name:  "money moved between your own accounts is neither income nor spending",
			setup: []given{accounts("Assets:Bank EUR", "Assets:Savings EUR"), moved("Assets:Bank", "Assets:Savings", 5000, "2026-10-05")},
			req:   BudgetRequest{From: "2026-10-01", To: "2026-10-31"},
			want:  BudgetReport{Base: "EUR", IncomeTotal: eur(0), ExpenseTotal: eur(0)},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.BudgetReport(t.Context(), tc.req)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("report mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// moved transfers an amount between two accounts of the same currency.
func moved(from, to string, minor int64, date string) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.Transfer(t.Context(), TransferRequest{From: from, To: to, Amount: eur(minor), Date: day(date)}); err != nil {
			t.Fatal(err)
		}
	}
}

func Test_categoryTotals(t *testing.T) {
	setup := []given{
		accounts("Assets:Bank EUR"),
		spending("2026-09-30", "Expenses:Food", 300),
		spending("2026-10-01", "Expenses:Food", 500),
		spending("2026-10-15", "Expenses:Food", 250),
		spending("2026-10-20", "Expenses:Rent", 100000),
		earning("2026-10-02", "Income:Salary", 200000),
	}
	tcs := []struct {
		name     string
		from, to string
		want     []datedBalance
	}{
		{
			name: "one month", from: "2026-10-01", to: "2026-10-31",
			want: []datedBalance{
				{Balance{"Expenses:Food", ledger.Expenses, eur(500)}, "2026-10-01"},
				{Balance{"Expenses:Food", ledger.Expenses, eur(250)}, "2026-10-15"},
				{Balance{"Expenses:Rent", ledger.Expenses, eur(100000)}, "2026-10-20"},
				{Balance{"Income:Salary", ledger.Income, eur(-200000)}, "2026-10-02"},
			},
		},
		{
			name: "all time, a line per day", from: "", to: "",
			want: []datedBalance{
				{Balance{"Expenses:Food", ledger.Expenses, eur(300)}, "2026-09-30"},
				{Balance{"Expenses:Food", ledger.Expenses, eur(500)}, "2026-10-01"},
				{Balance{"Expenses:Food", ledger.Expenses, eur(250)}, "2026-10-15"},
				{Balance{"Expenses:Rent", ledger.Expenses, eur(100000)}, "2026-10-20"},
				{Balance{"Income:Salary", ledger.Income, eur(-200000)}, "2026-10-02"},
			},
		},
		{name: "from a date on", from: "2026-10-16", to: "", want: []datedBalance{{Balance{"Expenses:Rent", ledger.Expenses, eur(100000)}, "2026-10-20"}}},
		{name: "up to a date", from: "", to: "2026-09-30", want: []datedBalance{{Balance{"Expenses:Food", ledger.Expenses, eur(300)}, "2026-09-30"}}},
		{name: "a period with nothing", from: "2025-01-01", to: "2025-01-31"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, setup...)

			// when
			got, err := st.repo.queries().categoryTotals(t.Context(), tc.from, tc.to)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty, cmp.AllowUnexported(datedBalance{})); diff != "" {
				t.Errorf("totals mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
