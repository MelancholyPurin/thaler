package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// ignoreHashes leaves out import hashes, which are long digests; a re-import
// proves they are stored.
var ignoreHashes = cmpopts.IgnoreFields(TxView{}, "ImportHash")

// tv is a stored transaction as it is listed.
func tv(id int64, date, payee, description string, postings ...PostingView) TxView {
	return TxView{ID: id, Date: date, Payee: payee, Description: description, Postings: postings}
}

// linked says tx is one half of a transfer, with the other half being other.
func linked(tx TxView, other int64) TxView {
	tx.TransferWith = other
	return tx
}

// importCase is an import request, the ledger it runs against, and what it
// should report and leave behind.
type importCase struct {
	name    string
	setup   []given
	req     ImportRequest // Account defaults to Assets:Bank
	want    ImportResult
	wantTxs []TxView // what is stored afterwards, import hashes aside
	wantErr string
	wantIs  error
}

func (tc importCase) request() ImportRequest {
	req := tc.req
	if req.Account == "" {
		req.Account = "Assets:Bank"
	}
	return req
}

// verify is the "then": the result, the failure, what is stored, and, for a real
// import, that running it again adds nothing.
func (tc importCase) verify(t *testing.T, st *Local, got ImportResult, err error) {
	t.Helper()
	checkErr(t, err, tc.wantErr, tc.wantIs)
	if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
		t.Errorf("result mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(tc.wantTxs, stateOf(t, st).Transactions, equalEmpty, ignoreHashes); diff != "" {
		t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
	}
	checkConsistent(t, st)
	if err != nil || tc.req.DryRun {
		return
	}
	// Importing the same file again changes nothing.
	again, err := st.Import(ctx, tc.request())
	if err != nil {
		t.Fatalf("importing again: %v", err)
	}
	type counts struct{ Inserted, Adopted, Transfers, Duplicates int }
	wantAgain := counts{Duplicates: got.Inserted + got.Adopted + got.Duplicates}
	if diff := cmp.Diff(wantAgain, counts{again.Inserted, again.Adopted, again.Transfers, again.Duplicates}); diff != "" {
		t.Errorf("importing the same rows again should find them all stored (-want +got):\n%s", diff)
	}
}

