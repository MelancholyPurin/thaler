package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// cashFlowBarMax is the widest the bar of a month is drawn: more would only stretch it.
const cashFlowBarMax = 40

// gauge draws how much of the income was spent in width cells: filled to the eighth of
// a cell for what was spent, light for what was kept. A month that spent more than it
// earned, or spent with nothing earned, is full and ends in a !; one that spent exactly
// what it earned is full. A month with neither is empty.
func gauge(spent, income int64, width int) string {
	switch {
	case width <= 0:
		return ""
	case spent > max(income, 0):
		return strings.Repeat("█", width-1) + "!"
	case income <= 0:
		return ""
	}
	eighths := 0
	if spent > 0 {
		eighths = max(int(spent*int64(width)*8/income), 1) // below width*8, since spent is less than income
	}
	out := strings.Repeat("█", eighths/8)
	if rest := eighths % 8; rest > 0 {
		out += string(barEighths[rest-1])
	}
	return out + strings.Repeat("░", width-lipgloss.Width(out))
}

// percent writes a share in basis points as a percentage to a tenth, or a dash when
// there is none.
func (m *Model) percent(s core.SavingsRate) string {
	if !s.Known {
		return "–"
	}
	tenths := (abs64(s.BasisPoints) + 5) / 10
	if s.BasisPoints < 0 {
		tenths = -tenths
	}
	return m.loc.T("percent", map[string]any{"Value": m.loc.Decimal(float64(tenths)/10, 1)})
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// signedAmount is an amount with a + when it is above zero, without its currency.
func (m *Model) signedAmount(v ledger.Money) string {
	if v.Minor > 0 {
		return "+" + m.loc.Amount(v)
	}
	return m.loc.Amount(v)
}

// scrollMonths handles the keys of a report that shows the newest months first and goes
// back through the older ones: back is how many months it is scrolled back, n how many
// there are.
func scrollMonths(back *int, n int, key string, page int) {
	switch key {
	case "up", "k":
		*back++
	case "down", "j":
		*back--
	case "pgup", "ctrl+u":
		*back += max(1, page-1)
	case "pgdown", "ctrl+d":
		*back -= max(1, page-1)
	case "home", "g":
		*back = n
	case "end", "G":
		*back = 0
	}
	*back = max(*back, 0)
}

// cashFlowBody is the income report: a line for each month, then the total.
func (m *Model) cashFlowBody(l layout) string {
	iw := m.width - 2
	title := m.loc.T("report_cashflow", nil)
	head := m.reportRange(iw, monthsOf(m.cashFlow), m.cashFlow.Missing)
	if len(m.cashFlow.Months) == 0 {
		return box(title, head, m.width, l.avail, true, "")
	}

	// The columns are as wide as their heading or the biggest figure, the month's name
	// first, and the bar takes what is left.
	labels := []string{m.loc.T("report_income", nil), m.loc.T("report_spent", nil), m.loc.T("report_kept", nil), m.loc.T("report_saved", nil)}
	cells := make([][4]string, 0, len(m.cashFlow.Months)+1)
	for _, c := range append(append([]core.CashFlowMonth(nil), m.cashFlow.Months...), m.cashFlow.Total) {
		cells = append(cells, [4]string{m.loc.Amount(c.Income), m.loc.Amount(c.Spending), m.signedAmount(c.Net), m.percent(c.Savings)})
	}
	widths := [4]int{}
	for i, label := range labels {
		widths[i] = lipgloss.Width(label)
		for _, row := range cells {
			widths[i] = max(widths[i], lipgloss.Width(row[i]))
		}
	}
	monthW := lipgloss.Width(m.loc.T("report_total", nil))
	for _, c := range m.cashFlow.Months {
		monthW = max(monthW, lipgloss.Width(m.monthLabel(c.Month)))
	}
	used := 1 + monthW
	for _, w := range widths {
		used += 2 + w
	}
	barW := min(iw-used-2, cashFlowBarMax)
	if barW < 6 {
		barW = 0
	}
	row := func(label string, cell [4]string, bar string) string {
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
		hint = wrapped(" "+m.loc.T("report_gauge_hint", nil), iw, dim)
	}
	lines := head
	lines = append(lines, dim.Render(row("", [4]string{labels[0], labels[1], labels[2], labels[3]}, "")))
	fixed := len(lines) + 2 + len(hint) // the rule and the total, and the hint
	h := max(l.avail-2-fixed, 1)
	n := len(m.cashFlow.Months)
	m.cfBack = max(0, min(m.cfBack, n-min(h, n)))
	end := n - m.cfBack
	for i := max(end-h, 0); i < end; i++ {
		c := m.cashFlow.Months[i]
		bar := gauge(c.Spending.Minor, c.Income.Minor, barW)
		if strings.HasSuffix(bar, "!") {
			bar = warn.Render(bar)
		}
		lines = append(lines, row(m.monthLabel(c.Month), cells[i], bar))
	}
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	t := m.cashFlow.Total
	lines = append(lines, bold.Render(row(m.loc.T("report_total", nil), cells[n], gauge(t.Spending.Minor, t.Income.Minor, barW))))
	lines = append(lines, hint...)
	footer := ""
	if n > h {
		footer = m.loc.T("position_of", map[string]any{"N": end, "Total": n})
	}
	return box(title, lines, m.width, l.avail, true, footer)
}

func monthsOf(r core.CashFlowReport) []string {
	out := make([]string, len(r.Months))
	for i, c := range r.Months {
		out[i] = c.Month
	}
	return out
}
