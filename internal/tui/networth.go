package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// netWorthBarMax is the widest the bar of a month is drawn.
const netWorthBarMax = 40

// netWorthBody is the net worth report: a line for each month with what the accounts
// were worth at the end of it, how much that changed from the month before (a dash for
// the first month, which has none in view), and a bar against the highest. The figures
// are the service's NetWorthReport.
func (m *Model) netWorthBody(l layout) string {
	iw := m.width - 2
	title := m.loc.T("report_networth", nil)
	months := make([]string, len(m.worthHistory.Months))
	for i, p := range m.worthHistory.Months {
		months[i] = p.Month
	}
	head := m.reportRange(iw, months, m.worthHistory.Missing)
	if len(months) == 0 {
		return box(title, head, m.width, l.avail, true, "")
	}

	// The figures are as wide as their heading or the biggest of them, the month's name
	// first, and the bar takes what is left.
	labels := [2]string{m.loc.T("label_net_worth", nil), m.loc.T("report_change", nil)}
	cells := make([][2]string, len(months))
	top := int64(0)
	for i, p := range m.worthHistory.Months {
		cells[i] = [2]string{m.loc.Amount(p.Total), "–"}
		if i > 0 {
			cells[i][1] = m.signedAmount(ledger.Money{Minor: p.Total.Minor - m.worthHistory.Months[i-1].Total.Minor, Currency: p.Total.Currency})
		}
		top = max(top, abs64(p.Total.Minor))
	}
	widths := [2]int{}
	for i, label := range labels {
		widths[i] = lipgloss.Width(label)
		for _, c := range cells {
			widths[i] = max(widths[i], lipgloss.Width(c[i]))
		}
	}
	monthW := 1
	for _, month := range months {
		monthW = max(monthW, lipgloss.Width(m.monthLabel(month)))
	}
	used := 1 + monthW
	for _, w := range widths {
		used += 2 + w
	}
	barW := min(iw-used-2, netWorthBarMax)
	if barW < 6 {
		barW = 0
	}
	row := func(label string, cell [2]string, bar string) string {
		line := " " + fit(label, monthW)
		for i, w := range widths {
			line += "  " + fitRight(cell[i], w)
		}
		if barW > 0 {
			line += "  " + bar
		}
		return fit(line, iw)
	}

	var hint []string
	if barW > 0 {
		hint = wrapped(" "+m.loc.T("report_worth_hint", nil), iw, dim)
	}
	lines := head
	lines = append(lines, dim.Render(row("", labels, "")))
	h := max(l.avail-2-len(lines)-len(hint), 1)
	n := len(months)
	m.nwBack = max(0, min(m.nwBack, n-min(h, n)))
	end := n - m.nwBack
	for i := max(end-h, 0); i < end; i++ {
		lines = append(lines, row(m.monthLabel(months[i]), cells[i], accent.Render(bar(m.worthHistory.Months[i].Total.Minor, top, barW))))
	}
	lines = append(lines, hint...)
	footer := ""
	if n > h {
		footer = m.loc.T("position_of", map[string]any{"N": end, "Total": n})
	}
	return box(title, lines, m.width, l.avail, true, footer)
}
