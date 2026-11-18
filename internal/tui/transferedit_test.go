package tui

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// movedModel is transferAccounts with 500.00 moved from the bank to savings on
// 2026-10-04 (halves #1 and #2) and a purchase on the bank (#3), on the
// Transactions panel of all accounts. inDate, if given, moves the half that arrives
// to a day of its own, as the bank statements can have it.
func movedModel(t *testing.T, inDate string) (*Model, *core.Local) {
	t.Helper()
	m, st := transferAccounts(t)
	day := func(s string) time.Time {
		d, err := ledger.ParseDate(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if _, err := st.Transfer(t.Context(), core.TransferRequest{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(50000), Date: day("2026-10-04")}); err != nil {
		t.Fatal(err)
	}
	if inDate != "" {
		e := core.TransferEdit{From: "Assets:Bank", To: "Assets:Savings", Amount: eur(50000), Date: day("2026-10-04"), InDate: day(inDate)}
		if _, err := st.ReplaceTransfer(t.Context(), 1, e); err != nil {
			t.Fatal(err)
		}
	}
	book(t, st, "2026-10-05", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.setPeriod("")
	m.applyFilter(false)
	return m, st
}

func Test_Update_transferEdit(t *testing.T) {
	type observed struct {
		Flow
		Transfer string // the transfer #1 is one half of, if it still is one
		Undoable int
	}
	const (
		out = "2026-10-04 Transfer to Savings | Assets:Bank -500.00 | Equity:Transfers 500.00"
		in  = "2026-10-04 Transfer from Bank | Assets:Savings 500.00 | Equity:Transfers -500.00"
	)
	linked500 := "Assets:Bank -> Assets:Savings 500.00 EUR, 2026-10-04 / 2026-10-04"
	tcs := []struct {
		name   string
		inDate string
		on     string // the payee of the row the script starts on
		script string
		want   observed
		shows  []string
	}{
		{
			name: "e on the half that leaves opens the transfer filled in", on: "Transfer to Savings", script: "e",
			want:  observed{Flow: Flow{Mode: "transfer"}, Transfer: linked500},
			shows: []string{"Transfer #1 and #2", "500.00", "2026-10-04", "Assets:Bank · EUR", "Assets:Savings · EUR"},
		},
		{
			name: "and on the half that arrives, the same", on: "Transfer from Bank", script: "e",
			want:  observed{Flow: Flow{Mode: "transfer"}, Transfer: linked500},
			shows: []string{"Transfer #1 and #2", "500.00"},
		},
		{
			name: "a new amount changes both halves", on: "Transfer from Bank", script: "e ctrl+u text:600 ctrl+s",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Changed the transfer #1 and #2 · z undoes",
					Added: []string{
						"2026-10-04 Transfer to Savings | Assets:Bank -600.00 | Equity:Transfers 600.00",
						"2026-10-04 Transfer from Bank | Assets:Savings 600.00 | Equity:Transfers -600.00",
					},
					Removed: []string{out, in},
				},
				Transfer: "Assets:Bank -> Assets:Savings 600.00 EUR, 2026-10-04 / 2026-10-04", Undoable: 1,
			},
		},
		{
			name: "z changes it back", on: "Transfer to Savings", script: "e ctrl+u text:600 ctrl+s z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid the change to the transfer #1 and #2"}, Transfer: linked500},
		},
		{
			name: "another account, across currencies, with what arrived", on: "Transfer to Savings",
			script: "e tab tab tab up tab tab text:540 ctrl+s", // Amount, Date, From, To: up on To picks Dollars, Received appears after Amount
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Changed the transfer #1 and #2 · 540.00 USD arrived, 1 EUR = 1.0800 USD · z undoes",
					Added: []string{
						"2026-10-04 Transfer to Dollars | Assets:Bank -500.00 | Equity:Conversions 500.00",
						"2026-10-04 Transfer from Bank | Assets:Dollars 540.00 | Equity:Conversions -540.00",
					},
					Removed: []string{out, in},
				},
				Transfer: "Assets:Bank -> Assets:Dollars 500.00 EUR, 2026-10-04 / 2026-10-04", Undoable: 1,
			},
		},
		{
			name: "across currencies, what arrived is filled in too", on: "Transfer to Savings",
			script: "e tab tab tab up tab tab text:540 ctrl+s e ctrl+s",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Changed the transfer #1 and #2 · 540.00 USD arrived, 1 EUR = 1.0800 USD · z undoes",
					Added: []string{
						"2026-10-04 Transfer to Dollars | Assets:Bank -500.00 | Equity:Conversions 500.00",
						"2026-10-04 Transfer from Bank | Assets:Dollars 540.00 | Equity:Conversions -540.00",
					},
					Removed: []string{out, in},
				},
				Transfer: "Assets:Bank -> Assets:Dollars 500.00 EUR, 2026-10-04 / 2026-10-04", Undoable: 2,
			},
		},
		{
			name: "a refusal stays in the popup", on: "Transfer to Savings", script: "e ctrl+u text:0 ctrl+s",
			want: observed{Flow: Flow{Mode: "transfer", Err: "amount must be greater than zero"}, Transfer: linked500},
		},
		{
			name: "two halves on different days: the date field says so", inDate: "2026-10-06", on: "Transfer to Savings", script: "e tab",
			want:  observed{Flow: Flow{Mode: "transfer"}, Transfer: "Assets:Bank -> Assets:Savings 500.00 EUR, 2026-10-04 / 2026-10-06"},
			shows: []string{"The half that arrives is dated 2026-10-06"},
		},
		{
			name: "...and keeps its day when the date is left alone", inDate: "2026-10-06", on: "Transfer to Savings", script: "e ctrl+u text:600 ctrl+s",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Changed the transfer #1 and #2 · z undoes",
					Added: []string{
						"2026-10-04 Transfer to Savings | Assets:Bank -600.00 | Equity:Transfers 600.00",
						"2026-10-06 Transfer from Bank | Assets:Savings 600.00 | Equity:Transfers -600.00",
					},
					Removed: []string{out, "2026-10-06 Transfer from Bank | Assets:Savings 500.00 | Equity:Transfers -500.00"},
				},
				Transfer: "Assets:Bank -> Assets:Savings 600.00 EUR, 2026-10-04 / 2026-10-06", Undoable: 1,
			},
		},
		{
			name: "...and moves with it when it is changed", inDate: "2026-10-06", on: "Transfer to Savings", script: "e tab ctrl+u text:2026-10-09 ctrl+s",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Changed the transfer #1 and #2 · z undoes",
					Added: []string{
						"2026-10-09 Transfer to Savings | Assets:Bank -500.00 | Equity:Transfers 500.00",
						"2026-10-09 Transfer from Bank | Assets:Savings 500.00 | Equity:Transfers -500.00",
					},
					Removed: []string{out, "2026-10-06 Transfer from Bank | Assets:Savings 500.00 | Equity:Transfers -500.00"},
				},
				Transfer: "Assets:Bank -> Assets:Savings 500.00 EUR, 2026-10-09 / 2026-10-09", Undoable: 1,
			},
		},
		{
			name: "z puts both days back", inDate: "2026-10-06", on: "Transfer to Savings", script: "e tab ctrl+u text:2026-10-09 ctrl+s z",
			want: observed{
				Flow:     Flow{Mode: "list", Status: "Undid the change to the transfer #1 and #2"},
				Transfer: "Assets:Bank -> Assets:Savings 500.00 EUR, 2026-10-04 / 2026-10-06",
			},
		},
		{
			name: "d asks, showing both halves", on: "Transfer from Bank", script: "d",
			want:  observed{Flow: Flow{Mode: "confirm"}, Transfer: linked500},
			shows: []string{"Delete transfer #1 and #2", "#1  2026-10-04  Bank  -500.00", "#2  2026-10-04  Savings  +500.00", "Delete both halves?"},
		},
		{
			name: "n keeps it", on: "Transfer from Bank", script: "d n",
			want: observed{Flow: Flow{Mode: "list"}, Transfer: linked500},
		},
		{
			name: "y deletes both", on: "Transfer from Bank", script: "d y",
			want: observed{
				Flow:     Flow{Mode: "list", Status: "Deleted the transfer #1 and #2  (z to undo)", Removed: []string{out, in}},
				Transfer: "not a transfer", Undoable: 1,
			},
		},
		{
			name: "z brings both back, linked", on: "Transfer from Bank", script: "d y z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid deleting the transfer #1 and #2"}, Transfer: linked500},
		},
		{
			name: "one half marked takes the other with it", on: "REWE", script: "space space d y",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Deleted 3 transactions  (z to undo)",
					Removed: []string{out, in, "2026-10-05 REWE | Assets:Bank -10.00 | Expenses:Groceries 10.00"},
				},
				Transfer: "not a transfer", Undoable: 1,
			},
		},
		{
			name: "both halves marked are deleted once", on: "REWE", script: "down space space d y",
			want: observed{
				Flow:     Flow{Mode: "list", Status: "Deleted 2 transactions  (z to undo)", Removed: []string{out, in}},
				Transfer: "not a transfer", Undoable: 1,
			},
		},
		{
			name: "and z brings all three back, the transfer linked", on: "REWE", script: "space space d y z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid deleting 3 transactions"}, Transfer: linked500},
		},
	}
	modes := map[mode]string{modeList: "list", modeTransfer: "transfer", modeConfirm: "confirm"}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := movedModel(t, tc.inDate)
			selectPayee(t, m, tc.on)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), Undoable: len(m.undo), Transfer: "not a transfer"}
			got.Mode = modes[m.mode]
			if v, err := st.GetTransfer(t.Context(), 1); err == nil {
				got.Transfer = v.From + " -> " + v.To + " " + v.Amount.String() + ", " + v.Out.Date + " / " + v.In.Date
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
			checkConsistent(t, st)
		})
	}
}

