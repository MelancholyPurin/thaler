package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The export popup (e on the Reports) saves the report on show as a CSV file for a
// spreadsheet, for the months it covers. The file is asked from the service again, so
// that it holds every payee, not the first hundred the screen lists. A file that is
// already there is never overwritten: say another name.

type exportForm struct {
	kind reportKind // the report being saved
	path textinput.Model
	err  string
}

// reportName is the title of a report, as the screen shows it.
func (m *Model) reportName(k reportKind) string {
	switch k {
	case reportCashFlow:
		return m.loc.T("report_cashflow", nil)
	case reportPayees:
		return m.loc.T("report_payees", nil)
	case reportNetWorth:
		return m.loc.T("report_networth", nil)
	}
	return m.loc.T("report_spending", nil)
}

// exportSlug names a report in a file name; it is not translated.
func exportSlug(k reportKind) string {
	switch k {
	case reportCashFlow:
		return "cashflow"
	case reportPayees:
		return "payees"
	case reportNetWorth:
		return "networth"
	}
	return "categories"
}

// shownMonths are the first and the last month of the report on show.
func (m *Model) shownMonths() (from, to string) {
	months := m.spending.Months
	if len(months) == 0 {
		return "", ""
	}
	return months[0], months[len(months)-1]
}

// exportFileName is the name offered: thaler-categories-2025-11-2026-10.csv.
func (m *Model) exportFileName() string {
	from, to := m.shownMonths()
	return "thaler-" + exportSlug(m.report) + "-" + from + "-" + to + ".csv"
}

func (m *Model) exportWidth() int { return min(max(m.width-4, 30), 76) }

// exportInputWidth is the width of the path: the popup less its border, the label, a
// space and the cursor cell.
func (m *Model) exportInputWidth() int { return max(8, m.exportWidth()-2-backupLabelWidth-1-1) }

// openExport asks where to save the report on show.
func (m *Model) openExport() tea.Cmd {
	f := &exportForm{kind: m.report, path: newInput("")}
	f.path.CharLimit = 0 // paths are long; set before the value, or it is cut at 80
	f.path.Width = m.exportInputWidth()
	f.path.SetValue(m.exportFileName())
	m.exp, m.mode = f, modeExport
	return f.path.Focus()
}

func (m *Model) updateExport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.exp
	switch msg.String() {
	case "esc":
		m.mode, m.exp = modeList, nil
		return m, nil
	case "enter", "ctrl+s":
		m.saveExport()
		return m, nil
	}
	var cmd tea.Cmd
	f.path, cmd = f.path.Update(msg)
	f.err = ""
	return m, cmd
}

// exportTarget turns what was typed into the file to write: a folder gets the usual
// name inside it.
func (m *Model) exportTarget(typed string) (string, error) {
	if strings.TrimSpace(typed) == "" {
		return "", coded.New("export_say_where", nil)
	}
	dest, err := resolvePath(typed)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		dest = filepath.Join(dest, m.exportFileName())
	}
	return dest, nil
}

// reportCSV asks the service for the report on show, for the months it covers, and
// writes it as CSV. It also says which currencies the report left out.
func (m *Model) reportCSV(kind reportKind) (data []byte, missing []ledger.Currency, err error) {
	from, to := m.reportMonths()
	rateDate := m.now().Format(ledger.DateLayout)
	switch kind {
	case reportCashFlow:
		r, err := m.svc.CashFlowReport(m.ctx, core.CashFlowRequest{From: from, To: to, RateDate: rateDate})
		if err != nil {
			return nil, nil, err
		}
		data, err = core.CashFlowCSV(r)
		return data, r.Missing, err
	case reportPayees:
		r, err := m.svc.PayeeReport(m.ctx, core.PayeeRequest{From: from, To: to, RateDate: rateDate})
		if err != nil {
			return nil, nil, err
		}
		data, err = core.PayeesCSV(r)
		return data, r.Missing, err
	case reportNetWorth:
		r, err := m.svc.NetWorthReport(m.ctx, core.NetWorthRequest{From: from, To: to, RateDate: rateDate})
		if err != nil {
			return nil, nil, err
		}
		data, err = core.NetWorthCSV(r)
		return data, r.Missing, err
	}
	r, err := m.svc.SpendingReport(m.ctx, core.SpendingRequest{From: from, To: to, RateDate: rateDate})
	if err != nil {
		return nil, nil, err
	}
	data, err = core.SpendingCSV(r)
	return data, r.Missing, err
}

// saveExport checks where, writes the file and says so.
func (m *Model) saveExport() {
	f := m.exp
	dest, err := m.exportTarget(f.path.Value())
	if err != nil {
		f.err = m.errText(err)
		return
	}
	data, missing, err := m.reportCSV(f.kind)
	if err != nil {
		f.err = m.errText(err)
		return
	}
	if err = core.WriteNewFile(dest, data); err != nil {
		f.err = m.errText(err)
		return
	}
	m.mode, m.exp = modeList, nil
	// The end of the path is what tells the files apart, so it gets what the words leave.
	args := map[string]any{"Report": m.reportName(f.kind), "Path": ""}
	room := max(12, m.width-lipgloss.Width(m.loc.T("export_saved", args))-2)
	args["Path"] = tailFit(dest, room)
	parts := []string{m.loc.T("export_saved", args)}
	for _, c := range missing {
		parts = append(parts, m.loc.T("budget_no_rate", map[string]any{"From": c, "To": m.spending.Base}))
	}
	m.setStatus(m.statusLine(parts), false)
}

func (m *Model) exportPopup() string {
	f := m.exp
	w := m.exportWidth()
	iw := w - 2
	from, to := m.shownMonths()
	label := func(s string) string { return dim.Render(fit("  "+s, backupLabelWidth)) }
	lines := []string{
		label(m.loc.T("label_report", nil)) + " " + fit(m.loc.T("export_what", map[string]any{"Report": m.reportName(f.kind), "From": m.monthLabel(from), "To": m.monthLabel(to)}), iw-backupLabelWidth-1),
		accent.Render(fit("▸ "+m.loc.T("label_save_to", nil), backupLabelWidth)) + " " + f.path.View(),
		dim.Render(strings.Repeat("─", iw)),
	}
	lines = append(lines, wrapped(m.loc.T("export_hint", map[string]any{"Currency": m.spending.Base}), iw, dim)...)
	lines = append(lines, "")
	msg := wrapped(f.err, iw, errStyle)
	for len(msg) < 2 {
		msg = append(msg, "")
	}
	lines = append(lines, msg...)
	return box(m.loc.T("export_title", nil), lines, w, len(lines)+2, true, m.loc.T("export_footer", nil))
}
