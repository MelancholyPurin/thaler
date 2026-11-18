package tui

import (
	"fmt"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// linkModel has, on transferAccounts: 500.00 out of the bank on the 4th (#1) and into
// savings on the 5th (#2), entered apart; 540.00 USD into the dollars (#3); and a
// transfer of 200.00 from the bank to savings on the 6th (#4 and #5). All months show.
func linkModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	m, st := transferAccounts(t)
	book(t, st, "2026-10-04", "Dauerauftrag", "Assets:Bank", ledger.UncategorizedExpense, -50000)
	book(t, st, "2026-10-05", "Sparen", "Assets:Savings", "Income:Interest", 50000)
	d, err := ledger.ParseDate("2026-10-04")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTransaction(t.Context(), core.NewTransaction{Date: d, Payee: "Dollars in", Postings: []ledger.Posting{
		{Account: "Assets:Dollars", Amount: ledger.Money{Minor: 54000, Currency: "USD"}},
		{Account: ledger.UncategorizedIncome, Amount: ledger.Money{Minor: -54000, Currency: "USD"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transfer(t.Context(), core.TransferRequest{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(20000), Date: d.AddDate(0, 0, 2)}); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.setPeriod("")
	m.applyFilter(false)
	return m, st
}

func Test_Update_link(t *testing.T) {
	type observed struct {
		Flow
		Pairs    []string // the transfers, "#out-#in"
		Marked   int
		Undoable int
	}
	modes := map[mode]string{modeList: "list", modeLink: "link"}
	const (
		out     = "2026-10-04 Dauerauftrag | Assets:Bank -500.00 | Expenses:Uncategorized 500.00"
		in      = "2026-10-05 Sparen | Assets:Savings 500.00 | Income:Interest -500.00"
		outXfer = "2026-10-04 Dauerauftrag | Assets:Bank -500.00 | Equity:Transfers 500.00"
		inXfer  = "2026-10-05 Sparen | Assets:Savings 500.00 | Equity:Transfers -500.00"
	)
	tcs := []struct {
		name   string
		on     string   // the payee of the row the script starts on
		marks  []string // the payees marked first
		setup  func(t *testing.T, st *core.Local)
		script string
		want   observed
		shows  []string
	}{
		{
			name: "l on one lists what could be its other half", on: "Dauerauftrag", script: "l",
			want:  observed{Flow: Flow{Mode: "link"}, Pairs: []string{"#4-#5"}},
			shows: []string{"Link #1 as a transfer", "Dauerauftrag", "with the other half, on another account:", "Sparen", "+500.00", "enter link"},
		},
		{
			name: "enter links them", on: "Sparen", script: "l enter",
			want: observed{
				Flow:     Flow{Mode: "list", Status: "Linked #1 and #2 as a transfer · z undoes", Added: []string{outXfer, inXfer}, Removed: []string{out, in}},
				Pairs:    []string{"#1-#2", "#4-#5"},
				Undoable: 1,
			},
		},
		{
			name: "z puts both back as they were", on: "Dauerauftrag", script: "l enter z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid linking #1 and #2"}, Pairs: []string{"#4-#5"}},
		},
		{
			name: "esc changes nothing", on: "Dauerauftrag", script: "l esc",
			want: observed{Flow: Flow{Mode: "list"}, Pairs: []string{"#4-#5"}},
		},
		{
			name: "nothing to offer says how to link anyway", on: "Dollars in", script: "l",
			want: observed{
				Flow:  Flow{Mode: "list", Status: "No other half found: mark two with space, then press l", StatusIsError: true},
				Pairs: []string{"#4-#5"},
			},
		},
		{
			name: "two marked are linked, across currencies too", on: "Dauerauftrag", marks: []string{"Dauerauftrag", "Dollars in"}, script: "l",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Linked #1 and #3 as a transfer · z undoes",
					Added: []string{
						"2026-10-04 Dauerauftrag | Assets:Bank -500.00 | Equity:Conversions 500.00",
						"2026-10-04 Dollars in | Assets:Dollars 540.00 | Equity:Conversions -540.00",
					},
					Removed: []string{out, "2026-10-04 Dollars in | Assets:Dollars 540.00 | Income:Uncategorized -540.00"},
				},
				Pairs: []string{"#1-#3", "#4-#5"}, Undoable: 1,
			},
		},
		{
			name: "one marked is the one", on: "Dollars in", marks: []string{"Dauerauftrag"}, script: "l",
			want:  observed{Flow: Flow{Mode: "link"}, Pairs: []string{"#4-#5"}, Marked: 1},
			shows: []string{"Link #1 as a transfer"},
		},
		{
			name: "three marked are refused", on: "Dauerauftrag", marks: []string{"Dauerauftrag", "Sparen", "Dollars in"}, script: "l",
			want: observed{Flow: Flow{Mode: "list", Status: "3 transactions are marked: mark two to link them", StatusIsError: true}, Pairs: []string{"#4-#5"}, Marked: 3},
		},
		{
			name: "two that do not fit are refused, and say why", on: "Dauerauftrag", marks: []string{"Sparen", "Dollars in"}, script: "l",
			want: observed{
				Flow:  Flow{Mode: "list", Status: "#2 and #3 move money the same way", StatusIsError: true},
				Pairs: []string{"#4-#5"}, Marked: 2,
			},
		},
		{
			name: "z after linking one that was in Equity:Transfers already puts it back there", on: "Dauerauftrag", script: "l enter z",
			setup: func(t *testing.T, st *core.Local) {
				if _, err := st.SetCategories(t.Context(), []core.CategoryChange{{TransactionID: 1, Category: ledger.TransferAccount}}); err != nil {
					t.Fatal(err)
				}
			},
			want: observed{Flow: Flow{Mode: "list", Status: "Undid linking #1 and #2"}, Pairs: []string{"#4-#5"}},
		},
		{
			name: "l on a half unlinks the two", on: "Transfer to Savings", script: "l",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Unlinked #4 and #5: both are uncategorized now · z undoes",
					Added: []string{
						"2026-10-06 Transfer to Savings | Assets:Bank -200.00 | Expenses:Uncategorized 200.00",
						"2026-10-06 Transfer from Bank | Assets:Savings 200.00 | Income:Uncategorized -200.00",
					},
					Removed: []string{
						"2026-10-06 Transfer to Savings | Assets:Bank -200.00 | Equity:Transfers 200.00",
						"2026-10-06 Transfer from Bank | Assets:Savings 200.00 | Equity:Transfers -200.00",
					},
				},
				Undoable: 1,
			},
		},
		{
			name: "z links them again", on: "Transfer from Bank", script: "l z",
			want: observed{Flow: Flow{Mode: "list", Status: "Linked #4 and #5 again"}, Pairs: []string{"#4-#5"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := linkModel(t)
			if tc.setup != nil {
				tc.setup(t, st)
				if err := m.reload(); err != nil {
					t.Fatal(err)
				}
				m.applyFilter(false)
			}
			for _, payee := range tc.marks {
				selectPayee(t, m, payee)
				m.toggleMark()
			}
			selectPayee(t, m, tc.on)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), Undoable: len(m.undo), Marked: len(m.marked)}
			got.Mode = modes[m.mode]
			txs, err := st.ListTransactions(t.Context(), core.TxFilter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, tx := range txs {
				if source, _, ok := tx.Legs(); ok && tx.TransferWith != 0 && source.Amount.Minor < 0 {
					got.Pairs = append(got.Pairs, fmt.Sprintf("#%d-#%d", tx.ID, tx.TransferWith))
				}
			}
			sort.Strings(got.Pairs)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
			checkConsistent(t, st)
		})
	}
}

// bookOpposite gives the model a second account in the first account's currency and
// books 5.00 out of the first and into it on today, entered apart, as #a and #a+1,
// the newest transactions; so l on the newest has something to offer.
func bookOpposite(t *testing.T, m *Model) {
	t.Helper()
	first := m.accounts[0].account
	if _, err := m.svc.AddAccount(t.Context(), "Assets:Spare", first.Currency); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		account, payee string
		minor          int64
	}{{first.Name, "Umbuchung", -500}, {"Assets:Spare", "Sparen", 500}} {
		other := ledger.UncategorizedExpense
		if p.minor > 0 {
			other = ledger.UncategorizedIncome
		}
		amount := ledger.Money{Minor: p.minor, Currency: first.Currency}
		if _, err := m.svc.AddTransaction(t.Context(), core.NewTransaction{Date: m.now(), Payee: p.payee, Postings: []ledger.Posting{
			{Account: p.account, Amount: amount}, {Account: other, Amount: amount.Neg()},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.applyFilter(false)
}
