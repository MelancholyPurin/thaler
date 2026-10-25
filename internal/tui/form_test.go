package tui

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
)

func Test_parseFormDate(t *testing.T) {
	now := time.Date(2026, 10, 22, 23, 59, 0, 0, time.Local)
	tcs := []struct {
		name    string
		in      string
		want    string // YYYY-MM-DD
		wantErr bool
	}{
		{name: "empty is today", in: "", want: "2026-10-22"},
		{name: "t is today", in: "t", want: "2026-10-22"},
		{name: "today in full", in: "today", want: "2026-10-22"},
		{name: "y is yesterday", in: "y", want: "2026-10-21"},
		{name: "yesterday in full", in: "Yesterday", want: "2026-10-21"},
		{name: "-2 is two days ago", in: "-2", want: "2026-10-20"},
		{name: "-0 is today", in: "-0", want: "2026-10-22"},
		{name: "an iso date", in: "2026-10-01", want: "2026-10-01"},
		{name: "a german date", in: "01.10.2026", want: "2026-10-01"},
		{name: "spaces around it", in: "  y ", want: "2026-10-21"},
		{name: "a word", in: "someday", wantErr: true},
		{name: "a date that does not exist", in: "31.02.2026", wantErr: true},
		{name: "minus a word", in: "-x", wantErr: true},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in and the clock

			// when
			got, err := parseFormDate(tc.in, now)

			// then
			if diff := cmp.Diff(tc.wantErr, err != nil); diff != "" {
				t.Fatalf("failure mismatch (-want +got):\n%s (err = %v)", diff, err)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, got.Format("2006-01-02")); diff != "" {
				t.Errorf("date mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Saving the add form stores the transaction and says so.
func Test_Update_addForm(t *testing.T) {
	type observed struct {
		Flow
		Selected    string // the payee of the transaction the list is on
		LastAccount string
	}
	tcs := []struct {
		name   string
		script string
		want   observed
	}{
		{
			name:   "the fast path: amount, a known payee, its usual category",
			script: "a text:8.40 enter text:rew enter enter",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Added #6  REWE  -8.40 EUR → Expenses:Groceries",
					Added: []string{"2026-10-22 REWE | Assets:Bank -8.40 | Expenses:Groceries 8.40"},
				},
				Selected: "REWE", LastAccount: "Assets:Bank",
			},
		},
		{
			name:   "income, with a category that does not exist yet",
			script: "a text:+2450 enter text:Muster_GmbH enter text:Income:Salary down enter",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Added #6  Muster GmbH  +2450.00 EUR → Income:Salary",
					Added: []string{"2026-10-22 Muster GmbH | Assets:Bank 2450.00 | Income:Salary -2450.00"},
				},
				Selected: "Muster GmbH", LastAccount: "Assets:Bank",
			},
		},
		{
			name:   "left to decide later it is uncategorized",
			script: "a text:3 enter text:Kiosk enter down down down down down down enter",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Added #6  Kiosk  -3.00 EUR → Expenses:Uncategorized",
					Added: []string{"2026-10-22 Kiosk | Assets:Bank -3.00 | Expenses:Uncategorized 3.00"},
				},
				Selected: "Kiosk", LastAccount: "Assets:Bank",
			},
		},
		{
			name:   "to another date, saved from the date field, with whatever category is suggested first",
			script: "a text:1.50 tab tab tab ctrl+u text:y ctrl+s",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Added #6    -1.50 EUR → Expenses:Groceries",
					Added: []string{"2026-10-21  | Assets:Bank -1.50 | Expenses:Groceries 1.50"},
				},
				Selected: "", LastAccount: "Assets:Bank",
			},
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
			selected := ""
			if r, ok := m.selected(); ok {
				selected = r.tx.Payee
			}
			got := observed{Flow: observe(t, m, st, before), Selected: selected, LastAccount: m.lastAccount}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// While the add form is being filled in it suggests payees and categories.
