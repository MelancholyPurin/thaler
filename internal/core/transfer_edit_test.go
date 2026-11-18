package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// transferWorld has a bank, a savings account, a cash account in euros and one in
// dollars, and 500.00 moved from the bank to savings on 2026-10-04, as halves #1 and #2.
func transferWorld() []given {
	return []given{
		accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Cash EUR", "Assets:Dollars USD"),
		moved("Assets:Bank", "Assets:Savings", 50000, "2026-10-04"),
	}
}

// importedPair is the same transfer as the two bank statements have it: the bank's
// half on the 4th, the savings account's on the 6th, with the bank's own texts.
func importedPair() []given {
	return []given{
		accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Cash EUR"),
		importing("Assets:Bank", nil, row(2, "2026-10-04", "Umbuchung", "Sparen", eur(-50000))),
		importing("Assets:Savings", nil, row(2, "2026-10-06", "Max Muster", "Sparen Oktober", eur(50000))),
	}
}

func Test_GetTransfer(t *testing.T) {
	sameView := TransferView{
		Out: linked(transferOut(), 2), In: linked(transferIn(), 1),
		TransferEdit: TransferEdit{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(50000), Date: day("2026-10-04"), InDate: day("2026-10-04")},
	}
	tcs := []struct {
		name    string
		setup   []given
		id      int64
		want    TransferView
		wantErr string
	}{
		{name: "from the half that leaves", setup: transferWorld(), id: 1, want: sameView},
		{name: "from the half that arrives, the same", setup: transferWorld(), id: 2, want: sameView},
		{
			name: "across currencies", id: 2,
			setup: []given{accounts("Assets:Bank EUR", "Assets:Dollars USD"), func(t *testing.T, st *Local) {
				_, err := st.Transfer(t.Context(), TransferRequest{From: "Assets:Bank", To: "Assets:Dollars", Amount: eur(10000), ToAmount: usd(10850), Date: day("2026-10-05")})
				check(t, err)
			}},
			want: TransferView{
				Out: linked(tv(1, "2026-10-05", "Transfer to Dollars", "", pe("Assets:Bank", -10000), pe(ledger.ConversionAccount, 10000)), 2),
				In:  linked(tv(2, "2026-10-05", "Transfer from Bank", "", PostingView{"Assets:Dollars", usd(10850)}, PostingView{ledger.ConversionAccount, usd(-10850)}), 1),
				TransferEdit: TransferEdit{
					From: "Assets:Bank", To: "Assets:Dollars", Amount: eur(10000), ToAmount: usd(10850), Date: day("2026-10-05"), InDate: day("2026-10-05"),
				},
				CrossCurrency: true,
			},
		},
		{
			name: "two halves a few days apart keep their own days", setup: importedPair(), id: 1,
			want: TransferView{
				Out: linked(tv(1, "2026-10-04", "Umbuchung", "Sparen", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				In:  linked(tv(2, "2026-10-06", "Max Muster", "Sparen Oktober", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
				TransferEdit: TransferEdit{
					From: "Assets:Bank", To: "Assets:Savings", Amount: eur(50000), Date: day("2026-10-04"), InDate: day("2026-10-06"),
				},
			},
		},
		{
			name: "an ordinary transaction is not a transfer", id: 3,
			setup:   append(transferWorld(), handEntered("Assets:Bank", "2026-10-05", "REWE", "Expenses:Food", -1000)),
			wantErr: "transaction #3 is not one half of a transfer",
		},
		{name: "one that does not exist", setup: transferWorld(), id: 9, wantErr: "transaction #9 does not exist"},
		{
			name: "a link between two that are no transfer is not taken for one", id: 1,
			setup: []given{
				accounts("Assets:Bank EUR", "Assets:Savings EUR"),
				handEntered("Assets:Bank", "2026-10-05", "REWE", "Expenses:Food", -1000),
				handEntered("Assets:Savings", "2026-10-05", "Gift", "Income:Gifts", 1000),
				rawSQL(`UPDATE transactions SET transfer_with = CASE id WHEN 1 THEN 2 ELSE 1 END`),
			},
			wantErr: "transaction #1 is not one half of a transfer",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.GetTransfer(t.Context(), tc.id)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty, ignoreHashes); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ReplaceTransfer(t *testing.T) {
	edit := func(change func(*TransferEdit)) TransferEdit {
		e := TransferEdit{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(50000), Date: day("2026-10-04")}
		change(&e)
		return e
	}
	same := TransferResult{AddResult: AddResult{ID: 1}, InID: 2, In: eur(50000)}
	tcs := []struct {
		name    string
		setup   []given
		id      int64
		edit    TransferEdit
		want    TransferResult
		wantTxs []TxView
		wantErr string
	}{
		{
			name: "the amount changes on both halves, which keep their numbers and their link", setup: transferWorld(), id: 1,
			edit: edit(func(e *TransferEdit) { e.Amount = eur(60000) }),
			want: TransferResult{AddResult: AddResult{ID: 1}, InID: 2, In: eur(60000)},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -60000), pe(ledger.TransferAccount, 60000)), 2),
				linked(tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 60000), pe(ledger.TransferAccount, -60000)), 1),
			},
		},
		{
			name: "given the half that arrives, the same", setup: transferWorld(), id: 2,
			edit: edit(func(e *TransferEdit) { e.Amount = eur(60000) }),
			want: TransferResult{AddResult: AddResult{ID: 1}, InID: 2, In: eur(60000)},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -60000), pe(ledger.TransferAccount, 60000)), 2),
				linked(tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 60000), pe(ledger.TransferAccount, -60000)), 1),
			},
		},
		{
			name: "one date moves both", setup: transferWorld(), id: 1,
			edit: edit(func(e *TransferEdit) { e.Date = day("2026-10-09") }),
			want: same,
			wantTxs: []TxView{
				linked(tv(1, "2026-10-09", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-09", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "the half that arrives can have a day of its own", setup: transferWorld(), id: 1,
			edit: edit(func(e *TransferEdit) { e.InDate = day("2026-10-06") }),
			want: same,
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-06", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "another account: the payee Thaler wrote names it", setup: transferWorld(), id: 1,
			edit: edit(func(e *TransferEdit) { e.To = "Assets:Cash" }),
			want: same,
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Transfer to Cash", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Cash", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "the other way round", setup: transferWorld(), id: 1,
			edit: edit(func(e *TransferEdit) { e.From, e.To = "Assets:Savings", "Assets:Bank" }),
			want: same,
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Transfer to Bank", "", pe("Assets:Savings", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-04", "Transfer from Savings", "", pe("Assets:Bank", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "across currencies it goes through Equity:Conversions", setup: transferWorld(), id: 1,
			edit: edit(func(e *TransferEdit) { e.To, e.ToAmount = "Assets:Dollars", usd(54000) }),
			want: TransferResult{AddResult: AddResult{ID: 1, Created: []string{ledger.ConversionAccount}}, InID: 2, In: usd(54000), CrossCurrency: true},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Transfer to Dollars", "", pe("Assets:Bank", -50000), pe(ledger.ConversionAccount, 50000)), 2),
				linked(tv(2, "2026-10-04", "Transfer from Bank", "", PostingView{"Assets:Dollars", usd(54000)}, PostingView{ledger.ConversionAccount, usd(-54000)}), 1),
			},
		},
		{
			name: "a memo becomes the description of both", setup: transferWorld(), id: 1,
			edit: edit(func(e *TransferEdit) { e.Memo = "holiday" }),
			want: same,
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Transfer to Savings", "holiday", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-04", "Transfer from Bank", "holiday", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "imported halves keep the bank's payees and texts, and their days when given", setup: importedPair(), id: 2,
			edit: TransferEdit{From: "Assets:Bank", To: "Assets:Cash", Amount: eur(50000), Date: day("2026-10-04"), InDate: day("2026-10-06")},
			want: same,
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "Umbuchung", "Sparen", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 2),
				linked(tv(2, "2026-10-06", "Max Muster", "Sparen Oktober", pe("Assets:Cash", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
		{
			name: "to the same account is refused, and nothing changes", setup: transferWorld(), id: 1,
			edit:    edit(func(e *TransferEdit) { e.To = "Assets:Bank" }),
			wantTxs: []TxView{linked(transferOut(), 2), linked(transferIn(), 1)},
			wantErr: "source and destination are the same account",
		},
		{
			name: "to a closed account is refused, and nothing changes", setup: append(transferWorld(), closing("Assets:Cash")), id: 1,
			edit:    edit(func(e *TransferEdit) { e.To = "Assets:Cash" }),
			wantTxs: []TxView{linked(transferOut(), 2), linked(transferIn(), 1)},
			wantErr: "Assets:Cash is closed: reopen it first",
		},
		{
			name: "a half on a closed account is not changed, and neither is the other",
			setup: []given{accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Cash EUR"), moved("Assets:Bank", "Assets:Savings", 50000, "2026-10-04"),
				moved("Assets:Savings", "Assets:Cash", 50000, "2026-10-05"), closing("Assets:Savings")},
			id:   1,
			edit: edit(func(e *TransferEdit) { e.To = "Assets:Cash" }),
			wantTxs: []TxView{
				linked(transferOut(), 2), linked(transferIn(), 1),
				linked(tv(3, "2026-10-05", "Transfer to Cash", "", pe("Assets:Savings", -50000), pe(ledger.TransferAccount, 50000)), 4),
				linked(tv(4, "2026-10-05", "Transfer from Savings", "", pe("Assets:Cash", 50000), pe(ledger.TransferAccount, -50000)), 3),
			},
			wantErr: "Assets:Savings is closed: reopen it first",
		},
		{
			name: "an ordinary transaction is refused", id: 3,
			setup: append(transferWorld(), handEntered("Assets:Bank", "2026-10-05", "REWE", "Expenses:Food", -1000)),
			edit:  edit(func(*TransferEdit) {}),
			wantTxs: []TxView{
				linked(transferOut(), 2), linked(transferIn(), 1),
				tv(3, "2026-10-05", "REWE", "", pe("Assets:Bank", -1000), pe("Expenses:Food", 1000)),
			},
			wantErr: "transaction #3 is not one half of a transfer",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.ReplaceTransfer(t.Context(), tc.id, tc.edit)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantTxs, stateOf(t, st).Transactions, equalEmpty, ignoreHashes); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

// An imported half that was edited still stands for its bank row: importing the
// statement again adds nothing.
func Test_ReplaceTransfer_keepsImportHashes(t *testing.T) {
	tcs := []struct {
		name    string
		account string
		row     importer.Row
	}{
		{name: "the half that leaves", account: "Assets:Bank", row: row(2, "2026-10-04", "Umbuchung", "Sparen", eur(-50000))},
		{name: "the half that arrives", account: "Assets:Savings", row: row(2, "2026-10-06", "Max Muster", "Sparen Oktober", eur(50000))},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, importedPair()...)
			_, err := st.ReplaceTransfer(t.Context(), 1, TransferEdit{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(40000), Date: day("2026-10-05")})
			check(t, err)

			// when
			got, err := st.Import(t.Context(), ImportRequest{Account: tc.account, Rows: []importer.Row{tc.row}})

			// then
			check(t, err)
			if diff := cmp.Diff([2]int{0, 1}, [2]int{got.Inserted, got.Duplicates}); diff != "" {
				t.Errorf("inserted, duplicates (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_DeleteTransfer(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		id      int64
		wantIDs [2]int64 // of the halves deleted, out then in
		wantTxs []TxView
		wantErr string
	}{
		{name: "both halves go", setup: transferWorld(), id: 1, wantIDs: [2]int64{1, 2}},
		{name: "given the half that arrives, the same", setup: transferWorld(), id: 2, wantIDs: [2]int64{1, 2}},
		{
			name: "the rest stays", id: 2,
			setup:   append(transferWorld(), handEntered("Assets:Bank", "2026-10-05", "REWE", "Expenses:Food", -1000)),
			wantIDs: [2]int64{1, 2},
			wantTxs: []TxView{tv(3, "2026-10-05", "REWE", "", pe("Assets:Bank", -1000), pe("Expenses:Food", 1000))},
		},
		{
			name: "an ordinary transaction is refused", id: 3,
			setup: append(transferWorld(), handEntered("Assets:Bank", "2026-10-05", "REWE", "Expenses:Food", -1000)),
			wantTxs: []TxView{
				linked(transferOut(), 2), linked(transferIn(), 1),
				tv(3, "2026-10-05", "REWE", "", pe("Assets:Bank", -1000), pe("Expenses:Food", 1000)),
			},
			wantErr: "transaction #3 is not one half of a transfer",
		},
		{
			name: "a half on a closed account stops both",
			setup: []given{accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Cash EUR"), moved("Assets:Bank", "Assets:Savings", 50000, "2026-10-04"),
				moved("Assets:Savings", "Assets:Cash", 50000, "2026-10-05"), closing("Assets:Savings")},
			id: 1,
			wantTxs: []TxView{
				linked(transferOut(), 2), linked(transferIn(), 1),
				linked(tv(3, "2026-10-05", "Transfer to Cash", "", pe("Assets:Savings", -50000), pe(ledger.TransferAccount, 50000)), 4),
				linked(tv(4, "2026-10-05", "Transfer from Savings", "", pe("Assets:Cash", 50000), pe(ledger.TransferAccount, -50000)), 3),
			},
			wantErr: "Assets:Savings is closed: reopen it first",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.DeleteTransfer(t.Context(), tc.id)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.wantIDs, [2]int64{got.Out.ID, got.In.ID}); diff != "" {
				t.Errorf("halves deleted (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantTxs, stateOf(t, st).Transactions, equalEmpty); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

// Restoring the halves of a transfer links them again, so that deleting a transfer,
// or one of its halves, can be undone.
func Test_RestoreTransactions_transfers(t *testing.T) {
	restoreFrom := func(views ...TxView) func(t *testing.T) []RestoredTransaction {
		return func(t *testing.T) []RestoredTransaction {
			var out []RestoredTransaction
			for _, v := range views {
				nt, err := v.NewTransaction()
				check(t, err)
				out = append(out, RestoredTransaction{ID: v.ID, Transaction: nt})
			}
			return out
		}
	}
	both := []TxView{linked(transferOut(), 2), linked(transferIn(), 1)}
	tcs := []struct {
		name    string
		setup   []given
		restore func(t *testing.T) []RestoredTransaction
		want    []TxView
	}{
		{
			name:    "both halves, deleted together, come back linked",
			setup:   append(transferWorld(), func(t *testing.T, st *Local) { _, err := st.DeleteTransfer(t.Context(), 1); check(t, err) }),
			restore: restoreFrom(both...),
			want:    both,
		},
		{
			name:    "in the other order too",
			setup:   append(transferWorld(), func(t *testing.T, st *Local) { _, err := st.DeleteTransfer(t.Context(), 1); check(t, err) }),
			restore: restoreFrom(both[1], both[0]),
			want:    both,
		},
		{
			name:    "one half deleted alone comes back linked to the other",
			setup:   append(transferWorld(), deleting(2)),
			restore: restoreFrom(both[1]),
			want:    both,
		},
		{
			name:    "a half whose other is gone comes back on its own",
			setup:   append(transferWorld(), deleting(1, 2)),
			restore: restoreFrom(both[0]),
			want:    []TxView{transferOut()},
		},
		{
			name: "a half whose other was recategorized since is not linked to it",
			setup: append(transferWorld(), deleting(2), func(t *testing.T, st *Local) {
				_, err := st.SetCategories(t.Context(), []CategoryChange{{TransactionID: 1, Category: "Expenses:Gifts"}})
				check(t, err)
			}),
			restore: restoreFrom(both[1]),
			want: []TxView{
				tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe("Expenses:Gifts", 50000)),
				transferIn(),
			},
		},
		{
			name: "nor is one whose other was paired with another since",
			setup: append(transferWorld(), handEntered("Assets:Cash", "2026-10-01", "Kiosk", "Expenses:Food", -100), deleting(2), func(t *testing.T, st *Local) {
				_, err := st.Import(t.Context(), ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{row(2, "2026-10-04", "in", "", eur(50000))}})
				check(t, err)
			}),
			restore: restoreFrom(both[1]),
			want: []TxView{
				tv(3, "2026-10-01", "Kiosk", "", pe("Assets:Cash", -100), pe("Expenses:Food", 100)),
				linked(transferOut(), 4),
				transferIn(),
				linked(tv(4, "2026-10-04", "in", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 1),
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			err := st.RestoreTransactions(t.Context(), tc.restore(t))

			// then
			check(t, err)
			if diff := cmp.Diff(tc.want, stateOf(t, st).Transactions, equalEmpty, ignoreHashes); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
