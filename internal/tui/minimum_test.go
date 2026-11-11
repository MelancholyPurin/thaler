package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
)

func Test_Update_minimum(t *testing.T) {
	type observed struct {
		Mode     string
		Status   string
		Err      string // what the popup says is wrong
		Amount   string // what is typed in the popup, when it is open
		Account  string // the account the popup is on, when it is open
		Minimums []core.MinimumBalance
		Lows     []string // the accounts that would fall under theirs: account, day, balance
		Undo     int      // how many things z would take back
	}
	bank := func(minor int64) core.MinimumBalance {
		return core.MinimumBalance{Account: "Assets:Bank", Amount: eur(minor)}
	}
	tcs := []struct {
		name    string
		defined []core.NewSchedule
		keys    string
		want    observed
	}{
		{name: "m opens the popup with nothing in the amount", keys: "F m", want: observed{Mode: "minimum", Account: "Assets:Bank"}},
		{
			name: "it opens on the account selected in the panel",
			keys: "tab down down F m",
			want: observed{Mode: "minimum", Account: "Assets:Dollars"},
		},
		{
			name: "a minimum is set",
			keys: "F m text:500 enter",
			want: observed{
				Mode: "list", Status: "Minimum of Bank: 500.00 EUR · z undoes", Minimums: []core.MinimumBalance{bank(50000)}, Undo: 1,
				Lows: []string{"Assets:Bank 2026-10-22 -10.00 EUR"},
			},
		},
		{
			name: "an agreed overdraft",
			keys: "F m text:-300 enter",
			want: observed{Mode: "list", Status: "Minimum of Bank: -300.00 EUR · z undoes", Minimums: []core.MinimumBalance{bank(-30000)}, Undo: 1},
		},
		{
			name: "setting it again shows the old one",
			keys: "F m text:500 enter m",
			want: observed{Mode: "minimum", Account: "Assets:Bank", Amount: "500.00", Minimums: []core.MinimumBalance{bank(50000)}, Undo: 1, Lows: []string{"Assets:Bank 2026-10-22 -10.00 EUR"}},
		},
		{
			name: "a new amount replaces it",
			keys: "F m text:500 enter m ctrl+u text:200 enter",
			want: observed{Mode: "list", Status: "Minimum of Bank: 200.00 EUR · z undoes", Minimums: []core.MinimumBalance{bank(20000)}, Undo: 2, Lows: []string{"Assets:Bank 2026-10-22 -10.00 EUR"}},
		},
		{
			name: "the same amount changes nothing",
			keys: "F m text:500 enter m enter",
			want: observed{Mode: "list", Minimums: []core.MinimumBalance{bank(50000)}, Undo: 1, Lows: []string{"Assets:Bank 2026-10-22 -10.00 EUR"}},
		},
		{
			name: "an empty amount clears it",
			keys: "F m text:500 enter m ctrl+u enter",
			want: observed{Mode: "list", Status: "Minimum of Bank cleared · z undoes", Undo: 2},
		},
		{
			name: "nothing to clear",
			keys: "F m enter",
			want: observed{Mode: "minimum", Account: "Assets:Bank", Err: "type an amount: there is no minimum to clear"},
		},
		{
			name: "not an amount",
			keys: "F m text:lots enter",
			want: observed{Mode: "minimum", Account: "Assets:Bank", Amount: "lots", Err: `amount: invalid amount "lots"`},
		},
		{
			name: "esc leaves it as it was",
			keys: "F m text:500 esc",
			want: observed{Mode: "list"},
		},
		{
			name: "down is another account, with its own minimum",
			keys: "F m text:500 enter m down",
			want: observed{Mode: "minimum", Account: "Assets:Dollars", Minimums: []core.MinimumBalance{bank(50000)}, Undo: 1, Lows: []string{"Assets:Bank 2026-10-22 -10.00 EUR"}},
		},
		{
			name: "the other account gets its own",
			keys: "F m down text:100 enter",
			want: observed{
				Mode: "list", Status: "Minimum of Dollars: 100.00 USD · z undoes", Undo: 1,
				Minimums: []core.MinimumBalance{{Account: "Assets:Dollars", Amount: usd(10000)}},
				Lows:     []string{"Assets:Dollars 2026-10-22 0.00 USD"},
			},
		},
		{
			name: "z takes a minimum back",
			keys: "F m text:500 enter z",
			want: observed{Mode: "list", Status: "Minimum of Bank taken back"},
		},
		{
			name: "z puts the old one back after a change",
			keys: "F m text:500 enter m ctrl+u text:200 enter z",
			want: observed{Mode: "list", Status: "Minimum of Bank taken back", Minimums: []core.MinimumBalance{bank(50000)}, Undo: 1, Lows: []string{"Assets:Bank 2026-10-22 -10.00 EUR"}},
		},
		{
			name: "z puts back one that was cleared",
			keys: "F m text:500 enter m ctrl+u enter z",
			want: observed{Mode: "list", Status: "Minimum of Bank taken back", Minimums: []core.MinimumBalance{bank(50000)}, Undo: 1, Lows: []string{"Assets:Bank 2026-10-22 -10.00 EUR"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := scheduleModel(t, tc.defined...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, Undo: len(m.undo)}
			if m.minForm != nil {
				got.Err = m.minForm.err
				got.Amount = m.minForm.amount.Value()
				got.Account = m.minForm.rows[m.minForm.list.cursor].account.Name
			}
			var err error
			if got.Minimums, err = st.MinimumBalances(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, l := range m.lows {
				got.Lows = append(got.Lows, l.Account+" "+l.Date+" "+l.Balance.String())
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_minimum_noAccounts(t *testing.T) {
	tcs := []struct {
		name       string
		keys       string
		wantMode   string
		wantStatus string
	}{
		{name: "no Assets account to give one to", keys: "F m", wantMode: "list", wantStatus: "no Assets account yet: create one first"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := noAccountsModel(t)

			// when
			play(m, tc.keys)

			// then
			if diff := cmp.Diff([]string{tc.wantMode, tc.wantStatus}, []string{modeNames[m.mode], m.status}); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_openMinimum_rows(t *testing.T) {
	tcs := []struct {
		name  string
		extra []string
		want  []string
	}{
		{name: "the Assets accounts, by name", want: []string{"Assets:Bank", "Assets:Dollars", "Assets:Savings"}},
		{name: "a liability has none", extra: []string{"Liabilities:Card EUR"}, want: []string{"Assets:Bank", "Assets:Dollars", "Assets:Savings"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := scheduleModel(t)
			openAccounts(t, st, tc.extra...)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, "F m")

			// then
			var got []string
			for _, r := range m.minForm.rows {
				got = append(got, r.account.Name)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("rows mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
