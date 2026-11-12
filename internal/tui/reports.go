package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The Reports screen. Its first report is the spending of each category over the 12
// months up to the one on show (up to this month, for all time): on the left the
// categories, each with a small chart of its months and its total, on the right the
// months of the one selected as bars. The first entry of the list is all of them.

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

// refreshSpending loads the spending of the 12 months up to the one on show.
func (m *Model) refreshSpending() (err error) {
	m.spending, err = m.svc.SpendingReport(m.ctx, core.SpendingRequest{To: m.period, RateDate: m.now().Format(ledger.DateLayout)})
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

// updateReports handles the keys of the Reports screen: moving through the categories.
func (m *Model) updateReports(key string) {
	m.repList.navigate(key, len(m.spendEntries()), max(1, m.layout().avail-2-m.reportHeader(m.layout().leftW-2)))
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
func (m *Model) reportHeader(iw int) int { return len(m.reportRange(iw)) }

// reportRange are the lines above the list: the months it covers, and the currencies
// that could not be counted. They are here, not with the bars, which a short terminal
// cuts.
func (m *Model) reportRange(iw int) []string {
	months := m.spending.Months
	if len(months) == 0 {
		return nil
	}
	text := m.loc.T("report_range", map[string]any{"From": m.monthLabel(months[0]), "To": m.monthLabel(months[len(months)-1]), "Currency": m.spending.Base})
	lines := wrapped(" "+text, iw, dim)
	for _, c := range m.spending.Missing {
		lines = append(lines, wrapped(" "+m.loc.T("budget_no_rate", map[string]any{"From": c, "To": m.spending.Base}), iw, warn)...)
	}
	return lines
}

// reportsBody is the Reports screen.
func (m *Model) reportsBody(l layout) string {
	entries := m.spendEntries()
	title := m.loc.T("report_spending", nil)
	if len(entries) == 0 {
		lines := append(m.reportRange(m.width-2), "", dim.Render(" "+m.loc.T("report_nothing", nil)))
		return box(title, lines, m.width, l.avail, true, "")
	}
	iw := l.leftW - 2
	head := m.reportRange(iw)
	h := max(l.avail-2-len(head), 1)
	m.repList.clamp(len(entries), h)

	nameW := max(iw-1-len(m.spending.Months)-1-reportTotalW, 6)
	lines := head
	for i := m.repList.offset; i < min(m.repList.offset+h, len(entries)); i++ {
		e := entries[i]
		line := " " + fit(e.name, nameW-1) + " " + sparkline(minors(e.months)) + " " + fitRight(m.loc.Amount(e.total), reportTotalW)
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
	var lines []string
	for i, month := range m.spending.Months {
		v := e.months[i]
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
