package tui

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
)

// closedModel is newModel (Bank and Savings with money in them) with an empty account, Spare,
// and an old one, Old, that took money and paid it all out again and is closed.
func closedModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	m, st := newModel(t)
	openAccounts(t, st, "Assets:Spare EUR", "Assets:Old EUR")
	book(t, st, "2026-10-05", "Pay", "Assets:Old", "Income:Salary", 5000)
	book(t, st, "2026-10-06", "Shop", "Assets:Old", "Expenses:Groceries", -5000)
	if _, err := st.CloseAccount(t.Context(), "Assets:Old"); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.applyFilter(false)
	m.undo = nil
	return m, st
}

// storedAccounts are the accounts of your own the service has, with " closed" after a closed one.
func storedAccounts(t *testing.T, st *core.Local) []string {
	t.Helper()
	accs, err := st.ListAccounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range accs {
		if !a.Type.NeedsCurrency() {
			continue
		}
		name := a.Name
		if a.Closed {
			name += " closed"
		}
		out = append(out, name)
	}
	return out
}

func Test_Update_accountEdit(t *testing.T) {
	type observed struct {
		Mode     string
		Status   string
		Err      string   // what the rename popup says is wrong
		Typed    string   // what is in its field
		Cursor   int      // in the Accounts panel
		Panel    []string // the lines of the Accounts panel after "All accounts"
		Accounts []string // what the service has
		Undo     int
	}
	open := []string{"Assets:Bank", "Assets:Old closed", "Assets:Savings", "Assets:Spare"}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{
			name: "the closed accounts are not in the panel",
			keys: "2",
			want: observed{Mode: "list", Cursor: 0, Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "v shows them after the others",
			keys: "2 v",
			want: observed{Mode: "list", Status: "Closed accounts shown (v hides them)", Panel: []string{"Bank", "Savings", "Spare", "Old"}, Accounts: open},
		},
		{
			name: "...and hides them again",
			keys: "2 v v",
			want: observed{Mode: "list", Status: "Closed accounts hidden", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "hiding them takes the cursor off a closed account",
			keys: "2 v down down down down v",
			want: observed{Mode: "list", Status: "Closed accounts hidden", Cursor: 3, Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "r asks for the new name, starting from the old one",
			keys: "2 down r",
			want: observed{Cursor: 1, Mode: "rename", Typed: "Bank", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "r on All accounts asks for an account first",
			keys: "2 r",
			want: observed{Mode: "list", Status: "select an account first: press 2, then ↑/↓", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "enter renames it, the cursor stays on it, and z can take it back",
			keys: "2 down down r ctrl+u text:Sparkasse enter",
			want: observed{
				Mode: "list", Status: "Renamed Savings to Sparkasse · z undoes", Cursor: 3,
				Panel:    []string{"Bank", "Spare", "Sparkasse"},
				Accounts: []string{"Assets:Bank", "Assets:Old closed", "Assets:Spare", "Assets:Sparkasse"}, Undo: 1,
			},
		},
		{
			name: "z puts the name back",
			keys: "2 down down r ctrl+u text:Sparkasse enter z",
			want: observed{Mode: "list", Status: "Undid renaming Savings to Sparkasse", Cursor: 2, Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "what is typed with its type is taken as it is",
			keys: "2 down r ctrl+u text:Assets:Sparkasse enter",
			want: observed{Mode: "list", Status: "Renamed Bank to Sparkasse · z undoes", Cursor: 3, Panel: []string{"Savings", "Spare", "Sparkasse"}, Accounts: []string{"Assets:Old closed", "Assets:Savings", "Assets:Spare", "Assets:Sparkasse"}, Undo: 1},
		},
		{
			name: "the same name changes nothing and asks nothing",
			keys: "2 down r enter",
			want: observed{Cursor: 1, Mode: "list", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "a name that is taken is said in the popup",
			keys: "2 down r ctrl+u text:Savings enter",
			want: observed{Cursor: 1, Mode: "rename", Err: "account already exists: Assets:Savings", Typed: "Savings", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "so is another type",
			keys: "2 down r ctrl+u text:Expenses:Bank enter",
			want: observed{Cursor: 1, Mode: "rename", Err: "cannot rename Assets:Bank to Expenses:Bank: an account stays Assets, so the new name starts with Assets:", Typed: "Expenses:Bank", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "so is a name that is not one",
			keys: "2 down r ctrl+u raw:My,Bank enter",
			want: observed{Cursor: 1, Mode: "rename", Err: `account "Assets:My,Bank": invalid character "," (use letters, digits, '-' and '_')`, Typed: "My,Bank", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "typing clears what was said",
			keys: "2 down r ctrl+u text:Savings enter x",
			want: observed{Cursor: 1, Mode: "rename", Typed: "Savingsx", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "esc leaves it as it was",
			keys: "2 down r ctrl+u text:Sparkasse esc",
			want: observed{Cursor: 1, Mode: "list", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "a closed account can be renamed",
			keys: "2 v down down down down r ctrl+u text:Archive enter",
			want: observed{
				Mode: "list", Status: "Renamed Old to Archive · z undoes", Cursor: 4,
				Panel:    []string{"Bank", "Savings", "Spare", "Archive"},
				Accounts: []string{"Assets:Archive closed", "Assets:Bank", "Assets:Savings", "Assets:Spare"}, Undo: 1,
			},
		},
		{
			name: "c closes an account that holds nothing, and says how to see it",
			keys: "2 down down down c",
			want: observed{
				Mode: "list", Status: "Closed Spare · z undoes · v shows closed accounts", Cursor: 2,
				Panel:    []string{"Bank", "Savings"},
				Accounts: []string{"Assets:Bank", "Assets:Old closed", "Assets:Savings", "Assets:Spare closed"}, Undo: 1,
			},
		},
		{
			name: "...and z reopens it",
			keys: "2 down down down c z",
			want: observed{Mode: "list", Status: "Undid closing Spare", Cursor: 3, Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "closing one that is shown does not hide it",
			keys: "2 v down down down c",
			want: observed{
				Mode: "list", Status: "Closed Spare · z undoes", Cursor: 4,
				Panel:    []string{"Bank", "Savings", "Old", "Spare"},
				Accounts: []string{"Assets:Bank", "Assets:Old closed", "Assets:Savings", "Assets:Spare closed"}, Undo: 1,
			},
		},
		{
			name: "an account with money in it is refused, with the amount",
			keys: "2 down c",
			want: observed{Cursor: 1, Mode: "list", Status: "Assets:Bank still holds -77.00 EUR: move it out before closing the account", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "c on a closed account reopens it",
			keys: "2 v down down down down c",
			want: observed{
				Mode: "list", Status: "Reopened Old · z undoes", Cursor: 2,
				Panel:    []string{"Bank", "Old", "Savings", "Spare"},
				Accounts: []string{"Assets:Bank", "Assets:Old", "Assets:Savings", "Assets:Spare"}, Undo: 1,
			},
		},
		{
			name: "...and z closes it again",
			keys: "2 v down down down down c z",
			want: observed{Mode: "list", Status: "Undid reopening Old", Cursor: 4, Panel: []string{"Bank", "Savings", "Spare", "Old"}, Accounts: open},
		},
		{
			name: "c on All accounts asks for an account first",
			keys: "2 c",
			want: observed{Mode: "list", Status: "select an account first: press 2, then ↑/↓", Panel: []string{"Bank", "Savings", "Spare"}, Accounts: open},
		},
		{
			name: "the balance of a closed account is not set",
			keys: "2 v down down down down b",
			want: observed{Mode: "list", Status: "Assets:Old is closed: reopen it first", Cursor: 4, Panel: []string{"Bank", "Savings", "Spare", "Old"}, Accounts: open},
		},
		{
			name: "a transaction of a closed account is not edited",
			keys: "2 v down down down down 3 e",
			want: observed{Mode: "list", Status: "Assets:Old is closed: reopen it first", Cursor: 4, Panel: []string{"Bank", "Savings", "Spare", "Old"}, Accounts: open},
		},
		{
			name: "...nor renamed",
			keys: "2 v down down down down 3 r",
			want: observed{Mode: "list", Status: "Assets:Old is closed: reopen it first", Cursor: 4, Panel: []string{"Bank", "Savings", "Spare", "Old"}, Accounts: open},
		},
		{
			name: "...but its category can be changed",
			keys: "2 v down down down down 3 c",
			want: observed{Mode: "pick", Cursor: 4, Panel: []string{"Bank", "Savings", "Spare", "Old"}, Accounts: open},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := closedModel(t)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, Cursor: m.accts.cursor, Accounts: storedAccounts(t, st), Undo: len(m.undo)}
			for i := 1; i < m.panelLen(); i++ {
				e, _ := m.panelEntry(i)
				got.Panel = append(got.Panel, strings.TrimPrefix(e.account.Name, "Assets:"))
			}
			if m.rename != nil {
				got.Err, got.Typed = m.rename.err, m.rename.name.Value()
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// What is on the screen: a closed account dimmed with its tag, its detail, and the
// transactions of the account, which are its history.
func Test_accountEdit_screen(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{name: "a closed account is not in the panel", keys: "2", hides: []string{"Old", "closed"}},
		{name: "shown, it says it is closed", keys: "2 v down down down down", shows: []string{"Old", "closed"}},
		{name: "its detail says what that means", keys: "2 v down down down down", shows: []string{"Closed: it keeps its history and takes no new money", "Assets:Old"}},
		{name: "its transactions are its history", keys: "2 v down down down down", shows: []string{"Transactions · Old", "Pay", "Shop"}},
		{name: "the rename popup", keys: "2 down r", shows: []string{"Rename account", "Now: Assets:Bank", "The type stays: type only what follows Assets:", "enter rename · esc cancel"}},
		{name: "the key help lists them", keys: "? pgdown", shows: []string{"r  c  v", "in the Accounts panel: rename the account"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := closedModel(t)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// The forms offer the accounts that are open, and the closed one the Ledger is scoped to is not
// the one a new transaction goes to.
func Test_accountEdit_formsKeepOpen(t *testing.T) {
	tcs := []struct {
		name string
		keys string
	}{
		{name: "the add form", keys: "2 v down down down down a"},
		{name: "the transfer form", keys: "2 v down down down down x"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := closedModel(t)

			// when
			play(m, tc.keys)

			// then
			var offered []string
			for _, e := range m.accounts {
				offered = append(offered, e.account.Name)
			}
			if diff := cmp.Diff([]string{"Assets:Bank", "Assets:Savings", "Assets:Spare"}, offered); diff != "" {
				t.Errorf("the accounts the forms choose from (-want +got):\n%s", diff)
			}
			if m.mode != modeForm && m.mode != modeTransfer {
				t.Fatalf("no form opened: mode %s, status %q", modeNames[m.mode], m.status)
			}
			if m.mode == modeForm {
				if got := m.accounts[m.form.account].account.Name; got == "Assets:Old" {
					t.Errorf("the add form is on the closed account")
				}
			}
		})
	}
}

// The popups and statuses of renaming and closing fit 80 by 24 in every language.
func Test_View_accountEdit(t *testing.T) {
	want := map[string][]string{
		"en": {"Rename account", "Now: Assets:Old"},
		"es": {"Renombrar cuenta", "Ahora: Activos:Old"},
		"de": {"Konto umbenennen", "Jetzt: Vermögen:Old"},
	}
	for _, l := range i18n.Languages {
		for _, tc := range []struct{ name, keys string }{
			{"rename", "2 v down down down down r"},
			{"rename, an error", "2 v down down down down r ctrl+u text:Bank enter"},
			{"renamed", "2 v down down down down r ctrl+u text:Archive enter"},
			{"closed shown", "2 v"},
			{"closed shown, selected", "2 v down down down down"},
			{"closed, hidden again", "2 v v"},
			{"closed", "2 down down down c"},
			{"reopened", "2 v down down down down c"},
			{"closed and undone", "2 down down down c z"},
			{"nothing closed", "2 v v"},
			{"refused", "2 down c"},
			{"balance refused", "2 v down down down down b"},
		} {
			t.Run(l.Code+", "+tc.name, func(t *testing.T) {
				// given
				m := fitModel(t, l.Tag)
				st, ok := m.svc.(*core.Local)
				if !ok {
					t.Fatal("the model does not run on a local store")
				}
				openAccounts(t, st, "Assets:Old EUR", "Assets:Spare EUR")
				book(t, st, "2026-10-05", "Pay", "Assets:Old", "Income:Salary", 5000)
				book(t, st, "2026-10-06", "Shop", "Assets:Old", "Expenses:Groceries", -5000)
				if _, err := st.CloseAccount(t.Context(), "Assets:Old"); err != nil {
					t.Fatal(err)
				}
				if err := m.reload(); err != nil {
					t.Fatal(err)
				}
				m.applyFilter(false)

				// when
				play(m, tc.keys)
				view := m.View()

				// then
				if diff := cmp.Diff([]string(nil), cutIn(view)); diff != "" {
					t.Errorf("cut short or too big (-want +got):\n%s\n\nthe screen:\n%s", diff, view)
				}
				if tc.name == "rename" {
					checkView(t, view, want[l.Code], nil)
				}
			})
		}
	}
}
