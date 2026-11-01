package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/i18n"
)

// languageForm is the popup that chooses the language.
type languageForm struct {
	cursor int // index into i18n.Languages
}

// openLanguage shows the languages with the one in use under the cursor.
func (m *Model) openLanguage() {
	f := &languageForm{}
	for i, l := range i18n.Languages {
		if l.Code == m.loc.Code() {
			f.cursor = i
		}
	}
	m.lang, m.mode = f, modeLanguage
}

func (m *Model) updateLanguage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.lang
	switch msg.String() {
	case "esc":
		m.mode, m.lang = modeList, nil
	case "up", "k":
		f.cursor = max(f.cursor-1, 0)
	case "down", "j":
		f.cursor = min(f.cursor+1, len(i18n.Languages)-1)
	case "enter":
		m.mode, m.lang = modeList, nil
		m.setLanguage(i18n.Languages[f.cursor])
	}
	return m, nil
}

// setLanguage switches to l at once and writes it to config.toml, so that it is the
// language the next start opens in. The switch stands when that cannot be written:
// the status says so.
func (m *Model) setLanguage(l i18n.Language) {
	m.loc = i18n.New(l.Tag)
	m.filter.Placeholder = m.loc.T("filter_placeholder", nil)
	m.picker.Placeholder = m.loc.T("picker_placeholder", nil)
	// The rows hold text for the search, written in the language of the time.
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)

	args := map[string]any{"Name": l.Name}
	var err error
	if m.opts.ConfigPath != "" {
		err = config.SetLanguage(m.opts.ConfigPath, m.opts.ConfigTemplate, l.Code)
	}
	switch {
	case m.opts.ConfigPath == "":
		m.setStatus(m.loc.T("language_session", args), false)
	case err != nil:
		args["Error"] = m.errText(err)
		m.setStatus(m.loc.T("language_not_saved", args), true)
	default:
		m.setStatus(m.loc.T("language_saved", args), false)
	}
}

func (m *Model) languagePopup() string {
	f := m.lang
	w := min(max(m.width-6, 30), 54)
	iw := w - 2
	var lines []string
	for i, l := range i18n.Languages {
		mark := "  "
		if l.Code == m.loc.Code() {
			mark = "✓ "
		}
		line := fit(" "+mark+l.Name, iw)
		if i == f.cursor {
			line = selFocused.Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "")
	lines = append(lines, wrapped(m.loc.T("language_hint", nil), iw, dim)...)
	return box(m.loc.T("language_title", nil), lines, w, len(lines)+2, true, m.loc.T("language_footer", nil))
}
