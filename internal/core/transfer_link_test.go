package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// twoMoves is 500.00 that left the bank on the 4th (#1) and 500.00 that came into
// savings on the 5th (#2), entered as two ordinary transactions.
func twoMoves() []given {
	return []given{
		accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Dollars USD"),
		handEntered("Assets:Bank", "2026-10-04", "Dauerauftrag", ledger.UncategorizedExpense, -50000),
		handEntered("Assets:Savings", "2026-10-05", "Sparen", "Income:Interest", 50000),
	}
}

func Test_LinkTransfer(t *testing.T) {
	out := tv(1, "2026-10-04", "Dauerauftrag", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000))
	in := tv(2, "2026-10-05", "Sparen", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000))
	before := []TxView{
		tv(1, "2026-10-04", "Dauerauftrag", "", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000)),
		tv(2, "2026-10-05", "Sparen", "", pe("Assets:Savings", 50000), pe("Income:Interest", -50000)),
	}
	was := []CategoryChange{{TransactionID: 1, Category: ledger.UncategorizedExpense}, {TransactionID: 2, Category: "Income:Interest"}}
	tcs := []struct {
		name    string
		setup   []given
		a, b    int64
		want    LinkResult
		wantTxs []TxView
		wantErr string
	}{
		{
			name: "two that cancel become a transfer, whatever they were booked to", setup: twoMoves(), a: 1, b: 2,
			want:    LinkResult{OutID: 1, InID: 2, Was: was, Created: []string{ledger.TransferAccount}},
			wantTxs: []TxView{linked(out, 2), linked(in, 1)},
		},
		{
			name: "in the other order, the same", setup: twoMoves(), a: 2, b: 1,
			want:    LinkResult{OutID: 1, InID: 2, Was: []CategoryChange{was[1], was[0]}, Created: []string{ledger.TransferAccount}},
			wantTxs: []TxView{linked(out, 2), linked(in, 1)},
		},
		{
			name: "across currencies they go through Equity:Conversions", a: 1, b: 2,
			setup: []given{
				accounts("Assets:Bank EUR", "Assets:Dollars USD"),
				handEntered("Assets:Bank", "2026-10-04", "FX", ledger.UncategorizedExpense, -10000),
				stored(NewTransaction{Date: day("2026-10-04"), Payee: "FX", Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(10850)}, {Account: ledger.UncategorizedIncome, Amount: usd(-10850)}}}),
			},
			want: LinkResult{OutID: 1, InID: 2, Was: []CategoryChange{{TransactionID: 1, Category: ledger.UncategorizedExpense}, {TransactionID: 2, Category: ledger.UncategorizedIncome}}, Created: []string{ledger.ConversionAccount}},
			wantTxs: []TxView{
				linked(tv(1, "2026-10-04", "FX", "", pe("Assets:Bank", -10000), pe(ledger.ConversionAccount, 10000)), 2),
				linked(tv(2, "2026-10-04", "FX", "", PostingView{"Assets:Dollars", usd(10850)}, PostingView{ledger.ConversionAccount, usd(-10850)}), 1),
			},
		},
		{
			name: "one already booked to Equity:Transfers stays there", a: 1, b: 2,
			setup: []given{
				accounts("Assets:Bank EUR", "Assets:Savings EUR"),
				handEntered("Assets:Bank", "2026-10-04", "Dauerauftrag", ledger.TransferAccount, -50000),
				handEntered("Assets:Savings", "2026-10-05", "Sparen", "Income:Interest", 50000),
			},
			want:    LinkResult{OutID: 1, InID: 2, Was: []CategoryChange{{TransactionID: 1, Category: ledger.TransferAccount}, was[1]}},
			wantTxs: []TxView{linked(out, 2), linked(in, 1)},
		},
		{name: "not with itself", setup: twoMoves(), a: 1, b: 1, wantTxs: before, wantErr: "transaction #1 cannot be linked with itself"},
		{name: "not with one that does not exist", setup: twoMoves(), a: 1, b: 9, wantTxs: before, wantErr: "transaction #9 does not exist"},
		{
			name: "not on the same account", a: 1, b: 3, setup: append(twoMoves(), handEntered("Assets:Bank", "2026-10-06", "Back", "Income:Gifts", 50000)),
			wantTxs: append(before, tv(3, "2026-10-06", "Back", "", pe("Assets:Bank", 50000), pe("Income:Gifts", -50000))),
			wantErr: "source and destination are the same account",
		},
		{
			name: "not two that both take money out", a: 1, b: 3, setup: append(twoMoves(), handEntered("Assets:Savings", "2026-10-06", "Out", "Expenses:Food", -50000)),
			wantTxs: append(before, tv(3, "2026-10-06", "Out", "", pe("Assets:Savings", -50000), pe("Expenses:Food", 50000))),
			wantErr: "#1 and #3 move money the same way",
		},
		{
			name: "not two amounts that differ in one currency", a: 1, b: 3, setup: append(twoMoves(), handEntered("Assets:Savings", "2026-10-06", "In", "Income:Gifts", 49999)),
			wantTxs: append(before, tv(3, "2026-10-06", "In", "", pe("Assets:Savings", 49999), pe("Income:Gifts", -49999))),
			wantErr: "#1 and #3 do not move the same amount",
		},
		{
			name: "not a split", a: 1, b: 3,
			setup: append(twoMoves(), stored(NewTransaction{Date: day("2026-10-06"), Payee: "Split", Postings: []ledger.Posting{
				{Account: "Assets:Savings", Amount: eur(50000)}, {Account: "Income:Gifts", Amount: eur(-20000)}, {Account: "Income:Interest", Amount: eur(-30000)},
			}})),
			wantTxs: append(before, tv(3, "2026-10-06", "Split", "", pe("Assets:Savings", 50000), pe("Income:Gifts", -20000), pe("Income:Interest", -30000))),
			wantErr: "transaction #3 is not one account and one category",
		},
		{
			name: "not one that is a half already", a: 1, b: 4, setup: append(twoMoves(), moved("Assets:Savings", "Assets:Bank", 50000, "2026-10-06")),
			wantTxs: append(before,
				linked(tv(3, "2026-10-06", "Transfer to Bank", "", pe("Assets:Savings", -50000), pe(ledger.TransferAccount, 50000)), 4),
				linked(tv(4, "2026-10-06", "Transfer from Savings", "", pe("Assets:Bank", 50000), pe(ledger.TransferAccount, -50000)), 3)),
			wantErr: "transaction #4 is already one half of a transfer",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.LinkTransfer(t.Context(), tc.a, tc.b)

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

// UnlinkTransfer, then SetCategories with what LinkTransfer reported, puts the two
// back as they were: the undo of a link.
func Test_LinkTransfer_undone(t *testing.T) {
	ruled := []given{
		accounts("Assets:Bank EUR", "Assets:Savings EUR"),
		handEntered("Assets:Bank", "2026-10-04", "Dauerauftrag", ledger.TransferAccount, -50000),
		handEntered("Assets:Savings", "2026-10-05", "Sparen", "Income:Interest", 50000),
	}
	tcs := []struct {
		name  string
		setup []given
		a, b  int64
	}{
		{name: "linked one way", setup: twoMoves(), a: 1, b: 2},
		{name: "linked the other way", setup: twoMoves(), a: 2, b: 1},
		{name: "one was in Equity:Transfers already", setup: ruled, a: 1, b: 2},
		{name: "one was in Equity:Transfers already, linked the other way", setup: ruled, a: 2, b: 1},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)
			before := stateOf(t, st).Transactions
			res, err := st.LinkTransfer(t.Context(), tc.a, tc.b)
			check(t, err)

			// when
			_, err = st.UnlinkTransfer(t.Context(), res.OutID)
			check(t, err)
			_, err = st.SetCategories(t.Context(), res.Was)

			// then
			check(t, err)
			if diff := cmp.Diff(before, stateOf(t, st).Transactions, equalEmpty); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_UnlinkTransfer(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		id      int64
		wantIDs [2]int64
		wantTxs []TxView
		wantErr string
	}{
		{
			name: "both halves go to Uncategorized, unlinked", setup: transferWorld(), id: 2, wantIDs: [2]int64{1, 2},
			wantTxs: []TxView{
				tv(1, "2026-10-04", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000)),
				tv(2, "2026-10-04", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe(ledger.UncategorizedIncome, -50000)),
			},
		},
		{
			name: "across currencies too", id: 1, wantIDs: [2]int64{1, 2},
			setup: []given{accounts("Assets:Bank EUR", "Assets:Dollars USD"), func(t *testing.T, st *Local) {
				_, err := st.Transfer(t.Context(), TransferRequest{From: "Assets:Bank", To: "Assets:Dollars", Amount: eur(10000), ToAmount: usd(10850), Date: day("2026-10-05")})
				check(t, err)
			}},
			wantTxs: []TxView{
				tv(1, "2026-10-05", "Transfer to Dollars", "", pe("Assets:Bank", -10000), pe(ledger.UncategorizedExpense, 10000)),
				tv(2, "2026-10-05", "Transfer from Bank", "", PostingView{"Assets:Dollars", usd(10850)}, PostingView{ledger.UncategorizedIncome, usd(-10850)}),
			},
		},
		{
			name: "an ordinary transaction is refused", setup: twoMoves(), id: 1,
			wantTxs: []TxView{
				tv(1, "2026-10-04", "Dauerauftrag", "", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000)),
				tv(2, "2026-10-05", "Sparen", "", pe("Assets:Savings", 50000), pe("Income:Interest", -50000)),
			},
			wantErr: "transaction #1 is not one half of a transfer",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.UnlinkTransfer(t.Context(), tc.id)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.wantIDs, [2]int64{got.Out.ID, got.In.ID}); diff != "" {
				t.Errorf("halves (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantTxs, stateOf(t, st).Transactions, equalEmpty, ignoreHashes); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_TransferCandidates(t *testing.T) {
	world := append(twoMoves(),
		stored(NewTransaction{Date: day("2026-10-04"), Payee: "USD in", Postings: []ledger.Posting{ // #3: another currency
			{Account: "Assets:Dollars", Amount: usd(50000)}, {Account: ledger.UncategorizedIncome, Amount: usd(-50000)},
		}}),
		handEntered("Assets:Bank", "2026-10-04", "Same account", "Income:Gifts", 50000),    // #4: the same account
		handEntered("Assets:Savings", "2026-10-20", "Later", "Income:Gifts", 50000),        // #5: further away
		handEntered("Assets:Savings", "2026-10-04", "Other amount", "Income:Gifts", 40000), // #6
		handEntered("Assets:Savings", "2026-10-04", "Out too", "Expenses:Food", -50000),    // #7: the same direction
		moved("Assets:Bank", "Assets:Savings", 50000, "2026-10-04"),                        // #8 and #9: #9 comes into savings, but is a half already
		stored(NewTransaction{Date: day("2026-10-04"), Payee: "Split", Postings: []ledger.Posting{ // #10: a split
			{Account: "Assets:Savings", Amount: eur(50000)}, {Account: "Income:Gifts", Amount: eur(-20000)}, {Account: "Income:Interest", Amount: eur(-30000)},
		}}),

		handEntered("Assets:Savings", "2026-10-04", "Same day", "Income:Gifts", 50000), // #11: the nearest, though the newest
	)
	tcs := []struct {
		name    string
		setup   []given
		id      int64
		want    []int64
		wantErr string
	}{
		{name: "the opposite amount on another account, the nearest first", setup: world, id: 1, want: []int64{11, 2, 5}},
		{name: "from the other side", setup: world, id: 2, want: []int64{1}},
		{name: "none", setup: world, id: 6},
		{name: "a half already is refused", setup: world, id: 8, wantErr: "transaction #8 is already one half of a transfer"},
		{name: "one that does not exist", setup: world, id: 99, wantErr: "transaction #99 does not exist"},
		{
			name: "at most ten", id: 1,
			setup: append(twoMoves(), func(t *testing.T, st *Local) {
				for range 12 {
					handEntered("Assets:Savings", "2026-10-10", "In", "Income:Gifts", 50000)(t, st)
				}
			}),
			want: []int64{2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.TransferCandidates(t.Context(), tc.id)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, ids(got), equalEmpty); diff != "" {
				t.Errorf("candidates (-want +got):\n%s", diff)
			}
		})
	}
}
