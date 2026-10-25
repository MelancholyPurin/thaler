package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_Transfer(t *testing.T) {
	accts := accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Cash-USD USD", "Expenses:Food")
	allAccounts := []string{"Assets:Bank", "Assets:Cash-USD", "Assets:Savings", "Expenses:Food"}
	when := day("2026-10-05")
	tcs := []struct {
		name      string
		req       TransferRequest
		want      TransferResult
		wantTxs   []TxView
		wantAccts []string
		wantErr   string
	}{
		{
			name: "within one currency: two linked transactions on their own accounts",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(5000), Date: when, Memo: "monthly"},
			want: TransferResult{AddResult: AddResult{ID: 1, Created: []string{ledger.TransferAccount}}, InID: 2, In: eur(5000)},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-05", "Transfer to Savings", "monthly", pe("Assets:Bank", -5000), pe(ledger.TransferAccount, 5000)), 2),
				linked(tv(2, "2026-10-05", "Transfer from Bank", "monthly", pe("Assets:Savings", 5000), pe(ledger.TransferAccount, -5000)), 1),
			},
			wantAccts: append(append([]string{}, allAccounts...), ledger.TransferAccount),
		},
		{
			name: "across currencies the amount received fixes the rate, and the pair goes through Equity:Conversions",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Cash-USD", Amount: eur(10000), ToAmount: usd(10850), Date: when},
			want: TransferResult{AddResult: AddResult{ID: 1, Created: []string{ledger.ConversionAccount}}, InID: 2, In: usd(10850), CrossCurrency: true},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-05", "Transfer to Cash-USD", "", pe("Assets:Bank", -10000), pe(ledger.ConversionAccount, 10000)), 2),
				linked(tv(2, "2026-10-05", "Transfer from Bank", "", ledgerView("Assets:Cash-USD", usd(10850)), ledgerView(ledger.ConversionAccount, usd(-10850))), 1),
			},
			wantAccts: append(append([]string{}, allAccounts...), ledger.ConversionAccount),
		},
		{
			name: "from an account that does not exist",
			req:  TransferRequest{From: "Assets:Nope", To: "Assets:Savings", Amount: eur(1), Date: when}, wantAccts: allAccounts,
			wantErr: "account does not exist: Assets:Nope",
		},
		{
			name: "to a category",
			req:  TransferRequest{From: "Assets:Bank", To: "Expenses:Food", Amount: eur(1), Date: when}, wantAccts: allAccounts,
			wantErr: "transfers work between accounts with a fixed currency (Assets or Liabilities)",
		},
		{
			name: "to the same account",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Bank", Amount: eur(1), Date: when}, wantAccts: allAccounts,
			wantErr: "source and destination are the same account",
		},
		{
			name: "of nothing",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(0), Date: when}, wantAccts: allAccounts,
			wantErr: "amount must be greater than zero",
		},
		{
			name: "in the wrong currency",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Savings", Amount: usd(100), Date: when}, wantAccts: allAccounts,
			wantErr: "amount is in USD, but Assets:Bank holds EUR",
		},
		{
			name: "with an amount received where both accounts hold the same currency",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(100), ToAmount: eur(100), Date: when}, wantAccts: allAccounts,
			wantErr: "both accounts use EUR, so there is no separate amount received",
		},
		{
			name: "across currencies without saying what arrives",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Cash-USD", Amount: eur(100), Date: when}, wantAccts: allAccounts,
			wantErr: "EUR -> USD crosses currencies; say how much arrives in USD",
		},
		{
			name: "across currencies with what arrives in the wrong currency",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Cash-USD", Amount: eur(100), ToAmount: eur(100), Date: when}, wantAccts: allAccounts,
			wantErr: "amount received is in EUR, but Assets:Cash-USD holds USD",
		},
		{
			name: "across currencies with nothing arriving",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Cash-USD", Amount: eur(100), ToAmount: usd(0), Date: when}, wantAccts: allAccounts,
			wantErr: "amount received must be greater than zero",
		},
		{
			name: "without a date writes nothing, not even the clearing account",
			req:  TransferRequest{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(100)}, wantAccts: allAccounts,
			wantErr: "transaction has no date",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accts)

			// when
			got, err := st.Transfer(ctx, tc.req)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
			state := stateOf(t, st)
			if diff := cmp.Diff(tc.wantTxs, state.Transactions, equalEmpty); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			wantAccts := tc.wantAccts
			if diff := cmp.Diff(sorted(wantAccts), sorted(state.Accounts)); diff != "" {
				t.Errorf("accounts mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func sorted(in []string) []string {
	out := append([]string{}, in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func Test_Transfer_balances(t *testing.T) {
	tcs := []struct {
		name  string
		moves []TransferRequest
		want  []Balance
	}{
		{
			name: "a same-currency and a cross-currency transfer out of the same account",
			moves: []TransferRequest{
				{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(5000), Date: day("2026-10-05")},
				{From: "Assets:Bank", To: "Assets:Cash-USD", Amount: eur(10000), ToAmount: usd(10850), Date: day("2026-10-05")},
			},
			want: []Balance{
				{"Assets:Bank", ledger.Assets, eur(-15000)},
				{"Assets:Cash-USD", ledger.Assets, usd(10850)},
				{"Assets:Savings", ledger.Assets, eur(5000)},
			},
		},
		{
			name: "back and forth cancels",
			moves: []TransferRequest{
				{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(5000), Date: day("2026-10-05")},
				{From: "Assets:Savings", To: "Assets:Bank", Amount: eur(5000), Date: day("2026-10-06")},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Cash-USD USD"))

			// when
			for _, m := range tc.moves {
				if _, err := st.Transfer(ctx, m); err != nil {
					t.Fatal(err)
				}
			}

			// then
			report, err := st.BalanceReport(ctx, BalanceRequest{})
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, report.Lines, equalEmpty); diff != "" {
				t.Errorf("balances mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// openingBalance sets an opening balance for its side effects.
func openingBalance(account string, balance ledger.Money, asOf string) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.SetOpeningBalance(ctx, OpeningBalanceRequest{Account: account, Balance: balance, AsOf: day(asOf)}); err != nil {
			t.Fatal(err)
		}
	}
}

func Test_SetOpeningBalance(t *testing.T) {
	bank := accounts("Assets:Bank EUR")
	opening := func(id int64, date string, minor int64) TxView {
		return tv(id, date, "Opening balance", "", pe("Assets:Bank", minor), pe(ledger.OpeningBalanceAccount, -minor))
	}
	// January with an opening balance that was a guess, and then the statement.
	january := []given{
		bank, openingBalance("Assets:Bank", eur(100000), "2026-01-01"),
		spending("2026-01-10", "Expenses:Food", 20000),
		spending("2026-02-01", "Expenses:Food", 30000),
		spending("2026-03-01", "Expenses:Food", 5000), // after the day the bank statement is about
	}
	food := func(id int64, date string, minor int64) TxView {
		return tv(id, date, "shop", "", pe("Assets:Bank", -minor), pe("Expenses:Food", minor))
	}
	tcs := []struct {
		name    string
		setup   []given
		req     OpeningBalanceRequest
		want    OpeningBalanceResult
		wantTxs []TxView
		wantErr string
		wantIs  error
	}{
		{
			name: "a new account with nothing booked: on the day given", setup: []given{bank},
			req:     OpeningBalanceRequest{Account: "Assets:Bank", Balance: eur(100000), AsOf: day("2026-01-01")},
			want:    OpeningBalanceResult{ID: 1, Date: "2026-01-01", Amount: eur(100000), Created: []string{ledger.OpeningBalanceAccount}},
			wantTxs: []TxView{opening(1, "2026-01-01", 100000)},
		},
		{
			name: "once transactions are there: the day before the first, and what they do not explain", setup: january,
			req: OpeningBalanceRequest{Account: "Assets:Bank", Balance: eur(50000), AsOf: day("2026-02-01")},
			want: OpeningBalanceResult{
				ID: 1, Date: "2026-01-09", Amount: eur(100000),
				Before: &TxView{ID: 1, Date: "2026-01-01", Payee: "Opening balance", Postings: []PostingView{pe("Assets:Bank", 100000), pe(ledger.OpeningBalanceAccount, -100000)}},
			},
			wantTxs: []TxView{opening(1, "2026-01-09", 100000), food(2, "2026-01-10", 20000), food(3, "2026-02-01", 30000), food(4, "2026-03-01", 5000)},
		},
		{
			name: "a later reading corrects the same transaction", setup: january,
			req: OpeningBalanceRequest{Account: "Assets:Bank", Balance: eur(40000), AsOf: day("2026-03-01")},
			want: OpeningBalanceResult{
				ID: 1, Date: "2026-01-09", Amount: eur(95000),
				Before: &TxView{ID: 1, Date: "2026-01-01", Payee: "Opening balance", Postings: []PostingView{pe("Assets:Bank", 100000), pe(ledger.OpeningBalanceAccount, -100000)}},
			},
			wantTxs: []TxView{opening(1, "2026-01-09", 95000), food(2, "2026-01-10", 20000), food(3, "2026-02-01", 30000), food(4, "2026-03-01", 5000)},
		},
		{
			name: "a debt is a negative balance in the ledger", setup: []given{accounts("Liabilities:Card EUR")},
			req:     OpeningBalanceRequest{Account: "Liabilities:Card", Balance: eur(-50000), AsOf: day("2026-01-01")},
			want:    OpeningBalanceResult{ID: 1, Date: "2026-01-01", Amount: eur(-50000), Created: []string{ledger.OpeningBalanceAccount}},
			wantTxs: []TxView{tv(1, "2026-01-01", "Opening balance", "", pe("Liabilities:Card", -50000), pe(ledger.OpeningBalanceAccount, 50000))},
		},
		{
			name: "nothing to explain: no transaction", setup: []given{bank, spending("2026-01-10", "Expenses:Food", 1000)},
			req:     OpeningBalanceRequest{Account: "Assets:Bank", Balance: eur(-1000), AsOf: day("2026-01-31")},
			wantTxs: []TxView{food(1, "2026-01-10", 1000)},
		},
		{
			name: "an account that does not exist", setup: []given{bank},
			req:     OpeningBalanceRequest{Account: "Assets:Nope", Balance: eur(1), AsOf: day("2026-01-01")},
			wantErr: "account does not exist: Assets:Nope", wantIs: ErrNoAccount,
		},
		{
			name: "a category", setup: []given{bank, accounts("Income:Salary")},
			req:     OpeningBalanceRequest{Account: "Income:Salary", Balance: eur(1), AsOf: day("2026-01-01")},
			wantErr: "Income:Salary is a Income account; only asset and liability accounts have a balance to set",
		},
		{
			name: "in the wrong currency", setup: []given{bank},
			req:     OpeningBalanceRequest{Account: "Assets:Bank", Balance: usd(1), AsOf: day("2026-01-01")},
			wantErr: "Assets:Bank holds EUR, but the balance is in USD",
		},
		{
			name: "without a date", setup: []given{bank},
			req:     OpeningBalanceRequest{Account: "Assets:Bank", Balance: eur(1)},
			wantErr: "the balance has no date",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)
			if tc.wantTxs == nil { // a refused call or nothing to book leaves the ledger as it was
				tc.wantTxs = stateOf(t, st).Transactions
			}

			// when
			got, err := st.SetOpeningBalance(ctx, tc.req)

			// then
			checkErr(t, err, tc.wantErr, tc.wantIs)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantTxs, stateOf(t, st).Transactions, equalEmpty); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

// pairOfTransfers is 500.00 moved from the bank to savings, as halves #1 and #2.
func pairOfTransfers() []given {
	return []given{accounts("Assets:Bank EUR", "Assets:Savings EUR"), moved("Assets:Bank", "Assets:Savings", 50000, "2026-10-04")}
}

func transferOut() TxView {
	return tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000))
}

func transferIn() TxView {
	return tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000))
}

// A link means: both halves booked to the clearing account on different own
// accounts, with amounts that cancel. When an edit breaks that, the link goes
// and the half that was left alone returns to Uncategorized so it is noticed.
func Test_SetCategories_transferLinks(t *testing.T) {
	tcs := []struct {
		name    string
		changes []CategoryChange
		want    []TxView
	}{
		{
			name:    "one half recategorized: the pair is dissolved and the other half is uncategorized again",
			changes: []CategoryChange{{TransactionID: 1, Category: "Expenses:Gifts"}},
			want: []TxView{
				tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe("Expenses:Gifts", 50000)),
				tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe(ledger.UncategorizedIncome, -50000)),
			},
		},
		{
			name:    "the other half recategorized",
			changes: []CategoryChange{{TransactionID: 2, Category: "Income:Gift"}},
			want: []TxView{
				tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000)),
				tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe("Income:Gift", -50000)),
			},
		},
		{
			name:    "both halves recategorized in one call",
			changes: []CategoryChange{{TransactionID: 1, Category: "Expenses:Gifts"}, {TransactionID: 2, Category: "Income:Gift"}},
			want: []TxView{
				tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe("Expenses:Gifts", 50000)),
				tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe("Income:Gift", -50000)),
			},
		},
		{
			name:    "to the clearing account it is already in: nothing changes",
			changes: []CategoryChange{{TransactionID: 1, Category: ledger.TransferAccount}},
			want:    []TxView{linked(transferOut(), 2), linked(transferIn(), 1)},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, pairOfTransfers()...)

			// when
			_, err := st.SetCategories(ctx, tc.changes)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, stateOf(t, st).Transactions); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}

	t.Run("across currencies too", func(t *testing.T) {
		// given
		st := newLedger(t, accounts("Assets:Bank EUR", "Assets:Dollars USD"), func(t *testing.T, st *Local) {
			req := TransferRequest{From: "Assets:Bank", To: "Assets:Dollars", Amount: eur(10000), ToAmount: usd(10850), Date: day("2026-10-06")}
			if _, err := st.Transfer(ctx, req); err != nil {
				t.Fatal(err)
			}
		})

		// when
		_, err := st.SetCategories(ctx, []CategoryChange{{TransactionID: 1, Category: "Expenses:Fees"}})

		// then
		checkErr(t, err, "", nil)
		want := []TxView{
			tv(1, "2026-10-06", "Transfer to Dollars", "", pe("Assets:Bank", -10000), pe("Expenses:Fees", 10000)),
			tv(2, "2026-10-06", "Transfer from Bank", "", ledgerView("Assets:Dollars", usd(10850)), ledgerView(ledger.UncategorizedIncome, usd(-10850))),
		}
		if diff := cmp.Diff(want, stateOf(t, st).Transactions); diff != "" {
			t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
		}
	})
}

