package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// paid books money leaving Assets:Bank for a category, to a payee.
func paid(date, payee, category string, amount int64) given {
	return stored(NewTransaction{
		Date: day(date), Payee: payee,
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-amount)}, {Account: category, Amount: eur(amount)}},
	})
}

func payee(name string, amount int64, txs int) PayeeSpending {
	return PayeeSpending{Name: name, Amount: eur(amount), Transactions: txs}
}

func Test_PayeeReport(t *testing.T) {
	bank := accounts("Assets:Bank EUR", "Assets:Dollars USD")
	tcs := []struct {
		name    string
		setup   []given
		req     PayeeRequest
		want    PayeeReport
		wantErr string
	}{
		{
			name: "the payee that took most first, with how many transactions",
			setup: []given{
				bank,
				paid("2026-10-01", "Landlord", "Expenses:Rent", 90000),
				paid("2026-10-03", "REWE", "Expenses:Food", 4000), paid("2026-10-10", "REWE", "Expenses:Food", 2500), paid("2026-10-17", "REWE", "Expenses:Food", 1500),
				paid("2026-10-05", "Cinema", "Expenses:Fun", 1200),
			},
			req: PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("Landlord", 90000, 1), payee("REWE", 8000, 3), payee("Cinema", 1200, 1)},
				Total:  eur(99200),
			},
		},
		{
			name: "the same payee written in another case or with other spaces is one",
			setup: []given{
				bank,
				paid("2026-10-01", "REWE", "Expenses:Food", 1000), paid("2026-10-02", "rewe", "Expenses:Food", 2000),
				paid("2026-10-03", " Rewe  Markt ", "Expenses:Food", 4000), paid("2026-10-04", "REWE MARKT", "Expenses:Food", 8000),
			},
			req: PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("REWE MARKT", 12000, 2), payee("rewe", 3000, 2)}, // as last written
				Total:  eur(15000),
			},
		},
		{
			name: "the name is the one written last, whatever order they were entered in",
			setup: []given{
				bank,
				paid("2026-10-20", "NEW Spelling", "Expenses:Food", 1000), paid("2026-10-01", "new spelling", "Expenses:Food", 2000),
			},
			req: PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("NEW Spelling", 3000, 2)}, Total: eur(3000),
			},
		},
		{
			name: "a transaction with expenses in two currencies is one transaction",
			setup: []given{
				bank, setRate("2026-10-01", "USD", "EUR", "2"),
				stored(NewTransaction{Date: day("2026-10-10"), Payee: "Shop", Postings: []ledger.Posting{
					{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Fun", Amount: eur(1000)},
					{Account: "Assets:Dollars", Amount: usd(-1000)}, {Account: "Expenses:Fun", Amount: usd(1000)}}}),
			},
			req: PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("Shop", 3000, 1)}, Total: eur(3000),
			},
		},
		{
			name:  "transactions with no payee are one line with no name",
			setup: []given{bank, paid("2026-10-01", "", "Expenses:Food", 500), paid("2026-10-02", "  ", "Expenses:Food", 700), paid("2026-10-03", "Cinema", "Expenses:Fun", 100)},
			req:   PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("", 1200, 2), payee("Cinema", 100, 1)},
				Total:  eur(1300),
			},
		},
		{
			name:  "a refund counts against its payee, and a payee refunded more comes last",
			setup: []given{bank, paid("2026-10-01", "Shop", "Expenses:Fun", 5000), paid("2026-10-05", "Shop", "Expenses:Fun", -2000), paid("2026-10-06", "Zara", "Expenses:Fun", -300), paid("2026-10-07", "Aldi", "Expenses:Food", 100)},
			req:   PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("Shop", 3000, 2), payee("Aldi", 100, 1), payee("Zara", -300, 1)},
				Total:  eur(2800),
			},
		},
		{
			name: "income and transfers are not spending",
			setup: []given{
				bank,
				stored(NewTransaction{Date: day("2026-10-01"), Payee: "Employer", Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(100000)}, {Account: "Income:Salary", Amount: eur(-100000)}}}),
				stored(NewTransaction{Date: day("2026-10-03"), Payee: "To savings", Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-5000)}, {Account: ledger.TransferAccount, Amount: eur(5000)}}}),
				paid("2026-10-04", "Cinema", "Expenses:Fun", 100),
			},
			req: PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("Cinema", 100, 1)}, Total: eur(100),
			},
		},
		{
			name: "one transaction split over categories is one transaction, and the whole amount",
			setup: []given{
				bank,
				stored(NewTransaction{Date: day("2026-10-02"), Payee: "REWE", Postings: []ledger.Posting{
					{Account: "Assets:Bank", Amount: eur(-5000)}, {Account: "Expenses:Food", Amount: eur(3000)}, {Account: "Expenses:Fun", Amount: eur(2000)}}}),
			},
			req: PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("REWE", 5000, 1)}, Total: eur(5000),
			},
		},
		{
			name: "a transaction whose expenses cancel is not there",
			setup: []given{
				bank,
				stored(NewTransaction{Date: day("2026-10-02"), Payee: "REWE", Postings: []ledger.Posting{
					{Account: "Expenses:Food", Amount: eur(3000)}, {Account: "Expenses:Fun", Amount: eur(-3000)}}}),
			},
			req: PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10", Total: eur(0),
			},
		},
		{
			name:  "equal amounts go by name, without regard to case",
			setup: []given{bank, paid("2026-10-01", "zoo", "Expenses:Fun", 100), paid("2026-10-01", "Art", "Expenses:Fun", 100), paid("2026-10-01", "Bar", "Expenses:Fun", 100)},
			req:   PayeeRequest{From: "2026-10", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("Art", 100, 1), payee("Bar", 100, 1), payee("zoo", 100, 1)}, Total: eur(300),
			},
		},
		{
			name: "a limit lists that many and adds up the others",
			setup: []given{
				bank,
				paid("2026-10-01", "A", "Expenses:Fun", 5000), paid("2026-10-01", "B", "Expenses:Fun", 4000),
				paid("2026-10-01", "C", "Expenses:Fun", 300), paid("2026-10-02", "C", "Expenses:Fun", 200), paid("2026-10-01", "D", "Expenses:Fun", -100),
			},
			req: PayeeRequest{From: "2026-10", To: "2026-10", Limit: 2},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees:      []PayeeSpending{payee("A", 5000, 1), payee("B", 4000, 1)},
				Others:      PayeeSpending{Amount: eur(400), Transactions: 3},
				OtherPayees: 2,
				Total:       eur(9400),
			},
		},
		{
			name:  "a limit as big as the list leaves nothing in the others",
			setup: []given{bank, paid("2026-10-01", "A", "Expenses:Fun", 5000), paid("2026-10-01", "B", "Expenses:Fun", 4000)},
			req:   PayeeRequest{From: "2026-10", To: "2026-10", Limit: 2},
			want: PayeeReport{
				Base: "EUR", From: "2026-10", To: "2026-10",
				Payees: []PayeeSpending{payee("A", 5000, 1), payee("B", 4000, 1)}, Total: eur(9000),
			},
		},
		{
			name: "only the months asked for, the first and the last day included",
			setup: []given{
				bank,
				paid("2026-08-31", "A", "Expenses:Fun", 1), paid("2026-09-01", "A", "Expenses:Fun", 10), paid("2026-10-31", "A", "Expenses:Fun", 100), paid("2026-11-01", "A", "Expenses:Fun", 1000),
			},
			req: PayeeRequest{From: "2026-09", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-09", To: "2026-10",
				Payees: []PayeeSpending{payee("A", 110, 2)}, Total: eur(110),
			},
		},
		{
			name:  "no range: the 12 months up to this one",
			setup: []given{bank, today("2026-10-22"), paid("2025-10-31", "A", "Expenses:Fun", 1), paid("2025-11-01", "A", "Expenses:Fun", 10), paid("2026-10-22", "A", "Expenses:Fun", 100)},
			req:   PayeeRequest{},
			want: PayeeReport{
				Base: "EUR", From: "2025-11", To: "2026-10",
				Payees: []PayeeSpending{payee("A", 110, 2)}, Total: eur(110),
			},
		},
		{
			name: "dollars at the rate of their day, a currency without a rate left out whole",
			setup: []given{
				bank, accounts("Assets:Yen JPY"),
				setRate("2026-09-01", "USD", "EUR", "0.90"), setRate("2026-10-01", "USD", "EUR", "2"), today("2026-12-01"),
				stored(NewTransaction{Date: day("2026-09-10"), Payee: "Shop", Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(-1000)}, {Account: "Expenses:Fun", Amount: usd(1000)}}}),
				stored(NewTransaction{Date: day("2026-10-10"), Payee: "Shop", Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(-1000)}, {Account: "Expenses:Fun", Amount: usd(1000)}}}),
				stored(NewTransaction{Date: day("2026-10-12"), Payee: "Tokyo", Postings: []ledger.Posting{
					{Account: "Assets:Yen", Amount: ledger.Money{Minor: -500, Currency: "JPY"}}, {Account: "Expenses:Fun", Amount: ledger.Money{Minor: 500, Currency: "JPY"}}}}),
			},
			req: PayeeRequest{From: "2026-09", To: "2026-10"},
			want: PayeeReport{
				Base: "EUR", From: "2026-09", To: "2026-10",
				Payees: []PayeeSpending{payee("Shop", 2900, 2)}, Total: eur(2900), Missing: []ledger.Currency{"JPY"},
			},
		},
		{name: "the start is not a month", setup: []given{bank}, req: PayeeRequest{From: "soon", To: "2026-10"}, wantErr: `invalid month "soon": use YYYY-MM`},
		{name: "backwards", setup: []given{bank}, req: PayeeRequest{From: "2026-10", To: "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
		{name: "more than ten years", setup: []given{bank}, req: PayeeRequest{From: "2016-10", To: "2026-10"}, wantErr: "a report covers at most 120 months"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.PayeeReport(t.Context(), tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if tc.wantErr != "" {
				return
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("PayeeReport mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_PayeeReport_Share(t *testing.T) {
	tcs := []struct {
		name  string
		total int64
		payee int64
		want  SavingsRate
	}{
		{name: "a quarter", total: 400, payee: 100, want: SavingsRate{2500, true}},
		{name: "all", total: 400, payee: 400, want: SavingsRate{10000, true}},
		{name: "to the hundredth, a half up", total: 32, payee: 1, want: SavingsRate{313, true}},
		{name: "a refund is a negative share", total: 400, payee: -40, want: SavingsRate{-1000, true}},
		{name: "no total, no share", total: 0, payee: 100, want: SavingsRate{}},
		{name: "a refund overall, no share", total: -100, payee: -100, want: SavingsRate{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			r := PayeeReport{Total: eur(tc.total)}

			// when
			got := r.Share(PayeeSpending{Amount: eur(tc.payee)})

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Share mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
