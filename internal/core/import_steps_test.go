package core

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_rowsIn(t *testing.T) {
	bank := Account{ID: 1, Name: "Assets:Bank", Currency: "EUR"}
	inEUR := row(1, "2026-10-04", "REWE", "", eur(-2347))
	inUSD := row(2, "2026-10-05", "Amazon", "", usd(-1999))
	tcs := []struct {
		name         string
		rows         []importer.Row
		wantRows     []importer.Row
		wantRejected []RejectedRow
	}{
		{name: "rows in the currency of the account are kept", rows: []importer.Row{inEUR}, wantRows: []importer.Row{inEUR}},
		{
			name: "the others are rejected by their line", rows: []importer.Row{inEUR, inUSD}, wantRows: []importer.Row{inEUR},
			wantRejected: []RejectedRow{{Line: 2, Err: coded.New("row_currency_mismatch", map[string]any{"Currency": ledger.Currency("USD"), "Account": "Assets:Bank", "Holds": ledger.Currency("EUR")})}},
		},
		{name: "no rows", rows: nil},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given bank and tc.rows

			// when
			rows, rejected := rowsIn(bank, tc.rows)

			// then
			if diff := cmp.Diff(tc.wantRows, rows, equalEmpty); diff != "" {
				t.Errorf("rows mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(errTexts(tc.wantRejected), errTexts(rejected), equalEmpty); diff != "" {
				t.Errorf("rejected mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// errTexts is each rejected row as its line and its message in English.
func errTexts(rows []RejectedRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%d: %s", r.Line, errText(r.Err)))
	}
	return out
}

func Test_checkChosenCategories(t *testing.T) {
	tcs := []struct {
		name    string
		chosen  map[int]string
		wantErr string
	}{
		{name: "none chosen", chosen: nil},
		{name: "categories of each type", chosen: map[int]string{1: "Expenses:Food", 2: "Income:Salary", 3: ledger.TransferAccount}},
		{name: "an account of yours", chosen: map[int]string{1: "Expenses:Food", 2: "Assets:Savings"}, wantErr: "Assets:Savings is not a category; use an Income, Expenses or Equity account"},
		{name: "a name of no type", chosen: map[int]string{1: "Food"}, wantErr: `account "Food" must start with one of Assets, Liabilities, Income, Expenses, Equity`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.chosen

			// when
			err := checkChosenCategories(tc.chosen)

			// then
			checkErr(t, err, tc.wantErr, nil)
		})
	}
}

func Test_importTransactions(t *testing.T) {
	bank := Account{ID: 7, Name: "Assets:Bank", Currency: "EUR"}
	rules, err := importer.NewRuleset([]importer.Rule{{Contains: "REWE", Category: "Expenses:Food"}})
	if err != nil {
		t.Fatal(err)
	}
	rewe := row(1, "2026-10-04", "REWE", "Einkauf", eur(-2347))
	salary := row(2, "2026-10-05", "ACME", "", eur(250000))
	salary.BookingText = "GUTSCHRIFT"
	kino := row(3, "2026-10-06", "Kino", "", eur(-1200))
	posted := func(r importer.Row, category, description string) NewTransaction {
		return NewTransaction{Date: r.Date, Payee: r.Payee, Description: description, Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: r.Amount}, {Account: category, Amount: r.Amount.Neg()},
		}}
	}
	tcs := []struct {
		name      string
		rows      []importer.Row
		chosen    map[int]string
		wantTxs   []NewTransaction
		wantRuled []bool
	}{
		{
			name: "a rule chooses, else Uncategorized of the direction; the booking text stands in for a purpose",
			rows: []importer.Row{rewe, salary, kino},
			wantTxs: []NewTransaction{
				posted(rewe, "Expenses:Food", "Einkauf"), posted(salary, ledger.UncategorizedIncome, "GUTSCHRIFT"), posted(kino, ledger.UncategorizedExpense, ""),
			},
			wantRuled: []bool{true, false, false},
		},
		{
			name: "a category chosen for a line wins over the rules", rows: []importer.Row{rewe, kino},
			chosen:    map[int]string{1: "Expenses:Household", 3: "Expenses:Fun"},
			wantTxs:   []NewTransaction{posted(rewe, "Expenses:Household", "Einkauf"), posted(kino, "Expenses:Fun", "")},
			wantRuled: []bool{true, true},
		},
		{
			name: "Uncategorized chosen counts as no choice", rows: []importer.Row{rewe},
			chosen:    map[int]string{1: ledger.UncategorizedExpense},
			wantTxs:   []NewTransaction{posted(rewe, ledger.UncategorizedExpense, "Einkauf")},
			wantRuled: []bool{false},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given the hashes the rows get, in file order
			dedupe := importer.NewDeduper("7")
			for i, r := range tc.rows {
				tc.wantTxs[i].ImportHash = dedupe.Key(r)
			}

			// when
			txs, ruled := importTransactions(bank, tc.rows, rules, tc.chosen)

			// then
			if diff := cmp.Diff(tc.wantTxs, txs, equalEmpty); diff != "" {
				t.Errorf("transactions mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantRuled, ruled); diff != "" {
				t.Errorf("ruled mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
