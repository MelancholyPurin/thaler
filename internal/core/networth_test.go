package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// worth is a month of the report with its total in euros.
func worth(month string, minor int64) NetWorthMonth {
	return NetWorthMonth{Month: month, Total: eur(minor)}
}

// gift books amount of currency into an account against Income:Gift.
func gift(date, account string, amount ledger.Money) given {
	return stored(NewTransaction{Date: day(date), Postings: []ledger.Posting{
		{Account: account, Amount: amount}, {Account: "Income:Gift", Amount: ledger.Money{Minor: -amount.Minor, Currency: amount.Currency}}}})
}

func Test_NetWorthReport(t *testing.T) {
	bank := accounts("Assets:Bank EUR", "Assets:Dollars USD", "Liabilities:Card EUR")
	tcs := []struct {
		name    string
		setup   []given
		req     NetWorthRequest
		want    NetWorthReport
		wantErr string
	}{
		{
			name: "the balance at the end of each month, from before the range too",
			setup: []given{
				bank,
				earning("2026-07-15", "Income:Salary", 100000), // before the range
				earning("2026-09-01", "Income:Salary", 50000), spending("2026-09-10", "Expenses:Rent", 30000),
				spending("2026-11-02", "Expenses:Food", 1000),
			},
			req:  NetWorthRequest{From: "2026-09", To: "2026-11"},
			want: NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-09", 120000), worth("2026-10", 120000), worth("2026-11", 119000)}},
		},
		{
			name:  "a liability takes away from it",
			setup: []given{bank, earning("2026-09-01", "Income:Salary", 100000), gift("2026-10-05", "Liabilities:Card", eur(-40000))},
			req:   NetWorthRequest{From: "2026-09", To: "2026-10"},
			want:  NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-09", 100000), worth("2026-10", 60000)}},
		},
		{
			name:  "income and expense accounts are not worth anything",
			setup: []given{bank, gift("2026-10-05", "Assets:Bank", eur(1000))},
			req:   NetWorthRequest{From: "2026-10", To: "2026-10"},
			want:  NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-10", 1000)}},
		},
		{
			name:  "the last day of a month is in it, the first of the next is not",
			setup: []given{bank, earning("2026-09-30", "Income:Salary", 1), earning("2026-10-01", "Income:Salary", 10), earning("2026-10-31", "Income:Salary", 100), earning("2026-11-01", "Income:Salary", 1000)},
			req:   NetWorthRequest{From: "2026-09", To: "2026-10"},
			want:  NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-09", 1), worth("2026-10", 111)}},
		},
		{
			name:  "nothing in the ledger: every month, at nothing",
			setup: []given{bank},
			req:   NetWorthRequest{From: "2026-09", To: "2026-10"},
			want:  NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-09", 0), worth("2026-10", 0)}},
		},
		{
			name: "no range: the 12 months up to this one",
			setup: []given{
				bank, today("2026-10-22"), earning("2025-10-31", "Income:Salary", 7), earning("2026-03-01", "Income:Salary", 500),
			},
			req: NetWorthRequest{},
			want: func() NetWorthReport {
				r := NetWorthReport{Base: "EUR"}
				for i, m := range []string{"2025-11", "2025-12", "2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10"} {
					total := int64(7)
					if i >= 4 {
						total = 507
					}
					r.Months = append(r.Months, worth(m, total))
				}
				return r
			}(),
		},
		{
			name: "dollars at the rate of the end of each month, a rate entered later leaves a past month alone",
			setup: []given{
				bank, today("2026-12-01"),
				setRate("2026-09-15", "USD", "EUR", "0.90"), setRate("2026-10-20", "USD", "EUR", "2"), setRate("2026-11-30", "USD", "EUR", "3"),
				gift("2026-09-01", "Assets:Dollars", usd(1000)),
			},
			req: NetWorthRequest{From: "2026-09", To: "2026-11"},
			want: NetWorthReport{Base: "EUR", Months: []NetWorthMonth{
				worth("2026-09", 900),  // 0.90 on 09-30
				worth("2026-10", 2000), // 2 on 10-31
				worth("2026-11", 3000), // 3 on 11-30, the rate of the last day itself
			}},
		},
		{
			name: "a month before the first rate takes the rate of the end of the range",
			setup: []given{
				bank, today("2026-12-01"), setRate("2026-10-10", "USD", "EUR", "2"),
				gift("2026-08-01", "Assets:Dollars", usd(1000)),
			},
			req:  NetWorthRequest{From: "2026-08", To: "2026-10"},
			want: NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-08", 2000), worth("2026-09", 2000), worth("2026-10", 2000)}},
		},
		{
			name: "a rate dated after the day counted as today is not used",
			setup: []given{
				bank, today("2026-10-22"), setRate("2026-10-01", "USD", "EUR", "2"), setRate("2026-12-01", "USD", "EUR", "5"),
				gift("2026-10-01", "Assets:Dollars", usd(1000)),
			},
			req:  NetWorthRequest{From: "2026-10", To: "2026-12"},
			want: NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-10", 2000), worth("2026-11", 2000), worth("2026-12", 2000)}},
		},
		{
			name: "the day counted as today can be asked for",
			setup: []given{
				bank, today("2026-12-01"), setRate("2026-10-01", "USD", "EUR", "2"), setRate("2026-11-15", "USD", "EUR", "5"),
				gift("2026-10-01", "Assets:Dollars", usd(1000)),
			},
			req:  NetWorthRequest{From: "2026-10", To: "2026-12", RateDate: "2026-11-01"},
			want: NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-10", 2000), worth("2026-11", 2000), worth("2026-12", 2000)}},
		},
		{
			name: "a currency with no rate at all is named and left out, the rest still counts",
			setup: []given{
				bank, accounts("Assets:Yen JPY"),
				earning("2026-10-01", "Income:Salary", 500),
				gift("2026-10-02", "Assets:Yen", ledger.Money{Minor: 900, Currency: "JPY"}),
			},
			req:  NetWorthRequest{From: "2026-10", To: "2026-10"},
			want: NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-10", 500)}, Missing: []ledger.Currency{"JPY"}},
		},
		{
			name: "an account that holds nothing needs no rate",
			setup: []given{
				bank, accounts("Assets:Yen JPY"), earning("2026-10-01", "Income:Salary", 500),
				gift("2026-10-02", "Assets:Yen", ledger.Money{Minor: 900, Currency: "JPY"}),
				gift("2026-10-03", "Assets:Yen", ledger.Money{Minor: -900, Currency: "JPY"}),
			},
			req:  NetWorthRequest{From: "2026-10", To: "2026-10"},
			want: NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-10", 500)}},
		},
		{
			name: "a currency held only in some months is named when it is in one of them",
			setup: []given{
				bank, accounts("Assets:Yen JPY"),
				gift("2026-11-02", "Assets:Yen", ledger.Money{Minor: 900, Currency: "JPY"}),
			},
			req:  NetWorthRequest{From: "2026-10", To: "2026-11"},
			want: NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-10", 0), worth("2026-11", 0)}, Missing: []ledger.Currency{"JPY"}},
		},
		{name: "the start is not a month", setup: []given{bank}, req: NetWorthRequest{From: "soon", To: "2026-10"}, wantErr: `invalid month "soon": use YYYY-MM`},
		{name: "backwards", setup: []given{bank}, req: NetWorthRequest{From: "2026-10", To: "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
		{name: "more than ten years", setup: []given{bank}, req: NetWorthRequest{From: "2016-10", To: "2026-10"}, wantErr: "a report covers at most 120 months"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.NetWorthReport(t.Context(), tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if tc.wantErr != "" {
				return
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("NetWorthReport mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A month of the report is what BalanceReport says for its last day, which is also how
// each account's rounding is done (an account at a time, not a currency).
func Test_NetWorthReport_balanceMatches(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		from  string
		to    string
	}{
		{
			name: "two dollar accounts that round apart, a liability, rates that change",
			setup: []given{
				accounts("Assets:Bank EUR", "Assets:Dollars USD", "Assets:Cash USD", "Liabilities:Card EUR"),
				today("2026-12-15"),
				setRate("2026-08-20", "USD", "EUR", "0.915"), setRate("2026-10-12", "USD", "EUR", "0.8765"),
				earning("2026-08-01", "Income:Salary", 123456), spending("2026-09-10", "Expenses:Rent", 30001),
				gift("2026-08-05", "Assets:Dollars", usd(1001)), gift("2026-09-05", "Assets:Cash", usd(1001)),
				gift("2026-10-05", "Assets:Dollars", usd(-333)), gift("2026-11-30", "Liabilities:Card", eur(-777)),
			},
			from: "2026-07",
			to:   "2026-12",
		},
		{
			name: "a cent in each of two accounts at half a cent: each rounds up, the two together would not",
			setup: []given{
				accounts("Assets:Dollars USD", "Assets:Cash USD"), today("2026-10-15"), setRate("2026-10-01", "USD", "EUR", "0.5"),
				gift("2026-10-02", "Assets:Dollars", usd(1)), gift("2026-10-03", "Assets:Cash", usd(1)),
			},
			from: "2026-10",
			to:   "2026-10",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.NetWorthReport(t.Context(), NetWorthRequest{From: tc.from, To: tc.to})
			if err != nil {
				t.Fatal(err)
			}

			// then
			var want, have []ledger.Money
			for _, m := range got.Months {
				bal, err := st.BalanceReport(t.Context(), BalanceRequest{AsOf: lastDayOf(m.Month)})
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, bal.NetWorth.Total)
				have = append(have, m.Total)
			}
			if diff := cmp.Diff(want, have); diff != "" {
				t.Errorf("net worth mismatch (-BalanceReport +NetWorthReport):\n%s", diff)
			}
		})
	}
}

func Test_lastDayOf(t *testing.T) {
	tcs := []struct{ name, month, want string }{
		{"thirty-one days", "2026-10", "2026-10-31"},
		{"thirty", "2026-11", "2026-11-30"},
		{"february", "2026-02", "2026-02-28"},
		{"a leap february", "2028-02", "2028-02-29"},
		{"december", "2026-12", "2026-12-31"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := lastDayOf(tc.month)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("lastDayOf mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
