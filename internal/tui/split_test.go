package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func postingsOf(m *Model, id int64) []string {
	r, ok := m.findRow(id)
	if !ok {
		return nil
	}
	var out []string
	for _, p := range r.tx.Postings {
		out = append(out, m.loc.ShortName(p.Account)+" "+m.loc.Amount(p.Amount))
	}
	return out
}

// splitModel is newModel with an account that was closed, to try the split on.
func splitModel(t *testing.T) *Model {
	t.Helper()
	m, st := newModel(t)
	openAccounts(t, st, "Assets:Old EUR")
	book(t, st, "2026-09-05", "Gift", "Assets:Old", "Income:Gifts", 1000)
	book(t, st, "2026-09-06", "Rent", "Assets:Old", "Expenses:Rent", -1000)
	if _, err := st.CloseAccount(t.Context(), "Assets:Old"); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.applyFilter(false)
	return m
}

// In newModel the transactions are #1 Edeka (September), #2 REWE 10.00 groceries, #3 REWE 20.00,
// #4 Kino 12.00, #5 the transfer to savings; the list shows October newest first, so after "3" the
// cursor is on the transfer, and "j j j" reaches #2. #6 and #7 are on a closed account.
func Test_Update_split(t *testing.T) {
	type observed struct {
		Mode     string
		Status   string
		Err      string
		Pos      int
		Lines    int
		First    string   // the amount of the first line
		Postings []string // of the transaction watched
		Undos    int
	}
	const toRewe = "3 j j j"
	groceries10 := []string{"Bank -10.00", "Groceries 10.00"}
	tcs := []struct {
		name  string
		keys  string
		watch int64
		want  observed
	}{
		{name: "s opens the split on the amount of its first line", keys: toRewe + " s", watch: 2, want: observed{Mode: "split", Pos: splitHead + 1, Lines: 2, First: "10.00", Postings: groceries10}},
		{name: "a transfer cannot be split", keys: "3 s", watch: 5, want: observed{Mode: "list", Status: "only an ordinary transaction with an income or expense category, or one that already is a split, can be split", Postings: []string{"Bank -5.00", "Savings 5.00"}}},
		{name: "esc leaves it as it was", keys: toRewe + " s ctrl+u text:1 esc", watch: 2, want: observed{Mode: "list", Postings: groceries10}},
		{
			name: "the first line made smaller, the other takes the rest", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab ctrl+s", watch: 2,
			want: observed{Mode: "list", Status: "Updated split #2  REWE  -10.00 EUR in 2 categories", Postings: []string{"Bank -10.00", "Groceries 6.00", "Household 4.00"}, Undos: 1},
		},
		{
			name: "undone", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab ctrl+s z", watch: 2,
			want: observed{Mode: "list", Status: "Undid edit of #2", Postings: groceries10},
		},
		{
			name: "all amounts typed", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab text:4 ctrl+s", watch: 2,
			want: observed{Mode: "list", Status: "Updated split #2  REWE  -10.00 EUR in 2 categories", Postings: []string{"Bank -10.00", "Groceries 6.00", "Household 4.00"}, Undos: 1},
		},
		{
			name: "short of the amount", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab text:3 ctrl+s", watch: 2,
			want: observed{Mode: "split", Err: "the lines are 1.00 short of the amount", Pos: splitHead + 3, Lines: 2, Postings: groceries10},
		},
		{
			name: "over the amount", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab text:5 ctrl+s", watch: 2,
			want: observed{Mode: "split", Err: "the lines are 1.00 over the amount", Pos: splitHead + 3, Lines: 2, Postings: groceries10},
		},
		{
			name: "two lines without an amount", keys: toRewe + " s ctrl+u tab text:Expenses:Household tab ctrl+s", watch: 2,
			want: observed{Mode: "split", Err: "only one line can be left without an amount", Pos: splitHead + 3, Lines: 2, Postings: groceries10},
		},
		{
			name: "nothing left for the line without an amount", keys: toRewe + " s tab text:Expenses:Household tab ctrl+s", watch: 2,
			want: observed{Mode: "split", Err: "nothing is left for line 2: the other lines already add up to the amount or more", Pos: splitHead + 3, Lines: 2, Postings: groceries10},
		},
		{
			name: "a line without a category", keys: toRewe + " s ctrl+u text:6 ctrl+s", watch: 2,
			want: observed{Mode: "split", Err: "line 2 has no category", Pos: splitHead + 1, Lines: 2, Postings: groceries10},
		},
		{
			name: "an amount that is not one", keys: toRewe + " s ctrl+u text:six ctrl+s", watch: 2,
			want: observed{Mode: "split", Err: "line 1: invalid amount \"six\"", Pos: splitHead + 1, Lines: 2, Postings: groceries10},
		},
		{
			name: "zero", keys: toRewe + " s ctrl+u text:0 ctrl+s", watch: 2,
			want: observed{Mode: "split", Err: "the amount of line 1 must be greater than zero", Pos: splitHead + 1, Lines: 2, Postings: groceries10},
		},
		{
			name: "ctrl+n adds a line below", keys: toRewe + " s ctrl+n", watch: 2,
			want: observed{Mode: "split", Pos: splitHead + 2, Lines: 3, Postings: groceries10},
		},
		{
			name: "ctrl+x drops the line", keys: toRewe + " s ctrl+n ctrl+x", watch: 2,
			want: observed{Mode: "split", Pos: splitHead + 2, Lines: 2, Postings: groceries10},
		},
		{
			name: "the last line stays", keys: toRewe + " s ctrl+x ctrl+x", watch: 2,
			want: observed{Mode: "split", Pos: splitHead, Lines: 1, Postings: groceries10},
		},
		{
			name: "one line left is an ordinary transaction again", keys: toRewe + " s tab ctrl+x ctrl+s", watch: 2,
			want: observed{Mode: "list", Status: "Updated split #2  REWE  -10.00 EUR in 1 category", Postings: groceries10, Undos: 1},
		},
		{
			name: "a split is edited in the split popup", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab ctrl+s e", watch: 2,
			want: observed{Mode: "split", Pos: splitAmount, Lines: 2, Postings: []string{"Bank -10.00", "Groceries 6.00", "Household 4.00"}, Undos: 1},
		},
		{
			name: "r opens it on the payee", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab ctrl+s r", watch: 2,
			want: observed{Mode: "split", Pos: splitPayee, Lines: 2, Postings: []string{"Bank -10.00", "Groceries 6.00", "Household 4.00"}, Undos: 1},
		},
		{
			name: "a split can be split further", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab ctrl+s s ctrl+n text:Expenses:Gifts tab ctrl+s", watch: 2,
			want: observed{Mode: "split", Err: "nothing is left for line 3: the other lines already add up to the amount or more", Pos: splitHead + 5, Lines: 3, Postings: []string{"Bank -10.00", "Groceries 6.00", "Household 4.00"}, Undos: 1},
		},
		{
			name: "the amount can change, with the lines", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab ctrl+s e ctrl+u text:12 tab tab tab tab tab ctrl+u text:8 ctrl+s", watch: 2,
			want: observed{Mode: "list", Status: "Updated split #2  REWE  -12.00 EUR in 2 categories", Postings: []string{"Bank -12.00", "Groceries 8.00", "Household 4.00"}, Undos: 2},
		},
		{
			name: "from the add form, ctrl+o keeps what was typed", keys: "a text:30 tab text:Aldi ctrl+o", watch: 8,
			want: observed{Mode: "split", Pos: splitHead + 1, Lines: 2, First: "30"},
		},
		{
			name: "from the add form, the sign of an income stays out of the line", keys: "a text:+100 tab text:Boss ctrl+o", watch: 8,
			want: observed{Mode: "split", Pos: splitHead + 1, Lines: 2, First: "100"},
		},
		{
			name: "an amount of zero", keys: "a text:0 tab text:Aldi ctrl+o ctrl+s", watch: 8,
			want: observed{Mode: "split", Err: "amount must be greater than zero", Pos: splitHead + 1, Lines: 2, First: "0"},
		},
		{
			name: "a new split", keys: "a text:30 tab text:Aldi ctrl+o ctrl+u text:12 tab text:Expenses:Household ctrl+s", watch: 8,
			want: observed{Mode: "list", Status: "Added split #8  Aldi  -30.00 EUR in 2 categories", Postings: []string{"Bank -30.00", "Groceries 12.00", "Household 18.00"}, Undos: 1},
		},
		{
			name: "a new split of income", keys: "a text:+100 tab text:Boss ctrl+o ctrl+u text:80 tab text:Income:Bonus ctrl+s", watch: 8,
			want: observed{Mode: "list", Status: "Added split #8  Boss  +100.00 EUR in 2 categories", Postings: []string{"Bank 100.00", "Gifts -80.00", "Bonus -20.00"}, Undos: 1},
		},
		{
			name: "undoing a new split removes it", keys: "a text:30 tab text:Aldi ctrl+o ctrl+u text:12 tab text:Expenses:Household ctrl+s z", watch: 8,
			want: observed{Mode: "list", Status: "Undid adding #8"},
		},
		{
			name: "a transaction on a closed account is not split", keys: "3 [ j s", watch: 7,
			want: observed{Mode: "list", Status: "Assets:Old is closed: reopen it first", Postings: []string{"Old -10.00", "Rent 10.00"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := splitModel(t)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, Postings: postingsOf(m, tc.watch), Undos: len(m.undo)}
			if m.split != nil {
				got.Err, got.Pos, got.Lines, got.First = m.split.err, m.split.pos, len(m.split.lines), m.split.lines[0].amount.Value()
			}
			if tc.want.First == "" {
				got.First = "" // only looked at where a case asks
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_split_screen(t *testing.T) {
	const toRewe = "3 j j j"
	tcs := []struct {
		name  string
		keys  string
		lang  language.Tag
		shows []string
		hides []string
	}{
		{name: "the popup", keys: toRewe + " s", shows: []string{"Split #2", "Account", "Amount", "Payee", "Date", "Description", "Groceries", "10.00", "the rest", "ctrl+n add line · ctrl+x remove · ctrl+s save · esc cancel"}},
		{name: "what is left to give out", keys: toRewe + " s ctrl+u text:6", shows: []string{"the line without an amount gets 4.00"}},
		{name: "balanced", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab text:4", shows: []string{"the lines add up to the amount"}},
		{name: "short", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab text:3", shows: []string{"1.00 left to give out"}},
		{name: "over", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab text:5", shows: []string{"1.00 over the amount"}},
		{name: "the categories are offered", keys: toRewe + " s tab text:Gro", shows: []string{"Expenses:Groceries"}},
		{name: "a hint for the amount", keys: toRewe + " s", shows: []string{"this category's share, without a sign"}},
		{name: "the form says ctrl+o", keys: "a", shows: []string{"ctrl+o split"}},
		{name: "the form's split has a new title", keys: "a text:30 ctrl+o", shows: []string{"New split transaction"}},
		{name: "the key help lists it", keys: "? pgdown", shows: []string{"split the selected transaction between categories"}},
		{name: "the split is marked in the list", keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab ctrl+s", shows: []string{"» REWE"}},
		{name: "Spanish", lang: language.Spanish, keys: toRewe + " s", shows: []string{"Dividir #2", "el resto", "ctrl+n añadir línea"}},
		{name: "Spanish, short", lang: language.Spanish, keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab text:3", shows: []string{"quedan 1,00 por repartir"}},
		{name: "German", lang: language.German, keys: toRewe + " s", shows: []string{"#2 teilen", "der Rest", "ctrl+n Zeile"}},
		{name: "German, over", lang: language.German, keys: toRewe + " s ctrl+u text:6 tab text:Expenses:Household tab text:5", shows: []string{"1,00 zu viel"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := splitModel(t)
			for _, l := range i18n.Languages {
				if tc.lang != language.Und && l.Tag == tc.lang {
					m.setLanguage(l)
				}
			}

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// splitOf is core.TxView.Split, for two lines or more: one is an ordinary transaction.
func Test_splitOf(t *testing.T) {
	pv := func(account string, minor int64) core.PostingView {
		return core.PostingView{Account: account, Amount: ledger.Money{Minor: minor, Currency: "EUR"}}
	}
	tcs := []struct {
		name     string
		postings []core.PostingView
		want     bool
	}{
		{name: "spending in two", postings: []core.PostingView{pv("Assets:Bank", -1000), pv("Expenses:Food", 600), pv("Expenses:Home", 400)}, want: true},
		{name: "income in two", postings: []core.PostingView{pv("Assets:Bank", 1000), pv("Income:Salary", -600), pv("Income:Bonus", -400)}, want: true},
		{name: "the category first", postings: []core.PostingView{pv("Expenses:Food", 600), pv("Assets:Bank", -1000), pv("Expenses:Home", 400)}, want: true},
		{name: "one category is an ordinary transaction", postings: []core.PostingView{pv("Assets:Bank", -1000), pv("Expenses:Food", 1000)}},
		{name: "a line that gives something back", postings: []core.PostingView{pv("Assets:Bank", -1000), pv("Expenses:Food", 1200), pv("Expenses:Home", -200)}},
		{name: "a line of nothing", postings: []core.PostingView{pv("Assets:Bank", -1000), pv("Expenses:Food", 1000), pv("Expenses:Home", 0)}},
		{name: "two of your own accounts", postings: []core.PostingView{pv("Assets:Bank", -1000), pv("Assets:Savings", 600), pv("Expenses:Food", 400)}},
		{name: "a transfer", postings: []core.PostingView{pv("Assets:Bank", -1000), pv("Assets:Savings", 1000)}},
		{name: "equity is not a category to split into", postings: []core.PostingView{pv("Assets:Bank", -1000), pv("Equity:Transfers", 600), pv("Expenses:Food", 400)}},
		{name: "an amount of nothing", postings: []core.PostingView{pv("Assets:Bank", 0), pv("Expenses:Food", 0), pv("Expenses:Home", 0)}},
		{name: "another currency", postings: []core.PostingView{pv("Assets:Bank", -1000), {Account: "Expenses:Food", Amount: ledger.Money{Minor: 600, Currency: "USD"}}, pv("Expenses:Home", 400)}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			tx := core.TxView{Date: "2026-10-04", Postings: tc.postings}

			// when
			_, got := splitOf(tx)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("split mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
