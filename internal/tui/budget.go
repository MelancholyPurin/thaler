package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// refreshBudget loads the Budget screen's figures for the current period: where
// the money came from and where it went, and, for a month, how that stands against
// the month's targets. Envelopes come later.
func (m *Model) refreshBudget() error {
	from, to := periodRange(m.period)
	rateDate := m.now().Format(ledger.DateLayout)
	report, err := m.svc.BudgetReport(m.ctx, core.BudgetRequest{From: from, To: to, RateDate: rateDate})
	if err != nil {
		return err
	}
	m.budget = report
	m.monthly = core.MonthBudget{}
	if m.period == "" { // targets belong to a month
		return nil
	}
	m.monthly, err = m.svc.MonthBudget(m.ctx, core.MonthBudgetRequest{Month: m.period, RateDate: rateDate})
	return err
}

// budgetItems are the entries of the months list: all time, then each month.
func (m *Model) budgetItems() []string { return append([]string{""}, m.months...) }

func (m *Model) budgetCursor() int {
	for i, p := range m.budgetItems() {
		if p == m.period {
			return i
		}
	}
	return 0
}

// updateBudget moves through the months list; the selected month is the period.
// e sets a target for the month, c copies the month before's, and z takes back the last change.
func (m *Model) updateBudget(key string) tea.Cmd {
	switch key {
	case "e":
		return m.openTarget()
	case "c":
		m.copyTargets()
		return nil
	case "z":
		m.undoLast()
		return nil
	}
	items := m.budgetItems()
	l := m.layout()
	ls := listState{cursor: m.budgetCursor(), offset: m.budgetTop.offset}
	if ls.navigate(key, len(items), max(l.avail-2, 1)) {
		m.budgetTop.offset = ls.offset
		m.setPeriod(items[ls.cursor])
	}
	return nil
}

func (m *Model) budgetDetail(w int) []string {
	var lines []string
	net := ledger.Money{Minor: m.budget.IncomeTotal.Minor - m.budget.ExpenseTotal.Minor, Currency: m.netWorth.Base}
	lines = append(lines, bold.Render(m.periodLabel(m.period)), "")
	lines = append(lines, leftRight(m.loc.T("budget_income", nil), accent.Render(m.loc.Money(m.budget.IncomeTotal)), w))
	lines = append(lines, leftRight(m.loc.T("budget_spent", nil), m.loc.Money(m.budget.ExpenseTotal), w))
	netText := m.loc.Money(net)
	if net.Minor < 0 {
		netText = warn.Render(netText)
	}
	lines = append(lines, leftRight(m.loc.T("budget_net", nil), bold.Render(netText), w))
	missing := m.budget.Missing
	if m.period != "" {
		missing = m.monthly.Missing // the same, and the currencies of targets set before the base changed
	}
	for _, c := range missing {
		lines = append(lines, wrapped(m.loc.T("budget_no_rate", map[string]any{"From": c, "To": m.netWorth.Base}), w, warn)...)
	}

	if m.hasPlan() {
		lines = append(lines, leftRight(m.loc.T("budget_target", nil), m.loc.Money(m.monthly.TargetTotal), w))
		if m.monthly.CarriedTotal.Minor != 0 {
			lines = append(lines, leftRight(m.loc.T("budget_carried_total", nil), m.signed(m.monthly.CarriedTotal), w))
		}
		leftText := m.loc.Money(m.monthly.LeftTotal)
		if m.monthly.LeftTotal.Minor < 0 {
			leftText = warn.Render(leftText)
		}
		lines = append(lines, leftRight(m.loc.T("budget_left", nil), bold.Render(leftText), w))
	}

	switch {
	case m.hasPlan():
		lines = append(lines, "", dim.Render(m.loc.T("budget_section_targets", nil)))
		lines = append(lines, m.targetRows(w)...)
		if rest := m.unplanned(); len(rest) > 0 {
			lines = append(lines, "", dim.Render(m.loc.T("budget_section_no_target", nil)))
			lines = append(lines, categoryRows(m.loc, rest, m.monthly.SpentTotal, w, false)...)
		}
	default:
		lines = append(lines, "", dim.Render(m.loc.T("budget_spending", nil)))
		lines = append(lines, categoryRows(m.loc, m.budget.Expenses, m.budget.ExpenseTotal, w, false)...)
		if len(m.budget.Expenses) == 0 {
			lines = append(lines, dim.Render("  "+m.loc.T("budget_nothing_spent", nil)))
		}
	}
	if len(m.budget.Income) > 0 {
		lines = append(lines, "", dim.Render(m.loc.T("budget_income", nil)))
		lines = append(lines, categoryRows(m.loc, m.budget.Income, m.budget.IncomeTotal, w, true)...)
	}
	if m.period != "" && !m.hasPlan() {
		lines = append(lines, "")
		lines = append(lines, wrapped(m.loc.T("budget_targets_hint", nil), w, dim)...)
	}
	return lines
}

