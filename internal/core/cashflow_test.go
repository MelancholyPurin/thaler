package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_savingsRate(t *testing.T) {
	tcs := []struct {
		name        string
		net, income int64
		want        SavingsRate
	}{
		{name: "a quarter", net: 50000, income: 200000, want: SavingsRate{2500, true}},
		{name: "all of it", net: 100, income: 100, want: SavingsRate{10000, true}},
		{name: "nothing kept", net: 0, income: 100, want: SavingsRate{0, true}},
		{name: "overspent by all of it", net: -100, income: 100, want: SavingsRate{-10000, true}},
		{name: "more than came in", net: -150, income: 100, want: SavingsRate{-15000, true}},
		{name: "a third rounds down", net: 1, income: 3, want: SavingsRate{3333, true}},
		{name: "two thirds round up", net: 2, income: 3, want: SavingsRate{6667, true}},
		{name: "minus two thirds round away from zero", net: -2, income: 3, want: SavingsRate{-6667, true}},
		{name: "a half goes up", net: 1, income: 32, want: SavingsRate{313, true}},
		{name: "a negative half goes away from zero", net: -1, income: 32, want: SavingsRate{-313, true}},
		{name: "just under a half goes down", net: 1, income: 33, want: SavingsRate{303, true}},
		{name: "exact", net: 1, income: 16, want: SavingsRate{625, true}},
		{name: "no income: no share", net: 100, income: 0, want: SavingsRate{}},
		{name: "no income and spending: no share", net: -100, income: 0, want: SavingsRate{}},
		{name: "negative income: no share", net: 100, income: -50, want: SavingsRate{}},
		{name: "amounts too big to multiply in 64 bits", net: 1_000_000_000_000_000, income: 2_000_000_000_000_000, want: SavingsRate{5000, true}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := savingsRate(tc.net, tc.income)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("savingsRate mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// month is a month of the report, with what was kept and its share written out, not
// worked out the way the code does.
func month(m string, income, spending, net, basisPoints int64, known bool) CashFlowMonth {
	return CashFlowMonth{Month: m, Income: eur(income), Spending: eur(spending), Net: eur(net), Savings: SavingsRate{basisPoints, known}}
}

func Test_CashFlowReport(t *testing.T) {
	bank := accounts("Assets:Bank EUR", "Assets:Dollars USD")
	tcs := []struct {
		name    string
		setup   []given
		req     CashFlowRequest
		want    CashFlowReport
		wantErr string
	}{
		{
			name: "what came in, what went out, what was kept",
			setup: []given{
				bank,
				earning("2026-09-01", "Income:Salary", 300000), earning("2026-09-15", "Income:Gift", 10000),
				spending("2026-09-10", "Expenses:Rent", 90000), spending("2026-09-20", "Expenses:Food", 20000),
				spending("2026-10-31", "Expenses:Food", 5000),
			},
			req: CashFlowRequest{From: "2026-09", To: "2026-11"},
			want: CashFlowReport{
				Base:   "EUR",
				Months: []CashFlowMonth{month("2026-09", 310000, 110000, 200000, 6452, true), month("2026-10", 0, 5000, -5000, 0, false), month("2026-11", 0, 0, 0, 0, false)},
				Total:  month("", 310000, 115000, 195000, 6290, true),
			},
		},
		{
			name:  "the share kept: a month with income has one, one without does not",
			setup: []given{bank, earning("2026-09-01", "Income:Salary", 200000), spending("2026-09-10", "Expenses:Rent", 150000), spending("2026-10-10", "Expenses:Rent", 1000)},
			req:   CashFlowRequest{From: "2026-09", To: "2026-10"},
			want: CashFlowReport{
				Base:   "EUR",
				Months: []CashFlowMonth{month("2026-09", 200000, 150000, 50000, 2500, true), month("2026-10", 0, 1000, -1000, 0, false)},
				Total:  month("", 200000, 151000, 49000, 2450, true),
			},
		},
		{
			name:  "more spent than came in is a negative share",
			setup: []given{bank, earning("2026-10-01", "Income:Salary", 1000), spending("2026-10-10", "Expenses:Rent", 1500)},
			req:   CashFlowRequest{From: "2026-10", To: "2026-10"},
			want:  CashFlowReport{Base: "EUR", Months: []CashFlowMonth{month("2026-10", 1000, 1500, -500, -5000, true)}, Total: month("", 1000, 1500, -500, -5000, true)},
		},
		{
			name:  "a refund of income comes off it, and of spending too",
			setup: []given{bank, earning("2026-10-01", "Income:Salary", 100000), earning("2026-10-20", "Income:Salary", -20000), spending("2026-10-10", "Expenses:Fun", 5000), spending("2026-10-12", "Expenses:Fun", -2000)},
			req:   CashFlowRequest{From: "2026-10", To: "2026-10"},
			want:  CashFlowReport{Base: "EUR", Months: []CashFlowMonth{month("2026-10", 80000, 3000, 77000, 9625, true)}, Total: month("", 80000, 3000, 77000, 9625, true)},
		},
		{
			name: "a transfer between your accounts is neither",
			setup: []given{
				bank, earning("2026-10-01", "Income:Salary", 100000),
				stored(NewTransaction{Date: day("2026-10-03"), Postings: []ledger.Posting{
					{Account: "Assets:Bank", Amount: eur(-5000)}, {Account: ledger.TransferAccount, Amount: eur(5000)}}}),
			},
			req:  CashFlowRequest{From: "2026-10", To: "2026-10"},
			want: CashFlowReport{Base: "EUR", Months: []CashFlowMonth{month("2026-10", 100000, 0, 100000, 10000, true)}, Total: month("", 100000, 0, 100000, 10000, true)},
		},
		{
			name:  "the first and the last day of a month are in it",
			setup: []given{bank, earning("2026-09-30", "Income:Salary", 1), earning("2026-10-01", "Income:Salary", 10), earning("2026-10-31", "Income:Salary", 100), earning("2026-11-01", "Income:Salary", 1000)},
			req:   CashFlowRequest{From: "2026-10", To: "2026-10"},
			want:  CashFlowReport{Base: "EUR", Months: []CashFlowMonth{month("2026-10", 110, 0, 110, 10000, true)}, Total: month("", 110, 0, 110, 10000, true)},
		},
		{
			name:  "no range: the 12 months up to this one",
			setup: []given{bank, today("2026-10-22"), earning("2026-10-01", "Income:Salary", 500), earning("2025-10-31", "Income:Salary", 7)},
			req:   CashFlowRequest{},
			want: func() CashFlowReport {
				r := CashFlowReport{Base: "EUR", Total: month("", 500, 0, 500, 10000, true)}
				for _, m := range []string{"2025-11", "2025-12", "2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09"} {
					r.Months = append(r.Months, month(m, 0, 0, 0, 0, false))
				}
				r.Months = append(r.Months, month("2026-10", 500, 0, 500, 10000, true))
				return r
			}(),
		},
		{
			name: "dollars at the rate of their day, a currency without a rate left out",
			setup: []given{
				bank, accounts("Assets:Yen JPY"),
				setRate("2026-09-01", "USD", "EUR", "0.90"), setRate("2026-10-01", "USD", "EUR", "2"), today("2026-12-01"),
				stored(NewTransaction{Date: day("2026-09-10"), Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(1000)}, {Account: "Income:Gift", Amount: usd(-1000)}}}),
				stored(NewTransaction{Date: day("2026-10-10"), Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(1000)}, {Account: "Income:Gift", Amount: usd(-1000)}}}),
				stored(NewTransaction{Date: day("2026-10-12"), Postings: []ledger.Posting{
					{Account: "Assets:Yen", Amount: ledger.Money{Minor: -500, Currency: "JPY"}}, {Account: "Expenses:Travel", Amount: ledger.Money{Minor: 500, Currency: "JPY"}}}}),
			},
			req: CashFlowRequest{From: "2026-09", To: "2026-10"},
			want: CashFlowReport{
				Base: "EUR", Months: []CashFlowMonth{month("2026-09", 900, 0, 900, 10000, true), month("2026-10", 2000, 0, 2000, 10000, true)}, Total: month("", 2900, 0, 2900, 10000, true),
				Missing: []ledger.Currency{"JPY"},
			},
		},
		{name: "the start is not a month", setup: []given{bank}, req: CashFlowRequest{From: "soon", To: "2026-10"}, wantErr: `invalid month "soon": use YYYY-MM`},
		{name: "backwards", setup: []given{bank}, req: CashFlowRequest{From: "2026-10", To: "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
		{name: "more than ten years", setup: []given{bank}, req: CashFlowRequest{From: "2016-10", To: "2026-10"}, wantErr: "a report covers at most 120 months"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.CashFlowReport(t.Context(), tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if tc.wantErr != "" {
				return
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("CashFlowReport mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// What the cash flow calls spending is what the spending report adds up to.
func Test_CashFlowReport_spendingMatches(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
	}{
		{
			name: "several months and categories, a refund",
			setup: []given{
				accounts("Assets:Bank EUR"),
				spending("2026-08-10", "Expenses:Food", 4000), spending("2026-09-01", "Expenses:Rent", 90000), spending("2026-09-30", "Expenses:Food", -500),
				spending("2026-10-31", ledger.UncategorizedExpense, 2500), earning("2026-10-01", "Income:Salary", 250000),
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			flow, err := st.CashFlowReport(t.Context(), CashFlowRequest{From: "2026-08", To: "2026-10"})
			if err != nil {
				t.Fatal(err)
			}
			spent, err := st.SpendingReport(t.Context(), SpendingRequest{From: "2026-08", To: "2026-10"})
			if err != nil {
				t.Fatal(err)
			}

			// then
			var got []ledger.Money
			for _, m := range flow.Months {
				got = append(got, m.Spending)
			}
			if diff := cmp.Diff(spent.Totals, got); diff != "" {
				t.Errorf("monthly spending mismatch (-spending report +cash flow):\n%s", diff)
			}
			if diff := cmp.Diff(spent.Total, flow.Total.Spending); diff != "" {
				t.Errorf("total spending mismatch (-spending report +cash flow):\n%s", diff)
			}
		})
	}
}
