package tui

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// filterModel is newModel with a salary and an old, large purchase in other months and a
// payment in dollars, so that there is something to filter by size, date and category.
func filterModel(t *testing.T) *Model {
	t.Helper()
	m, st := newModel(t)
	openAccounts(t, st, "Assets:Dollars USD")
	book(t, st, "2026-10-05", "Gehalt", "Assets:Bank", "Income:Salary", 250000)
	book(t, st, "2026-09-20", "Amazon", "Assets:Bank", "Expenses:Shopping", -8000)
	if _, err := st.AddTransaction(t.Context(), core.NewTransaction{
		Date: dayOf(t, "2026-10-06"), Payee: "Hotel",
		Postings: []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(-1500)}, {Account: "Expenses:Travel", Amount: usd(1500)}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.applyFilter(false)
	return m
}

func dayOf(t *testing.T, s string) (d time.Time) {
	t.Helper()
	d, err := ledger.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// shown are the transactions the list has, in order: the date, the payee and the amount on
// your account.
func shown(m *Model) []string {
	var out []string
	for _, i := range m.view {
		r := m.rows[i]
		out = append(out, r.tx.Date+" "+r.tx.Payee+" "+m.loc.Amount(r.source.Amount))
	}
	return out
}

func Test_Update_filter(t *testing.T) {
	type observed struct {
		Mode   string
		Status string
		Err    string
		Shown  []string
	}
	const (
		kino    = "2026-10-03 Kino -12.00"
		rewe2   = "2026-10-02 REWE -20.00"
		rewe1   = "2026-10-01 REWE -10.00"
		savings = "2026-10-04 to savings -5.00"
		salary  = "2026-10-05 Gehalt 2,500.00"
		hotel   = "2026-10-06 Hotel -15.00"
		amazon  = "2026-09-20 Amazon -80.00"
		edeka   = "2026-09-15 Edeka -30.00"
	)
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "the month on show, newest first, as it always was", keys: "", want: observed{Mode: "list", Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "f opens the popup", keys: "f", want: observed{Mode: "filter", Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "a smallest size", keys: "f text:15 enter", want: observed{Mode: "list", Status: "3 transactions match", Shown: []string{hotel, salary, rewe2}}},
		{name: "a largest size", keys: "f tab text:12 enter", want: observed{Mode: "list", Status: "3 transactions match", Shown: []string{savings, kino, rewe1}}},
		{name: "both, the ends are in", keys: "f text:10 tab text:15 enter", want: observed{Mode: "list", Status: "3 transactions match", Shown: []string{hotel, kino, rewe1}}},
		{name: "the sign is not looked at", keys: "f text:+2000 enter", want: observed{Mode: "list", Status: "1 transaction matches", Shown: []string{salary}}},
		{name: "a minus sign is not looked at either, in the minimum", keys: "f text:-15 enter", want: observed{Mode: "list", Status: "3 transactions match", Shown: []string{hotel, salary, rewe2}}},
		{name: "...nor in the maximum", keys: "f tab text:-12 enter", want: observed{Mode: "list", Status: "3 transactions match", Shown: []string{savings, kino, rewe1}}},
		{name: "the minimum above the maximum is seen through a sign", keys: "f text:-20 tab text:10 enter", want: observed{Mode: "filter", Err: "the minimum amount is more than the maximum", Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "a category filter looks at categories, not at your accounts", keys: "f tab tab tab tab text:bank enter", want: observed{Mode: "list", Status: "0 transactions match", Shown: nil}},
		{name: "a decimal comma is a comma in the language, a point here", keys: "f text:12.00 tab text:12.00 enter", want: observed{Mode: "list", Status: "1 transaction matches", Shown: []string{kino}}},
		{name: "a range of dates takes the place of the month", keys: "f tab tab text:2026-09-01 tab text:2026-09-30 enter", want: observed{Mode: "list", Status: "2 transactions match", Shown: []string{amazon, edeka}}},
		{name: "from a date on", keys: "f tab tab text:2026-10-04 enter", want: observed{Mode: "list", Status: "3 transactions match", Shown: []string{hotel, salary, savings}}},
		{name: "until a date", keys: "f tab tab tab text:2026-09-16 enter", want: observed{Mode: "list", Status: "1 transaction matches", Shown: []string{edeka}}},
		{name: "a date in the form the language writes it, and the ends are in", keys: "f tab tab text:2026-10-02 tab text:2026-10-03 enter", want: observed{Mode: "list", Status: "2 transactions match", Shown: []string{kino, rewe2}}},
		{name: "a category is part of its name, in the month on show", keys: "f tab tab tab tab text:groc enter", want: observed{Mode: "list", Status: "1 transaction matches", Shown: []string{rewe1}}},
		{name: "...and with all time, the others", keys: "T f tab tab tab tab text:groc enter", want: observed{Mode: "list", Status: "2 transactions match", Shown: []string{rewe1, edeka}}},
		{name: "size, in each currency of its own", keys: "f text:15 tab text:15 enter", want: observed{Mode: "list", Status: "1 transaction matches", Shown: []string{hotel}}},
		{name: "with the uncategorized toggle", keys: "u f text:15 enter", want: observed{Mode: "list", Status: "1 transaction matches", Shown: []string{rewe2}}},
		{name: "largest first", keys: "f tab tab tab tab tab right right enter", want: observed{Mode: "list", Status: "Sorted: Amount, largest first", Shown: []string{salary, rewe2, hotel, kino, rewe1, savings}}},
		{name: "smallest first", keys: "f tab tab tab tab tab right right right enter", want: observed{Mode: "list", Status: "Sorted: Amount, smallest first", Shown: []string{savings, rewe1, kino, hotel, rewe2, salary}}},
		{name: "oldest first", keys: "f tab tab tab tab tab right enter", want: observed{Mode: "list", Status: "Sorted: Date, oldest first", Shown: []string{rewe1, rewe2, kino, savings, salary, hotel}}},
		{name: "by payee", keys: "f tab tab tab tab tab right right right right enter", want: observed{Mode: "list", Status: "Sorted: Payee, A to Z", Shown: []string{salary, hotel, kino, rewe2, rewe1, savings}}},
		{name: "by category", keys: "f tab tab tab tab tab right right right right right enter", want: observed{Mode: "list", Status: "Sorted: Category, A to Z", Shown: []string{savings, rewe1, hotel, kino, rewe2, salary}}},
		{name: "left goes back, round the end", keys: "f tab tab tab tab tab left enter", want: observed{Mode: "list", Status: "Sorted: Category, A to Z", Shown: []string{savings, rewe1, hotel, kino, rewe2, salary}}},
		{name: "a filter and an order together", keys: "f text:15 tab tab tab tab tab right right enter", want: observed{Mode: "list", Status: "3 transactions match", Shown: []string{salary, rewe2, hotel}}},
		{name: "nothing matches", keys: "f text:99999 enter", want: observed{Mode: "list", Status: "0 transactions match", Shown: nil}},
		{name: "a size that is not a number is said in the popup", keys: "f text:lots enter", want: observed{Mode: "filter", Err: `amount: invalid amount "lots"`, Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "a minimum above the maximum", keys: "f text:20 tab text:10 enter", want: observed{Mode: "filter", Err: "the minimum amount is more than the maximum", Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "a date that is not one", keys: "f tab tab text:soon enter", want: observed{Mode: "filter", Err: `date: invalid date "soon": use YYYY-MM-DD or DD.MM.YYYY`, Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "dates the wrong way round", keys: "f tab tab text:2026-10-05 tab text:2026-10-01 enter", want: observed{Mode: "filter", Err: "the first date is after the last", Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "typing takes the message away", keys: "f text:lots enter backspace", want: observed{Mode: "filter", Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "esc leaves it as it was", keys: "f text:15 esc", want: observed{Mode: "list", Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "ctrl+x clears the filter, and keeps the order", keys: "f text:15 tab tab tab tab tab right right enter f ctrl+x", want: observed{Mode: "list", Status: "Filter cleared", Shown: []string{salary, rewe2, hotel, kino, rewe1, savings}}},
		{name: "esc in the list clears the filter, and keeps the order", keys: "f text:15 tab tab tab tab tab right right enter esc", want: observed{Mode: "list", Shown: []string{salary, rewe2, hotel, kino, rewe1, savings}}},
		{name: "emptying the fields clears it", keys: "f text:15 enter f ctrl+u enter", want: observed{Mode: "list", Status: "Filter cleared", Shown: []string{hotel, salary, savings, kino, rewe2, rewe1}}},
		{name: "the same again changes nothing", keys: "f text:15 enter f enter", want: observed{Mode: "list", Shown: []string{hotel, salary, rewe2}}},
		{name: "the popup shows what is set", keys: "f text:15 enter f", want: observed{Mode: "filter", Shown: []string{hotel, salary, rewe2}}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := filterModel(t)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, Shown: shown(m)}
			if m.filt != nil {
				got.Err = m.filt.err
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_filter_screen(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		lang  language.Tag
		shows []string
		hides []string
	}{
		{name: "the popup", keys: "f", shows: []string{"Filter and sort", "Min amount", "Max amount", "From date", "To date", "Category", "Sort by", "Date, newest first", "tab field · enter apply · ctrl+x clear · esc cancel"}},
		{name: "the order is chosen with the arrows", keys: "f tab tab tab tab tab", shows: []string{"‹ Date, newest first ›"}},
		{name: "the popup shows what is set", keys: "f text:15 enter f", shows: []string{"15"}},
		{name: "the list says it is filtered", keys: "f text:15 enter", shows: []string{"Transactions · filtered", "3 transactions match"}},
		{name: "and sorted", keys: "f tab tab tab tab tab right right enter", shows: []string{"Transactions · sorted"}},
		{name: "dates are in the bar at the top, instead of the month", keys: "f tab tab text:2026-09-01 tab text:2026-09-30 enter", shows: []string{"[ 2026-09-01 – 2026-09-30 ]"}, hides: []string{"[ Oct 2026 ]"}},
		{name: "from a date on", keys: "f tab tab text:2026-10-04 enter", shows: []string{"[ from 2026-10-04 ]"}},
		{name: "until a date", keys: "f tab tab tab text:2026-09-16 enter", shows: []string{"[ until 2026-09-16 ]"}},
		{name: "the month is back when the filter goes", keys: "f tab tab text:2026-10-04 enter esc", shows: []string{"[ Oct 2026 ]"}, hides: []string{"from"}},
		{name: "the other screens keep the month", keys: "f tab tab text:2026-10-04 enter B", shows: []string{"[ Oct 2026 ]"}},
		{name: "the key help lists it", keys: "? pgdown pgdown", shows: []string{"filter the transactions by amount, dates and category"}},
		{name: "Spanish", lang: language.Spanish, keys: "f", shows: []string{"Filtrar y ordenar", "Importe mín.", "Fecha desde", "Orden", "Fecha, la más reciente primero", "tab campo · enter aplicar · ctrl+x borrar · esc volver"}, hides: []string{"Filter and sort"}},
		{name: "German", lang: language.German, keys: "f", shows: []string{"Filtern und sortieren", "Betrag ab", "Datum bis", "Sortierung", "Datum, neueste zuerst"}, hides: []string{"Filter and sort"}},
		{name: "Spanish, filtered", lang: language.Spanish, keys: "f text:15 enter", shows: []string{"3 transacciones coinciden", "filtrado"}},
		{name: "German, one match", lang: language.German, keys: "f text:2000 enter", shows: []string{"1 Buchung passt", "gefiltert"}},
		{name: "German, dates", lang: language.German, keys: "f tab tab text:04.10.2026 enter", shows: []string{"[ ab 04.10.2026 ]"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := filterModel(t)
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

// The payees are put in order as a person would, whatever the case of the first letter.
func Test_filter_sortByPayee(t *testing.T) {
	tcs := []struct {
		name string
		want []string
	}{{name: "apple before Banana", want: []string{"apple", "Banana"}}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			book(t, st, "2026-10-07", "Banana", "Assets:Bank", "Expenses:Groceries", -300)
			book(t, st, "2026-10-08", "apple", "Assets:Bank", "Expenses:Groceries", -300)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}
			m.applyFilter(false)

			// when
			play(m, "f text:2 tab tab tab tab tab right right right right enter") // 2.00 and up: leaves the two fruits and the others

			// then
			var got []string
			for _, i := range m.view {
				if p := m.rows[i].tx.Payee; p == "apple" || p == "Banana" {
					got = append(got, p)
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("order mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