func Test_Import(t *testing.T) {
	bank := accounts("Assets:Bank EUR", "Expenses:Food")
	rules := []importer.Rule{{Contains: "rewe", Category: "Expenses:Groceries"}, {Contains: "gehalt", Category: "Income:Salary"}}
	rows := []importer.Row{
		row(2, "2026-10-01", "Muster GmbH", "Gehalt Oktober", eur(245000)),
		row(3, "2026-10-02", "REWE Markt", "", eur(-2347)),
		row(4, "2026-10-02", "Bakery", "", eur(-320)),
		row(5, "2026-10-02", "Bakery", "", eur(-320)), // genuinely identical: must not collapse
		row(6, "2026-10-03", "Verification", "card check", eur(0)),
	}
	imported := []TxView{
		tv(1, "2026-10-01", "Muster GmbH", "Gehalt Oktober", pe("Assets:Bank", 245000), pe("Income:Salary", -245000)),
		tv(2, "2026-10-02", "REWE Markt", "", pe("Assets:Bank", -2347), pe("Expenses:Groceries", 2347)),
		tv(3, "2026-10-02", "Bakery", "", pe("Assets:Bank", -320), pe(ledger.UncategorizedExpense, 320)),
		tv(4, "2026-10-02", "Bakery", "", pe("Assets:Bank", -320), pe(ledger.UncategorizedExpense, 320)),
		tv(5, "2026-10-03", "Verification", "card check", pe("Assets:Bank", 0), pe(ledger.UncategorizedExpense, 0)),
	}
	tcs := []importCase{
		{
			name: "a dry run says what each row would become and writes nothing", setup: []given{bank},
			req: ImportRequest{Rows: rows, Rules: rules, DryRun: true},
			want: ImportResult{
				Inserted: 5, Uncategorized: 3,
				Rows: []ImportedRow{
					{Row: rows[0], Category: "Income:Salary"},
					{Row: rows[1], Category: "Expenses:Groceries"},
					{Row: rows[2], Category: ledger.UncategorizedExpense},
					{Row: rows[3], Category: ledger.UncategorizedExpense},
					{Row: rows[4], Category: ledger.UncategorizedExpense},
				},
			},
		},
		{
			name: "for real: rules choose categories, identical rows stay two, a zero amount is kept", setup: []given{bank},
			req:     ImportRequest{Rows: rows, Rules: rules},
			want:    ImportResult{Inserted: 5, IDs: []int64{1, 2, 3, 4, 5}, Uncategorized: 3, Created: []string{"Income:Salary", "Expenses:Groceries", ledger.UncategorizedExpense}},
			wantTxs: imported,
		},
		{
			name: "the same file again is all duplicates", setup: []given{bank, importing("Assets:Bank", rules, rows...)},
			req:     ImportRequest{Rows: rows, Rules: rules},
			want:    ImportResult{Duplicates: 5, Uncategorized: 3},
			wantTxs: imported,
		},
		{
			name: "a dry run after an import marks every row as stored", setup: []given{bank, importing("Assets:Bank", rules, rows...)},
			req: ImportRequest{Rows: rows, Rules: rules, DryRun: true},
			want: ImportResult{
				Duplicates: 5, Uncategorized: 3,
				Rows: []ImportedRow{
					{Row: rows[0], Category: "Income:Salary", Duplicate: true},
					{Row: rows[1], Category: "Expenses:Groceries", Duplicate: true},
					{Row: rows[2], Category: ledger.UncategorizedExpense, Duplicate: true},
					{Row: rows[3], Category: ledger.UncategorizedExpense, Duplicate: true},
					{Row: rows[4], Category: ledger.UncategorizedExpense, Duplicate: true},
				},
			},
			wantTxs: imported,
		},
		{
			name: "an overlapping export adds only the new row", setup: []given{bank, importing("Assets:Bank", rules, rows...)},
			req:  ImportRequest{Rows: append(append([]importer.Row{}, rows...), row(7, "2026-10-04", "Kiosk", "", eur(-150))), Rules: rules},
			want: ImportResult{Inserted: 1, IDs: []int64{6}, Duplicates: 5, Uncategorized: 4},
			wantTxs: append(append([]TxView{}, imported...),
				tv(6, "2026-10-04", "Kiosk", "", pe("Assets:Bank", -150), pe(ledger.UncategorizedExpense, 150))),
		},
		{
			name: "the booking text stands in for an empty purpose", setup: []given{bank},
			req:     ImportRequest{Rows: []importer.Row{{Line: 2, Date: day("2026-10-01"), Payee: "Shop", BookingText: "KARTENZAHLUNG", Amount: eur(-100)}}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{tv(1, "2026-10-01", "Shop", "KARTENZAHLUNG", pe("Assets:Bank", -100), pe(ledger.UncategorizedExpense, 100))},
		},
		{
			name: "income with no rule goes to the uncategorized income account", setup: []given{bank},
			req:     ImportRequest{Rows: []importer.Row{row(2, "2026-10-01", "Someone", "", eur(5000))}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedIncome}},
			wantTxs: []TxView{tv(1, "2026-10-01", "Someone", "", pe("Assets:Bank", 5000), pe(ledger.UncategorizedIncome, -5000))},
		},
		{
			name: "no rows at all", setup: []given{bank}, req: ImportRequest{},
		},
		{
			name: "a row in another currency is refused, and nothing at all is written", setup: []given{bank},
			req:     ImportRequest{Rows: []importer.Row{row(2, "2026-10-01", "Shop", "", eur(-100)), row(3, "2026-10-01", "US Shop", "", usd(-100))}},
			want:    ImportResult{Rejected: []RejectedRow{{Line: 3, Reason: "currency USD does not match Assets:Bank (EUR)"}}},
			wantErr: "some rows do not fit the account: 1 row(s)", wantIs: ErrRowsRejected,
		},
		{
			name: "with IgnoreRejected the rows that fit are imported", setup: []given{bank},
			req: ImportRequest{Rows: []importer.Row{row(2, "2026-10-01", "Shop", "", eur(-100)), row(3, "2026-10-01", "US Shop", "", usd(-100))}, IgnoreRejected: true},
			want: ImportResult{
				Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense},
				Rejected: []RejectedRow{{Line: 3, Reason: "currency USD does not match Assets:Bank (EUR)"}},
			},
			wantTxs: []TxView{tv(1, "2026-10-01", "Shop", "", pe("Assets:Bank", -100), pe(ledger.UncategorizedExpense, 100))},
		},
		{
			name: "a row that cannot be booked aborts the whole batch", setup: []given{bank},
			req: ImportRequest{
				Rows:  []importer.Row{row(2, "2026-10-02", "Fine", "", eur(-1)), row(3, "2026-10-02", "Ghost", "", eur(-2))},
				Rules: []importer.Rule{{Contains: "ghost", Category: "Assets:Nowhere"}},
			},
			wantErr: "transaction 2 (2026-10-02 Ghost): account Assets:Nowhere does not exist; create it first as a Assets account in EUR",
		},
		{
			name: "an account without a fixed currency", setup: []given{bank}, req: ImportRequest{Account: "Expenses:Food"},
			wantErr: "Expenses:Food has no fixed currency; import into an Assets or Liabilities account",
		},
		{
			name: "an account that does not exist", setup: []given{bank}, req: ImportRequest{Account: "Assets:Nope"},
			wantErr: "account does not exist: Assets:Nope", wantIs: ErrNoAccount,
		},
		{
			name: "a rule that is not valid", setup: []given{bank}, req: ImportRequest{Rules: []importer.Rule{{Category: "Expenses:X"}}},
			wantErr: "invalid rules: rule 1: set exactly one of contains and regex",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.Import(ctx, tc.request())

			// then
			tc.verify(t, st, got, err)
		})
	}
}

