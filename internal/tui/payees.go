package tui

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The payees report ranks where the money went in the months on show: a line for each
// payee, the one that took most first, with a bar against the biggest, what it took,
// its share of all the spending and how many transactions that was. The figures are
// the service's PayeeReport.

// payeeLimit is how many payees are listed; the service adds up the rest in one line.
const payeeLimit = 100

// payeeBarMax is the widest the bar of a payee is drawn.
const payeeBarMax = 30

// shareBasisPoints is part of whole in basis points, rounded half away from zero, or
// false when there is no whole to take a share of (nothing, or a refund overall).
func shareBasisPoints(part, whole int64) (int64, bool) {
	if whole <= 0 {
		return 0, false
	}
	num := new(big.Int).Mul(big.NewInt(part), big.NewInt(10000))
	half := big.NewInt(whole / 2)
	if num.Sign() < 0 {
		half.Neg(half)
	}
	return new(big.Int).Quo(num.Add(num, half), big.NewInt(whole)).Int64(), true
}

// payeeShare is what part of the spending a payee took, as the language writes it.
func (m *Model) payeeShare(part, whole ledger.Money) string {
	bp, ok := shareBasisPoints(part.Minor, whole.Minor)
	return m.percent(core.SavingsRate{BasisPoints: bp, Known: ok})
}

// payeeName is how a payee is called on screen: the name as written, or a word for the
// transactions that have none.
func (m *Model) payeeName(p core.PayeeSpending) string {
	if p.Name == "" {
		return m.loc.T("report_no_payee", nil)
	}
	return p.Name
}

// payeeCells are the amount, share and count of a line, as text.
func (m *Model) payeeCells(p core.PayeeSpending) [3]string {
	return [3]string{m.loc.Amount(p.Amount), m.payeeShare(p.Amount, m.payeeRank.Total), strconv.Itoa(p.Transactions)}
}

// scrollPayees handles the keys of the payees report: scrolling by line.
func (m *Model) scrollPayees(key string) {
	// The lines that scroll are what is left of the screen after the months, the heading
	// and the total; a page keeps one of them. A warning above would make a page a line long.
	page := max(1, m.layout().avail-7)
	switch key {
	case "up", "k":
		m.payTop--
	case "down", "j":
		m.payTop++
	case "pgup", "ctrl+u":
		m.payTop -= page
	case "pgdown", "ctrl+d":
		m.payTop += page
	case "home", "g":
		m.payTop = 0
	case "end", "G":
		m.payTop = 1 << 20 // payeesBody holds it at the end
	}
	m.payTop = max(m.payTop, 0)
}

// payeesBody is the payees report.
func (m *Model) payeesBody(l layout) string {
	iw := m.width - 2
	title := m.loc.T("report_payees", nil)
	head := m.reportRange(iw, []string{m.payeeRank.From, m.payeeRank.To}, m.payeeRank.Missing)
	if len(m.payeeRank.Payees) == 0 {
		return box(title, append(head, "", dim.Render(" "+m.loc.T("report_nothing", nil))), m.width, l.avail, true, "")
	}

	// The figures are as wide as their heading or the biggest of them, and the name and
	// the bar share what is left.
	labels := [3]string{m.loc.T("label_amount", nil), m.loc.T("report_share", nil), m.loc.T("report_count", nil)}
	names := make([]string, 0, len(m.payeeRank.Payees)+1)
	cells := make([][3]string, 0, len(m.payeeRank.Payees)+1)
	for _, p := range m.payeeRank.Payees {
		names = append(names, m.payeeName(p))
		cells = append(cells, m.payeeCells(p))
	}
	if m.payeeRank.OtherPayees > 0 {
		names = append(names, m.loc.T("report_others", map[string]any{"Count": m.payeeRank.OtherPayees}))
		cells = append(cells, m.payeeCells(m.payeeRank.Others))
	}
	count := 0
	for _, p := range m.payeeRank.Payees {
		count += p.Transactions
	}
	count += m.payeeRank.Others.Transactions
	total := [3]string{m.loc.Amount(m.payeeRank.Total), "", strconv.Itoa(count)}

	widths := [3]int{}
	for i, label := range labels {
		widths[i] = lipgloss.Width(label)
		for _, c := range append(cells[:len(cells):len(cells)], total) {
			widths[i] = max(widths[i], lipgloss.Width(c[i]))
		}
	}
	used := 2 // the space before the name, and one after the last column
	for _, w := range widths {
		used += 2 + w
	}
	room := iw - used
	barW := min(room*2/5, payeeBarMax)
	if barW < 6 {
		barW = 0
	}
	nameW := room
	if barW > 0 {
		nameW = room - barW - 2
	}
	nameW = max(nameW, 6)
	row := func(name, bar string, cell [3]string) string {
		line := " " + fit(name, nameW)
		if barW > 0 {
			line += "  " + bar + strings.Repeat(" ", barW-lipgloss.Width(bar))
		}
		for i, w := range widths {
			line += "  " + fitRight(cell[i], w)
		}
		return fit(line, iw)
	}

	top := int64(0)
	for _, p := range m.payeeRank.Payees {
		top = max(top, max(p.Amount.Minor, -p.Amount.Minor))
	}
	out := head
	out = append(out, dim.Render(row("", "", labels)))
	body := make([]string, 0, len(cells))
	for i := range cells {
		b := ""
		if i < len(m.payeeRank.Payees) {
			b = accent.Render(bar(m.payeeRank.Payees[i].Amount.Minor, top, barW))
		}
		line := row(names[i], b, cells[i])
		if i >= len(m.payeeRank.Payees) { // the payees left out are one line, not a payee
			line = dim.Render(line)
		}
		body = append(body, line)
	}
	fixed := len(out) + 2 // the rule and the total
	h := max(l.avail-2-fixed, 1)
	n := len(body)
	m.payTop = max(0, min(m.payTop, n-min(h, n)))
	out = append(out, body[m.payTop:min(m.payTop+h, n)]...)
	out = append(out, dim.Render(strings.Repeat("─", iw)))
	out = append(out, bold.Render(row(m.loc.T("report_total", nil), "", total)))
	footer := ""
	if n > h {
		footer = m.loc.T("position_of", map[string]any{"N": min(m.payTop+h, n), "Total": n})
	}
	return box(title, out, m.width, l.avail, true, footer)
}
