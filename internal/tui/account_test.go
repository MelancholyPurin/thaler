package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
)

// balanceLines are the balances of the asset and liability accounts.
func balanceLines(t *testing.T, st core.Service) []string {
	t.Helper()
	report, err := st.BalanceReport(t.Context(), core.BalanceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range report.Lines {
		out = append(out, l.Account+" "+l.Amount.String())
	}
	return out
}

func accountNames(t *testing.T, st core.Service) []string {
	t.Helper()
	accs, err := st.ListAccounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range accs {
		out = append(out, a.Name)
	}
	return out
}

func Test_Update_newAccount(t *testing.T) {
	type observed struct {
		Flow
		Accounts []string
		Balances []string
		Scope    string
	}
	tcs := []struct {
		name   string
		script string
		want   observed
		shows  []string
	}{
		{
			name: "the add form points at n when there are no accounts", script: "a",
			want: observed{Flow: Flow{Mode: "list", Status: "no accounts yet: press n to create one", StatusIsError: true}},
		},
		{
			name: "n opens the popup, which shows the full name as it is typed", script: "n text:Sparkasse",
			want:  observed{Flow: Flow{Mode: "account"}},
			shows: []string{"New account", "will be called Assets:Sparkasse"},
		},
		{
			name:   "a name, a balance and a date",
			script: "n text:Sparkasse enter enter enter text:1234.56 enter enter",
			want: observed{
				Flow: Flow{
					Mode:   "list",
					Status: "Created Assets:Sparkasse (EUR) · opening balance of 1,234.56 EUR booked on 2026-10-22 · i imports a bank export",
					Added:  []string{"2026-10-22 Opening balance | Assets:Sparkasse 1234.56 | Equity:Opening -1234.56"},
				},
				Accounts: []string{"Assets:Sparkasse", "Equity:Opening"}, Balances: []string{"Assets:Sparkasse 1234.56 EUR"}, Scope: "Assets:Sparkasse",
			},
		},
		{
			name: "z takes back the balance and leaves the account", script: "n text:Sparkasse enter enter enter text:1234.56 enter enter z",
			want: observed{
				Flow:     Flow{Mode: "list", Status: "Undid the opening balance (#1)"},
				Accounts: []string{"Assets:Sparkasse", "Equity:Opening"}, Scope: "Assets:Sparkasse",
			},
		},
		{
			name:   "a liability is given as the amount owed",
			script: "n text:Card enter right enter enter text:500 enter enter",
			want: observed{
				Flow: Flow{
					Mode:   "list",
					Status: "Created Liabilities:Card (EUR) · opening balance of -500.00 EUR booked on 2026-10-22 · i imports a bank export",
					Added:  []string{"2026-10-22 Opening balance | Liabilities:Card -500.00 | Equity:Opening 500.00"},
				},
				Accounts: []string{"Equity:Opening", "Liabilities:Card"}, Balances: []string{"Liabilities:Card -500.00 EUR"}, Scope: "Liabilities:Card",
			},
		},
		{
			name: "without a balance nothing is booked", script: "n text:Cash ctrl+s",
			want: observed{
				Flow:     Flow{Mode: "list", Status: "Created Assets:Cash (EUR) · i imports a bank export"},
				Accounts: []string{"Assets:Cash"}, Scope: "Assets:Cash",
			},
		},
		{name: "esc writes nothing", script: "n text:Cash esc", want: observed{Flow: Flow{Mode: "list"}}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := noAccountsModel(t)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), Accounts: accountNames(t, st), Balances: balanceLines(t, st), Scope: m.scopeAccount()}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			view := ""
			if m.mode == modeAccount {
				view = m.View()
			}
			checkView(t, view, tc.shows, nil)
		})
	}

	t.Run("a liability asks for the amount owed", func(t *testing.T) {
		// given
		m, _ := noAccountsModel(t)

		// when
		play(m, "n text:Card enter right enter enter")

		// then
		checkView(t, m.View(), []string{"the amount you owe"}, nil)
	})
}