func Test_ReplaceTransaction_transferLinks(t *testing.T) {
	renamed := func(tx TxView, payee string) NewTransaction {
		nt, err := tx.NewTransaction()
		if err != nil {
			t.Fatal(err)
		}
		nt.Payee = payee
		return nt
	}
	smaller := NewTransaction{
		Date: day("2026-10-04"), Payee: "Transfer to Savings",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-49000)}, {Account: ledger.TransferAccount, Amount: eur(49000)}},
	}
	tcs := []struct {
		name string
		nt   NewTransaction
		want []TxView
	}{
		{
			name: "renaming a half keeps the pair",
			nt:   renamed(transferOut(), "Sparplan"),
			want: []TxView{linked(tv(1, "2026-10-04", "Sparplan", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2), linked(transferIn(), 1)},
		},
		{
			name: "changing the amount of a half dissolves the pair: the edited half keeps what was chosen, the other is noticed",
			nt:   smaller,
			want: []TxView{
				tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -49000), pe(ledger.TransferAccount, 49000)),
				tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe(ledger.UncategorizedIncome, -50000)),
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, pairOfTransfers()...)

			// when
			_, err := st.ReplaceTransaction(ctx, 1, tc.nt)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, stateOf(t, st).Transactions); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

// Deleting one half leaves the other unlinked, and still waiting in the
// clearing account.
func Test_DeleteTransaction_transferLink(t *testing.T) {
	tcs := []struct {
		name string
		id   int64
		want []TxView
	}{
		{name: "the outgoing half", id: 1, want: []TxView{transferIn()}},
		{name: "the incoming half", id: 2, want: []TxView{transferOut()}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, pairOfTransfers()...)

			// when
			err := st.DeleteTransaction(ctx, tc.id)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, stateOf(t, st).Transactions); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

// What the interface does to take back an import that paired a transfer: put
// the paired half back where it was, then delete what the import added. It must
// leave the ledger as it was before the import.
func Test_importUndo_afterPairing(t *testing.T) {
	out := row(2, "2026-10-04", "out", "", eur(-50000))
	in := row(2, "2026-10-04", "in", "", eur(50000))
	tcs := []struct {
		name  string
		setup []given
		rules []importer.Rule
		want  []TxView // the first account's row, as it was before the second import
	}{
		{
			name:  "the waiting half was uncategorized",
			setup: []given{accounts("Assets:Bank EUR", "Assets:Savings EUR"), importing("Assets:Bank", nil, out)},
			want:  []TxView{tv(1, "2026-10-04", "out", "", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000))},
		},
		{
			name: "the waiting half had been marked as a transfer by a rule",
			setup: []given{accounts("Assets:Bank EUR", "Assets:Savings EUR"),
				importing("Assets:Bank", []importer.Rule{{Contains: "out", Category: ledger.TransferAccount}}, out)},
			want: []TxView{tv(1, "2026-10-04", "out", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000))},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)
			res, err := st.Import(ctx, ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{in}})
			if err != nil || len(res.Linked) != 1 {
				t.Fatalf("setup: %+v, %v", res, err)
			}

			// when
			var back []CategoryChange
			for _, l := range res.Linked {
				back = append(back, CategoryChange{TransactionID: l.OtherID, Category: l.OtherWas})
			}
			_, err = st.SetCategories(ctx, back)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range res.IDs {
				if err := st.DeleteTransaction(ctx, id); err != nil {
					t.Fatal(err)
				}
			}

			// then
			if diff := cmp.Diff(tc.want, stateOf(t, st).Transactions, ignoreHashes); diff != "" {
				t.Errorf("the ledger should be as before the second import (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
			again, err := st.Import(ctx, ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{in}})
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(1, again.Transfers); diff != "" {
				t.Errorf("importing again after the undo should pair again (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_transferHalves(t *testing.T) {
	half := func(account, clearing string, amount ledger.Money) TxView {
		return TxView{Postings: []PostingView{{Account: account, Amount: amount}, {Account: clearing, Amount: amount.Neg()}}}
	}
	tcs := []struct {
		name string
		a, b TxView
		want bool
	}{
		{name: "the same amount the other way round", a: half("Assets:A", ledger.TransferAccount, eur(-500)), b: half("Assets:B", ledger.TransferAccount, eur(500)), want: true},
		{name: "on the same account", a: half("Assets:A", ledger.TransferAccount, eur(-500)), b: half("Assets:A", ledger.TransferAccount, eur(500))},
		{name: "another amount", a: half("Assets:A", ledger.TransferAccount, eur(-500)), b: half("Assets:B", ledger.TransferAccount, eur(499))},
		{name: "the same direction", a: half("Assets:A", ledger.TransferAccount, eur(-500)), b: half("Assets:B", ledger.TransferAccount, eur(-500))},
		{name: "another currency", a: half("Assets:A", ledger.TransferAccount, eur(-500)), b: half("Assets:B", ledger.TransferAccount, usd(500))},
		{name: "one half is not in the clearing account", a: half("Assets:A", ledger.TransferAccount, eur(-500)), b: half("Assets:B", "Income:Gift", eur(500))},
		{name: "a conversion: out in one currency, in in another", a: half("Assets:A", ledger.ConversionAccount, eur(-500)), b: half("Assets:B", ledger.ConversionAccount, usd(540)), want: true},
		{name: "a conversion within one currency", a: half("Assets:A", ledger.ConversionAccount, eur(-500)), b: half("Assets:B", ledger.ConversionAccount, eur(500))},
		{name: "a conversion with both halves going out", a: half("Assets:A", ledger.ConversionAccount, eur(-500)), b: half("Assets:B", ledger.ConversionAccount, usd(-540))},
		{name: "one half through each clearing account", a: half("Assets:A", ledger.TransferAccount, eur(-500)), b: half("Assets:B", ledger.ConversionAccount, usd(540))},
		{name: "a half that is not a plain transaction", a: TxView{Postings: []PostingView{pe("Assets:A", -500)}}, b: half("Assets:B", ledger.TransferAccount, eur(500))},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.a and tc.b

			// when
			got := transferHalves(tc.a, tc.b)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_uncategorizedFor(t *testing.T) {
	tcs := []struct {
		name string
		in   ledger.Money
		want string
	}{
		{name: "money out", in: eur(-1), want: ledger.UncategorizedExpense},
		{name: "money in", in: eur(1), want: ledger.UncategorizedIncome},
		{name: "nothing", in: eur(0), want: ledger.UncategorizedExpense},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			got := uncategorizedFor(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_shortAccountName(t *testing.T) {
	tcs := []struct{ in, want string }{
		{"Assets:Savings", "Savings"},
		{"Assets:Bank:Giro", "Bank:Giro"},
		{"Expenses", "Expenses"},
		{"", ""},
	}
	for _, tc := range tcs {
		t.Run(tc.in, func(t *testing.T) {
			// given tc.in

			// when
			got := shortAccountName(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