func Test_Import_adoptingHandEnteredTransactions(t *testing.T) {
	accts := accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Cash EUR")
	bankRow := row(2, "2026-10-05", "REWE Markt GmbH", "Einkauf", eur(-2347))
	mine := tv(1, "2026-10-03", "REWE", "", pe("Assets:Bank", -2347), pe("Expenses:Groceries", 2347))
	rewe := handEntered("Assets:Bank", "2026-10-03", "REWE", "Expenses:Groceries", -2347)

	// Things a bank row of -10.00 on the 10th must not take over.
	tenOnTheTenth := []importer.Row{row(2, "2026-10-10", "Bank booking", "", eur(-1000))}
	notAdopted := func(name string, extra given, existing ...TxView) importCase {
		txs := append(append([]TxView{}, existing...), tv(int64(len(existing)+1), "2026-10-10", "Bank booking", "", pe("Assets:Bank", -1000), pe(ledger.UncategorizedExpense, 1000)))
		created := []string{ledger.UncategorizedExpense} // unless an earlier row made it already
		for _, tx := range existing {
			for _, p := range tx.Postings {
				if p.Account == ledger.UncategorizedExpense {
					created = nil
				}
			}
		}
		return importCase{
			name: name, setup: []given{accts, extra}, req: ImportRequest{Rows: tenOnTheTenth},
			want:    ImportResult{Inserted: 1, IDs: []int64{int64(len(existing) + 1)}, Uncategorized: 1, Created: created},
			wantTxs: txs,
		}
	}
	entry := func(id int64, date, payee, category string, minor int64, account string) TxView {
		return tv(id, date, payee, "", pe(account, minor), pe(category, -minor))
	}
	tcs := []importCase{
		{
			name: "previewed: the row would take the entry over", setup: []given{accts, rewe},
			req: ImportRequest{Rows: []importer.Row{bankRow}, DryRun: true},
			want: ImportResult{
				Adopted: 1,
				Rows:    []ImportedRow{{Row: bankRow, Category: "Expenses:Groceries", Adopts: &AdoptedEntry{ID: 1, Date: "2026-10-03", Payee: "REWE", Category: "Expenses:Groceries"}}},
			},
			wantTxs: []TxView{mine},
		},
		{
			name: "the entry keeps its payee, date and category and remembers the bank row", setup: []given{accts, rewe},
			req:     ImportRequest{Rows: []importer.Row{bankRow}},
			want:    ImportResult{Adopted: 1, AdoptedIDs: []int64{1}},
			wantTxs: []TxView{mine},
		},
		{
			name: "whatever the rules would have said", setup: []given{accts, handEntered("Assets:Bank", "2026-10-03", "REWE", "Expenses:Dining", -2347)},
			req:     ImportRequest{Rows: []importer.Row{bankRow}, Rules: []importer.Rule{{Contains: "rewe", Category: "Expenses:Groceries"}}},
			want:    ImportResult{Adopted: 1, AdoptedIDs: []int64{1}},
			wantTxs: []TxView{entry(1, "2026-10-03", "REWE", "Expenses:Dining", -2347, "Assets:Bank")},
		},
		{
			name: "the closest entry first and each only once", setup: []given{
				accts,
				handEntered("Assets:Bank", "2026-10-01", "coffee", "Expenses:Food", -350),
				handEntered("Assets:Bank", "2026-10-03", "coffee", "Expenses:Food", -350),
			},
			req: ImportRequest{Rows: []importer.Row{
				row(2, "2026-10-03", "Bakery", "a", eur(-350)), // nearest the entry of the 3rd
				row(3, "2026-10-02", "Bakery", "b", eur(-350)), // the entry of the 1st is what is left
				row(4, "2026-10-02", "Bakery", "c", eur(-350)), // nothing left to take over
			}},
			want: ImportResult{Inserted: 1, IDs: []int64{3}, Adopted: 2, AdoptedIDs: []int64{2, 1}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{ // listed by date
				entry(1, "2026-10-01", "coffee", "Expenses:Food", -350, "Assets:Bank"),
				tv(3, "2026-10-02", "Bakery", "c", pe("Assets:Bank", -350), pe(ledger.UncategorizedExpense, 350)),
				entry(2, "2026-10-03", "coffee", "Expenses:Food", -350, "Assets:Bank"),
			},
		},
		{
			name: "three days away is still the same purchase", setup: []given{accts, rewe},
			req:     ImportRequest{Rows: []importer.Row{row(2, "2026-10-06", "REWE", "", eur(-2347))}},
			want:    ImportResult{Adopted: 1, AdoptedIDs: []int64{1}},
			wantTxs: []TxView{mine},
		},
		notAdopted("an entry four days earlier is another purchase", handEntered("Assets:Bank", "2026-10-06", "earlier", "Expenses:Food", -1000),
			entry(1, "2026-10-06", "earlier", "Expenses:Food", -1000, "Assets:Bank")),
		notAdopted("an entry of another amount", handEntered("Assets:Bank", "2026-10-10", "other amount", "Expenses:Food", -999),
			entry(1, "2026-10-10", "other amount", "Expenses:Food", -999, "Assets:Bank")),
		notAdopted("an entry for money coming in", handEntered("Assets:Bank", "2026-10-10", "money in", "Income:Gift", 1000),
			entry(1, "2026-10-10", "money in", "Income:Gift", 1000, "Assets:Bank")),
		notAdopted("an entry on another account", handEntered("Assets:Cash", "2026-10-10", "cash", "Expenses:Food", -1000),
			entry(1, "2026-10-10", "cash", "Expenses:Food", -1000, "Assets:Cash")),
		notAdopted("an opening balance", handEntered("Assets:Bank", "2026-10-10", "Opening balance", ledger.OpeningBalanceAccount, -1000),
			entry(1, "2026-10-10", "Opening balance", ledger.OpeningBalanceAccount, -1000, "Assets:Bank")),
		notAdopted("an entry that was imported already", importing("Assets:Bank", nil, row(2, "2026-10-10", "earlier import", "", eur(-1000))),
			tv(1, "2026-10-10", "earlier import", "", pe("Assets:Bank", -1000), pe(ledger.UncategorizedExpense, 1000))),
		notAdopted("a split", stored(NewTransaction{Date: day("2026-10-10"), Payee: "split", Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Food", Amount: eur(600)}, {Account: "Expenses:Fun", Amount: eur(400)}}}),
			tv(1, "2026-10-10", "split", "", pe("Assets:Bank", -1000), pe("Expenses:Food", 600), pe("Expenses:Fun", 400))),
		notAdopted("a transaction that spans two of your accounts", stored(NewTransaction{Date: day("2026-10-10"), Payee: "old style", Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Assets:Cash", Amount: eur(1000)}}}),
			tv(1, "2026-10-10", "old style", "", pe("Assets:Bank", -1000), pe("Assets:Cash", 1000))),
		{
			name: "a zero amount never takes anything over", setup: []given{accts, handEntered("Assets:Bank", "2026-10-11", "free", "Expenses:Food", 0)},
			req:  ImportRequest{Rows: []importer.Row{row(2, "2026-10-11", "verification", "", eur(0))}},
			want: ImportResult{Inserted: 1, IDs: []int64{2}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{
				entry(1, "2026-10-11", "free", "Expenses:Food", 0, "Assets:Bank"),
				tv(2, "2026-10-11", "verification", "", pe("Assets:Bank", 0), pe(ledger.UncategorizedExpense, 0)),
			},
		},
		{
			name: "the second statement takes over the other half of a transfer entered by hand",
			setup: []given{accts, moved("Assets:Bank", "Assets:Savings", 50000, "2026-10-04"),
				importing("Assets:Bank", nil, row(2, "2026-10-05", "Umbuchung Sparen", "", eur(-50000)))},
			req:  ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{row(2, "2026-10-05", "Umbuchung", "", eur(50000))}},
			want: ImportResult{Adopted: 1, AdoptedIDs: []int64{2}},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "your own entry is taken over before the row is paired with another account's", setup: []given{
				accts,
				importing("Assets:Savings", nil, row(2, "2026-10-04", "Umbuchung", "", eur(50000))),
				handEntered("Assets:Bank", "2026-10-04", "to savings", "Expenses:Gifts", -50000),
			},
			req:  ImportRequest{Rows: []importer.Row{row(2, "2026-10-04", "Umbuchung Sparen", "", eur(-50000))}},
			want: ImportResult{Adopted: 1, AdoptedIDs: []int64{2}},
			wantTxs: []TxView{
				tv(1, "2026-10-04", "Umbuchung", "", pe("Assets:Savings", 50000), pe(ledger.UncategorizedIncome, -50000)),
				entry(2, "2026-10-04", "to savings", "Expenses:Gifts", -50000, "Assets:Bank"),
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.Import(ctx, tc.request())

			// then
			tc.verify(t, st, got, err)
		})
	}
}

func Test_Import_pairingTransfers(t *testing.T) {
	accts := accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Dollars USD")
	groceries := []importer.Rule{{Contains: "rewe", Category: "Expenses:Groceries"}}
	out := row(2, "2026-10-04", "Umbuchung Sparen", "", eur(-50000))
	in := row(2, "2026-10-05", "Umbuchung", "", eur(50000))
	bankFirst := []given{accts, importing("Assets:Bank", groceries, out, row(3, "2026-10-04", "REWE", "", eur(-2000)))}
	waitingOut := tv(1, "2026-10-04", "Umbuchung Sparen", "", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000))
	groceryRow := tv(2, "2026-10-04", "REWE", "", pe("Assets:Bank", -2000), pe("Expenses:Groceries", 2000))

	// Rows that look like the other half but are not.
	waitingOnly := []given{accts, importing("Assets:Bank", nil, row(2, "2026-10-10", "out", "", eur(-50000)))}
	waiting := tv(1, "2026-10-10", "out", "", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000))
	notPaired := func(name string, r importer.Row, account string, category string) importCase {
		amount := r.Amount
		var created []string
		if category != ledger.UncategorizedExpense { // the waiting row has made that one already
			created = []string{category}
		}
		return importCase{
			name: name, setup: waitingOnly, req: ImportRequest{Account: account, Rows: []importer.Row{r}},
			want:    ImportResult{Inserted: 1, IDs: []int64{2}, Uncategorized: 1, Created: created},
			wantTxs: []TxView{waiting, tv(2, r.Date.Format(ledger.DateLayout), r.Payee, "", pe(account, amount.Minor), pe(category, -amount.Minor))},
		}
	}
	tcs := []importCase{
		{
			name: "previewed: the row would complete the transfer", setup: bankFirst,
			req: ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{in}, DryRun: true},
			want: ImportResult{
				Inserted: 1, Transfers: 1,
				Rows: []ImportedRow{{Row: in, Category: ledger.TransferAccount, Transfer: &TransferMatch{ID: 1, Account: "Assets:Bank", Date: "2026-10-04", Was: ledger.UncategorizedExpense}}},
			},
			wantTxs: []TxView{waitingOut, groceryRow},
		},
		{
			name: "both halves go to Equity:Transfers and point at each other", setup: bankFirst,
			req: ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{in}},
			want: ImportResult{
				Inserted: 1, IDs: []int64{3}, Transfers: 1, Created: []string{ledger.TransferAccount},
				Linked: []TransferLink{{ID: 3, OtherID: 1, OtherWas: ledger.UncategorizedExpense}},
			},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Umbuchung Sparen", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 3),
				groceryRow,
				linked(tv(3, "2026-10-05", "Umbuchung", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "in the other order the waiting half was income", setup: []given{accts, importing("Assets:Savings", nil, in)},
			req: ImportRequest{Rows: []importer.Row{out}},
			want: ImportResult{
				Inserted: 1, IDs: []int64{2}, Transfers: 1, Created: []string{ledger.TransferAccount},
				Linked: []TransferLink{{ID: 2, OtherID: 1, OtherWas: ledger.UncategorizedIncome}},
			},
			wantTxs: []TxView{ // listed by date
				linked(tv(2, "2026-10-04", "Umbuchung Sparen", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 1),
				linked(tv(1, "2026-10-05", "Umbuchung", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 2),
			},
		},
		{
			name: "three days apart still pairs", setup: waitingOnly,
			req: ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{row(2, "2026-10-13", "in", "", eur(50000))}},
			want: ImportResult{
				Inserted: 1, IDs: []int64{2}, Transfers: 1, Created: []string{ledger.TransferAccount},
				Linked: []TransferLink{{ID: 2, OtherID: 1, OtherWas: ledger.UncategorizedExpense}},
			},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-10", "out", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-13", "in", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		notPaired("four days apart is not the same transfer", row(2, "2026-10-14", "in", "", eur(50000)), "Assets:Savings", ledger.UncategorizedIncome),
		notPaired("another amount", row(2, "2026-10-10", "in", "", eur(49999)), "Assets:Savings", ledger.UncategorizedIncome),
		notPaired("the same direction", row(2, "2026-10-10", "out too", "", eur(-50000)), "Assets:Savings", ledger.UncategorizedExpense),
		notPaired("the same account", row(2, "2026-10-10", "in", "", eur(50000)), "Assets:Bank", ledger.UncategorizedIncome),
		{
			name: "another currency never pairs, whatever the number", setup: waitingOnly,
			req:     ImportRequest{Account: "Assets:Dollars", Rows: []importer.Row{row(2, "2026-10-10", "in", "", usd(50000))}},
			want:    ImportResult{Inserted: 1, IDs: []int64{2}, Uncategorized: 1, Created: []string{ledger.UncategorizedIncome}},
			wantTxs: []TxView{waiting, tv(2, "2026-10-10", "in", "", ledgerView("Assets:Dollars", usd(50000)), ledgerView(ledger.UncategorizedIncome, usd(-50000)))},
		},
		{
			name: "each half is paired once", setup: []given{accts, importing("Assets:Savings", nil, row(2, "2026-10-04", "in", "", eur(50000)))},
			req: ImportRequest{Rows: []importer.Row{row(2, "2026-10-04", "out", "a", eur(-50000)), row(3, "2026-10-04", "out", "b", eur(-50000))}},
			want: ImportResult{
				Inserted: 2, IDs: []int64{2, 3}, Transfers: 1, Uncategorized: 1, Created: []string{ledger.TransferAccount, ledger.UncategorizedExpense},
				Linked: []TransferLink{{ID: 2, OtherID: 1, OtherWas: ledger.UncategorizedIncome}},
			},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "in", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 2),
				linked(tv(2, "2026-10-04", "out", "a", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 1),
				tv(3, "2026-10-04", "out", "b", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000)),
			},
		},
		{
			name: "the half that waited finds the late arrival", setup: []given{
				accts, importing("Assets:Savings", nil, row(2, "2026-10-04", "in", "", eur(50000))),
				importing("Assets:Bank", nil, row(2, "2026-10-04", "out", "a", eur(-50000)), row(3, "2026-10-04", "out", "b", eur(-50000))),
			},
			req: ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{row(3, "2026-10-04", "in", "b", eur(50000))}},
			want: ImportResult{
				Inserted: 1, IDs: []int64{4}, Transfers: 1,
				Linked: []TransferLink{{ID: 4, OtherID: 3, OtherWas: ledger.UncategorizedExpense}},
			},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "in", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 2),
				linked(tv(2, "2026-10-04", "out", "a", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 1),
				linked(tv(3, "2026-10-04", "out", "b", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 4),
				linked(tv(4, "2026-10-04", "in", "b", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 3),
			},
		},
		{
			name: "a rule that names a real category is the user's decision", setup: []given{accts, importing("Assets:Savings", nil, row(2, "2026-10-04", "x", "", eur(80000)))},
			req:  ImportRequest{Rows: []importer.Row{row(2, "2026-10-04", "Miete Oktober", "", eur(-80000))}, Rules: []importer.Rule{{Contains: "miete", Category: "Expenses:Rent"}}},
			want: ImportResult{Inserted: 1, IDs: []int64{2}, Created: []string{"Expenses:Rent"}},
			wantTxs: []TxView{
				tv(1, "2026-10-04", "x", "", pe("Assets:Savings", 80000), pe(ledger.UncategorizedIncome, -80000)),
				tv(2, "2026-10-04", "Miete Oktober", "", pe("Assets:Bank", -80000), pe("Expenses:Rent", 80000)),
			},
		},
		{
			name: "a rule that says transfer is paired like an uncategorized row", setup: []given{accts, importing("Assets:Savings", nil, row(2, "2026-10-06", "y", "", eur(50000)))},
			req: ImportRequest{Rows: []importer.Row{row(2, "2026-10-06", "Umbuchung", "", eur(-50000))}, Rules: []importer.Rule{{Contains: "umbuchung", Category: ledger.TransferAccount}}},
			want: ImportResult{
				Inserted: 1, IDs: []int64{2}, Transfers: 1, Created: []string{ledger.TransferAccount},
				Linked: []TransferLink{{ID: 2, OtherID: 1, OtherWas: ledger.UncategorizedIncome}},
			},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-06", "y", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 2),
				linked(tv(2, "2026-10-06", "Umbuchung", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 1),
			},
		},
		{
			name: "a transfer rule with no partner yet waits for it", setup: []given{accts},
			req:     ImportRequest{Rows: []importer.Row{out}, Rules: []importer.Rule{{Contains: "umbuchung", Category: ledger.TransferAccount}}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Created: []string{ledger.TransferAccount}},
			wantTxs: []TxView{tv(1, "2026-10-04", "Umbuchung Sparen", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000))},
		},
		{
			name: "the other half then pairs with the one a rule had marked", setup: []given{
				accts, importing("Assets:Bank", []importer.Rule{{Contains: "umbuchung", Category: ledger.TransferAccount}}, out)},
			req:  ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{in}},
			want: ImportResult{Inserted: 1, IDs: []int64{2}, Transfers: 1, Linked: []TransferLink{{ID: 2, OtherID: 1, OtherWas: ledger.TransferAccount}}},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Umbuchung Sparen", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-05", "Umbuchung", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "a zero amount is never paired", setup: []given{accts, importing("Assets:Bank", nil, row(2, "2026-10-10", "check", "", eur(0)))},
			req:  ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{row(2, "2026-10-10", "check", "", eur(0))}},
			want: ImportResult{Inserted: 1, IDs: []int64{2}, Uncategorized: 1},
			wantTxs: []TxView{
				tv(1, "2026-10-10", "check", "", pe("Assets:Bank", 0), pe(ledger.UncategorizedExpense, 0)),
				tv(2, "2026-10-10", "check", "", pe("Assets:Savings", 0), pe(ledger.UncategorizedExpense, 0)),
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.Import(ctx, tc.request())

			// then
			tc.verify(t, st, got, err)
		})
	}
}

