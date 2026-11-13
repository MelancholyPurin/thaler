package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The Reports screen holds reports, and tab goes from one to the next. They all cover
// the 12 months up to the one on show (up to this month, for all time).
//
// Spending by category: on the left the categories, each with a small chart of its
// months and its total, on the right the months of the one selected as bars. The first
// entry of the list is all of them.
//
// Income against spending: a line for each month with what came in, what went out,
// what was kept and the share of the income that is, and a bar of how much of the
// income was spent.
//
// Payees: a line for each payee, the one that took most first, with a bar, what it
// took, its share of the spending and how many transactions that was.
//
// Net worth: a line for each month with what the accounts were worth at the end of it,
// how much that changed from the month before, and a bar.

// reportKind says which report the screen shows.
type reportKind int

const (
	reportSpending reportKind = iota
	reportCashFlow
	reportPayees
	reportNetWorth
	numReports
)

// Widths of the amount columns, wide enough for -123,456.78.
const (
	reportTotalW  = 11
	reportAmountW = 11
)

// sparkLevels are the eight heights of a small chart, from a hair to the full cell.
var sparkLevels = []rune("▁▂▃▄▅▆▇█")

// sparkline draws values as one cell each, as high as they are against the largest. A
// month with nothing is a dot and a negative one (a refund) a dash, so that neither is
// taken for a small spending.
func sparkline(values []int64) string {
	top := int64(0)
	for _, v := range values {
		top = max(top, v)
	}
	var b strings.Builder
	for _, v := range values {
		switch {
		case v < 0:
			b.WriteRune('-')
		case v == 0:
			b.WriteRune('·')
		default:
			level := int((v*int64(len(sparkLevels)) + top - 1) / top) // rounded up, so that a little is still seen
			b.WriteRune(sparkLevels[level-1])
		}
	}
	return b.String()
}

// sparkCells is the most cells a small chart has. A longer range is drawn with each
// cell standing for several months, as many as the range needs.
const sparkCells = 12

// bucketed cuts values into at most cells groups of neighbours and gives the mean of
// each, rounded away from zero so that a month with something in it is never drawn as
// one with nothing.
func bucketed(values []int64, cells int) []int64 {
	n := len(values)
	if n <= cells {
		return values
	}
	out := make([]int64, cells)
	for i := range out {
		from, to := i*n/cells, (i+1)*n/cells
		var sum int64
		for _, v := range values[from:to] {
			sum += v
		}
		count := int64(to - from)
		if sum < 0 {
			out[i] = (sum - count + 1) / count
		} else {
			out[i] = (sum + count - 1) / count
		}
	}
	return out
}

// barEighths are the partial cells at the end of a bar, from an eighth to seven.
var barEighths = []rune("▏▎▍▌▋▊▉")

// bar draws v against top (the largest amount shown) in at most width cells, to the
// eighth of a cell. A refund is drawn with ▒, whole cells only, so that it is not taken
// for spending.
func bar(v, top int64, width int) string {
	if top <= 0 || width <= 0 || v == 0 {
		return ""
	}
	if v < 0 {
		cells := max(1, int((-v*int64(width)+top/2)/top))
		return strings.Repeat("▒", min(cells, width))
	}
	eighths := min(max(int(v*int64(width)*8/top), 1), width*8)
	out := strings.Repeat("█", eighths/8)
	if rest := eighths % 8; rest > 0 {
		out += string(barEighths[rest-1])
	}
	return out
}

// refreshReports loads the figures of the reports for their range: the 12 months up to
// the one on show, or the range chosen (range.go).
func (m *Model) refreshReports() (err error) {
	rateDate := m.now().Format(ledger.DateLayout)
	from, to := m.reportMonths()
	if m.spending, err = m.svc.SpendingReport(m.ctx, core.SpendingRequest{From: from, To: to, RateDate: rateDate}); err != nil {
		return err
	}
	if m.cashFlow, err = m.svc.CashFlowReport(m.ctx, core.CashFlowRequest{From: from, To: to, RateDate: rateDate}); err != nil {
		return err
	}
	if m.payeeRank, err = m.svc.PayeeReport(m.ctx, core.PayeeRequest{From: from, To: to, RateDate: rateDate, Limit: payeeLimit}); err != nil {
		return err
	}
	m.worthHistory, err = m.svc.NetWorthReport(m.ctx, core.NetWorthRequest{From: from, To: to, RateDate: rateDate})
	return err
}

// spendEntry is a line of the list: all spending, or one category.
type spendEntry struct {
	name   string
	months []ledger.Money
	total  ledger.Money
}

// spendEntries are the lines of the list, all spending first.
func (m *Model) spendEntries() []spendEntry {
	if len(m.spending.Categories) == 0 {
		return nil
	}
	out := []spendEntry{{name: m.loc.T("report_all_spending", nil), months: m.spending.Totals, total: m.spending.Total}}
	for _, c := range m.spending.Categories {
		out = append(out, spendEntry{name: m.loc.ShortName(c.Name), months: c.Months, total: c.Total})
	}
	return out
}

func minors(ms []ledger.Money) []int64 {
	out := make([]int64, len(ms))
	for i, m := range ms {
		out[i] = m.Minor
	}
	return out
}