func Test_Update_addForm_suggestions(t *testing.T) {
	type observed struct {
		Mode          string
		PayeeHighlit  int      // index of the payee that Enter would complete, -1 for none
		FirstPayees   []string // the suggestions, best first
		FirstCategory pickItem // the category Enter would choose
		Income        bool
		LastCategory  pickItem // what ends the list
	}
	tcs := []struct {
		name   string
		script string
		want   observed
	}{
		{
			name:   "the start of a known payee is highlighted so Enter completes it",
			script: "a text:8.40 enter text:rew",
			want:   observed{Mode: "form", PayeeHighlit: 0, FirstPayees: []string{"REWE"}, FirstCategory: pickItem{name: "Expenses:Groceries", hint: "2×"}, LastCategory: pickItem{name: "Expenses:Uncategorized", hint: "decide later"}},
		},
		{
			name:   "then the category that payee went to last time comes first",
			script: "a text:8.40 enter text:rew enter",
			want: observed{Mode: "form", PayeeHighlit: 0, FirstPayees: []string{"REWE"},
				FirstCategory: pickItem{name: "Expenses:Groceries", hint: "same payee"}, LastCategory: pickItem{name: "Expenses:Uncategorized", hint: "decide later"}},
		},
		{
			name:   "a payee never seen before has no suggestions",
			script: "a text:8.40 enter text:Zzz",
			want:   observed{Mode: "form", PayeeHighlit: -1, FirstCategory: pickItem{name: "Expenses:Groceries", hint: "2×"}, LastCategory: pickItem{name: "Expenses:Uncategorized", hint: "decide later"}},
		},
		{
			name:   "a leading plus means income, and a new full name is offered as a new category",
			script: "a text:+2450 enter text:Muster_GmbH enter text:Income:Salary",
			want: observed{Mode: "form", PayeeHighlit: -1, Income: true,
				FirstCategory: pickItem{name: "Income:Salary", hint: "new category", create: true}, LastCategory: pickItem{name: "Income:Salary", hint: "new category", create: true}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			play(m, tc.script)

			// then
			f := m.form
			var payees []string
			for _, p := range f.payeeItems {
				payees = append(payees, p.name)
			}
			got := observed{
				Mode: modeNames[m.mode], PayeeHighlit: f.payeeCur, FirstPayees: payees, Income: f.isIncome(),
				FirstCategory: f.catItems[0], LastCategory: f.catItems[len(f.catItems)-1],
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty, cmp.AllowUnexported(pickItem{})); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The account the form starts on: the one selected in the Accounts panel, else
// the one used last in this session, else the one of the newest transaction.
func Test_Update_addForm_account(t *testing.T) {
	tcs := []struct {
		name   string
		setup  func(t *testing.T, m *Model, st *core.Local)
		script string
		want   string
	}{
		{name: "the first account when nothing says otherwise", script: "a", want: "Assets:Bank"},
		{name: "the account selected in the Accounts panel", script: "2 down down a", want: "Assets:Savings"},
		{name: "on all accounts, the one used last in this session", script: "2 down down a text:1 ctrl+s g a", want: "Assets:Savings"},
		{
			name: "else the account of the newest transaction in the database",
			setup: func(t *testing.T, m *Model, st *core.Local) {
				book(t, st, "2026-10-20", "Cash tip", "Assets:Savings", "Expenses:Tips", -200)
				if err := m.reload(); err != nil {
					t.Fatal(err)
				}
				m.applyFilter(true)
			},
			script: "a", want: "Assets:Savings",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			if tc.setup != nil {
				tc.setup(t, m, st)
			}

			// when
			play(m, tc.script)

			// then
			if diff := cmp.Diff(tc.want, m.accounts[m.form.account].account.Name); diff != "" {
				t.Errorf("account mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Mistakes are reported in the form and nothing is written.
func Test_Update_addForm_validation(t *testing.T) {
	tcs := []struct {
		name   string
		script string
		want   Flow
	}{
		{
			name: "an amount that is not a number", script: "a text:abc ctrl+s",
			want: Flow{Mode: "form", Err: `amount: invalid amount "abc"`},
		},
		{
			name: "a date that does not exist", script: "a text:5 tab tab tab ctrl+u text:31.02.2026 ctrl+s",
			want: Flow{Mode: "form", Err: `date: invalid date "31.02.2026": use YYYY-MM-DD or DD.MM.YYYY`},
		},
		{
			name: "no amount at all", script: "a ctrl+s",
			want: Flow{Mode: "form", Err: `amount: invalid amount ""`},
		},
		{
			name: "nothing but zero", script: "a text:0 ctrl+s",
			want: Flow{Mode: "form", Err: "amount must be greater than zero"},
		},
		{
			name: "a category that matches nothing and is not a full name", script: "a text:5 enter text:x enter text:zzzz ctrl+s",
			want: Flow{Mode: "form", Err: "no category matches; type a full name like Expenses:Gifts to create one"},
		},
		{name: "esc cancels", script: "a text:5 esc", want: Flow{Mode: "list"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			if diff := cmp.Diff(tc.want, observe(t, m, st, before), equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_editForm(t *testing.T) {
	type observed struct {
		Flow
		Amount, Payee string // the form's fields, while it is open
	}
	const kino = "2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Uncategorized 12.00"
	tcs := []struct {
		name   string
		script string
		want   observed
	}{
		{name: "opens prefilled", script: "3 u e", want: observed{Flow: Flow{Mode: "form"}, Amount: "12.00", Payee: "Kino"}},
		{
			name: "a new amount; the category stays as it was", script: "3 u e ctrl+u text:15.50 ctrl+s",
			want: observed{Flow: Flow{
				Mode: "list", Status: "Updated #4  Kino  -15.50 EUR → Expenses:Uncategorized",
				Added: []string{"2026-10-03 Kino | Assets:Bank -15.50 | Expenses:Uncategorized 15.50"}, Removed: []string{kino},
			}},
		},
		{name: "z puts the old amount back", script: "3 u e ctrl+u text:15.50 ctrl+s z", want: observed{Flow: Flow{Mode: "list", Status: "Undid edit of #4"}}},
		{name: "esc leaves it as it was", script: "3 u e ctrl+u text:15.50 esc", want: observed{Flow: Flow{Mode: "list"}}},
		{
			name: "a transfer cannot be edited here", script: "3 g e",
			want: observed{Flow: Flow{Mode: "list", Status: "transfers and split transactions cannot be edited here", StatusIsError: true}},
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
			got := observed{Flow: observe(t, m, st, before)}
			if m.form != nil {
				got.Amount, got.Payee = m.form.amount.Value(), m.form.payee.Value()
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Banks book zero amounts (card verifications) and may correct a charge to zero,
// so an edit may be zero; a new entry typed by hand may not.
func Test_Update_editForm_zeroAmounts(t *testing.T) {
	setup := func(t *testing.T) (*Model, *core.Local) {
		m, st := emptyModel(t)
		book(t, st, "2026-10-05", "Verifizierung", "Assets:Bank", "Expenses:Uncategorized", 0)
		book(t, st, "2026-10-05", "Bakery", "Assets:Bank", "Expenses:Uncategorized", -320)
		if err := m.reload(); err != nil {
			t.Fatal(err)
		}
		m.applyFilter(false)
		return m, st
	}
	const (
		verification = "2026-10-05 Verifizierung | Assets:Bank 0.00 | Expenses:Uncategorized 0.00"
		bakery       = "2026-10-05 Bakery | Assets:Bank -3.20 | Expenses:Uncategorized 3.20"
	)
	tcs := []struct {
		name    string
		payee   string // the transaction to start on
		script  string
		want    Flow
		wantAmt string // the amount shown when the form opens
	}{
		{name: "the form opens on 0.00", payee: "Verifizierung", script: "e", want: Flow{Mode: "form"}, wantAmt: "0.00"},
		{
			name: "a zero transaction can be renamed and stays zero", payee: "Verifizierung", script: "e enter text:_Karte ctrl+s",
			want: Flow{
				Mode: "list", Status: "Updated #1  Verifizierung Karte  0.00 EUR → Expenses:Uncategorized",
				Added: []string{"2026-10-05 Verifizierung Karte | Assets:Bank 0.00 | Expenses:Uncategorized 0.00"}, Removed: []string{verification},
			},
		},
		{
			name: "z restores it", payee: "Verifizierung", script: "e enter text:_Karte ctrl+s z",
			want: Flow{Mode: "list", Status: "Undid edit of #1"},
		},
		{
			name: "it can be given a real amount", payee: "Verifizierung", script: "e ctrl+u text:1.50 ctrl+s",
			want: Flow{
				Mode: "list", Status: "Updated #1  Verifizierung  -1.50 EUR → Expenses:Uncategorized",
				Added: []string{"2026-10-05 Verifizierung | Assets:Bank -1.50 | Expenses:Uncategorized 1.50"}, Removed: []string{verification},
			},
		},
		{
			name: "a real amount can be edited down to zero", payee: "Bakery", script: "e ctrl+u text:0 ctrl+s",
			want: Flow{
				Mode: "list", Status: "Updated #2  Bakery  0.00 EUR → Expenses:Uncategorized",
				Added: []string{"2026-10-05 Bakery | Assets:Bank 0.00 | Expenses:Uncategorized 0.00"}, Removed: []string{bakery},
			},
		},
		{
			name: "but a new entry cannot be zero", payee: "Bakery", script: "a text:0 ctrl+s",
			want: Flow{Mode: "form", Err: "amount must be greater than zero"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := setup(t)
			before := storedLines(t, st)
			selectPayee(t, m, tc.payee)

			// when
			play(m, tc.script)

			// then
			if diff := cmp.Diff(tc.want, observe(t, m, st, before), equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if tc.wantAmt != "" {
				if diff := cmp.Diff(tc.wantAmt, m.form.amount.Value()); diff != "" {
					t.Errorf("amount in the form (-want +got):\n%s", diff)
				}
			}
		})
	}
}
