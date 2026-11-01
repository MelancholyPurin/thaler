package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
)

// inLanguage is a model on the store, writing amounts and dates in that language.
func inLanguage(t *testing.T, st *core.Local, tag language.Tag) *Model {
	t.Helper()
	m, err := newWithClock(t.Context(), st, Options{LoadConfig: testConfig, Loc: i18n.New(tag)}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	return m
}

// A language writes the dates and amounts it shows its own way; English keeps ISO
// dates and gains thousands separators.
func Test_View_language(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		shows []string
		hides []string
	}{
		{name: "English", tag: language.English, shows: []string{"2026-10-01", "-1,234.56"}, hides: []string{"01.10.2026", "01/10/2026"}},
		{name: "German", tag: language.German, shows: []string{"01.10.2026", "-1.234,56"}, hides: []string{"2026-10-01", "-1,234.56"}},
		{name: "Spanish", tag: language.Spanish, shows: []string{"01/10/2026", "-1.234,56"}, hides: []string{"2026-10-01", "01.10.2026"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -123456)
			m := inLanguage(t, st, tc.tag)

			// when
			view := m.View()

			// then
			checkView(t, view, tc.shows, tc.hides)
		})
	}
}

// The form opens on today in the language's format and reads what is typed in it.
func Test_Update_addForm_language(t *testing.T) {
	tcs := []struct {
		name      string
		tag       language.Tag
		script    string
		shows     string // on the form, before it is saved
		wantAdded string
	}{
		{name: "German date and amount", tag: language.German, script: "a text:5,50 tab tab tab ctrl+u text:03.10.2026 ctrl+s", shows: "22.10.2026", wantAdded: "2026-10-03  | Assets:Bank -5.50 | Expenses:Groceries 5.50"},
		{name: "Spanish date", tag: language.Spanish, script: "a text:5,50 tab tab tab ctrl+u text:3/10/2026 ctrl+s", shows: "22/10/2026", wantAdded: "2026-10-03  | Assets:Bank -5.50 | Expenses:Groceries 5.50"},
		{name: "English date", tag: language.English, script: "a text:5.50 tab tab tab ctrl+u text:2026-10-03 ctrl+s", shows: "2026-10-22", wantAdded: "2026-10-03  | Assets:Bank -5.50 | Expenses:Groceries 5.50"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
			m := inLanguage(t, st, tc.tag)
			before := storedLines(t, st)

			// when
			play(m, "a")
			opened := m.View()
			play(m, "esc "+tc.script)

			// then
			checkView(t, opened, []string{tc.shows}, nil)
			added, _ := delta(before, storedLines(t, st))
			if diff := cmp.Diff([]string{tc.wantAdded}, added); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The search finds a date as it is written on screen, and as it is stored.
func Test_Update_filter_language(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		typed string
		shows []string
		hides []string
	}{
		{name: "German date", tag: language.German, typed: "01.10", shows: []string{"REWE"}, hides: []string{"Edeka"}},
		{name: "the stored form still works", tag: language.German, typed: "2026-10-01", shows: []string{"REWE"}, hides: []string{"Edeka"}},
		{name: "Spanish date", tag: language.Spanish, typed: "15/09", shows: []string{"Edeka"}, hides: []string{"REWE"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-09-15", "Edeka", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, "T /") // all time, then the search box
			typeText(m, tc.typed)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