// bookTransfer books 5.00 from the first account to the last on the model's today, the
// newest transaction, and shows it. A payee, if given, replaces the ones Thaler writes,
// on both halves; apart gives the half that arrives the next day.
func bookTransfer(t *testing.T, m *Model, payee string, apart bool) {
	t.Helper()
	from, to := m.accounts[0].account, m.accounts[len(m.accounts)-1].account
	req := core.TransferRequest{From: from.Name, To: to.Name, Amount: ledger.Money{Minor: 500, Currency: from.Currency}, Date: m.now()}
	if to.Currency != from.Currency {
		req.ToAmount = ledger.Money{Minor: 540, Currency: to.Currency}
	}
	res, err := m.svc.Transfer(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if payee != "" {
		for _, id := range []int64{res.ID, res.InID} {
			tx, err := m.svc.GetTransaction(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			nt, err := tx.NewTransaction()
			if err != nil {
				t.Fatal(err)
			}
			nt.Payee = payee
			if _, err := m.svc.ReplaceTransaction(t.Context(), id, nt); err != nil {
				t.Fatal(err)
			}
		}
	}
	if apart {
		v, err := m.svc.GetTransfer(t.Context(), res.ID)
		if err != nil {
			t.Fatal(err)
		}
		e := v.TransferEdit
		e.InDate = e.Date.AddDate(0, 0, 1)
		if _, err := m.svc.ReplaceTransfer(t.Context(), res.ID, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.applyFilter(false)
}