// A mistake keeps the popup open with a message; nothing is created.
func Test_Update_newAccount_validation(t *testing.T) {
	tcs := []struct {
		name   string
		script string
		want   string
	}{
		{name: "no name", script: "n ctrl+s", want: "give the account a name, e.g. Sparkasse"},
		{name: "a name that exists", script: "n text:Cash ctrl+s", want: "Assets:Cash already exists"},
		{name: "a currency that is not one", script: "n text:Bank tab tab ctrl+u text:E1 ctrl+s", want: `invalid currency code "E1": want three letters such as EUR, USD or MXN`},
		{name: "a balance that is not a number", script: "n text:Bank tab tab tab text:lots ctrl+s", want: `balance: invalid amount "lots"`},
		{name: "a category", script: "n text:Expenses:Food ctrl+s", want: "only Assets and Liabilities accounts are created here"},
		{name: "a name with a space", script: "n text:My_Bank ctrl+s", want: `account "Assets:My Bank": invalid character ' ' (use letters, digits, '-' and '_')`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := noAccountsModel(t)
			openAccounts(t, st, "Assets:Cash EUR")
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			want := struct {
				Flow
				Accounts []string
			}{Flow{Mode: "account", Err: tc.want}, []string{"Assets:Cash"}}
			got := struct {
				Flow
				Accounts []string
			}{observe(t, m, st, before), accountNames(t, st)}
			if diff := cmp.Diff(want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_setBalance(t *testing.T) {
	type observed struct {
		Flow
		Balances []string
	}
	line := func(amount string) string {
		return "2026-09-14 Opening balance | Assets:Bank " + amount + " | Equity:Opening -" + amount
	}
	tcs := []struct {
		name   string
		script string
		want   observed
		shows  []string
	}{
		{
			name: "with no account selected it refuses", script: "b",
			want: observed{Flow: Flow{Mode: "list", Status: "select an account first: press 2, then ↑/↓", StatusIsError: true}},
		},
		{
			name: "the popup names the account and what Thaler holds", script: "2 down b",
			want: observed{Flow: Flow{Mode: "account"}}, shows: []string{"Set balance · Assets:Bank", "Thaler currently holds"},
		},
		{
			// 5.00 is what the bank says; the 77.00 already spent before it is what the opening balance must explain
			name: "the difference becomes an opening balance before the first transaction", script: "2 down b text:500 enter enter",
			want: observed{
				Flow: Flow{
					Mode:   "list",
					Status: "opening balance of 577.00 EUR booked on 2026-09-14 · z undoes",
					Added:  []string{line("577.00")},
				},
				Balances: []string{"Assets:Bank 500.00 EUR", "Assets:Savings 5.00 EUR"},
			},
		},
		{
			name: "a second reading corrects the same transaction", script: "2 down b text:500 enter enter b text:700 enter enter",
			want: observed{
				Flow: Flow{
					Mode:   "list",
					Status: "opening balance of 777.00 EUR booked on 2026-09-14 · z undoes",
					Added:  []string{line("777.00")},
				},
				Balances: []string{"Assets:Bank 700.00 EUR", "Assets:Savings 5.00 EUR"},
			},
		},
		{
			name: "z restores the first reading", script: "2 down b text:500 enter enter b text:700 enter enter z",
			want: observed{
				Flow:     Flow{Mode: "list", Status: "Undid the opening balance (#6)", Added: []string{line("577.00")}},
				Balances: []string{"Assets:Bank 500.00 EUR", "Assets:Savings 5.00 EUR"},
			},
		},
		{
			name: "z again removes it", script: "2 down b text:500 enter enter b text:700 enter enter z z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid the opening balance (#6)"}, Balances: []string{"Assets:Bank -77.00 EUR", "Assets:Savings 5.00 EUR"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), Balances: balanceLines(t, st)}
			if tc.want.Balances == nil {
				got.Balances = nil
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			view := ""
			if m.mode == modeAccount {
				view = m.View()
			}
			checkView(t, view, tc.shows, nil)
		})
	}
}
