package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/config"
)

// A theme is the set of colours the interface is drawn in. "auto" gives each colour a pair, one
// for a light terminal and one for a dark one, and lipgloss picks by asking the terminal what
// its background is. That fails where the terminal does not answer (some multiplexers, a
// session over ssh), so "dark" and "light" use their half of each pair whatever the terminal
// says. The pairs are 256-colour numbers.
type palette struct{ accent, warn, err, dim, borderDim [2]string } // light, dark

var colours = palette{
	accent:    [2]string{"28", "42"},
	warn:      [2]string{"166", "214"},
	err:       [2]string{"160", "203"},
	dim:       [2]string{"242", "245"},
	borderDim: [2]string{"245", "240"},
}

// themeColour is the colour a theme gives to one of the pairs.
func themeColour(theme string, pair [2]string) lipgloss.TerminalColor {
	switch theme {
	case "light":
		return lipgloss.Color(pair[0])
	case "dark":
		return lipgloss.Color(pair[1])
	}
	return lipgloss.AdaptiveColor{Light: pair[0], Dark: pair[1]}
}

// themeInUse is the theme the styles are made from now: while a theme is tried in the popup it
// is not the one the model has (Model.theme).
var themeInUse string

// useTheme draws everything that is drawn from now on in the colours of the theme; a name
// that is not one is "auto". The styles are the package's own, so this is for the one
// interface a process runs.
func useTheme(name string) {
	if !config.ValidTheme(name) {
		name = "auto"
	}
	themeInUse = name
	accentColor = themeColour(name, colours.accent)
	warnColor = themeColour(name, colours.warn)
	errColor = themeColour(name, colours.err)
	dimColor = themeColour(name, colours.dim)
	borderDimColor = themeColour(name, colours.borderDim)

	dim = lipgloss.NewStyle().Foreground(dimColor)
	accent = lipgloss.NewStyle().Foreground(accentColor)
	warn = lipgloss.NewStyle().Foreground(warnColor)
	errStyle = lipgloss.NewStyle().Foreground(errColor)
	borderFocus = lipgloss.NewStyle().Foreground(accentColor)
	borderDim = lipgloss.NewStyle().Foreground(borderDimColor)
	titleFocus = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
}

// themeOf is the theme a setting names: auto when it names none.
func themeOf(setting string) string {
	if config.ValidTheme(setting) {
		return setting
	}
	return "auto"
}

// themeForm is the popup that chooses the theme. Moving through the list tries each theme on
// the screen behind it; esc puts the one in use back.
type themeForm struct {
	cursor int // index into config.Themes
}

func (m *Model) openTheme() {
	f := &themeForm{}
	for i, name := range config.Themes {
		if name == m.theme {
			f.cursor = i
		}
	}
	m.themeForm, m.mode = f, modeTheme
}

func (m *Model) updateTheme(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.themeForm
	switch msg.String() {
	case "esc":
		useTheme(m.theme)
		m.mode, m.themeForm = modeList, nil
	case "up", "k":
		f.cursor = max(f.cursor-1, 0)
		useTheme(config.Themes[f.cursor])
	case "down", "j":
		f.cursor = min(f.cursor+1, len(config.Themes)-1)
		useTheme(config.Themes[f.cursor])
	case "enter":
		m.mode, m.themeForm = modeList, nil
		m.setTheme(config.Themes[f.cursor])
	}
	return m, nil
}

// themeLabel is how a theme is called in the language of the person.
func (m *Model) themeLabel(name string) string {
	switch name {
	case "dark":
		return m.loc.T("theme_dark", nil)
	case "light":
		return m.loc.T("theme_light", nil)
	}
	return m.loc.T("theme_auto", nil)
}

// setTheme makes name the theme at once and writes it to config.toml, so that it is the
// theme the next start opens with. It stands when that cannot be written: the status says so.
func (m *Model) setTheme(name string) {
	m.theme = name
	useTheme(name)
	args := map[string]any{"Name": m.themeLabel(name)}
	var err error
	if m.opts.ConfigPath != "" {
		err = config.SetTheme(m.opts.ConfigPath, m.opts.ConfigTemplate, name)
	}
	switch {
	case m.opts.ConfigPath == "":
		m.setStatus(m.loc.T("theme_session", args), false)
	case err != nil:
		args["Error"] = m.errText(err)
		m.setStatus(m.loc.T("theme_not_saved", args), true)
	default:
		m.setStatus(m.loc.T("theme_saved", args), false)
	}
}

func (m *Model) themePopup() string {
	f := m.themeForm
	w := min(max(m.width-6, 30), 54)
	iw := w - 2
	var lines []string
	for i, name := range config.Themes {
		mark := "  "
		if name == m.theme {
			mark = "✓ "
		}
		line := fit(" "+mark+m.themeLabel(name), iw)
		if i == f.cursor {
			line = selFocused.Render(line)
		}
		lines = append(lines, line)
	}
	// What the colours are, drawn in the ones on trial.
	lines = append(lines, "", " "+accent.Render("●")+" "+warn.Render("●")+" "+errStyle.Render("●")+" "+dim.Render("●"), "")
	lines = append(lines, wrapped(m.loc.T("theme_hint", nil), iw, dim)...)
	return box(m.loc.T("theme_title", nil), lines, w, len(lines)+2, true, m.loc.T("theme_footer", nil))
}
