package tui

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_payeeWords(t *testing.T) {
	tcs := []struct {
		name  string
		payee string
		want  []string
	}{
		{name: "lower case words", payee: "REWE Markt", want: []string{"rewe", "markt"}},
		{name: "numbers and dates are not part of who is paid", payee: "REWE SAGT DANKE 4711 2026-10-03", want: []string{"rewe"}},
		{name: "legal forms and booking words are dropped", payee: "Kartenzahlung Bäckerei Müller GmbH", want: []string{"bäckerei", "müller"}},
		{name: "punctuation separates words", payee: "AMZN*Mktp.DE/Amazon", want: []string{"amzn", "mktp", "amazon"}},
		{name: "short words go", payee: "DM an der Ecke", want: []string{"der", "ecke"}},
		{name: "a word once", payee: "Kino Kino Royal", want: []string{"kino", "royal"}},
		{name: "nothing but numbers", payee: "4711 / 0815", want: nil},
		{name: "empty", payee: "", want: nil},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			// when
			got := payeeWords(tc.payee)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_similarCategory(t *testing.T) {
	// what has been booked: payee, category, how often
	history := []payeeStat{ // most recently used first
		{name: "REWE Markt Berlin", category: "Expenses:Groceries", count: 9},
		{name: "Deutsche Bahn", category: "Expenses:Travel", count: 3},
		{name: "Deutsche Bank", category: "Expenses:Fees", count: 2},
		{name: "Deutsche Telekom", category: "Expenses:Phone", count: 4},
		{name: "Deutsche Post Berlin", category: "Expenses:Post", count: 1},
		{name: "Cafe Berlin", category: "Expenses:Eating", count: 2},
		{name: "Bäckerei Müller", category: "Expenses:Groceries", count: 5},
		{name: "Müller Drogerie", category: "Expenses:Household", count: 1},
		{name: "Gehalt ACME GmbH", category: "Income:Salary", count: 6},
		{name: "ACME Erstattung", category: "Income:Refunds", count: 1},
		{name: "Kino Royal", category: "", count: 20}, // never categorized: no vote
		{name: "Kino Royal Berlin", category: "Expenses:Fun", count: 1},
	}
	tcs := []struct {
		name     string
		payee    string
		isIncome bool
		want     string
	}{
		{name: "another way of writing the shop", payee: "REWE SAGT DANKE 4711", want: "Expenses:Groceries"},
		{name: "the shop with more words than before", payee: "REWE Markt Berlin Mitte Filiale 12", want: "Expenses:Groceries"},
		{name: "a word many payees share is not enough", payee: "Deutsche Wohnen", want: ""},
		{name: "...but the one few have is", payee: "Bahn Deutsche Reise", want: "Expenses:Travel"},
		{name: "two common words and one that is new are not enough, though most of the words are shared", payee: "Deutsche Berlin Reise", want: ""},
		{name: "a good part of the words, with the one that says most", payee: "REWE Markt Filiale", want: "Expenses:Groceries"},
		{name: "a place is not a shop", payee: "Zoo Berlin", want: ""},
		{name: "the same payee is not a similar one", payee: "Deutsche Bahn", want: ""},
		{name: "the same payee in other letters", payee: "deutsche bahn", want: ""},
		{name: "income looks at income", payee: "Gehalt Oktober ACME", isIncome: true, want: "Income:Salary"},
		{name: "an expense is not suggested for money in", payee: "Müller Bäckerei Filiale", isIncome: true, want: ""},
		{name: "money out does not borrow an income category", payee: "Gehalt ACME Nachzahlung", want: ""},
		{name: "a payee with nothing but numbers", payee: "4711", want: ""},
		{name: "nothing typed", payee: "", want: ""},
		{name: "never categorized: no vote, even with many bookings", payee: "Royal Kino", want: "Expenses:Fun"},
		{name: "two shops with a word in common: the one in more of the name", payee: "Müller Bäckerei", want: "Expenses:Groceries"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := &Model{}
			for _, p := range history {
				p.words = payeeWords(p.name)
				m.payees = append(m.payees, p)
			}
			m.payeeDF = wordCounts(m.payees)

			// when
			got := m.similarCategory(tc.payee, tc.isIncome)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Between two categories that look alike, the one more of its payees were put in wins, and,
// between equals, the newer.
func Test_similarCategory_votes(t *testing.T) {
	tcs := []struct {
		name    string
		history []payeeStat
		want    string
	}{
		{
			name: "more bookings win",
			history: []payeeStat{
				{name: "Amazon Prime", category: "Expenses:Subscriptions", count: 2},
				{name: "Amazon Prime Video", category: "Expenses:Fun", count: 8},
			},
			want: "Expenses:Fun",
		},
		{
			name: "equal votes: the one used more recently",
			history: []payeeStat{
				{name: "Amazon Prime", category: "Expenses:Subscriptions", count: 3},
				{name: "Amazon Prime Video", category: "Expenses:Fun", count: 3},
			},
			want: "Expenses:Subscriptions",
		},
		{
			name: "...whichever way they are listed",
			history: []payeeStat{
				{name: "Amazon Prime Video", category: "Expenses:Fun", count: 3},
				{name: "Amazon Prime", category: "Expenses:Subscriptions", count: 3},
			},
			want: "Expenses:Fun",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := &Model{}
			for _, p := range tc.history {
				p.words = payeeWords(p.name)
				m.payees = append(m.payees, p)
			}
			m.payeeDF = wordCounts(m.payees)

			// when
			got := m.similarCategory("Amazon Prime Video Abo", false)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// booking is a transaction of the past, put in the ledger before the picker opens.
type booking struct {
	payee, category string
	minor           int64
	date            string
}

// In the picker the suggestion comes first, marked, after the category of the very same payee.
func Test_Update_categorize_similar(t *testing.T) {
	type observed struct {
		Flow
		First, Second pickItem
	}
	tcs := []struct {
		name   string
		books  []booking
		payee  string // the uncategorized one, and how much
		minor  int64
		script string
		want   observed
		shows  []string
	}{
		{
			name:  "a shop written another way: its category first, marked",
			books: []booking{{"dm Drogerie Markt Berlin", "Expenses:Shopping", -800, "2026-09-01"}},
			payee: "dm Drogerie Markt 4711", minor: -450, script: "c",
			want:  observed{Flow: Flow{Mode: "pick"}, First: pickItem{name: "Expenses:Shopping", hint: "similar payee"}, Second: pickItem{name: "Expenses:Groceries", hint: "2×"}},
			shows: []string{"Expenses:Shopping", "similar payee"},
		},
		{
			name:  "enter takes it",
			books: []booking{{"dm Drogerie Markt Berlin", "Expenses:Shopping", -800, "2026-09-01"}},
			payee: "dm Drogerie Markt 4711", minor: -450, script: "c enter",
			want: observed{Flow: Flow{
				Mode: "list", Status: "#7 → Expenses:Shopping",
				Added:   []string{"2026-10-20 dm Drogerie Markt 4711 | Assets:Bank -4.50 | Expenses:Shopping 4.50"},
				Removed: []string{"2026-10-20 dm Drogerie Markt 4711 | Assets:Bank -4.50 | Expenses:Uncategorized 4.50"},
			}},
		},
		{
			name:  "the very same payee comes before a look-alike",
			books: []booking{{"dm Drogerie Markt Berlin", "Expenses:Shopping", -800, "2026-09-01"}, {"dm Drogerie Markt 4711", "Expenses:Household", -300, "2026-08-01"}},
			payee: "dm Drogerie Markt 4711", minor: -450, script: "c",
			want: observed{Flow: Flow{Mode: "pick"}, First: pickItem{name: "Expenses:Household", hint: "same payee"}, Second: pickItem{name: "Expenses:Shopping", hint: "similar payee"}},
		},
		{
			name:  "nothing alike: the categories you use most, as before",
			books: []booking{{"Kino Royal Berlin", "Expenses:Fun", -800, "2026-09-01"}},
			payee: "dm Drogerie Markt 4711", minor: -450, script: "c",
			want: observed{Flow: Flow{Mode: "pick"}, First: pickItem{name: "Expenses:Groceries", hint: "2×"}, Second: pickItem{name: "Expenses:Fun", hint: "1×"}},
		},
		{
			name: "words many payees share do not make payees alike",
			books: []booking{
				{"Deutsche Post Berlin", "Expenses:Post", -300, "2026-09-01"}, {"Deutsche Bank", "Expenses:Fees", -300, "2026-09-02"},
				{"Deutsche Bahn", "Expenses:Travel", -300, "2026-09-03"}, {"Cafe Berlin", "Expenses:Eating", -300, "2026-09-04"},
			},
			payee: "Deutsche Berlin Reise", minor: -450, script: "c",
			want: observed{Flow: Flow{Mode: "pick"}, First: pickItem{name: "Expenses:Groceries", hint: "2×"}, Second: pickItem{name: "Expenses:Eating", hint: "1×"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			for _, b := range tc.books {
				book(t, st, b.date, b.payee, "Assets:Bank", b.category, b.minor)
			}
			book(t, st, "2026-10-20", tc.payee, "Assets:Bank", ledger.UncategorizedExpense, tc.minor)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}
			m.applyFilter(false)
			before := storedLines(t, st)
			selectPayee(t, m, tc.payee)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before)}
			if m.mode == modePick {
				got.First, got.Second = m.pickItems[0], m.pickItems[1]
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty, cmp.AllowUnexported(pickItem{})); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

// The mark of the suggestion fits the picker in every language, and is said in it.
func Test_View_similarHint(t *testing.T) {
	want := map[string]string{"en": "similar payee", "es": "destinatario parecido", "de": "ähnlicher Empfänger"}
	for _, l := range i18n.Languages {
		t.Run(l.Code, func(t *testing.T) {
			// given
			m := fitModel(t, l.Tag)
			st, ok := m.svc.(*core.Local)
			if !ok {
				t.Fatal("the model does not run on a local store")
			}
			book(t, st, "2026-09-01", "REWE Markt Berlin", "Assets:Bank", "Expenses:Groceries", -800)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}
			m.applyFilter(false)
			selectPayee(t, m, "REWE")

			// when
			play(m, "c")
			view := m.View()

			// then
			if diff := cmp.Diff([]string(nil), cutIn(view)); diff != "" {
				t.Errorf("cut short or too big (-want +got):\n%s\n\nthe screen:\n%s", diff, view)
			}
			if hint, ok := want[l.Code]; ok && !strings.Contains(view, hint) {
				t.Errorf("the picker lacks %q:\n%s", hint, view)
			}
		})
	}
}
