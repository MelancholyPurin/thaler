package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// shop is 30.00 spent on the bank account on the 4th, between food and the household.
func shop(lines ...SplitLine) Split {
	return Split{Date: day("2026-10-04"), Payee: "REWE", Description: "Einkauf", Account: "Assets:Bank", Amount: eur(-3000), Lines: lines}
}

func line(category string, minor int64) SplitLine {
	if minor == 0 {
		return SplitLine{Category: category}
	}
	return SplitLine{Category: category, Amount: eur(minor)}
}

func Test_TxView_Split(t *testing.T) {
	tcs := []struct {
		name   string
		tx     TxView
		want   Split
		wantOK bool
	}{
		{
			name:   "a split of spending",
			tx:     tv(1, "2026-10-04", "REWE", "Einkauf", pe("Assets:Bank", -3000), pe("Expenses:Food", 2000), pe("Expenses:Household", 1000)),
			want:   shop(line("Expenses:Food", 2000), line("Expenses:Household", 1000)),
			wantOK: true,
		},
		{
			name: "a split of income, whatever the order of its postings",
			tx:   tv(1, "2026-10-04", "ACME", "", pe("Income:Salary", -200000), pe("Assets:Bank", 250000), pe("Income:Bonus", -50000)),
			want: Split{Date: day("2026-10-04"), Payee: "ACME", Account: "Assets:Bank", Amount: eur(250000),
				Lines: []SplitLine{line("Income:Salary", 200000), line("Income:Bonus", 50000)}},
			wantOK: true,
		},
		{
			name:   "an ordinary transaction is a split of one line",
			tx:     tv(1, "2026-10-04", "REWE", "Einkauf", pe("Assets:Bank", -3000), pe("Expenses:Food", 3000)),
			want:   shop(line("Expenses:Food", 3000)),
			wantOK: true,
		},
		{name: "a transfer is not", tx: tv(1, "2026-10-04", "x", "", pe("Assets:Bank", -3000), pe(ledger.TransferAccount, 3000))},
		{name: "an opening balance is not", tx: tv(1, "2026-10-04", "x", "", pe("Assets:Bank", 3000), pe("Equity:Opening", -3000))},
		{name: "two accounts of yours are not", tx: tv(1, "2026-10-04", "x", "", pe("Assets:Bank", -3000), pe("Assets:Savings", -1000), pe("Expenses:Food", 4000))},
		{name: "a line that gives something back is not", tx: tv(1, "2026-10-04", "x", "", pe("Assets:Bank", -3000), pe("Expenses:Food", 4000), pe("Expenses:Deposit", -1000))},
		{name: "a line of nothing is not", tx: tv(1, "2026-10-04", "x", "", pe("Assets:Bank", -3000), pe("Expenses:Food", 3000), pe("Expenses:Deposit", 0))},
		{name: "a line in another currency is not", tx: tv(1, "2026-10-04", "x", "", pe("Assets:Bank", -3000), pe("Expenses:Food", 2000), PostingView{"Expenses:Household", usd(1000)})},
		{name: "a line of nothing in income is not", tx: tv(1, "2026-10-04", "x", "", pe("Assets:Bank", 3000), pe("Income:Salary", -3000), pe("Income:Bonus", 0))},
		{name: "nothing moved is not", tx: tv(1, "2026-10-04", "x", "", pe("Assets:Bank", 0), pe("Expenses:Food", 0))},
		{name: "no account of yours is not", tx: tv(1, "2026-10-04", "x", "", pe("Expenses:Food", 3000), pe("Income:Gifts", -3000))},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.tx

			// when
			got, ok := tc.tx.Split()

			// then
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("split mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantOK, ok); diff != "" {
				t.Errorf("ok mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_AddSplit(t *testing.T) {
	setup := accounts("Assets:Bank EUR", "Assets:Dollars USD", "Expenses:Food", "Expenses:Household", "Income:Salary")
	tcs := []struct {
		name    string
		split   Split
		want    AddResult
		wantTxs []TxView
		wantErr string
	}{
		{
			name:    "spending is booked to its categories as a plus",
			split:   shop(line("Expenses:Food", 2000), line("Expenses:Household", 1000)),
			want:    AddResult{ID: 1},
			wantTxs: []TxView{tv(1, "2026-10-04", "REWE", "Einkauf", pe("Assets:Bank", -3000), pe("Expenses:Food", 2000), pe("Expenses:Household", 1000))},
		},
		{
			name:    "income as a minus",
			split:   Split{Date: day("2026-10-04"), Payee: "ACME", Account: "Assets:Bank", Amount: eur(250000), Lines: []SplitLine{line("Income:Salary", 200000), line("Income:Bonus", 50000)}},
			want:    AddResult{ID: 1, Created: []string{"Income:Bonus"}},
			wantTxs: []TxView{tv(1, "2026-10-04", "ACME", "", pe("Assets:Bank", 250000), pe("Income:Salary", -200000), pe("Income:Bonus", -50000))},
		},
		{
			name:    "the line without an amount takes the rest",
			split:   shop(line("Expenses:Food", 0), line("Expenses:Household", 1000)),
			want:    AddResult{ID: 1},
			wantTxs: []TxView{tv(1, "2026-10-04", "REWE", "Einkauf", pe("Assets:Bank", -3000), pe("Expenses:Food", 2000), pe("Expenses:Household", 1000))},
		},
		{
			name:    "one line is an ordinary transaction",
			split:   shop(line("Expenses:Food", 0)),
			want:    AddResult{ID: 1},
			wantTxs: []TxView{tv(1, "2026-10-04", "REWE", "Einkauf", pe("Assets:Bank", -3000), pe("Expenses:Food", 3000))},
		},
		{name: "lines short of the amount", split: shop(line("Expenses:Food", 2000), line("Expenses:Household", 999)), wantErr: "the lines are 0.01 EUR short of the amount"},
		{name: "lines over the amount", split: shop(line("Expenses:Food", 2000), line("Expenses:Household", 1500)), wantErr: "the lines are 5.00 EUR over the amount"},
		{name: "nothing left for the rest", split: shop(line("Expenses:Food", 3000), line("Expenses:Household", 0)), wantErr: "nothing is left for line 2: the other lines already add up to the amount or more"},
		{name: "two lines without an amount", split: shop(line("Expenses:Food", 0), line("Expenses:Household", 0)), wantErr: "only one line can be left without an amount"},
		{name: "a line with a sign", split: shop(line("Expenses:Food", 4000), line("Expenses:Household", -1000)), wantErr: "the amount of line 2 must be greater than zero"},
		{name: "a line with no category", split: shop(line("Expenses:Food", 2000), line(" ", 1000)), wantErr: "line 2 has no category"},
		{name: "a line on an account of yours", split: shop(line("Expenses:Food", 2000), line("Assets:Savings", 1000)), wantErr: "line 2: Assets:Savings is not an Income or Expenses category"},
		{name: "a line on Equity", split: shop(line("Expenses:Food", 2000), line(ledger.TransferAccount, 1000)), wantErr: "line 2: Equity:Transfers is not an Income or Expenses category"},
		{
			name: "a line in another currency", split: shop(line("Expenses:Food", 2000), SplitLine{Category: "Expenses:Household", Amount: usd(1000)}),
			wantErr: "line 2 is in USD, the split in EUR",
		},
		{name: "no lines", split: shop(), wantErr: "a split needs at least one category"},
		{name: "an amount of nothing", split: Split{Date: day("2026-10-04"), Account: "Assets:Bank", Amount: eur(0), Lines: []SplitLine{line("Expenses:Food", 0)}}, wantErr: "the amount of a split cannot be zero"},
		{
			name: "on a category", split: Split{Date: day("2026-10-04"), Account: "Expenses:Food", Amount: eur(-3000), Lines: []SplitLine{line("Expenses:Household", 0)}},
			wantErr: "Expenses:Food is not an account of yours: a split is booked on an Assets or Liabilities account",
		},
		{name: "without a day", split: Split{Account: "Assets:Bank", Amount: eur(-3000), Lines: []SplitLine{line("Expenses:Food", 0)}}, wantErr: "transaction has no date"},
		{
			name: "on an account in another currency", split: Split{Date: day("2026-10-04"), Account: "Assets:Dollars", Amount: eur(-3000), Lines: []SplitLine{line("Expenses:Food", 0)}},
			wantErr: "account Assets:Dollars holds USD, but the posting is in EUR",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, setup)

			// when
			got, err := st.AddSplit(t.Context(), tc.split)

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

func Test_ReplaceSplit(t *testing.T) {
	// #1 is a split of 30.00, #2 an ordinary transaction, #3 and #4 a transfer.
	world := []given{
		accounts("Assets:Bank EUR", "Assets:Savings EUR", "Expenses:Food", "Expenses:Household"),
		stored(NewTransaction{Date: day("2026-10-04"), Payee: "REWE", Description: "Einkauf", ImportHash: "h1", Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-3000)}, {Account: "Expenses:Food", Amount: eur(2000)}, {Account: "Expenses:Household", Amount: eur(1000)},
		}}),
		handEntered("Assets:Bank", "2026-10-05", "Kino", "Expenses:Fun", -1200),
		moved("Assets:Bank", "Assets:Savings", 50000, "2026-10-06"),
	}
	split := tv(1, "2026-10-04", "REWE", "Einkauf", pe("Assets:Bank", -3000), pe("Expenses:Food", 2000), pe("Expenses:Household", 1000))
	split.ImportHash = "h1"
	kino := tv(2, "2026-10-05", "Kino", "", pe("Assets:Bank", -1200), pe("Expenses:Fun", 1200))
	out := linked(tv(3, "2026-10-06", "Transfer to Savings", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)), 4)
	in := linked(tv(4, "2026-10-06", "Transfer from Bank", "", pe("Assets:Savings", 50000), pe(ledger.TransferAccount, -50000)), 3)
	with := func(tx TxView, postings ...PostingView) TxView {
		tx.Postings = postings
		return tx
	}
	tcs := []struct {
		name        string
		id          int64
		split       Split
		wantCreated []string
		wantTxs     []TxView
		wantErr     string
	}{
		{
			name: "the lines change and the transaction keeps its number and import hash", id: 1,
			split:       shop(line("Expenses:Food", 500), line("Expenses:Household", 0), line("Expenses:Gifts", 1500)),
			wantCreated: []string{"Expenses:Gifts"},
			wantTxs: []TxView{
				with(split, pe("Assets:Bank", -3000), pe("Expenses:Food", 500), pe("Expenses:Household", 1000), pe("Expenses:Gifts", 1500)),
				kino, out, in,
			},
		},
		{
			name: "the amount changes with them", id: 1,
			split:   Split{Date: day("2026-10-04"), Payee: "REWE", Description: "Einkauf", Account: "Assets:Bank", Amount: eur(-3500), Lines: []SplitLine{line("Expenses:Food", 0), line("Expenses:Household", 1000)}},
			wantTxs: []TxView{with(split, pe("Assets:Bank", -3500), pe("Expenses:Food", 2500), pe("Expenses:Household", 1000)), kino, out, in},
		},
		{
			name: "one line left makes it an ordinary transaction", id: 1,
			split:   shop(line("Expenses:Food", 0)),
			wantTxs: []TxView{with(split, pe("Assets:Bank", -3000), pe("Expenses:Food", 3000)), kino, out, in},
		},
		{
			name: "an ordinary transaction becomes a split", id: 2,
			split:   Split{Date: day("2026-10-05"), Payee: "Kino", Account: "Assets:Bank", Amount: eur(-1200), Lines: []SplitLine{line("Expenses:Fun", 800), line("Expenses:Food", 0)}},
			wantTxs: []TxView{split, with(kino, pe("Assets:Bank", -1200), pe("Expenses:Fun", 800), pe("Expenses:Food", 400)), out, in},
		},
		{
			name: "lines that do not add up change nothing", id: 1,
			split:   shop(line("Expenses:Food", 500), line("Expenses:Household", 1000)),
			wantTxs: []TxView{split, kino, out, in},
			wantErr: "the lines are 15.00 EUR short of the amount",
		},
		{
			name: "a half of a transfer is refused, and stays one", id: 3,
			split:   Split{Date: day("2026-10-06"), Account: "Assets:Bank", Amount: eur(-50000), Lines: []SplitLine{line("Expenses:Food", 0)}},
			wantTxs: []TxView{split, kino, out, in},
			wantErr: "transaction #3 is already one half of a transfer",
		},
		{
			name: "one that does not exist", id: 9,
			split:   shop(line("Expenses:Food", 0)),
			wantTxs: []TxView{split, kino, out, in},
			wantErr: "transaction #9 does not exist",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, world...)

			// when
			created, err := st.ReplaceSplit(t.Context(), tc.id, tc.split)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.wantCreated, created, equalEmpty); diff != "" {
				t.Errorf("created mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantTxs, stateOf(t, st).Transactions, equalEmpty, ignoreImportID); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

// A split on a closed account cannot be written over, as no transaction there can.
func Test_ReplaceSplit_closed(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		wantErr string
	}{
		{
			name: "the account of the split is closed",
			setup: []given{
				accounts("Assets:Bank EUR", "Assets:Old EUR"),
				handEntered("Assets:Old", "2026-10-04", "REWE", "Expenses:Food", -3000),
				handEntered("Assets:Old", "2026-10-05", "Back", "Income:Gifts", 3000),
				closing("Assets:Old"),
			},
			wantErr: "Assets:Old is closed: reopen it first",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)
			before := stateOf(t, st)

			// when
			_, err := st.ReplaceSplit(t.Context(), 1, Split{Date: day("2026-10-04"), Account: "Assets:Bank", Amount: eur(-3000), Lines: []SplitLine{line("Expenses:Food", 0)}})

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(before, stateOf(t, st), equalEmpty); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
