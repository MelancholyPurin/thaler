package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
)

// The detail pane is 32 cells wide at the narrowest two-column layout (76
// columns), 30 inside its border. Every banner line must be the same width, or
// centring the lines one by one would shear the letters.
func Test_banners(t *testing.T) {
	type observed struct {
		Lines     int
		Width     int
		Rectangle bool // every line is as wide as the first
		FitsPane  bool // narrow enough for the narrowest pane
	}
	tcs := []struct {
		name   string
		banner []string
		want   observed
	}{
		{name: "the large one", banner: bannerAntiqua, want: observed{Lines: 7, Width: 28, Rectangle: true, FitsPane: true}},
		{name: "the compact one", banner: bannerCompact, want: observed{Lines: 3, Width: 23, Rectangle: true, FitsPane: true}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.banner

			// when
			got := observed{Lines: len(tc.banner), Width: lipgloss.Width(tc.banner[0]), Rectangle: true}
			for _, line := range tc.banner {
				if lipgloss.Width(line) != got.Width {
					got.Rectangle = false
				}
			}
			got.FitsPane = got.Width <= 30

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_homeBanner(t *testing.T) {
	type observed struct {
		Large      bool
		BlankAbove bool
	}
	tcs := []struct {
		name    string
		w, room int
		want    observed
	}{
		{name: "plenty of room", w: 60, room: 20, want: observed{true, true}},
		{name: "room for the art but not for a blank line above it", w: 60, room: 7, want: observed{true, false}},
		{name: "one line short", w: 60, room: 6, want: observed{false, true}},
		{name: "too narrow for the art", w: 27, room: 20, want: observed{false, true}},
		{name: "nothing fits: still the compact one", w: 10, room: 1, want: observed{false, true}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.w and tc.room

			// when
			art, blank := homeBanner(tc.w, tc.room)

			// then
			got := observed{Large: &art[0] == &bannerAntiqua[0], BlankAbove: blank}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The home pane shows the largest banner that fits, and never cuts its facts off.
func Test_View_homeBanner(t *testing.T) {
	type observed struct {
		Large    bool
		Compact  bool
		CutOff   bool
		HasFacts bool
	}
	tcs := []struct {
		name          string
		width, height int
		want          observed
	}{
		{name: "a roomy terminal", width: 110, height: 32, want: observed{Large: true, HasFacts: true}},
		{name: "the classic 24 rows: nothing cut off", width: 100, height: 24, want: observed{Large: true, HasFacts: true}},
		{name: "the smallest height with room, without a blank line above", width: 100, height: 23, want: observed{Large: true, HasFacts: true}},
		{name: "one row less and the compact banner takes over", width: 100, height: 22, want: observed{Compact: true, HasFacts: true}},
		{name: "a narrow pane wraps the facts, so the compact banner", width: 76, height: 24, want: observed{Compact: true, HasFacts: true}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})

			// when
			view := m.View()

			// then
			got := observed{
				Large:    strings.Contains(view, bannerAntiqua[3]),
				Compact:  strings.Contains(view, bannerCompact[0]),
				CutOff:   strings.Contains(view, "more lines"),
				HasFacts: strings.Contains(view, "Base currency"),
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s\nview:\n%s", diff, view)
			}
		})
	}
}
