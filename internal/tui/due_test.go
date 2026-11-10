package tui

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// overdueRent is a rent of 900.00 on the 28th that started in August: on the day of the
// tests, 22 October 2026, the dates of August and September are waiting.
func overdueRent() core.NewSchedule {
	ns := rentSchedule()
	ns.Start = startOn("2026-08-28")
	return ns
}

func monthlySaving(to string, received ledger.Money) core.NewSchedule {
	in := received
	if in.Currency == "" {
		in = eur(20000)
	}
	return core.NewSchedule{
		Name: "Saving", Start: startOn("2026-10-01"), Transfer: true,
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: to, Amount: in}},
	}
}

func Test_Update_due(t *testing.T) {
	type observed struct {
		Mode   string
		Status string
		Cursor int      // the date on show, when the popup is open
		Edit   bool     // typing an amount
		Err    string   // what the popup says is wrong
		Due    []string // dates still waiting, as of the day of the tests
		Booked []string // what went into the ledger by the schedules: date, payee and the amount on the account
	}
	tcs := []struct {
		name    string
		defined []core.NewSchedule
		keys    string
		want    observed
	}{
		{
			name:    "nothing is due",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F enter",
			want:    observed{Mode: "list", Status: "Nothing is due."},
		},
		{
			name: "no schedules at all",
			keys: "F enter",
			want: observed{Mode: "list", Status: "Nothing is due."},
		},
		{
			name:    "the popup opens on the oldest date and books nothing",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter",
			want:    observed{Mode: "due", Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "enter books it as it is, and goes on to the next",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter enter",
			want: observed{Mode: "due", Status: "Rent booked on 2026-08-28: -900.00 EUR · z undoes", Due: []string{"2026-09-28 Rent"},
				Booked: []string{"2026-08-28 Landlord -900.00 EUR"}},
		},
		{
			name:    "the last answer closes it",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter enter enter",
			want: observed{Mode: "list", Status: "Rent booked on 2026-09-28: -900.00 EUR · all answered · z undoes",
				Booked: []string{"2026-08-28 Landlord -900.00 EUR", "2026-09-28 Landlord -900.00 EUR"}},
		},
		{
			name:    "y books it too",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter y",
			want: observed{Mode: "due", Status: "Rent booked on 2026-08-28: -900.00 EUR · z undoes", Due: []string{"2026-09-28 Rent"},
				Booked: []string{"2026-08-28 Landlord -900.00 EUR"}},
		},
		{
			name:    "s skips it and books nothing",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter s",
			want:    observed{Mode: "due", Status: "Rent 2026-08-28 skipped · z undoes", Due: []string{"2026-09-28 Rent"}},
		},
		{
			name:    "both skipped: it closes",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter s s",
			want:    observed{Mode: "list", Status: "Rent 2026-09-28 skipped · all answered · z undoes"},
		},
		{
			name:    "down goes to another date, and that is the one answered",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter down enter",
			want: observed{Mode: "due", Status: "Rent booked on 2026-09-28: -900.00 EUR · z undoes", Due: []string{"2026-08-28 Rent"},
				Booked: []string{"2026-09-28 Landlord -900.00 EUR"}},
		},
		{
			name: "after a date in the middle the cursor stays where it was: on the one that followed",
			defined: []core.NewSchedule{func() core.NewSchedule {
				ns := overdueRent()
				ns.Start = startOn("2026-07-28")
				return ns
			}()},
			keys: "F enter down enter",
			want: observed{Mode: "due", Status: "Rent booked on 2026-08-28: -900.00 EUR · z undoes", Cursor: 1, Due: []string{"2026-07-28 Rent", "2026-09-28 Rent"},
				Booked: []string{"2026-08-28 Landlord -900.00 EUR"}},
		},
		{
			name:    "moving does not go past the ends",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter down down down",
			want:    observed{Mode: "due", Cursor: 1, Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "up and k come back",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter j k up",
			want:    observed{Mode: "due", Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "esc stops, and nothing is booked",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter esc",
			want:    observed{Mode: "list", Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "e opens the amount as it is",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter e",
			want:    observed{Mode: "due", Edit: true, Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "and books what it really was",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter e ctrl+u text:912.40 enter",
			want: observed{Mode: "due", Status: "Rent booked on 2026-08-28: -912.40 EUR · z undoes", Due: []string{"2026-09-28 Rent"},
				Booked: []string{"2026-08-28 Landlord -912.40 EUR"}},
		},
		{
			name:    "ctrl+s books it too",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter e ctrl+u text:912.40 ctrl+s",
			want: observed{Mode: "due", Status: "Rent booked on 2026-08-28: -912.40 EUR · z undoes", Due: []string{"2026-09-28 Rent"},
				Booked: []string{"2026-08-28 Landlord -912.40 EUR"}},
		},
		{
			name:    "esc goes back from the amount, and the date is still there",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter e esc",
			want:    observed{Mode: "due", Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "an amount that is no amount",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter e ctrl+u text:lots enter",
			want:    observed{Mode: "due", Edit: true, Err: `amount: invalid amount "lots"`, Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "an amount that is zero",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter e ctrl+u text:0 enter",
			want:    observed{Mode: "due", Edit: true, Err: "amount must be greater than zero", Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "z takes a booking back and the date is due again",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter enter esc z",
			want:    observed{Mode: "list", Status: "Undid booking Rent on 2026-08-28", Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "z takes a skip back",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter s esc z",
			want:    observed{Mode: "list", Status: "Rent 2026-08-28 is due again", Due: []string{"2026-08-28 Rent", "2026-09-28 Rent"}},
		},
		{
			name:    "an income is booked with its sign",
			defined: []core.NewSchedule{{Name: "Pay", Start: startOn("2026-10-02"), Payee: "Employer", Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(300000)}, {Account: "Income:Salary", Amount: eur(-300000)}}}},
			keys:    "F enter e ctrl+u text:+3100 enter",
			want:    observed{Mode: "list", Status: "Pay booked on 2026-10-02: +3,100.00 EUR · all answered · z undoes", Booked: []string{"2026-10-02 Employer +3,100.00 EUR"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := scheduleModel(t, tc.defined...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status}
			if d := m.due; d != nil {
				got.Cursor, got.Edit, got.Err = d.cur, d.edit, d.err
			}
			due, err := st.Due(t.Context(), "2026-10-22")
			if err != nil {
				t.Fatal(err)
			}
			for _, o := range due {
				got.Due = append(got.Due, o.Date+" "+o.Schedule.Name)
			}
			txs, err := st.ListTransactions(t.Context(), core.TxFilter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, tx := range txs {
				if tx.Payee == "REWE" { // the ledger the model starts from
					continue
				}
				got.Booked = append(got.Booked, fmt.Sprintf("%s %s %s", tx.Date, tx.Payee, m.loc.Signed(tx.Postings[0].Amount)))
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_dueTransfers(t *testing.T) {
	type observed struct {
		Mode   string
		Status string
		Err    string
		Txs    []string // date, payee, and each leg: the account and the amount
	}
	tcs := []struct {
		name    string
		defined []core.NewSchedule
		keys    string
		want    observed
	}{
		{
			name:    "a transfer is booked as two linked transactions",
			defined: []core.NewSchedule{monthlySaving("Assets:Savings", ledger.Money{})},
			keys:    "F enter enter",
			want: observed{Mode: "list", Status: "Saving booked on 2026-10-01: -200.00 EUR · all answered · z undoes", Txs: []string{
				"2026-10-01 Transfer to Savings: Assets:Bank -200.00 EUR, Equity:Transfers +200.00 EUR",
				"2026-10-01 Transfer from Bank: Assets:Savings +200.00 EUR, Equity:Transfers -200.00 EUR",
			}},
		},
		{
			name:    "for another amount, which is how much leaves",
			defined: []core.NewSchedule{monthlySaving("Assets:Savings", ledger.Money{})},
			keys:    "F enter e ctrl+u text:250 enter",
			want: observed{Mode: "list", Status: "Saving booked on 2026-10-01: -250.00 EUR · all answered · z undoes", Txs: []string{
				"2026-10-01 Transfer to Savings: Assets:Bank -250.00 EUR, Equity:Transfers +250.00 EUR",
				"2026-10-01 Transfer from Bank: Assets:Savings +250.00 EUR, Equity:Transfers -250.00 EUR",
			}},
		},
		{
			name:    "z takes both halves back",
			defined: []core.NewSchedule{monthlySaving("Assets:Savings", ledger.Money{})},
			keys:    "F enter enter z",
			want:    observed{Mode: "list", Status: "Undid booking Saving on 2026-10-01"},
		},
		{
			name:    "between currencies the amount that arrives is asked for, too",
			defined: []core.NewSchedule{monthlySaving("Assets:Dollars", usd(21500))},
			keys:    "F enter e",
			want:    observed{Mode: "due"},
		},
		{
			name:    "and booked with what really arrived",
			defined: []core.NewSchedule{monthlySaving("Assets:Dollars", usd(21500))},
			keys:    "F enter e enter ctrl+u text:220 enter",
			want: observed{Mode: "list", Status: "Saving booked on 2026-10-01: -200.00 EUR · all answered · z undoes", Txs: []string{
				"2026-10-01 Transfer to Dollars: Assets:Bank -200.00 EUR, Equity:Conversions +200.00 EUR",
				"2026-10-01 Transfer from Bank: Assets:Dollars +220.00 USD, Equity:Conversions -220.00 USD",
			}},
		},
		{
			name:    "tab moves between the two",
			defined: []core.NewSchedule{monthlySaving("Assets:Dollars", usd(21500))},
			keys:    "F enter e tab ctrl+u text:220 tab enter enter",
			want: observed{Mode: "list", Status: "Saving booked on 2026-10-01: -200.00 EUR · all answered · z undoes", Txs: []string{
				"2026-10-01 Transfer to Dollars: Assets:Bank -200.00 EUR, Equity:Conversions +200.00 EUR",
				"2026-10-01 Transfer from Bank: Assets:Dollars +220.00 USD, Equity:Conversions -220.00 USD",
			}},
		},
		{
			name:    "an amount that arrives that is no amount",
			defined: []core.NewSchedule{monthlySaving("Assets:Dollars", usd(21500))},
			keys:    "F enter e enter ctrl+u text:lots enter",
			want:    observed{Mode: "due", Err: `received: invalid amount "lots"`},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := scheduleModel(t, tc.defined...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status}
			if d := m.due; d != nil {
				got.Err = d.err
			}
			txs, err := st.ListTransactions(t.Context(), core.TxFilter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, tx := range txs {
				if tx.Payee == "REWE" {
					continue
				}
				line := fmt.Sprintf("%s %s:", tx.Date, tx.Payee)
				for i, p := range tx.Postings {
					if i > 0 {
						line += ","
					}
					line += fmt.Sprintf(" %s %s", p.Account, m.loc.Signed(p.Amount))
				}
				got.Txs = append(got.Txs, line)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_duePopup(t *testing.T) {
	tcs := []struct {
		name    string
		defined []core.NewSchedule
		keys    string
		shows   []string
		hides   []string
	}{
		{
			name:    "the date, how long ago, the amount, and the keys",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter",
			shows:   []string{"What is due", "Rent", "2026-08-28", "55 days ago", "-900.00 EUR", "Bank", "Expenses:Rent", "enter book it", "e change the amount", "s skip", "esc stop", "1 of 2"},
			hides:   []string{"estimate"},
		},
		{
			name:    "the next date",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter down",
			shows:   []string{"2026-09-28", "24 days ago", "2 of 2"},
		},
		{
			name: "one day ago",
			defined: []core.NewSchedule{func() core.NewSchedule {
				ns := rentSchedule()
				ns.Start = startOn("2026-10-21")
				return ns
			}()},
			keys:  "F enter",
			shows: []string{"2026-10-21", "1 day ago"},
			hides: []string{"1 days"},
		},
		{
			name: "today",
			defined: []core.NewSchedule{func() core.NewSchedule {
				ns := rentSchedule()
				ns.Start = startOn("2026-10-22")
				return ns
			}()},
			keys:  "F enter",
			shows: []string{"2026-10-22", "today"},
			hides: []string{"ago"},
		},
		{
			name:    "a transfer says where it goes",
			defined: []core.NewSchedule{monthlySaving("Assets:Savings", ledger.Money{})},
			keys:    "F enter",
			shows:   []string{"⇄ Saving", "-200.00 EUR", "Bank", "⇄ Savings"},
		},
		{
			name:    "and what arrives across currencies",
			defined: []core.NewSchedule{monthlySaving("Assets:Dollars", usd(21500))},
			keys:    "F enter",
			shows:   []string{"⇄ Dollars", "+215.00 USD"},
		},
		{
			name: "an estimate says what to do",
			defined: []core.NewSchedule{func() core.NewSchedule {
				ns := overdueRent()
				ns.Tolerance = 25
				return ns
			}()},
			keys:  "F enter",
			shows: []string{"an estimate (±25%)", "press e to enter what it really was"},
		},
		{
			name:    "the amount typed, with the sign it has",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter e",
			shows:   []string{"What is due", "Amount", "900.00", "EUR", "no sign: money out", "enter book it", "esc back"},
			hides:   []string{"s skip"},
		},
		{
			name:    "an error stays on the screen",
			defined: []core.NewSchedule{overdueRent()},
			keys:    "F enter e ctrl+u text:0 enter",
			shows:   []string{"amount must be greater than zero"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := scheduleModel(t, tc.defined...)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
