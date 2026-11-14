package core

import "strconv"

// TransactionsCSV is stored transactions as CSV for a spreadsheet, in the form of the
// reports (see reportcsv.go): a line for each posting, so that a split or a transfer
// is all there and the lines of a transaction add up to nothing in each currency. The
// columns are the transaction's id, date (YYYY-MM-DD), payee and description, the
// account, and the amount as a plain decimal with its currency. Names a spreadsheet
// would read as a formula are written with a ' before them.
func TransactionsCSV(txs []TxView) ([]byte, error) {
	rows := [][]string{{"id", "date", "payee", "description", "account", "amount", "currency"}}
	for _, tx := range txs {
		for _, p := range tx.Postings {
			rows = append(rows, []string{strconv.FormatInt(tx.ID, 10), tx.Date, csvText(tx.Payee), csvText(tx.Description), csvText(p.Account), p.Amount.Decimal(), string(p.Amount.Currency)})
		}
	}
	return encodeCSV(rows)
}
