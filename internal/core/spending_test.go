package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func eurs(minor ...int64) []ledger.Money {
	out := make([]ledger.Money, len(minor))
	for i, m := range minor {
		out[i] = eur(m)
	}
	return out
}

// emptySpending is the report of a ledger with no spending in the months from..to.
func emptySpending(from, to string) SpendingReport {
	r := SpendingReport{Base: "EUR", Total: eur(0)}
	for m, end := day(from+"-01"), day(to+"-01"); !m.After(end); m = m.AddDate(0, 1, 0) {
		r.Months = append(r.Months, m.Format(monthLayout))
		r.Totals = append(r.Totals, eur(0))
	}
	return r
}

func Test_SpendingReport(t *testing.T) {
	bank := accounts("Assets:Bank EUR", "Assets:Dollars USD")
	tcs := []struct {
		name    string
		setup   []given
		req     SpendingRequest
		want    SpendingReport
		wantErr string
	}{
		{
			name: "months side by side, the largest category first, a month nobody spent in",
			setup: []given{
				bank,
				spending("2026-08-10", "Expenses:Food", 4000), spending("2026-08-20", "Expenses:Food", 1000),
				spending("2026-08-01", "Expenses:Rent", 90000), spending("2026-10-01", "Expenses:Rent", 90000),
				spending("2026-10-31", "Expenses:Fun", 2500),
			},
			req: SpendingRequest{From: "2026-08", To: "2026-10"},
			want: SpendingReport{
				Base:   "EUR",
				Months: []string{"2026-08", "2026-09", "2026-10"},
				Categories: []CategorySpending{
					{Name: "Expenses:Rent", Months: eurs(90000, 0, 90000), Total: eur(180000)},
					{Name: "Expenses:Food", Months: eurs(5000, 0, 0), Total: eur(5000)},
					{Name: "Expenses:Fun", Months: eurs(0, 0, 2500), Total: eur(2500)},
				},
				Totals: eurs(95000, 0, 92500),
				Total:  eur(187500),
			},
		},
		{
			name: "the first and the last day of a month are in it",
			setup: []given{
				bank,
				spending("2026-09-30", "Expenses:Food", 100), spending("2026-10-01", "Expenses:Food", 200),
				spending("2026-10-31", "Expenses:Food", 400), spending("2026-11-01", "Expenses:Food", 800),
			},
			req: SpendingRequest{From: "2026-10", To: "2026-10"},
			want: SpendingReport{
				Base: "EUR", Months: []string{"2026-10"},
				Categories: []CategorySpending{{Name: "Expenses:Food", Months: eurs(600), Total: eur(600)}},
				Totals:     eurs(600), Total: eur(600),
			},
		},
		{
			name:  "over a new year",
			setup: []given{bank, spending("2025-12-24", "Expenses:Gifts", 7000), spending("2026-02-14", "Expenses:Gifts", 3000)},
			req:   SpendingRequest{From: "2025-12", To: "2026-02"},
			want: SpendingReport{
				Base: "EUR", Months: []string{"2025-12", "2026-01", "2026-02"},
				Categories: []CategorySpending{{Name: "Expenses:Gifts", Months: eurs(7000, 0, 3000), Total: eur(10000)}},
				Totals:     eurs(7000, 0, 3000), Total: eur(10000),
			},
		},
		{
			name: "income and transfers are not spending, Uncategorized is",
			setup: []given{
				bank,
				earning("2026-10-01", "Income:Salary", 250000),
				spending("2026-10-02", ledger.UncategorizedExpense, 1200),
				stored(NewTransaction{Date: day("2026-10-03"), Postings: []ledger.Posting{
					{Account: "Assets:Bank", Amount: eur(-5000)}, {Account: ledger.TransferAccount, Amount: eur(5000)}}}),
			},
			req: SpendingRequest{From: "2026-10", To: "2026-10"},
			want: SpendingReport{
				Base: "EUR", Months: []string{"2026-10"},
				Categories: []CategorySpending{{Name: ledger.UncategorizedExpense, Months: eurs(1200), Total: eur(1200)}},
				Totals:     eurs(1200), Total: eur(1200),
			},
		},
		{
			name:  "a refund counts against the month it came back in",
			setup: []given{bank, spending("2026-09-05", "Expenses:Fun", 5000), spending("2026-10-05", "Expenses:Fun", -2000)},
			req:   SpendingRequest{From: "2026-09", To: "2026-10"},
			want: SpendingReport{
				Base: "EUR", Months: []string{"2026-09", "2026-10"},
				Categories: []CategorySpending{{Name: "Expenses:Fun", Months: eurs(5000, -2000), Total: eur(3000)}},
				Totals:     eurs(5000, -2000), Total: eur(3000),
			},
		},
		{
			name:  "a category that came out even is still there",
			setup: []given{bank, spending("2026-09-05", "Expenses:Fun", 5000), spending("2026-10-05", "Expenses:Fun", -5000)},
			req:   SpendingRequest{From: "2026-09", To: "2026-10"},
			want: SpendingReport{
				Base: "EUR", Months: []string{"2026-09", "2026-10"},
				Categories: []CategorySpending{{Name: "Expenses:Fun", Months: eurs(5000, -5000), Total: eur(0)}},
				Totals:     eurs(5000, -5000), Total: eur(0),
			},
		},
		{
			name:  "equal totals go by name, and a negative one comes last",
			setup: []given{bank, spending("2026-10-05", "Expenses:Zoo", 100), spending("2026-10-05", "Expenses:Art", 100), spending("2026-10-05", "Expenses:Gift", -300)},
			req:   SpendingRequest{From: "2026-10", To: "2026-10"},
			want: SpendingReport{
				Base: "EUR", Months: []string{"2026-10"},
				Categories: []CategorySpending{
					{Name: "Expenses:Art", Months: eurs(100), Total: eur(100)},
					{Name: "Expenses:Zoo", Months: eurs(100), Total: eur(100)},
					{Name: "Expenses:Gift", Months: eurs(-300), Total: eur(-300)},
				},
				Totals: eurs(-100), Total: eur(-100),
			},
		},
		{
			name:  "nothing spent: the months are there, with nothing in them",
			setup: []given{bank},
			req:   SpendingRequest{From: "2026-09", To: "2026-10"},
			want:  SpendingReport{Base: "EUR", Months: []string{"2026-09", "2026-10"}, Totals: eurs(0, 0), Total: eur(0)},
		},
		{
			name:  "no range: the 12 months up to this one",
			setup: []given{bank, today("2026-10-22"), spending("2025-10-31", "Expenses:Food", 1), spending("2025-11-01", "Expenses:Food", 10), spending("2026-10-22", "Expenses:Food", 100)},
			req:   SpendingRequest{},
			want: SpendingReport{
				Base:       "EUR",
				Months:     []string{"2025-11", "2025-12", "2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10"},
				Categories: []CategorySpending{{Name: "Expenses:Food", Months: eurs(10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 100), Total: eur(110)}},
				Totals:     eurs(10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 100), Total: eur(110),
			},
		},
		{
			name:  "only a start: up to this month",
			setup: []given{bank, today("2026-10-22")},
			req:   SpendingRequest{From: "2026-08"},
			want:  SpendingReport{Base: "EUR", Months: []string{"2026-08", "2026-09", "2026-10"}, Totals: eurs(0, 0, 0), Total: eur(0)},
		},
		{
			name:  "only an end: the 12 months up to it",
			setup: []given{bank},
			req:   SpendingRequest{To: "2026-02"},
			want: SpendingReport{
				Base:   "EUR",
				Months: []string{"2025-03", "2025-04", "2025-05", "2025-06", "2025-07", "2025-08", "2025-09", "2025-10", "2025-11", "2025-12", "2026-01", "2026-02"},
				Totals: eurs(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0), Total: eur(0),
			},
		},
		{
			name: "dollars at the rate of the day they were spent, a currency without a rate left out with the category it alone spent in",
			setup: []given{
				bank, accounts("Assets:Yen JPY"),
				setRate("2026-09-01", "USD", "EUR", "0.90"), setRate("2026-10-01", "USD", "EUR", "2"),
				spendingUSD("2026-09-10", "Expenses:Food", 1000), spendingUSD("2026-10-10", "Expenses:Food", 1000),
				stored(NewTransaction{Date: day("2026-10-12"), Postings: []ledger.Posting{
					{Account: "Assets:Yen", Amount: ledger.Money{Minor: -500, Currency: "JPY"}}, {Account: "Expenses:Travel", Amount: ledger.Money{Minor: 500, Currency: "JPY"}}}}),
				today("2026-12-01"),
			},
			req: SpendingRequest{From: "2026-09", To: "2026-10"},
			want: SpendingReport{
				Base: "EUR", Months: []string{"2026-09", "2026-10"},
				Categories: []CategorySpending{{Name: "Expenses:Food", Months: eurs(900, 2000), Total: eur(2900)}},
				Totals:     eurs(900, 2000), Total: eur(2900), Missing: []ledger.Currency{"JPY"},
			},
		},
		{
			name:  "a day before the first rate takes the rate at the end of the range",
			setup: []given{bank, setRate("2026-10-15", "USD", "EUR", "0.80"), spendingUSD("2026-09-10", "Expenses:Food", 1000), today("2026-12-01")},
			req:   SpendingRequest{From: "2026-09", To: "2026-10"},
			want: SpendingReport{
				Base: "EUR", Months: []string{"2026-09", "2026-10"},
				Categories: []CategorySpending{{Name: "Expenses:Food", Months: eurs(800, 0), Total: eur(800)}},
				Totals:     eurs(800, 0), Total: eur(800),
			},
		},
		{name: "the start is not a month", setup: []given{bank}, req: SpendingRequest{From: "soon", To: "2026-10"}, wantErr: `invalid month "soon": use YYYY-MM`},
		{name: "the end is not a month", setup: []given{bank}, req: SpendingRequest{From: "2026-10", To: "2026-13"}, wantErr: `invalid month "2026-13": use YYYY-MM`},
		{name: "a day is not a month", setup: []given{bank}, req: SpendingRequest{From: "2026-10-01", To: "2026-10"}, wantErr: `invalid month "2026-10-01": use YYYY-MM`},
		{name: "backwards", setup: []given{bank}, req: SpendingRequest{From: "2026-10", To: "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
		{
			name:  "ten years is the most",
			setup: []given{bank},
			req:   SpendingRequest{From: "2016-11", To: "2026-10"},
			want:  emptySpending("2016-11", "2026-10"),
		},
		{name: "a month more is too long", setup: []given{bank}, req: SpendingRequest{From: "2016-10", To: "2026-10"}, wantErr: "a report covers at most 120 months"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.SpendingReport(t.Context(), tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if tc.wantErr != "" {
				return
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("SpendingReport mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
