package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
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

// descriptionOf is the description of a payee's newest transaction.
func descriptionOf(t *testing.T, st core.Service, payee string) string {
	t.Helper()
	return txByPayee(t, st, payee).Description
}

var formFieldNames = map[formField]string{
	fieldAmount: "amount", fieldPayee: "payee", fieldCategory: "category", fieldDate: "date",
	fieldDescription: "description", fieldAccount: "account",
}

// The title of a transaction (its payee) can be renamed, and a description can be
// added, when creating a transaction and when editing one. r renames: it opens
// the edit form already on the title.
func Test_Update_form_titleAndDescription(t *testing.T) {
	type observed struct {
		Flow
		Field       string // the field with the keyboard, while the form is open
		Title       string // the payee of the transaction the script is about
		Description string
	}
	const (
		kino = "2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Uncategorized 12.00"
		shop = "2026-10-05 Bakery | Assets:Bank -3.20 | Expenses:Groceries 3.20"
	)
	long := strings.Repeat("Sammelbuchung ", 30) // a bank purpose far longer than an input
	tcs := []struct {
		name   string
		start  string // the payee to select first, "" to select none
		script string
		about  string // the payee to report on afterwards
		want   observed
	}{
		{
			name: "a description can be typed when adding", script: "a text:3 enter text:Kiosk enter tab tab text:Sunday_paper ctrl+s", about: "Kiosk",
			want: observed{
				Flow:  Flow{Mode: "list", Status: "Added #8  Kiosk  -3.00 EUR → Expenses:Groceries", Added: []string{"2026-10-22 Kiosk | Assets:Bank -3.00 | Expenses:Groceries 3.00"}},
				Title: "Kiosk", Description: "Sunday paper",
			},
		},
		{
			name: "...and the quick path has no extra step: amount, payee, category", script: "a text:8.40 enter text:Kiosk enter enter", about: "Kiosk",
			want: observed{
				Flow:  Flow{Mode: "list", Status: "Added #8  Kiosk  -8.40 EUR → Expenses:Groceries", Added: []string{"2026-10-22 Kiosk | Assets:Bank -8.40 | Expenses:Groceries 8.40"}},
				Title: "Kiosk",
			},
		},
		{
			name: "r opens the edit form on the title", start: "Kino", script: "r", about: "Kino",
			want: observed{Flow: Flow{Mode: "form"}, Field: "payee", Title: "Kino"},
		},
		{
			name: "r then a new name renames it", start: "Kino", script: "r ctrl+u text:Cinema ctrl+s", about: "Cinema",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Updated #4  Cinema  -12.00 EUR → Expenses:Uncategorized",
					Added: []string{"2026-10-03 Cinema | Assets:Bank -12.00 | Expenses:Uncategorized 12.00"}, Removed: []string{kino},
				},
				Title: "Cinema",
			},
		},
		{
			name: "e still opens on the amount, and tab reaches the description", start: "Kino", script: "e tab tab tab tab", about: "Kino",
			want: observed{Flow: Flow{Mode: "form"}, Field: "description", Title: "Kino"},
		},
		{
			name: "a description added to an existing transaction", start: "Kino", script: "e tab tab tab tab text:Seen_with_Anna ctrl+s", about: "Kino",
			want: observed{
				Flow:  Flow{Mode: "list", Status: "Updated #4  Kino  -12.00 EUR → Expenses:Uncategorized"},
				Title: "Kino", Description: "Seen with Anna",
			},
		},
		{
			name: "the form opens with the description it has", start: "Bakery", script: "e tab tab tab tab", about: "Bakery",
			want: observed{Flow: Flow{Mode: "form"}, Field: "description", Title: "Bakery", Description: "weekly shop"},
		},
		{
			name: "editing something else keeps the description", start: "Bakery", script: "e ctrl+u text:4 ctrl+s", about: "Bakery",
			want: observed{
				Flow: Flow{
					Mode: "list", Status: "Updated #6  Bakery  -4.00 EUR → Expenses:Groceries",
					Added: []string{"2026-10-05 Bakery | Assets:Bank -4.00 | Expenses:Groceries 4.00"}, Removed: []string{shop},
				},
				Title: "Bakery", Description: "weekly shop",
			},
		},
		{
			name: "a description can be cleared", start: "Bakery", script: "e tab tab tab tab ctrl+u ctrl+s", about: "Bakery",
			want: observed{Flow: Flow{Mode: "list", Status: "Updated #6  Bakery  -3.20 EUR → Expenses:Groceries"}, Title: "Bakery"},
		},
		{
			name: "extra spaces in a description are tidied", start: "Kino", script: "e tab tab tab tab text:a___b ctrl+s", about: "Kino",
			want: observed{Flow: Flow{Mode: "list", Status: "Updated #4  Kino  -12.00 EUR → Expenses:Uncategorized"}, Title: "Kino", Description: "a b"},
		},
		{
			name: "a long bank purpose is not cut off by editing something else", start: "Sammler", script: "e ctrl+s", about: "Sammler",
			want: observed{Flow: Flow{Mode: "list", Status: "Updated #7  Sammler  -1.00 EUR → Expenses:Uncategorized"}, Title: "Sammler", Description: strings.TrimSpace(long)},
		},
		{
			name: "z puts back the old title and description", start: "Kino", script: "r ctrl+u text:Cinema tab tab tab text:a_note ctrl+s z", about: "Kino",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid edit of #4"}, Title: "Kino"},
		},
		{
			name: "esc leaves it as it was", start: "Kino", script: "r ctrl+u text:Cinema esc", about: "Kino",
			want: observed{Flow: Flow{Mode: "list"}, Title: "Kino"},
		},
		{
			name: "a transfer cannot be renamed here", start: "to savings", script: "r", about: "to savings",
			want: observed{Flow: Flow{Mode: "list", Status: "transfers and split transactions cannot be edited here", StatusIsError: true}, Title: "to savings"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			d := func(date string) time.Time { v, _ := ledger.ParseDate(date); return v }
			for _, n := range []struct{ date, payee, description string }{
				{"2026-10-05", "Bakery", "weekly shop"},
				{"2026-10-06", "Sammler", long},
			} {
				cat, amount := "Expenses:Groceries", int64(320)
				if n.payee == "Sammler" {
					cat, amount = ledger.UncategorizedExpense, 100
				}
				if _, err := st.AddTransaction(t.Context(), core.NewTransaction{
					Date: d(n.date), Payee: n.payee, Description: n.description,
					Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-amount)}, {Account: cat, Amount: eur(amount)}},
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}
			m.applyFilter(false)
			before := storedLines(t, st)
			if tc.start != "" {
				selectPayee(t, m, tc.start)
			}

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), Title: tc.about, Description: descriptionOf(t, st, tc.about)}
			if m.form != nil {
				got.Field = formFieldNames[m.form.field]
			}
			// The long purpose is checked by its length so a failure stays readable.
			if tc.about == "Sammler" {
				got.Added, got.Removed = nil, nil
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Every field of the form can be reached and left again, with Enter, the arrows
// or tab, and the footer says what Enter will do on the field you are on.
func Test_Update_form_navigation(t *testing.T) {
	type observed struct {
		Mode   string
		Field  string
		Footer string
		Saved  bool // the script ended by saving a change
	}
	const (
		next = "enter next · tab/shift+tab fields · ctrl+s save · esc cancel"
		save = "enter save · tab/shift+tab fields · ctrl+s save · esc cancel"
	)
	form := func(field, footer string) observed { return observed{Mode: "form", Field: field, Footer: footer} }
	tcs := []struct {
		name   string
		start  string // the payee to select first; "" selects none
		script string
		want   observed
	}{
		// adding
		{name: "adding starts on the amount", script: "a", want: form("amount", next)},
		{name: "enter moves from the amount to the payee", script: "a text:5 enter", want: form("payee", next)},
		{name: "and from the payee to the category, where Enter saves", script: "a text:5 enter enter", want: form("category", save)},
		{name: "which it does: the quick path", script: "a text:5 enter enter enter", want: observed{Mode: "list", Saved: true}},
		{name: "tab reaches the date", script: "a tab tab tab", want: form("date", next)},
		{name: "Enter on the date moves on to the description instead of saving", script: "a tab tab tab enter", want: form("description", save)},
		{name: "Enter on the description saves", script: "a text:5 tab tab tab enter enter", want: observed{Mode: "list", Saved: true}},
		{name: "the account is the last field", script: "a tab tab tab tab tab", want: form("account", save)},
		// going back and forward with the arrows
		{name: "down moves on from the amount", script: "a down", want: form("payee", next)},
		{name: "up on the amount has nowhere to go", script: "a up", want: form("amount", next)},
		{name: "up goes back from the payee", script: "a down up", want: form("amount", next)},
		{name: "up on a highlighted suggestion first drops it", script: "a text:5 enter text:rew up", want: form("payee", next)},
		{name: "and then goes back", script: "a text:5 enter text:rew up up", want: form("amount", next)},
		{name: "up goes back from the top of the categories", script: "a tab tab up", want: form("payee", next)},
		{name: "down inside the categories stays on them", script: "a tab tab down", want: form("category", save)},
		{name: "up and down on the date", script: "a tab tab tab down", want: form("description", save)},
		{name: "up from the description goes back to the date", script: "a tab tab tab tab up", want: form("date", next)},
		{name: "arrows on the account change the account, not the field", script: "a tab tab tab tab tab down", want: form("account", save)},
		{name: "shift+tab goes back a field", script: "a tab shift+tab", want: form("amount", next)},
		// editing
		{name: "editing starts on the amount", start: "Kino", script: "e", want: form("amount", next)},
		{name: "r starts on the title", start: "Kino", script: "r", want: form("payee", next)},
		{name: "Enter walks to the category, which no longer saves when editing", start: "Kino", script: "e enter enter", want: form("category", next)},
		{name: "on to the date", start: "Kino", script: "e enter enter enter", want: form("date", next)},
		{name: "and to the description, where it saves", start: "Kino", script: "e enter enter enter enter", want: form("description", save)},
		{name: "one more saves the edit", start: "Kino", script: "e enter enter enter enter enter", want: observed{Mode: "list", Saved: true}},
		{name: "up in the categories first moves up the list: the cursor starts on the current one", start: "Kino", script: "e enter enter up", want: form("category", next)},
		{name: "and goes back a field once it is at the top", start: "Kino", script: "e enter enter up up up", want: form("payee", next)},
		{name: "shift+tab does too", start: "Kino", script: "e enter enter shift+tab", want: form("payee", next)},
		{name: "ctrl+s saves from anywhere", start: "Kino", script: "e enter ctrl+s", want: observed{Mode: "list", Saved: true}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			before := storedLines(t, st)
			if tc.start != "" {
				selectPayee(t, m, tc.start)
			}

			// when
			play(m, tc.script)

			// then
			got := observed{Mode: modeNames[m.mode]}
			if m.form != nil {
				got.Field, got.Footer = formFieldNames[m.form.field], m.form.footer()
			}
			added, _ := delta(before, storedLines(t, st))
			got.Saved = len(added) > 0 || strings.HasPrefix(m.status, "Updated")
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The case that started this: reaching the description with Enter alone while
// editing, typing in it, and saving.
func Test_Update_form_reachingTheDescription(t *testing.T) {
	type observed struct {
		Mode        string
		Description string
		Status      string
	}
	tcs := []struct {
		name   string
		start  string
		script string
		about  string
		want   observed
	}{
		{
			name: "editing, with Enter all the way", start: "Kino", script: "e enter enter enter enter text:Seen_it enter", about: "Kino",
			want: observed{"list", "Seen it", "Updated #4  Kino  -12.00 EUR → Expenses:Uncategorized"},
		},
		{
			name: "renaming, then on to the description with Enter", start: "Kino", script: "r ctrl+u text:Cinema enter enter enter text:Date_night enter", about: "Cinema",
			want: observed{"list", "Date night", "Updated #4  Cinema  -12.00 EUR → Expenses:Uncategorized"},
		},
		{
			name: "adding: tab to it, then Enter saves", script: "a text:3 enter text:Kiosk enter tab tab text:Sunday_paper enter", about: "Kiosk",
			want: observed{"list", "Sunday paper", "Added #6  Kiosk  -3.00 EUR → Expenses:Groceries"},
		},
		{
			name: "adding: down from the date", script: "a text:3 enter text:Kiosk enter tab down text:Sunday_paper ctrl+s", about: "Kiosk",
			want: observed{"list", "Sunday paper", "Added #6  Kiosk  -3.00 EUR → Expenses:Groceries"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			if tc.start != "" {
				selectPayee(t, m, tc.start)
			}

			// when
			play(m, tc.script)

			// then
			got := observed{modeNames[m.mode], descriptionOf(t, st, tc.about), m.status}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
