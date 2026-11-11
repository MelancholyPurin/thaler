package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// Widths of the two columns on the right of the schedules, wide enough for the
// longest word in each language, the pseudo-language included.
const (
	scheduleAmountW = 13
	scheduleNextW   = 14
)

// refreshSchedules loads the schedules with when each is next due, as of today.
func (m *Model) refreshSchedules() (err error) {
	if m.schedules, err = m.svc.ScheduleStatuses(m.ctx, m.now().Format(ledger.DateLayout)); err != nil {
		return err
	}
	return m.refreshLows()
}

// overdue is how many dates, of all the schedules, wait for an answer.
func (m *Model) overdue() int {
	n := 0
	for _, s := range m.schedules {
		n += s.Overdue
	}
	return n
}

// updateForecast handles the keys of the Forecast screen: moving through the
// schedules, making a new one or changing the selected one, and pausing it.
func (m *Model) updateForecast(key string) tea.Cmd {
	switch key {
	case "enter":
		return m.openDue()
	case "n":
		return m.openSchedule(false)
	case "e":
		return m.openSchedule(true)
	case "p":
		m.toggleSchedulePause()
		return nil
	case "m":
		return m.openMinimum()
	case "z":
		m.undoLast()
		return nil
	}
	m.sched.navigate(key, len(m.schedules), max(1, m.layout().avail/2))
	return nil
}

// scheduleAmount is what the schedule moves on the account it is booked to, or false
// for one with several postings, which has no single amount.
func scheduleAmount(s core.Schedule) (ledger.Money, bool) {
	if s.Transfer && len(s.Postings) == 2 {
		return s.Postings[0].Amount, true
	}
	if src, _, ok := (core.TxView{Postings: s.Postings}).Legs(); ok {
		return src.Amount, true
	}
	return ledger.Money{}, false
}

// rhythm says how often a schedule falls: every month, every 2 weeks.
func (m *Model) rhythm(s core.Schedule) string {
	count := map[string]any{"Count": s.Every}
	switch s.Unit {
	case core.EveryWeek:
		return m.loc.T("every_week", count)
	case core.EveryYear:
		return m.loc.T("every_year", count)
	}
	return m.loc.T("every_month", count)
}

// scheduleNote is the dim line under a schedule: how often, and what is out of the
// ordinary about it.
func (m *Model) scheduleNote(s core.ScheduleStatus) []string {
	parts := []string{m.rhythm(s.Schedule)}
	if s.Transfer && len(s.Postings) == 2 {
		parts = append(parts, m.loc.T("schedule_transfer_to", map[string]any{"Account": m.loc.ShortName(s.Postings[1].Account)}))
	}
	if s.Tolerance > 0 {
		parts = append(parts, m.loc.T("schedule_estimate", map[string]any{"Percent": s.Tolerance}))
	}
	if s.Paused() {
		parts = append(parts, m.loc.T("schedule_paused", nil))
	}
	if s.End != "" {
		parts = append(parts, m.loc.T("schedule_ends", map[string]any{"Date": m.loc.StoredDate(s.End)}))
	}
	return parts
}

// scheduleLines are the lines of every schedule, two or more each, and the lines the
// selected one takes.
func (m *Model) scheduleLines(iw int) (lines []string, sel span) {
	sel = noSpan
	for i, s := range m.schedules {
		name := s.Name
		if s.Transfer {
			name = "⇄ " + name
		}
		amount := ""
		if amt, ok := scheduleAmount(s.Schedule); ok {
			amount = m.loc.Signed(amt)
			if s.Tolerance > 0 {
				amount = "~" + amount
			}
		}
		var next string
		late := s.Overdue > 0
		switch {
		case s.Next != "":
			next = m.loc.StoredDate(s.Next)
			if late {
				next = "! " + next
			}
		case s.Paused():
			next = m.loc.T("schedule_paused", nil)
		default:
			next = m.loc.T("schedule_ended", nil)
		}
		right := fitRight(amount, scheduleAmountW) + "  " + fitRight(next, scheduleNextW)
		first := len(lines)
		var line string
		switch {
		case i == m.sched.cursor:
			line = selFocused.Render(fit(leftRight(name, right, iw), iw))
		case late:
			line = leftRight(name, fitRight(amount, scheduleAmountW)+"  "+warn.Render(fitRight(next, scheduleNextW)), iw)
		default:
			line = leftRight(name, right, iw)
		}
		lines = append(lines, line)
		lines = append(lines, wrapped("  "+strings.Join(m.scheduleNote(s), " · "), iw, dim)...)
		if i == m.sched.cursor {
			sel = span{first, len(lines)}
		}
	}
	return lines, sel
}

// forecastBody is the Forecast screen: the schedules, the one that comes soonest
// first, each with what it moves and when it is next due. What is waiting for an
// answer is said above them.
func (m *Model) forecastBody(l layout) string {
	iw := m.width - 2
	title := m.loc.T("panel_schedules", nil)
	lows := m.lowLines(iw)
	if len(lows) > 0 {
		lows = append(lows, "")
	}
	if len(m.schedules) == 0 {
		lines := lows
		lines = append(lines, wrapped(" "+m.loc.T("schedules_empty", nil), iw, dim)...)
		return box(title, lines, m.width, l.avail, true, "")
	}
	head := lows
	if n := m.overdue(); n > 0 {
		head = append(head, wrapped(m.loc.T("schedules_waiting", map[string]any{"Count": n}), iw, warn)...)
		head = append(head, "")
	}
	head = append(head, leftRight("", dim.Render(fitRight(m.loc.T("label_amount", nil), scheduleAmountW)+"  "+fitRight(m.loc.T("label_next", nil), scheduleNextW)), iw))
	lines, sel := m.scheduleLines(iw)
	h := max(1, l.avail-2-len(head))
	lines = m.scheduleWindow(lines, h, sel)
	footer := m.loc.T("position_of", map[string]any{"N": m.sched.cursor + 1, "Total": len(m.schedules)})
	return box(title, append(head, lines...), m.width, l.avail, true, footer)
}

// scheduleWindow shows the lines that fit in h, scrolled so that the selected
// schedule is in view.
func (m *Model) scheduleWindow(lines []string, h int, sel span) []string {
	if len(lines) <= h {
		m.schedScroll = 0
		return lines
	}
	off := max(0, min(m.schedScroll, len(lines)-h))
	if sel.from >= 0 {
		if sel.from < off {
			off = sel.from
		}
		if sel.to > off+h {
			off = sel.to - h
		}
	}
	m.schedScroll = max(0, min(off, len(lines)-h))
	return lines[m.schedScroll : m.schedScroll+h]
}
