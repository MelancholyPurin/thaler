package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
)

// transferAccounts is an empty ledger with Assets:Bank, Assets:Dollars (USD) and
// Assets:Savings, in that order.
func transferAccounts(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	m, st := noAccountsModel(t)
	openAccounts(t, st, "Assets:Bank EUR", "Assets:Dollars USD", "Assets:Savings EUR")
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	return m, st
}

func Test_Update_transfer(t *testing.T) {
	type observed struct {
		Flow
		Balances []string
		Scope    string
		Undoable int
	}
	const (
		out    = "2026-10-22 Transfer to Savings | Assets:Bank -500.00 | Equity:Transfers 500.00"
		in     = "2026-10-22 Transfer from Bank | Assets:Savings 500.00 | Equity:Transfers -500.00"
		outUSD = "2026-10-22 Transfer to Dollars | Assets:Bank -100.00 | Equity:Conversions 100.00"
		inUSD  = "2026-10-22 Transfer from Bank | Assets:Dollars 108.50 | Equity:Conversions -108.50"
		// to Dollars: Amount, Date, From, To are the fields; up on To picks Dollars; tab wraps to Amount
		toDollars = "tab tab tab up tab"
	)
	tcs := []struct {
		name   string
		script string
		want   observed
		shows  []string
		hides  []string
	}{
		{
			name: "x opens the popup, with nothing to say about what arrives within a currency", script: "x",
			want:  observed{Flow: Flow{Mode: "transfer"}},
			shows: []string{"Transfer between your accounts", "Assets:Bank", "Assets:Savings"}, hides: []string{"Received"},
		},
		{
			name: "an amount and enter twice: two linked transactions", script: "x text:500 enter enter",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Transferred 500.00 EUR from Bank to Savings · z undoes",
					Added: []string{out, in},
				},
				Balances: []string{"Assets:Bank -500.00 EUR", "Assets:Savings 500.00 EUR"}, Scope: "Assets:Bank", Undoable: 1,
			},
		},
		{
			name: "z takes back both halves", script: "x text:500 enter enter z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid the transfer (#1 and #2)"}, Scope: "Assets:Bank"},
		},
		{
			name: "across currencies, Received appears and the rate is shown as it is typed", script: "x " + toDollars + " text:100 enter text:108.50",
			want:  observed{Flow: Flow{Mode: "transfer"}},
			shows: []string{"Received", "1 EUR = 1.0850 USD"},
		},
		{
			name: "...and saving books it through Equity:Conversions", script: "x " + toDollars + " text:100 enter text:108.50 enter enter",
			want: observed{
				Flow: Flow{
					Mode:   "list",
					Status: "Transferred 100.00 EUR from Bank to Dollars (108.50 USD arrived, 1 EUR = 1.0850 USD) · z undoes",
					Added:  []string{outUSD, inUSD},
				},
				Balances: []string{"Assets:Bank -100.00 EUR", "Assets:Dollars 108.50 USD"}, Scope: "Assets:Bank", Undoable: 1,
			},
		},
		{
			name: "choosing the other field's account swaps the two", script: "x tab tab tab down",
			want:  observed{Flow: Flow{Mode: "transfer"}},
			shows: []string{"Assets:Savings · EUR", "Assets:Bank · EUR"},
		},
		{name: "no amount", script: "x ctrl+s", want: observed{Flow: Flow{Mode: "transfer", Err: `amount: invalid amount ""`}}},
		{name: "an amount that is not a number", script: "x text:lots ctrl+s", want: observed{Flow: Flow{Mode: "transfer", Err: `amount: invalid amount "lots"`}}},
		{name: "an amount of nothing", script: "x text:0 ctrl+s", want: observed{Flow: Flow{Mode: "transfer", Err: "amount must be greater than zero"}}},
		{
			name: "a date that is not a date", script: "x text:5 tab ctrl+u text:someday ctrl+s",
			want: observed{Flow: Flow{Mode: "transfer", Err: `date: invalid date "someday": use YYYY-MM-DD or DD.MM.YYYY`}},
		},
		{
			name: "across currencies, what arrives is required", script: "x " + toDollars + " text:5 ctrl+s",
			want: observed{Flow: Flow{Mode: "transfer", Err: `received: invalid amount ""`}},
		},
		{name: "esc closes it", script: "x text:5 esc", want: observed{Flow: Flow{Mode: "list"}}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := transferAccounts(t)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), Balances: balanceLines(t, st), Scope: m.scopeAccount(), Undoable: len(m.undo)}
			if tc.want.Balances == nil {
				got.Balances = nil
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			view := ""
			if m.mode == modeTransfer {
				view = m.View()
			}
			checkView(t, view, tc.shows, tc.hides)
		})
	}

	t.Run("both halves are marked in the list and the detail pane names the other", func(t *testing.T) {
		// given
		m, _ := transferAccounts(t)
		play(m, "x text:500 enter enter")
		m.accts.cursor = 0
		m.applyFilter(false)

		// when
		selectPayee(t, m, "Transfer from Bank")

		// then
		checkView(t, m.View(), []string{"⇄ Transfer from Bank", "Transfer with #1 on Assets:Bank"}, nil)
	})
}

func Test_Update_transfer_accounts(t *testing.T) {
	type observed struct{ From, To string }
	tcs := []struct {
		name   string
		script string
		want   observed
	}{
		{name: "from the first account to one with the same currency", script: "x", want: observed{"Assets:Bank", "Assets:Savings"}},
		{name: "from the selected account", script: "2 down down down x", want: observed{"Assets:Savings", "Assets:Bank"}},
		{name: "To, one step up, is another currency", script: "x tab tab tab up", want: observed{"Assets:Bank", "Assets:Dollars"}},
		{name: "From stepped onto To swaps them", script: "x tab tab tab up shift+tab down", want: observed{"Assets:Dollars", "Assets:Bank"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := transferAccounts(t)

			// when
			play(m, tc.script)

			// then
			got := observed{m.accounts[m.xfer.from].account.Name, m.accounts[m.xfer.to].account.Name}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_transfer_needsTwoAccounts(t *testing.T) {
	tcs := []struct {
		name  string
		setup func(t *testing.T, m *Model, st *core.Local)
	}{
		{name: "no accounts at all", setup: func(t *testing.T, m *Model, st *core.Local) {}},
		{name: "only one", setup: func(t *testing.T, m *Model, st *core.Local) {
			openAccounts(t, st, "Assets:Bank EUR")
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := noAccountsModel(t)
			tc.setup(t, m, st)

			// when
			press(m, "x")

			// then
			want := Flow{Mode: "list", Status: "a transfer needs two accounts: press n to create another", StatusIsError: true}
			if diff := cmp.Diff(want, observe(t, m, st, nil), equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
