package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_TransactionsCSV(t *testing.T) {
	jpy := func(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "JPY"} }
	tcs := []struct {
		name string
		in   []TxView
		want string
	}{
		{
			name: "a line for each posting, with what its transaction says",
			in: []TxView{{ID: 7, Date: "2026-10-03", Payee: "REWE", Description: "weekly shop", Postings: []PostingView{
				{Account: "Assets:Bank", Amount: eur(-2347)}, {Account: "Expenses:Groceries", Amount: eur(2347)}}}},
			want: "id,date,payee,description,account,amount,currency\n" +
				"7,2026-10-03,REWE,weekly shop,Assets:Bank,-23.47,EUR\n" +
				"7,2026-10-03,REWE,weekly shop,Expenses:Groceries,23.47,EUR\n",
		},
		{
			name: "a split is all there, and a transaction in two currencies",
			in: []TxView{{ID: 1, Date: "2026-10-03", Postings: []PostingView{
				{Account: "Assets:Bank", Amount: eur(-3000)}, {Account: "Expenses:Food", Amount: eur(2000)}, {Account: "Expenses:Fun", Amount: eur(1000)}}},
				{ID: 2, Date: "2026-10-04", Postings: []PostingView{
					{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: ledger.ConversionAccount, Amount: eur(1000)},
					{Account: ledger.ConversionAccount, Amount: usd(-1100)}, {Account: "Assets:Dollars", Amount: usd(1100)}}}},
			want: "id,date,payee,description,account,amount,currency\n" +
				"1,2026-10-03,,,Assets:Bank,-30.00,EUR\n1,2026-10-03,,,Expenses:Food,20.00,EUR\n1,2026-10-03,,,Expenses:Fun,10.00,EUR\n" +
				"2,2026-10-04,,,Assets:Bank,-10.00,EUR\n2,2026-10-04,,," + ledger.ConversionAccount + ",10.00,EUR\n" +
				"2,2026-10-04,,," + ledger.ConversionAccount + ",-11.00,USD\n2,2026-10-04,,,Assets:Dollars,11.00,USD\n",
		},
		{
			name: "a comma, a quote and a line break are quoted",
			in:   []TxView{{ID: 1, Date: "2026-10-03", Payee: `Meyer, "Bäcker"`, Description: "one\ntwo", Postings: []PostingView{{Account: "Assets:Bank", Amount: eur(-100)}}}},
			want: "id,date,payee,description,account,amount,currency\n" +
				`1,2026-10-03,"Meyer, ""Bäcker""","one` + "\n" + `two",Assets:Bank,-1.00,EUR` + "\n",
		},
		{
			name: "a name that is a formula is made text, in the payee, the description and the account",
			in:   []TxView{{ID: 1, Date: "2026-10-03", Payee: "=EVIL()", Description: "@home", Postings: []PostingView{{Account: "-Odd", Amount: eur(100)}}}},
			want: "id,date,payee,description,account,amount,currency\n" +
				"1,2026-10-03,'=EVIL(),'@home,'-Odd,1.00,EUR\n",
		},
		{
			name: "a currency with no decimals",
			in:   []TxView{{ID: 1, Date: "2026-10-03", Postings: []PostingView{{Account: "Assets:Yen", Amount: jpy(-1200)}}}},
			want: "id,date,payee,description,account,amount,currency\n1,2026-10-03,,,Assets:Yen,-1200,JPY\n",
		},
		{
			name: "none: the header alone",
			in:   nil,
			want: "id,date,payee,description,account,amount,currency\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got, err := TransactionsCSV(tc.in)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("TransactionsCSV mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