// ledgerView is a posting in any currency.
func ledgerView(account string, amount ledger.Money) PostingView {
	return PostingView{Account: account, Amount: amount}
}

func Test_DetachImport(t *testing.T) {
	bankRow := func(date string, amount int64) importer.Row { return row(2, date, "REWE", "", eur(amount)) }
	setup := []given{
		accounts("Assets:Bank EUR"),
		handEntered("Assets:Bank", "2026-10-03", "REWE", "Expenses:Groceries", -2347),
		handEntered("Assets:Bank", "2026-10-04", "Edeka", "Expenses:Groceries", -1000),
		importing("Assets:Bank", nil, bankRow("2026-10-03", -2347), row(3, "2026-10-04", "Edeka", "", eur(-1000))),
	}
	tcs := []struct {
		name       string
		ids        []int64
		wantHashed []bool // for transactions 1 and 2: do they still stand for a bank row?
		wantErr    string
	}{
		{name: "none", ids: nil, wantHashed: []bool{true, true}},
		{name: "one of two", ids: []int64{1}, wantHashed: []bool{false, true}},
		{name: "both", ids: []int64{1, 2}, wantHashed: []bool{false, false}},
		{name: "one that does not exist changes nothing", ids: []int64{1, 9999}, wantHashed: []bool{true, true}, wantErr: "transaction #9999 does not exist"},
		{name: "one that was never imported is fine", ids: []int64{1, 1}, wantHashed: []bool{false, true}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, setup...)

			// when
			err := st.DetachImport(ctx, tc.ids)

			// then
			checkErr(t, err, tc.wantErr, nil)
			txs := stateOf(t, st).Transactions
			var hashed []bool
			for _, tx := range txs {
				hashed = append(hashed, tx.ImportHash != "")
			}
			if diff := cmp.Diff(tc.wantHashed, hashed); diff != "" {
				t.Errorf("which transactions stand for a bank row (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]int64{1, 2}, ids(txs)); diff != "" {
				t.Errorf("no transaction may disappear (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("a detached entry takes the same bank row over again", func(t *testing.T) {
		// given
		st := newLedger(t, setup...)
		if err := st.DetachImport(ctx, []int64{1}); err != nil {
			t.Fatal(err)
		}

		// when
		got, err := st.Import(ctx, ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{bankRow("2026-10-03", -2347)}})

		// then
		checkErr(t, err, "", nil)
		if diff := cmp.Diff(ImportResult{Adopted: 1, AdoptedIDs: []int64{1}}, got, equalEmpty); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
}

func Test_Recategorize(t *testing.T) {
	two := []given{
		accounts("Assets:Bank EUR"),
		stored(NewTransaction{Date: day("2026-10-01"), Payee: "REWE",
			Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-500)}, {Account: ledger.UncategorizedExpense, Amount: eur(500)}}}),
		spending("2026-10-02", ledger.UncategorizedExpense, 700),
		spending("2026-10-03", "Expenses:Fun", 900), // already categorized: never touched
	}
	rewe := []importer.Rule{{Contains: "rewe", Category: "Expenses:Groceries"}}
	uncategorized := []string{ledger.UncategorizedExpense, ledger.UncategorizedExpense, "Expenses:Fun"}
	tcs := []struct {
		name     string
		setup    []given
		req      RecategorizeRequest
		want     RecategorizeResult
		wantCats []string // the category of each transaction afterwards, by id
		wantErr  string
	}{
		{
			name: "previewed: one would move and nothing is written", setup: two, req: RecategorizeRequest{Rules: rewe, DryRun: true},
			want:     RecategorizeResult{Uncategorized: 2, Moves: []Move{{TransactionID: 1, Date: "2026-10-01", Payee: "REWE", Category: "Expenses:Groceries", Amount: eur(-500)}}},
			wantCats: uncategorized,
		},
		{
			name: "for real", setup: two, req: RecategorizeRequest{Rules: rewe},
			want:     RecategorizeResult{Uncategorized: 2, Created: []string{"Expenses:Groceries"}, Moves: []Move{{TransactionID: 1, Date: "2026-10-01", Payee: "REWE", Category: "Expenses:Groceries", Amount: eur(-500)}}},
			wantCats: []string{"Expenses:Groceries", ledger.UncategorizedExpense, "Expenses:Fun"},
		},
		{name: "without rules nothing moves", setup: two, req: RecategorizeRequest{}, want: RecategorizeResult{Uncategorized: 2}, wantCats: uncategorized},
		{
			name: "rules that match nothing", setup: two, req: RecategorizeRequest{Rules: []importer.Rule{{Contains: "zzz", Category: "Expenses:X"}}},
			want: RecategorizeResult{Uncategorized: 2}, wantCats: uncategorized,
		},
		{
			name: "a rule that sends it to where it already is does not count as a move", setup: two,
			req:  RecategorizeRequest{Rules: []importer.Rule{{Contains: "rewe", Category: ledger.UncategorizedExpense}}},
			want: RecategorizeResult{Uncategorized: 2}, wantCats: uncategorized,
		},
		{
			name: "income that is uncategorized moves too", setup: append(append([]given{}, two...), stored(NewTransaction{
				Date: day("2026-10-04"), Payee: "Gehalt Oktober",
				Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(5000)}, {Account: ledger.UncategorizedIncome, Amount: eur(-5000)}}})),
			req:      RecategorizeRequest{Rules: []importer.Rule{{Contains: "gehalt", Category: "Income:Salary"}}},
			want:     RecategorizeResult{Uncategorized: 3, Created: []string{"Income:Salary"}, Moves: []Move{{TransactionID: 4, Date: "2026-10-04", Payee: "Gehalt Oktober", Category: "Income:Salary", Amount: eur(5000)}}},
			wantCats: []string{ledger.UncategorizedExpense, ledger.UncategorizedExpense, "Expenses:Fun", "Income:Salary"},
		},
		{
			name: "an invalid rule", setup: two, req: RecategorizeRequest{Rules: []importer.Rule{{Category: "Expenses:X"}}},
			wantErr: "invalid rules: rule 1: set exactly one of contains and regex", wantCats: uncategorized,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.Recategorize(ctx, tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
			var cats []string
			for _, tx := range stateOf(t, st).Transactions {
				_, cat, _ := tx.Legs()
				cats = append(cats, cat.Account)
			}
			if diff := cmp.Diff(tc.wantCats, cats); diff != "" {
				t.Errorf("categories afterwards (-want +got):\n%s", diff)
			}
		})
	}
}
