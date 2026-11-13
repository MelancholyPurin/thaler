package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The range popup (r on the Reports) chooses the months the reports cover. They show
// the 12 months up to the one on show until a range is chosen. A chosen range is two
// months, From and To (YYYY-MM): an empty To is this month, an empty From is 11 months
// before To, and both empty go back to following the month on show. A chosen range
// stays as it is when the month on show changes, so that what the report adds up to
// does not move under you.

// customRange says whether a range was chosen, instead of following the month on show.
func (m *Model) customRange() bool { return m.repFrom != "" || m.repTo != "" }

// reportMonths are the first and last month the reports are asked for, either may be
// empty for the service to fill in: the range chosen, or the 12 months up to the one on show.
func (m *Model) reportMonths() (from, to string) {
	if !m.customRange() {
		return "", m.period
	}
	to = m.repTo
	if to == "" {
		to = m.now().Format(monthLayout)
	}
	return m.repFrom, to
}

type rangeForm struct {
	from, to textinput.Model
	onTo     bool
	err      string
}

func (m *Model) rangeSize() (w int) { return min(max(m.width-6, 30), 60) }

// openRange asks for the range, with the months being shown as the placeholders and
// what was chosen, if anything, in the fields.
func (m *Model) openRange() tea.Cmd {
	f := &rangeForm{from: newInput(m.repFrom), to: newInput(m.repTo)}
	f.from.CharLimit, f.to.CharLimit = len(monthLayout), len(monthLayout)
	if n := len(m.spending.Months); n > 0 {
		f.from.Placeholder, f.to.Placeholder = m.spending.Months[0], m.spending.Months[n-1]
	}
	f.from.Width, f.to.Width = len(monthLayout), len(monthLayout)
	m.rng, m.mode = f, modeRange
	return f.from.Focus()
}

func (m *Model) focusRange(onTo bool) tea.Cmd {
	f := m.rng
	f.onTo = onTo
	f.from.Blur()
	f.to.Blur()
	if onTo {
		return f.to.Focus()
	}
	return f.from.Focus()
}

func (m *Model) updateRange(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.rng
	switch msg.String() {
	case "esc":
		m.mode, m.rng = modeList, nil
		return m, nil
	case "tab", "shift+tab", "up", "down":
		return m, m.focusRange(!f.onTo)
	case "enter":
		if !f.onTo {
			return m, m.focusRange(true)
		}
		m.saveRange()
		return m, nil
	case "ctrl+s":
		m.saveRange()
		return m, nil
	}
	var cmd tea.Cmd
	if f.onTo {
		f.to, cmd = f.to.Update(msg)
	} else {
		f.from, cmd = f.from.Update(msg)
	}
	f.err = ""
	return m, cmd
}

// saveRange asks the service for the reports of the range typed. The service says
// whether it is one: a month that is not a month, a start after the end, more than
// ten years. A range it refuses is not kept.
func (m *Model) saveRange() {
	f := m.rng
	oldFrom, oldTo := m.repFrom, m.repTo
	m.repFrom, m.repTo = f.from.Value(), f.to.Value() // the fields take no more than a month
	if err := m.refreshReports(); err != nil {
		m.repFrom, m.repTo = oldFrom, oldTo
		f.err = m.errText(err)
		if again := m.refreshReports(); again != nil { // put back what was shown
			f.err = m.errText(again)
		}
		return
	}
	m.mode, m.rng = modeList, nil
	m.cfBack, m.payTop, m.repList = 0, 0, listState{}
	m.clampAll()
}

func (m *Model) rangePopup() string {
	f := m.rng
	w := m.rangeSize()
	iw := w - 2
	label := func(text string, active bool) string {
		if active {
			return accent.Render(fit("▸ "+text, 11))
		}
		return dim.Render(fit("  "+text, 11))
	}
	lines := []string{
		label(m.loc.T("label_from", nil), !f.onTo) + " " + f.from.View(),
		label(m.loc.T("label_until", nil), f.onTo) + " " + f.to.View(),
		dim.Render(strings.Repeat("─", iw)),
	}
	lines = append(lines, wrapped(m.loc.T("range_hint", nil), iw, dim)...)
	lines = append(lines, "")
	msg := wrapped(f.err, iw, errStyle)
	for len(msg) < 2 {
		msg = append(msg, "")
	}
	lines = append(lines, msg...)
	return box(m.loc.T("range_title", nil), lines, w, len(lines)+2, true, m.loc.T("range_footer", nil))
}