// hasPlan says whether the month on show has anything set aside to measure against: a
// target, or something carried in.
func (m *Model) hasPlan() bool {
	for _, l := range m.monthly.Lines {
		if l.Planned() {
			return true
		}
	}
	return false
}

// unplanned are the categories that spent something in the month with nothing set aside.
func (m *Model) unplanned() []core.BudgetLine {
	var out []core.BudgetLine
	for _, l := range m.monthly.Lines {
		if !l.Planned() && l.Spent.Minor != 0 {
			out = append(out, core.BudgetLine{Name: l.Name, Amount: l.Spent})
		}
	}
	return out
}

// signed writes an amount with a "+" when it is positive, and the currency.
func (m *Model) signed(a ledger.Money) string {
	if a.Minor > 0 {
		return "+" + m.loc.Money(a)
	}
	return m.loc.Money(a)
}

// targetRows draws the categories with money set aside, two lines each: the name, what
// was spent and a bar of how much of what is set aside that is, then the target, what
// was carried in and what is left, or by how much it was passed (in red, with a full red
// bar).
func (m *Model) targetRows(w int) []string {
	const barW = 10
	var planned []core.MonthBudgetLine
	amtW := 0
	for _, l := range m.monthly.Lines {
		if l.Planned() {
			planned = append(planned, l)
			amtW = max(amtW, len(m.loc.Amount(l.Spent)))
		}
	}
	nameW := max(8, w-amtW-barW-4)
	var out []string
	for _, l := range planned {
		over := l.Left.Minor < 0
		available := l.Target.Minor + l.Carried.Minor
		filled := barW
		if available > 0 {
			filled = int((max(l.Spent.Minor, 0)*barW + available/2) / available)
		} else if l.Spent.Minor <= 0 {
			filled = 0
		}
		filled = max(0, min(barW, filled))
		if filled == 0 && l.Spent.Minor > 0 {
			filled = 1
		}
		bar := accent.Render(strings.Repeat("█", filled) + strings.Repeat("░", barW-filled))
		style := dim
		if over {
			bar, style = warn.Render(strings.Repeat("█", barW)), warn
		}
		var parts []string
		if l.HasTarget {
			parts = append(parts, m.loc.T("budget_of", map[string]any{"Target": m.loc.Amount(l.Target)}))
		}
		if l.Carried.Minor != 0 {
			carried := m.loc.Amount(l.Carried)
			if l.Carried.Minor > 0 {
				carried = "+" + carried
			}
			parts = append(parts, m.loc.T("budget_carried", map[string]any{"Amount": carried}))
		}
		if over {
			parts = append(parts, m.loc.T("budget_over_part", map[string]any{"Amount": m.loc.Amount(l.Left.Neg())}))
		} else {
			parts = append(parts, m.loc.T("budget_left_part", map[string]any{"Amount": m.loc.Amount(l.Left)}))
		}
		out = append(out, "  "+fit(m.loc.ShortName(l.Name), nameW)+" "+fitRight(m.loc.Amount(l.Spent), amtW)+" "+bar)
		for _, line := range wrapped(strings.Join(parts, " · "), w-4, style) {
			out = append(out, "    "+line)
		}
	}
	return out
}

// categoryRows draws one line per category: name, amount, share bar.
func categoryRows(loc *i18n.Localizer, list []core.BudgetLine, total ledger.Money, w int, isIncome bool) []string {
	const barW = 10
	amtW := 0
	for _, l := range list {
		amtW = max(amtW, len(loc.Amount(l.Amount)))
	}
	nameW := max(8, w-amtW-barW-4)
	var out []string
	for _, l := range list {
		filled := 0
		if total.Minor > 0 {
			filled = int((l.Amount.Minor*barW + total.Minor/2) / total.Minor)
		}
		filled = max(0, min(barW, filled))
		if filled == 0 && l.Amount.Minor > 0 {
			filled = 1
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
		name := loc.ShortName(l.Name)
		if l.Name == ledger.UncategorizedExpense || l.Name == ledger.UncategorizedIncome {
			name = warn.Render(name)
		}
		style := dim
		if isIncome {
			style = accent
		}
		out = append(out, "  "+fit(name, nameW)+" "+fitRight(loc.Amount(l.Amount), amtW)+" "+style.Render(bar))
	}
	return out
}