// updateReports handles the keys of the Reports screen: the next report, choosing the
// range, and moving through the one on show.
func (m *Model) updateReports(key string) tea.Cmd {
	l := m.layout()
	switch {
	case key == "tab":
		m.report = (m.report + 1) % numReports
	case key == "r":
		return m.openRange()
	case key == "e":
		return m.openExport()
	case m.report == reportCashFlow:
		scrollMonths(&m.cfBack, len(m.cashFlow.Months), key, max(1, l.avail-2))
	case m.report == reportPayees:
		m.scrollPayees(key)
	case m.report == reportNetWorth:
		scrollMonths(&m.nwBack, len(m.worthHistory.Months), key, max(1, l.avail-5)) // the lines shown: the borders, the range, the heads and the hint take 5
	default:
		m.repList.navigate(key, len(m.spendEntries()), max(1, l.avail-2-m.reportHeader(l.leftW-2)))
	}
	return nil
}

// monthLabel is a month of the report as the language writes it.
func (m *Model) monthLabel(month string) string {
	t, err := time.Parse(monthLayout, month)
	if err != nil {
		return month
	}
	return m.loc.MonthYear(t)
}

// reportHeader is how many lines say, above the list, which months it covers.
func (m *Model) reportHeader(iw int) int {
	return len(m.reportRange(iw, m.spending.Months, m.spending.Missing))
}

// reportRange are the lines above the list: the months it covers, and the currencies
// that could not be counted. They are here, not with the bars, which a short terminal
// cuts.
func (m *Model) reportRange(iw int, months []string, missing []ledger.Currency) []string {
	if len(months) == 0 {
		return nil
	}
	args := map[string]any{"From": m.monthLabel(months[0]), "To": m.monthLabel(months[len(months)-1]), "Currency": m.spending.Base}
	text := m.loc.T("report_range", args)
	if m.customRange() {
		text = m.loc.T("report_range_chosen", args)
	}
	lines := wrapped(" "+text, iw, dim)
	for _, c := range missing {
		lines = append(lines, wrapped(" "+m.loc.T("budget_no_rate", map[string]any{"From": c, "To": m.spending.Base}), iw, warn)...)
	}
	return lines
}

// reportsBody is the Reports screen.
func (m *Model) reportsBody(l layout) string {
	switch m.report {
	case reportCashFlow:
		return m.cashFlowBody(l)
	case reportPayees:
		return m.payeesBody(l)
	case reportNetWorth:
		return m.netWorthBody(l)
	}
	entries := m.spendEntries()
	title := m.loc.T("report_spending", nil)
	if len(entries) == 0 {
		lines := append(m.reportRange(m.width-2, m.spending.Months, m.spending.Missing), "", dim.Render(" "+m.loc.T("report_nothing", nil)))
		return box(title, lines, m.width, l.avail, true, "")
	}
	iw := l.leftW - 2
	head := m.reportRange(iw, m.spending.Months, m.spending.Missing)
	h := max(l.avail-2-len(head), 1)
	m.repList.clamp(len(entries), h)

	cells := min(len(m.spending.Months), sparkCells)
	nameW := max(iw-1-cells-1-reportTotalW, 6)
	lines := head
	for i := m.repList.offset; i < min(m.repList.offset+h, len(entries)); i++ {
		e := entries[i]
		line := " " + fit(e.name, nameW-1) + " " + sparkline(bucketed(minors(e.months), sparkCells)) + " " + fitRight(m.loc.Amount(e.total), reportTotalW)
		if i == m.repList.cursor {
			line = selFocused.Render(fit(line, iw))
		} else {
			line = fit(line, iw)
		}
		lines = append(lines, line)
	}
	left := box(title, lines, l.leftW, l.avail, true, m.loc.T("position_of", map[string]any{"N": m.repList.cursor + 1, "Total": len(entries)}))
	if !l.two {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, m.reportDetail(l, entries[m.repList.cursor]))
}

// reportDetail is the months of one entry as bars, with what they come to.
func (m *Model) reportDetail(l layout, e spendEntry) string {
	iw := l.rightW - 2
	labelW := 1
	for _, month := range m.spending.Months {
		labelW = max(labelW, lipgloss.Width(m.monthLabel(month)))
	}
	labelW = min(labelW, 12)
	barW := max(iw-labelW-1-1-reportAmountW, 4)

	top := int64(0)
	for _, v := range e.months {
		top = max(top, max(v.Minor, -v.Minor))
	}
	// A range too long for the pane shows its newest months, and says how many are left
	// out; the total and the average stay in view.
	first := 0
	var lines []string
	if room := l.avail - 2 - 3; len(m.spending.Months) > room && room >= 1 { // 3: the rule, the total, the average
		left := func(shown int) []string {
			return wrapped(m.loc.T("report_earlier", map[string]any{"Count": len(m.spending.Months) - shown}), iw, dim)
		}
		shown := max(room-len(left(room-1)), 0)
		lines = left(shown)
		first = len(m.spending.Months) - shown
	}
	for i := first; i < len(m.spending.Months); i++ {
		month, v := m.spending.Months[i], e.months[i]
		b := bar(v.Minor, top, barW)
		lines = append(lines, dim.Render(fit(m.monthLabel(month), labelW))+" "+accent.Render(b)+strings.Repeat(" ", barW-lipgloss.Width(b))+" "+fitRight(m.loc.Amount(v), reportAmountW))
	}
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	lines = append(lines, leftRight(m.loc.T("report_total", nil), bold.Render(m.loc.Money(e.total)), iw))
	n := int64(len(m.spending.Months))
	lines = append(lines, leftRight(m.loc.T("report_per_month", nil), m.loc.Money(ledger.Money{Minor: e.total.Minor / n, Currency: e.total.Currency}), iw))
	if ih := l.avail - 2; ih > 1 && len(lines) > ih {
		lines = append(lines[:ih-1:ih-1], dim.Render(m.loc.T("more_lines", map[string]any{"Count": len(lines) - ih + 1})))
	}
	return box(e.name, lines, l.rightW, l.avail, false, "")
}
