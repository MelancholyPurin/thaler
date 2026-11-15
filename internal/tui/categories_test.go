package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// catLedger is newModel's ledger with a Fun category (and one below it) and the salary
// of cashFlowLedger: spending Groceries (2), Fun (1), Fun:Out (1), income Salary (2).
func catLedger(t *testing.T, st *core.Local) {
	t.Helper()
	cashFlowLedger(t, st)
	book(t, st, "2026-09-20", "Cafe", "Assets:Bank", "Expenses:Fun", -500)
	book(t, st, "2026-09-21", "Cafe", "Assets:Bank", "Expenses:Fun:Out", -700)
}

// catModel is a model on catLedger.
func catModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	m, st := newModel(t)
	catLedger(t, st)
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	return m, st
}

// noCategories is a model on a ledger without categories.
func noCategories(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	m, st := noAccountsModel(t)
	return m, st
}

func Test_categoriesScreen(t *testing.T) {
	tcs := []struct {
		name  string
		model func(t *testing.T) (*Model, *core.Local)
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "K lists the categories, spending first and income marked, with how many transactions",
			model: catModel,
			keys:  "K",
			shows: []string{"Categories", "Fun", "Fun:Out", "Groceries", "2 transactions", "1 transaction", "Salary +", "r rename · m merge", "esc close"},
			hides: []string{"Uncategorized"},
		},
		{
			name:  "r asks for the new name, with the old one in the field",
			model: catModel,
			keys:  "K r",
			shows: []string{"New name", "Expenses:Fun", "Type the whole name", "enter rename · esc back"},
		},
		{
			name:  "m lists where to merge it: the other categories of its kind",
			model: catModel,
			keys:  "K m",
			shows: []string{"Merge Fun into:", "Fun:Out", "Groceries", "enter choose · esc back"},
			hides: []string{"Salary"},
		},
		{
			name:  "enter asks before merging",
			model: catModel,
			keys:  "K m enter",
			shows: []string{"Merge Fun into Fun:Out? Its transaction, schedules, targets", "and carry-over move there, and Fun is removed", "enter merge · esc back"},
		},
		{
			name:  "a category with no other of its kind cannot be merged",
			model: catModel,
			keys:  "K down down down down m",
			shows: []string{"there is no other category of the same kind to merge it into"},
			hides: []string{"Merge"},
		},
		{
			name:  "no categories at all",
			model: noCategories,
			keys:  "K",
			shows: []string{"There are no categories yet"},
			hides: []string{"New name"},
		},
		{
			name:  "esc closes it",
			model: catModel,
			keys:  "K esc",
			hides: []string{"New name", "r rename"},
		},
		{
			name:  "the bottom bar of the Budget says K when there is room",
			model: catModel,
			keys:  "B",
			shows: []string{"K categories"},
		},
		{
			name:  "and the key help lists it",
			model: catModel,
			keys:  "? pgdown pgdown pgdown",
			shows: []string{"Categories: ↑ ↓ choose; r renames the category"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := tc.model(t)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_categories_actions(t *testing.T) {
	type observed struct {
		Mode       string
		Status     string
		Err        string // what the popup says is wrong
		Categories []string
		Edeka      string // the category of the Edeka transaction
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{
			name: "rename: the category, its transactions and the screens follow, and the rules that name it are counted",
			keys: "K down down r ctrl+u raw:Expenses:Dining enter",
			want: observed{
				Mode: "list", Status: "Groceries renamed to Dining · 1 rule in config.toml still names Expenses:Groceries · z undoes",
				Categories: []string{"Expenses:Dining", "Expenses:Fun", "Expenses:Fun:Out", "Income:Salary"}, Edeka: "Expenses:Dining",
			},
		},
		{
			name: "rename: a space after the name is not part of it",
			keys: "K down down r ctrl+u text:Expenses:Dining_ enter",
			want: observed{
				Mode: "list", Status: "Groceries renamed to Dining · 1 rule in config.toml still names Expenses:Groceries · z undoes",
				Categories: []string{"Expenses:Dining", "Expenses:Fun", "Expenses:Fun:Out", "Income:Salary"}, Edeka: "Expenses:Dining",
			},
		},
		{
			name: "rename: the categories below it come along",
			keys: "K r ctrl+u raw:Expenses:Play enter",
			want: observed{
				Mode: "list", Status: "Fun renamed to Play · z undoes",
				Categories: []string{"Expenses:Groceries", "Expenses:Play", "Expenses:Play:Out", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "rename: z takes it back",
			keys: "K down down r ctrl+u raw:Expenses:Dining enter z",
			want: observed{
				Mode: "list", Status: "Dining is called Groceries again",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "rename: a name that is taken is said, and nothing changes",
			keys: "K down down r ctrl+u raw:Expenses:Fun enter",
			want: observed{
				Mode: "categories", Err: "account already exists: Expenses:Fun",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "rename: a category keeps its type",
			keys: "K down down r ctrl+u raw:Income:Dining enter",
			want: observed{
				Mode: "categories", Err: "cannot rename Expenses:Groceries to Income:Dining: a category stays Expenses, so the new name starts with Expenses:",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "rename: an empty name",
			keys: "K r ctrl+u enter",
			want: observed{
				Mode: "categories", Err: "type the new name",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "rename: the name asked for is the one it has, and that is said",
			keys: "K r enter",
			want: observed{
				Mode: "categories", Err: "Expenses:Fun already has that name",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "rename: esc goes back to the list, and a second one closes it",
			keys: "K r esc esc",
			want: observed{
				Mode:       "list",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "merge: everything of the category goes to the other, after asking",
			keys: "K down down m enter enter",
			want: observed{
				Mode: "list", Status: "Groceries merged into Fun · 1 rule in config.toml still names Expenses:Groceries",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Income:Salary"}, Edeka: "Expenses:Fun",
			},
		},
		{
			name: "merge: y does as enter",
			keys: "K down down m down enter y",
			want: observed{
				Mode: "list", Status: "Groceries merged into Fun:Out · 1 rule in config.toml still names Expenses:Groceries",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Income:Salary"}, Edeka: "Expenses:Fun:Out",
			},
		},
		{
			name: "merge: esc at the question goes back to the choice, and again to the list; nothing is done",
			keys: "K down down m enter esc esc",
			want: observed{
				Mode:       "categories",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "merge: n is no",
			keys: "K down down m enter n esc esc esc",
			want: observed{
				Mode:       "list",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "merge: a category with categories below it is refused, and the choice stays",
			keys: "K m down enter enter",
			want: observed{
				Mode: "categories", Err: "Expenses:Fun has categories below it, such as Expenses:Fun:Out: merge or delete those first",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "merge: income with no other income category has nowhere to go",
			keys: "K down down down m",
			want: observed{
				Mode: "categories", Err: "there is no other category of the same kind to merge it into",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Expenses:Groceries", "Income:Salary"}, Edeka: "Expenses:Groceries",
			},
		},
		{
			name: "merge: z does not take it back",
			keys: "K down down m enter enter z",
			want: observed{
				Mode: "list", Status: "nothing to undo",
				Categories: []string{"Expenses:Fun", "Expenses:Fun:Out", "Income:Salary"}, Edeka: "Expenses:Fun",
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := catModel(t)

			// when
			play(m, tc.keys)

			// then
			accs, err := st.ListAccounts(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var cats []string
			for _, a := range accs {
				if (a.Type == ledger.Income || a.Type == ledger.Expenses) && a.Name != ledger.UncategorizedExpense {
					cats = append(cats, a.Name)
				}
			}
			got := observed{Mode: modeNames[m.mode], Status: m.status, Categories: cats, Edeka: txByPayee(t, st, "Edeka").Postings[1].Account}
			if m.catForm != nil {
				got.Err = m.catForm.err
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// What the screens know a category by follows it.
func Test_followCategory(t *testing.T) {
	tcs := []struct {
		name         string
		only, sel    string
		from, to     string
		wantOnly     string
		wantSelected string
	}{
		{name: "the category", only: "Expenses:Fun", sel: "Expenses:Fun", from: "Expenses:Fun", to: "Expenses:Play", wantOnly: "Expenses:Play", wantSelected: "Expenses:Play"},
		{name: "one below it", only: "Expenses:Fun:Out", sel: "Expenses:Fun:Out", from: "Expenses:Fun", to: "Expenses:Play", wantOnly: "Expenses:Play:Out", wantSelected: "Expenses:Play:Out"},
		{name: "another category is left alone", only: "Expenses:Funny", sel: "Expenses:Groceries", from: "Expenses:Fun", to: "Expenses:Play", wantOnly: "Expenses:Funny", wantSelected: "Expenses:Groceries"},
		{name: "none", only: "", sel: "", from: "Expenses:Fun", to: "Expenses:Play", wantOnly: "", wantSelected: ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			m.onlyCategory, m.budgetSel = tc.only, tc.sel

			// when
			m.followCategory(tc.from, tc.to)

			// then
			got := [2]string{m.onlyCategory, m.budgetSel}
			if diff := cmp.Diff([2]string{tc.wantOnly, tc.wantSelected}, got); diff != "" {
				t.Errorf("names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The popup starts on the category the screen is on.
func Test_categories_start(t *testing.T) {
	tcs := []struct {
		name string
		keys string
		sel  string // the category selected on the Budget, if the keys do not choose one
		want int
	}{
		{name: "the first, by default", keys: "K", want: 0},
		{name: "the category of the transaction selected in the Ledger", keys: "3 / text:ACME enter K", want: 3},
		{name: "the category selected on the Budget", keys: "B K", sel: "Expenses:Groceries", want: 2},
		{name: "one that is not there: the first", keys: "B K", sel: "Expenses:Gone", want: 0},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := catModel(t)
			m.budgetSel = tc.sel

			// when
			play(m, tc.keys)

			// then
			if diff := cmp.Diff(tc.want, m.catForm.list.cursor); diff != "" {
				t.Errorf("cursor mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_categoriesScreen_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		keys  string
		shows []string
		hides []string
	}{
		{name: "Spanish: the list", tag: language.Spanish, keys: "K", shows: []string{"Categorías", "2 transacciones", "1 transacción", "r renombrar · m fusionar"}, hides: []string{"transactions"}},
		{name: "Spanish: the question", tag: language.Spanish, keys: "K down down m enter", shows: []string{"Fusionar Groceries con Fun", "z no puede deshacerlo", "enter fusionar · esc volver"}, hides: []string{"Merge"}},
		{name: "German: the list", tag: language.German, keys: "K", shows: []string{"Kategorien", "2 Buchungen", "1 Buchung", "r umbenennen · m zusammenführen"}, hides: []string{"transactions"}},
		{name: "German: the new name", tag: language.German, keys: "K r", shows: []string{"Neuer Name", "enter umbenennen · esc zurück"}, hides: []string{"New name"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-09-10", "x", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-10", "x", "Assets:Bank", "Expenses:Groceries", -4200)
			book(t, st, "2026-10-12", "y", "Assets:Bank", "Expenses:Fun", -500)
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// The Ledger shows the new name at once, not after the next change.
func Test_categories_ledgerFollows(t *testing.T) {
	tcs := []struct {
		name string
		keys string
		want string // the category of the REWE transaction in the list that is not uncategorized
	}{
		{name: "after a rename", keys: "K down down r ctrl+u raw:Expenses:Dining enter", want: "Expenses:Dining"},
		{name: "after a merge", keys: "K down down m enter enter", want: "Expenses:Fun"},
		{name: "and after taking the rename back", keys: "K down down r ctrl+u raw:Expenses:Dining enter z", want: "Expenses:Groceries"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := catModel(t)

			// when
			play(m, tc.keys)

			// then
			got := ""
			for _, i := range m.view {
				if r := m.rows[i]; r.tx.Payee == "REWE" && !r.uncategorized {
					got = r.category.Account
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("category in the list mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Which step the popup is on.
func Test_categories_steps(t *testing.T) {
	const closed = catStep(-1)
	tcs := []struct {
		name string
		keys string
		want catStep
	}{
		{name: "the list", keys: "K", want: catChoose},
		{name: "r: the new name", keys: "K r", want: catRename},
		{name: "enter does the same", keys: "K enter", want: catRename},
		{name: "esc from the new name: the list", keys: "K r esc", want: catChoose},
		{name: "m: where to", keys: "K m", want: catInto},
		{name: "m with nowhere to go stays on the list", keys: "K down down down m", want: catChoose},
		{name: "enter: the question", keys: "K m enter", want: catConfirm},
		{name: "esc at the question: where to", keys: "K m enter esc", want: catInto},
		{name: "n at the question: where to", keys: "K m enter n", want: catInto},
		{name: "esc at where to: the list", keys: "K m esc", want: catChoose},
		{name: "a merge that is refused goes back to where to", keys: "K m down enter enter", want: catInto},
		{name: "a merge that is done closes it", keys: "K down down m enter enter", want: closed},
		{name: "a rename that is refused stays on the new name", keys: "K r ctrl+u raw:Expenses:Groceries enter", want: catRename},
		{name: "esc closes the list", keys: "K esc", want: closed},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := catModel(t)

			// when
			play(m, tc.keys)

			// then
			got := closed
			if m.catForm != nil {
				got = m.catForm.step
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("step mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// What the screens keep a category's name in follows a rename, a merge and taking the
// rename back.
func Test_categories_screensFollow(t *testing.T) {
	tcs := []struct {
		name string
		keys string
		want [2]string // the category the Ledger is narrowed to, and the one selected on the Budget
	}{
		{name: "a rename", keys: "K down down r ctrl+u raw:Expenses:Dining enter", want: [2]string{"Expenses:Dining", "Expenses:Dining"}},
		{name: "a merge", keys: "K down down m enter enter", want: [2]string{"Expenses:Fun", "Expenses:Fun"}},
		{name: "taking a rename back", keys: "K down down r ctrl+u raw:Expenses:Dining enter z", want: [2]string{"Expenses:Groceries", "Expenses:Groceries"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := catModel(t)
			m.onlyCategory, m.budgetSel = "Expenses:Groceries", "Expenses:Groceries"

			// when
			play(m, tc.keys)

			// then
			if diff := cmp.Diff(tc.want, [2]string{m.onlyCategory, m.budgetSel}); diff != "" {
				t.Errorf("names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The rules of config.toml that name the category, or one below it, are counted.
func Test_categories_rulesLeft(t *testing.T) {
	rule := func(category string) importer.Rule { return importer.Rule{Contains: "x", Category: category} }
	tcs := []struct {
		name     string
		rules    []importer.Rule
		noLoader bool
		keys     string
		want     string
	}{
		{name: "no config to read at all", noLoader: true, keys: "K g r ctrl+u raw:Expenses:Play enter", want: "Fun renamed to Play · z undoes"},
		{name: "none", rules: []importer.Rule{rule("Expenses:Groceries")}, keys: "K g r ctrl+u raw:Expenses:Play enter", want: "Fun renamed to Play · z undoes"},
		{name: "one", rules: []importer.Rule{rule("Expenses:Fun")}, keys: "K g r ctrl+u raw:Expenses:Play enter", want: "Fun renamed to Play · 1 rule in config.toml still names Expenses:Fun · z undoes"},
		{name: "one below it counts, one that only starts alike does not", rules: []importer.Rule{rule("Expenses:Fun:Out"), rule("Expenses:Funny"), rule("Expenses:Fun")}, keys: "K g r ctrl+u raw:Expenses:Play enter", want: "Fun renamed to Play · 2 rules in config.toml still name Expenses:Fun · z undoes"},
		{name: "a merge too", rules: []importer.Rule{rule("Expenses:Groceries")}, keys: "K g down down m enter enter", want: "Groceries merged into Fun · 1 rule in config.toml still names Expenses:Groceries"},
		{name: "a config that cannot be read says nothing", rules: nil, keys: "K g r ctrl+u raw:Expenses:Play enter", want: "Fun renamed to Play · z undoes"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR", "Assets:Savings EUR")
			catLedger(t, st)
			book(t, st, "2026-09-15", "Edeka", "Assets:Bank", "Expenses:Groceries", -3000)
			opts := Options{LoadConfig: func() (*config.Config, error) {
				if tc.rules == nil {
					return nil, errors.New("cannot read")
				}
				return &config.Config{Rules: tc.rules}, nil
			}}
			if tc.noLoader {
				opts = Options{}
			}
			m, err := newWithClock(t.Context(), st, opts, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})

			// when
			play(m, tc.keys)

			// then
			if diff := cmp.Diff(tc.want, m.status); diff != "" {
				t.Errorf("status mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A transaction with two lines on one category is one transaction of it.
func Test_categoriesScreen_split(t *testing.T) {
	tcs := []struct {
		name  string
		shows []string
		hides []string
	}{
		{name: "counted once", shows: []string{"Fun", "2 transactions"}, hides: []string{"3 transactions"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := catModel(t)
			if _, err := st.AddTransaction(t.Context(), core.NewTransaction{Date: startOn("2026-10-11"), Payee: "market", Postings: []ledger.Posting{
				{Account: "Assets:Bank", Amount: ledger.Money{Minor: -900, Currency: "EUR"}},
				{Account: "Expenses:Fun", Amount: ledger.Money{Minor: 500, Currency: "EUR"}},
				{Account: "Expenses:Fun", Amount: ledger.Money{Minor: 400, Currency: "EUR"}},
			}}); err != nil {
				t.Fatal(err)
			}
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, "K")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
