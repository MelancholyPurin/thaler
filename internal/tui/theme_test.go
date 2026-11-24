package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
	"github.com/muesli/termenv"
	"golang.org/x/text/language"
)

// A theme gives the colours it says, whatever the terminal answers when asked about its
// background, except "auto", which follows the answer.
func Test_useTheme(t *testing.T) {
	type observed struct{ Accent, Warn, Err, Dim, Border string }
	tcs := []struct {
		name       string
		theme      string
		darkTerm   bool
		want       observed
		wantInUse  string
		wantAccent lipgloss.TerminalColor
	}{
		{name: "dark, on a dark terminal", theme: "dark", darkTerm: true, wantInUse: "dark", want: observed{"38;5;42", "38;5;214", "38;5;203", "38;5;245", "38;5;240"}},
		{name: "dark, though the terminal says light", theme: "dark", darkTerm: false, wantInUse: "dark", want: observed{"38;5;42", "38;5;214", "38;5;203", "38;5;245", "38;5;240"}},
		{name: "light, on a light terminal", theme: "light", darkTerm: false, wantInUse: "light", want: observed{"38;5;28", "38;5;166", "38;5;160", "38;5;242", "38;5;245"}},
		{name: "light, though the terminal says dark", theme: "light", darkTerm: true, wantInUse: "light", want: observed{"38;5;28", "38;5;166", "38;5;160", "38;5;242", "38;5;245"}},
		{name: "auto follows a dark terminal", theme: "auto", darkTerm: true, wantInUse: "auto", want: observed{"38;5;42", "38;5;214", "38;5;203", "38;5;245", "38;5;240"}},
		{name: "auto follows a light terminal", theme: "auto", darkTerm: false, wantInUse: "auto", want: observed{"38;5;28", "38;5;166", "38;5;160", "38;5;242", "38;5;245"}},
		{name: "a name that is no theme is auto", theme: "solarized", darkTerm: false, wantInUse: "auto", want: observed{"38;5;28", "38;5;166", "38;5;160", "38;5;242", "38;5;245"}},
		{name: "nothing is auto", theme: "", darkTerm: true, wantInUse: "auto", want: observed{"38;5;42", "38;5;214", "38;5;203", "38;5;245", "38;5;240"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			oldProfile, oldDark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
			t.Cleanup(func() {
				lipgloss.SetColorProfile(oldProfile)
				lipgloss.SetHasDarkBackground(oldDark)
				useTheme("auto")
			})
			lipgloss.SetColorProfile(termenv.ANSI256) // tests have no terminal, so no colour unless asked for
			lipgloss.SetHasDarkBackground(tc.darkTerm)

			// when
			useTheme(tc.theme)

			// then
			got := observed{}
			for _, c := range []struct {
				out *string
				st  lipgloss.Style
			}{{&got.Accent, accent}, {&got.Warn, warn}, {&got.Err, errStyle}, {&got.Dim, dim}, {&got.Border, borderDim}} {
				*c.out = escapeOf(c.st.Render("x"))
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("colours mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantInUse, themeInUse); diff != "" {
				t.Errorf("theme in use mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(escapeOf(accent.Render("x")), escapeOf(borderFocus.Render("x"))); diff != "" {
				t.Errorf("the focused border is not in the accent colour (-accent +border):\n%s", diff)
			}
		})
	}
}

// escapeOf is the colour part of the escape sequence a styled text starts with: 38;5;42.
func escapeOf(rendered string) string {
	start := strings.Index(rendered, "\x1b[")
	end := strings.Index(rendered, "m")
	if start < 0 || end < start {
		return ""
	}
	return rendered[start+2 : end]
}

func Test_Update_theme(t *testing.T) {
	type observed struct {
		Mode   string
		Status string
		Theme  string // the model's
		InUse  string // what the styles are made from
		File   string
		Cursor int // in the popup, if it is open
	}
	tcs := []struct {
		name     string
		config   string // the file before; "-" is no config path, "dir" a path that cannot be written
		start    string // the theme in config.toml when it starts
		keys     string
		want     observed
		wantFile func(path string) string
	}{
		{
			name: "ctrl+t opens the list on the theme in use", start: "light", keys: "ctrl+t",
			want: observed{Mode: "theme", Theme: "light", InUse: "light", Cursor: 2},
		},
		{
			name: "no theme is auto", keys: "ctrl+t",
			want: observed{Mode: "theme", Theme: "auto", InUse: "auto", Cursor: 0},
		},
		{
			name: "a theme that does not exist is auto", start: "solarized", keys: "ctrl+t",
			want: observed{Mode: "theme", Theme: "auto", InUse: "auto", Cursor: 0},
		},
		{
			name: "moving tries a theme on the screen, and the model keeps its own", keys: "ctrl+t down",
			want: observed{Mode: "theme", Theme: "auto", InUse: "dark", Cursor: 1},
		},
		{
			name: "j and k move too", keys: "ctrl+t j j k",
			want: observed{Mode: "theme", Theme: "auto", InUse: "dark", Cursor: 1},
		},
		{
			name: "the first and the last stay", keys: "ctrl+t up down down down",
			want: observed{Mode: "theme", Theme: "auto", InUse: "light", Cursor: 2},
		},
		{
			name: "esc puts back the one in use", start: "dark", keys: "ctrl+t down esc",
			want: observed{Mode: "list", Theme: "dark", InUse: "dark"},
		},
		{
			name: "enter keeps it and writes it to config.toml", config: "# mine\nkeep_backups = 3\n", keys: "ctrl+t down enter",
			want:     observed{Mode: "list", Status: "Colours: For a dark terminal (saved in config.toml)", Theme: "dark", InUse: "dark"},
			wantFile: func(string) string { return "# mine\nkeep_backups = 3\ntheme = \"dark\"\n" },
		},
		{
			name: "choosing auto again", config: "theme = \"light\"\n", start: "light", keys: "ctrl+t up up enter",
			want:     observed{Mode: "list", Status: "Colours: Follow the terminal (saved in config.toml)", Theme: "auto", InUse: "auto"},
			wantFile: func(string) string { return "theme = \"auto\"\n" },
		},
		{
			name: "without a config file the choice lasts until quitting", config: "-", keys: "ctrl+t down enter",
			want: observed{Mode: "list", Status: "Colours: For a dark terminal (until you quit: there is no config file to save it in)", Theme: "dark", InUse: "dark"},
		},
		{
			name: "a file that cannot be written does not undo the choice", config: "dir", keys: "ctrl+t down down enter",
			want: observed{Mode: "list", Status: "Colours: For a light terminal (until you quit: could not save it: ", Theme: "light", InUse: "light"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			t.Cleanup(func() { useTheme("auto") })
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			switch tc.config {
			case "-":
				path = ""
			case "dir":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "":
			default:
				if err := os.WriteFile(path, []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			st := openLedger(t)
			m, err := newWithClock(t.Context(), st, Options{LoadConfig: testConfig, ConfigPath: path, ConfigTemplate: "# sample\n", Theme: tc.start}, fixedNow)
			if err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, Theme: m.theme, InUse: themeInUse}
			if m.themeForm != nil {
				got.Cursor = m.themeForm.cursor
			}
			if tc.config == "dir" { // the system's own words follow
				got.Status = got.Status[:min(len(got.Status), len(tc.want.Status))]
			}
			if tc.wantFile != nil {
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				got.File = string(body)
				tc.want.File = tc.wantFile(path)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// One model's theme does not carry over to the next: each starts from its own setting.
func Test_Update_theme_perModel(t *testing.T) {
	tcs := []struct {
		name   string
		before string // the theme of the model made first
		theme  string // the theme of the next one
		want   string // the theme in use once the next one is made
	}{
		{name: "light", before: "auto", theme: "light", want: "light"},
		{name: "unset after light is auto", before: "light", theme: "", want: "auto"},
		{name: "dark after light", before: "light", theme: "dark", want: "dark"},
		{name: "auto after dark", before: "dark", theme: "auto", want: "auto"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			t.Cleanup(func() { useTheme("auto") })
			st := openLedger(t)
			if _, err := newWithClock(t.Context(), st, Options{LoadConfig: testConfig, Theme: tc.before}, fixedNow); err != nil {
				t.Fatal(err)
			}

			// when
			if _, err := newWithClock(t.Context(), st, Options{LoadConfig: testConfig, Theme: tc.theme}, fixedNow); err != nil {
				t.Fatal(err)
			}

			// then
			if diff := cmp.Diff(tc.want, themeInUse); diff != "" {
				t.Errorf("theme in use mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The popup and its messages speak the language of the interface, and fit the terminal.
func Test_View_theme(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		keys  string
		shows []string
		hides []string
	}{
		{name: "English", tag: language.English, keys: "ctrl+t", shows: []string{"Colours", "Follow the terminal", "For a dark terminal", "For a light terminal", "enter choose · ↑/↓ try · esc cancel", "● ● ● ●"}},
		{name: "Spanish", tag: language.Spanish, keys: "ctrl+t", shows: []string{"Colores", "Seguir al terminal", "Para terminal oscuro", "enter elegir · ↑/↓ probar · esc cancelar"}, hides: []string{"Follow the terminal"}},
		{name: "German", tag: language.German, keys: "ctrl+t", shows: []string{"Farben", "Dem Terminal folgen", "Für helles Terminal", "enter wählen · ↑/↓ testen · esc abbrechen"}, hides: []string{"Follow the terminal"}},
		{name: "German, saved", tag: language.German, keys: "ctrl+t down enter", shows: []string{"Farben: Für dunkles Terminal (in config.toml gespeichert)"}, hides: []string{"saved"}},
		{name: "Spanish, saved", tag: language.Spanish, keys: "ctrl+t down enter", shows: []string{"Colores: Para terminal oscuro (guardado en config.toml)"}, hides: []string{"saved"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			t.Cleanup(func() { useTheme("auto") })
			m := fitModel(t, tc.tag)

			// when
			play(m, tc.keys)
			view := m.View()

			// then
			if diff := cmp.Diff([]string(nil), cutIn(view)); diff != "" {
				t.Errorf("cut short or too big (-want +got):\n%s\n\nthe screen:\n%s", diff, view)
			}
			checkView(t, view, tc.shows, tc.hides)
		})
	}
}
