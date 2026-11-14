package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_percentText(t *testing.T) {
	tcs := []struct {
		name string
		bp   int64
		want string
	}{
		{name: "a quarter", bp: 2500, want: "25.00"},
		{name: "all", bp: 10000, want: "100.00"},
		{name: "none", bp: 0, want: "0.00"},
		{name: "a hundredth", bp: 1, want: "0.01"},
		{name: "a tenth", bp: 10, want: "0.10"},
		{name: "to the hundredth", bp: 4167, want: "41.67"},
		{name: "more than all", bp: 15050, want: "150.50"},
		{name: "negative", bp: -4167, want: "-41.67"},
		{name: "negative, under one", bp: -5, want: "-0.05"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := percentText(tc.bp)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("percentText mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_csvText(t *testing.T) {
	tcs := []struct {
		name string
		text string
		want string
	}{
		{name: "a name", text: "REWE", want: "REWE"},
		{name: "empty", text: "", want: ""},
		{name: "a category", text: "Expenses:Groceries", want: "Expenses:Groceries"},
		{name: "a formula", text: "=SUM(A1:A9)", want: "'=SUM(A1:A9)"},
		{name: "a plus", text: "+49 30 1234", want: "'+49 30 1234"},
		{name: "a minus", text: "-Cafe", want: "'-Cafe"},
		{name: "an at", text: "@home", want: "'@home"},
		{name: "a tab", text: "\tx", want: "'\tx"},
		{name: "a carriage return", text: "\rx", want: "'\rx"},
		{name: "a sign inside is fine", text: "A=B", want: "A=B"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := csvText(tc.text)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("csvText mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_SpendingCSV(t *testing.T) {
	tcs := []struct {
		name string
		in   SpendingReport
		want string
	}{
		{
			name: "a line for each category, and the total",
			in: SpendingReport{
				Base: "EUR", Months: []string{"2026-09", "2026-10"},
				Categories: []CategorySpending{
					{Name: "Expenses:Groceries", Months: eurs(3000, 1000), Total: eur(4000)},
					{Name: "Expenses:Fun", Months: eurs(0, -250), Total: eur(-250)},
				},
				Totals: eurs(3000, 750), Total: eur(3750),
			},
			want: "category,currency,2026-09,2026-10,total\n" +
				"Expenses:Groceries,EUR,30.00,10.00,40.00\n" +
				"Expenses:Fun,EUR,0.00,-2.50,-2.50\n" +
				"Total,EUR,30.00,7.50,37.50\n",
		},
		{
			name: "no spending: the total alone",
			in:   SpendingReport{Base: "EUR", Months: []string{"2026-10"}, Totals: eurs(0), Total: eur(0)},
			want: "category,currency,2026-10,total\nTotal,EUR,0.00,0.00\n",
		},
		{
			name: "a currency with no decimals",
			in: SpendingReport{
				Base: "JPY", Months: []string{"2026-10"},
				Categories: []CategorySpending{{Name: "Expenses:Food", Months: []ledger.Money{{Minor: 1200, Currency: "JPY"}}, Total: ledger.Money{Minor: 1200, Currency: "JPY"}}},
				Totals:     []ledger.Money{{Minor: 1200, Currency: "JPY"}}, Total: ledger.Money{Minor: 1200, Currency: "JPY"},
			},
			want: "category,currency,2026-10,total\nExpenses:Food,JPY,1200,1200\nTotal,JPY,1200,1200\n",
		},
		{
			name: "a name with a comma and a quote is quoted, one that is a formula is made text",
			in: SpendingReport{
				Base: "EUR", Months: []string{"2026-10"},
				Categories: []CategorySpending{
					{Name: `Expenses:A, "B"`, Months: eurs(100), Total: eur(100)},
					{Name: "=1+1", Months: eurs(50), Total: eur(50)},
				},
				Totals: eurs(150), Total: eur(150),
			},
			want: "category,currency,2026-10,total\n" +
				`"Expenses:A, ""B""",EUR,1.00,1.00` + "\n" +
				"'=1+1,EUR,0.50,0.50\n" +
				"Total,EUR,1.50,1.50\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got, err := SpendingCSV(tc.in)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("SpendingCSV mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_CashFlowCSV(t *testing.T) {
	tcs := []struct {
		name string
		in   CashFlowReport
		want string
	}{
		{
			name: "a line for each month, the share only where there is income, and the total",
			in: CashFlowReport{
				Base: "EUR",
				Months: []CashFlowMonth{
					month("2026-09", 200000, 150000, 50000, 2500, true),
					month("2026-10", 0, 1000, -1000, 0, false),
				},
				Total: month("", 200000, 151000, 49000, 2450, true),
			},
			want: "month,currency,income,spending,net,saved_percent\n" +
				"2026-09,EUR,2000.00,1500.00,500.00,25.00\n" +
				"2026-10,EUR,0.00,10.00,-10.00,\n" +
				"Total,EUR,2000.00,1510.00,490.00,24.50\n",
		},
		{
			name: "more spent than came in is a negative share",
			in: CashFlowReport{
				Base:   "EUR",
				Months: []CashFlowMonth{month("2026-10", 1000, 1500, -500, -5000, true)},
				Total:  month("", 1000, 1500, -500, -5000, true),
			},
			want: "month,currency,income,spending,net,saved_percent\n" +
				"2026-10,EUR,10.00,15.00,-5.00,-50.00\n" +
				"Total,EUR,10.00,15.00,-5.00,-50.00\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got, err := CashFlowCSV(tc.in)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("CashFlowCSV mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_PayeesCSV(t *testing.T) {
	tcs := []struct {
		name string
		in   PayeeReport
		want string
	}{
		{
			name: "a line for each payee, with its share of the spending",
			in: PayeeReport{
				Base: "EUR",
				Payees: []PayeeSpending{
					{Name: "Edeka", Amount: eur(3000), Transactions: 1},
					{Name: "REWE", Amount: eur(3000), Transactions: 2},
					{Name: "Kino", Amount: eur(1200), Transactions: 1},
				},
				Total: eur(7200),
			},
			want: "payee,currency,amount,share_percent,transactions\n" +
				"Edeka,EUR,30.00,41.67,1\n" +
				"REWE,EUR,30.00,41.67,2\n" +
				"Kino,EUR,12.00,16.67,1\n",
		},
		{
			name: "no payee is an empty name, a refund a negative amount and share",
			in: PayeeReport{
				Base: "EUR",
				Payees: []PayeeSpending{
					{Name: "", Amount: eur(9000), Transactions: 1},
					{Name: "Zoo", Amount: eur(-500), Transactions: 1},
				},
				Total: eur(8500),
			},
			want: "payee,currency,amount,share_percent,transactions\n" +
				",EUR,90.00,105.88,1\n" +
				"Zoo,EUR,-5.00,-5.88,1\n",
		},
		{
			name: "nothing to take a share of: no share",
			in: PayeeReport{
				Base:   "EUR",
				Payees: []PayeeSpending{{Name: "Zoo", Amount: eur(-500), Transactions: 1}},
				Total:  eur(-500),
			},
			want: "payee,currency,amount,share_percent,transactions\nZoo,EUR,-5.00,,1\n",
		},
		{
			name: "no payees: the header",
			in:   PayeeReport{Base: "EUR", Total: eur(0)},
			want: "payee,currency,amount,share_percent,transactions\n",
		},
		{
			name: "a payee that is a formula is made text",
			in: PayeeReport{
				Base:   "EUR",
				Payees: []PayeeSpending{{Name: "=HYPERLINK(\"x\")", Amount: eur(100), Transactions: 1}},
				Total:  eur(100),
			},
			want: "payee,currency,amount,share_percent,transactions\n" +
				`"'=HYPERLINK(""x"")",EUR,1.00,100.00,1` + "\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got, err := PayeesCSV(tc.in)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("PayeesCSV mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_NetWorthCSV(t *testing.T) {
	tcs := []struct {
		name string
		in   NetWorthReport
		want string
	}{
		{
			name: "a line for each month, below zero too",
			in:   NetWorthReport{Base: "EUR", Months: []NetWorthMonth{worth("2026-09", 12345), worth("2026-10", -50), worth("2026-11", 0)}},
			want: "month,currency,net_worth\n2026-09,EUR,123.45\n2026-10,EUR,-0.50\n2026-11,EUR,0.00\n",
		},
		{
			name: "a currency with no decimals",
			in:   NetWorthReport{Base: "JPY", Months: []NetWorthMonth{{Month: "2026-10", Total: ledger.Money{Minor: 1200, Currency: "JPY"}}}},
			want: "month,currency,net_worth\n2026-10,JPY,1200\n",
		},
		{
			name: "no months: the header alone",
			in:   NetWorthReport{Base: "EUR"},
			want: "month,currency,net_worth\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got, err := NetWorthCSV(tc.in)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("NetWorthCSV mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ReportCSV(t *testing.T) {
	ledgerOf := []given{
		accounts("Assets:Bank EUR"),
		earning("2026-09-01", "Income:Salary", 100000),
		spending("2026-09-10", "Expenses:Rent", 30000), spending("2026-10-20", "Expenses:Food", 5000),
	}
	payee := func(date, name string, amount int64) given {
		return stored(NewTransaction{Date: day(date), Payee: name, Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-amount)}, {Account: "Expenses:Fun", Amount: eur(amount)}}})
	}
	dollars := []given{
		accounts("Assets:Dollars USD"), today("2026-12-01"), setRate("2026-10-01", "USD", "EUR", "2"),
		stored(NewTransaction{Date: day("2026-10-02"), Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(1000)}, {Account: "Income:Gift", Amount: usd(-1000)}}}),
	}
	// 9.00 USD spent and 10.00 USD earned on 2026-10-02, at 2 euros a dollar from 2026-10-01
	usdFlows := []given{
		accounts("Assets:Dollars USD"), today("2026-12-01"), setRate("2026-10-01", "USD", "EUR", "2"),
		stored(NewTransaction{Date: day("2026-10-02"), Payee: "Diner", Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(-900)}, {Account: "Expenses:Fun", Amount: usd(900)}}}),
		stored(NewTransaction{Date: day("2026-10-02"), Payee: "Aunt", Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(1000)}, {Account: "Income:Gift", Amount: usd(-1000)}}}),
	}
	usdMissing := []ledger.Currency{"USD"}
	tcs := []struct {
		name    string
		setup   []given
		req     ReportCSVRequest
		want    ReportCSV
		wantErr string
	}{
		{
			name:  "spending: converted at the rate of its day",
			setup: usdFlows,
			req:   ReportCSVRequest{Report: ReportSpending, From: "2026-10", To: "2026-10"},
			want:  ReportCSV{Data: []byte("category,currency,2026-10,total\nExpenses:Fun,EUR,18.00,18.00\nTotal,EUR,18.00,18.00\n")},
		},
		{
			name:  "spending: the day counted as today is passed on, and the currency is said",
			setup: usdFlows,
			req:   ReportCSVRequest{Report: ReportSpending, From: "2026-10", To: "2026-10", RateDate: "2026-09-30"},
			want:  ReportCSV{Data: []byte("category,currency,2026-10,total\nTotal,EUR,0.00,0.00\n"), Missing: usdMissing},
		},
		{
			name:  "income against spending: converted at the rate of its day",
			setup: usdFlows,
			req:   ReportCSVRequest{Report: ReportCashFlow, From: "2026-10", To: "2026-10"},
			want: ReportCSV{Data: []byte("month,currency,income,spending,net,saved_percent\n" +
				"2026-10,EUR,20.00,18.00,2.00,10.00\nTotal,EUR,20.00,18.00,2.00,10.00\n")},
		},
		{
			name:  "income against spending: the day is passed on, and the currency is said",
			setup: usdFlows,
			req:   ReportCSVRequest{Report: ReportCashFlow, From: "2026-10", To: "2026-10", RateDate: "2026-09-30"},
			want: ReportCSV{Data: []byte("month,currency,income,spending,net,saved_percent\n" +
				"2026-10,EUR,0.00,0.00,0.00,\nTotal,EUR,0.00,0.00,0.00,\n"), Missing: usdMissing},
		},
		{
			name:  "payees: converted at the rate of their day",
			setup: usdFlows,
			req:   ReportCSVRequest{Report: ReportPayees, From: "2026-10", To: "2026-10"},
			want:  ReportCSV{Data: []byte("payee,currency,amount,share_percent,transactions\nDiner,EUR,18.00,100.00,1\n")},
		},
		{
			name:  "payees: the day is passed on, and the currency is said",
			setup: usdFlows,
			req:   ReportCSVRequest{Report: ReportPayees, From: "2026-10", To: "2026-10", RateDate: "2026-09-30"},
			want:  ReportCSV{Data: []byte("payee,currency,amount,share_percent,transactions\n"), Missing: usdMissing},
		},
		{
			name:  "spending by category",
			setup: ledgerOf,
			req:   ReportCSVRequest{Report: ReportSpending, From: "2026-09", To: "2026-10"},
			want: ReportCSV{Data: []byte("category,currency,2026-09,2026-10,total\n" +
				"Expenses:Rent,EUR,300.00,0.00,300.00\nExpenses:Food,EUR,0.00,50.00,50.00\nTotal,EUR,300.00,50.00,350.00\n")},
		},
		{
			name:  "income against spending",
			setup: ledgerOf,
			req:   ReportCSVRequest{Report: ReportCashFlow, From: "2026-09", To: "2026-10"},
			want: ReportCSV{Data: []byte("month,currency,income,spending,net,saved_percent\n" +
				"2026-09,EUR,1000.00,300.00,700.00,70.00\n2026-10,EUR,0.00,50.00,-50.00,\nTotal,EUR,1000.00,350.00,650.00,65.00\n")},
		},
		{
			name:  "the payees, every one of them",
			setup: append(append([]given{}, ledgerOf...), payee("2026-10-01", "Kino", 1200), payee("2026-10-02", "Zoo", 800), payee("2026-10-03", "Cafe", 400)),
			req:   ReportCSVRequest{Report: ReportPayees, From: "2026-10", To: "2026-10"},
			want: ReportCSV{Data: []byte("payee,currency,amount,share_percent,transactions\n" +
				"shop,EUR,50.00,67.57,1\nKino,EUR,12.00,16.22,1\nZoo,EUR,8.00,10.81,1\nCafe,EUR,4.00,5.41,1\n")},
		},
		{
			name:  "net worth at the end of each month",
			setup: ledgerOf,
			req:   ReportCSVRequest{Report: ReportNetWorth, From: "2026-09", To: "2026-10"},
			want:  ReportCSV{Data: []byte("month,currency,net_worth\n2026-09,EUR,700.00\n2026-10,EUR,650.00\n")},
		},
		{
			name:  "no range: the 12 months up to this one",
			setup: []given{accounts("Assets:Bank EUR"), today("2026-10-22")},
			req:   ReportCSVRequest{Report: ReportNetWorth},
			want: ReportCSV{Data: []byte("month,currency,net_worth\n2025-11,EUR,0.00\n2025-12,EUR,0.00\n2026-01,EUR,0.00\n2026-02,EUR,0.00\n2026-03,EUR,0.00\n2026-04,EUR,0.00\n" +
				"2026-05,EUR,0.00\n2026-06,EUR,0.00\n2026-07,EUR,0.00\n2026-08,EUR,0.00\n2026-09,EUR,0.00\n2026-10,EUR,0.00\n")},
		},
		{
			name:  "the day counted as today is passed on: a rate dated after it is not used, and the currency is said",
			setup: dollars,
			req:   ReportCSVRequest{Report: ReportNetWorth, From: "2026-10", To: "2026-10", RateDate: "2026-09-30"},
			want:  ReportCSV{Data: []byte("month,currency,net_worth\n2026-10,EUR,0.00\n"), Missing: []ledger.Currency{"USD"}},
		},
		{
			name:  "and with the rate in reach it is counted",
			setup: dollars,
			req:   ReportCSVRequest{Report: ReportNetWorth, From: "2026-10", To: "2026-10"},
			want:  ReportCSV{Data: []byte("month,currency,net_worth\n2026-10,EUR,20.00\n")},
		},
		{
			name:  "a currency with no rate in spending is said, and left out of the file",
			setup: []given{accounts("Assets:Yen JPY"), stored(NewTransaction{Date: day("2026-10-02"), Postings: []ledger.Posting{{Account: "Assets:Yen", Amount: ledger.Money{Minor: -500, Currency: "JPY"}}, {Account: "Expenses:Fun", Amount: ledger.Money{Minor: 500, Currency: "JPY"}}}})},
			req:   ReportCSVRequest{Report: ReportSpending, From: "2026-10", To: "2026-10"},
			want:  ReportCSV{Data: []byte("category,currency,2026-10,total\nTotal,EUR,0.00,0.00\n"), Missing: []ledger.Currency{"JPY"}},
		},
		{name: "a report that is not one", setup: ledgerOf, req: ReportCSVRequest{Report: "balance", From: "2026-09", To: "2026-10"}, wantErr: "unknown report balance: use spending, cashflow, payees or networth"},
		{name: "none named", setup: ledgerOf, req: ReportCSVRequest{From: "2026-09", To: "2026-10"}, wantErr: "unknown report : use spending, cashflow, payees or networth"},
		{name: "a range that is not one", setup: ledgerOf, req: ReportCSVRequest{Report: ReportCashFlow, From: "2026-10", To: "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.ReportCSV(t.Context(), tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if tc.wantErr != "" {
				return
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ReportCSV mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Each report of the list is one the service can make as CSV.
func Test_ReportKinds(t *testing.T) {
	tcs := []struct {
		name string
		kind ReportKind
	}{
		{name: "spending", kind: ReportKinds[0]},
		{name: "cashflow", kind: ReportKinds[1]},
		{name: "payees", kind: ReportKinds[2]},
		{name: "networth", kind: ReportKinds[3]},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"))

			// when
			_, err := st.ReportCSV(t.Context(), ReportCSVRequest{Report: tc.kind, From: "2026-10", To: "2026-10"})

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.name, string(tc.kind)); diff != "" {
				t.Errorf("kind mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
