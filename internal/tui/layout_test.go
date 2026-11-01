package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"
)

// A text too wide for a line goes on to the next one, at the spaces.
func Test_wrapped(t *testing.T) {
	tcs := []struct {
		name string
		text string
		w    int
		want []string
	}{
		{name: "fits", text: "one two", w: 20, want: []string{"one two"}},
		{name: "wraps at a space", text: "one two three", w: 8, want: []string{"one two", "three"}},
		{name: "a word wider than the line is broken", text: "abcdefghij", w: 4, want: []string{"abcd", "efgh", "ij"}},
		{name: "wide characters count as two cells", text: "日本語 日本語", w: 6, want: []string{"日本語", "日本語"}},
		{name: "nothing", text: "", w: 10, want: []string{""}},
		{name: "no width, no lines", text: "one", w: 0, want: nil},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.text and tc.w

			// when
			got := wrapped(tc.text, tc.w, lipgloss.NewStyle())

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Parts go side by side while they fit, and a part is never split.
func Test_packed(t *testing.T) {
	sep := dim.Render(" · ")
	tcs := []struct {
		name  string
		parts []string
		w     int
		want  []string
	}{
		{name: "all on one line", parts: []string{"a", "b", "c"}, w: 20, want: []string{"a" + sep + "b" + sep + "c"}},
		{name: "the line is full", parts: []string{"aaaa", "bbbb", "cccc"}, w: 11, want: []string{"aaaa" + sep + "bbbb", "cccc"}},
		{name: "a part wider than the line stays whole", parts: []string{"aaaaaaaaaa", "b"}, w: 5, want: []string{"aaaaaaaaaa", "b"}},
		{name: "nothing", parts: nil, w: 10, want: nil},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.parts and tc.w

			// when
			got := packed(tc.parts, tc.w)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A status keeps the parts that fit, in order; the first is always there.
func Test_Model_statusLine(t *testing.T) {
	tcs := []struct {
		name  string
		width int
		parts []string
		want  string
	}{
		{name: "all fit", width: 30, parts: []string{"Imported 3", "2 new", "z undoes"}, want: "Imported 3 · 2 new · z undoes"},
		{name: "the last part gives way first", width: 20, parts: []string{"Imported 3", "2 new", "z undoes"}, want: "Imported 3 · 2 new"},
		{name: "then the next", width: 12, parts: []string{"Imported 3", "2 new", "z undoes"}, want: "Imported 3"},
		{name: "the first stays, even too wide", width: 5, parts: []string{"Imported 3", "2 new"}, want: "Imported 3"},
		{name: "one part", width: 30, parts: []string{"Done"}, want: "Done"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := &Model{width: tc.width}

			// when
			got := m.statusLine(tc.parts)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A label and a value share a line when they fit, and otherwise the value goes under
// the label instead of the label being cut short.
func Test_leftRightLines(t *testing.T) {
	tcs := []struct {
		name  string
		label string
		value string
		w     int
		want  []string
	}{
		{name: "side by side", label: "Rent", value: "-5.00", w: 12, want: []string{"Rent   -5.00"}},
		{name: "too wide: two lines", label: "Expenses:Uncategorized", value: "+10.00 EUR", w: 30, want: []string{"Expenses:Uncategorized        ", "                    +10.00 EUR"}},
		{name: "exactly fits", label: "abcd", value: "efgh", w: 9, want: []string{"abcd efgh"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.label, tc.value and tc.w

			// when
			got := leftRightLines(tc.label, tc.value, tc.w)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The help is longer than a terminal of 24 rows, so it scrolls, and the last key
// can be reached.
func Test_Update_help_scrolls(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{name: "the top", keys: "?", shows: []string{"Keys", "screens: Ledger", "↑/↓ scroll"}, hides: []string{"choose the language"}},
		{name: "the end is reachable", keys: "? pgdown pgdown pgdown", shows: []string{"choose the language", "quit"}, hides: []string{"screens: Ledger"}},
		{name: "one line down", keys: "? down", shows: []string{"previous / next month"}, hides: []string{"screens: Ledger"}},
		{name: "up stops at the top", keys: "? up up", shows: []string{"screens: Ledger"}},
		{name: "any other key closes it", keys: "? x", hides: []string{"screens: Ledger", "any other key closes"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := fitModel(t, language.English)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
