package tui

import (
	"strings"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// refreshBudget loads the Budget screen's figures for the current period: where
// the money came from and where it went. Targets and envelopes come later.
func (m *Model) refreshBudget() error {
	from, to := periodRange(m.period)
	report, err := m.svc.BudgetReport(m.ctx, core.BudgetRequest{From: from, To: to, RateDate: m.now().Format(ledger.DateLayout)})
	if err != nil {
		return err
	}
	m.budget = report
	return nil
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
func (m *Model) updateBudget(key string) {
	items := m.budgetItems()
	l := m.layout()
	ls := listState{cursor: m.budgetCursor(), offset: m.budgetTop.offset}
	if ls.navigate(key, len(items), max(l.avail-2, 1)) {
		m.budgetTop.offset = ls.offset
		m.setPeriod(items[ls.cursor])
	}
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
	for _, c := range m.budget.Missing {
		lines = append(lines, wrapped(m.loc.T("budget_no_rate", map[string]any{"From": c, "To": m.netWorth.Base}), w, warn)...)
	}

	lines = append(lines, "", dim.Render(m.loc.T("budget_spending", nil)))
	lines = append(lines, categoryRows(m.loc, m.budget.Expenses, m.budget.ExpenseTotal, w, false)...)
	if len(m.budget.Expenses) == 0 {
		lines = append(lines, dim.Render("  "+m.loc.T("budget_nothing_spent", nil)))
	}
	if len(m.budget.Income) > 0 {
		lines = append(lines, "", dim.Render(m.loc.T("budget_income", nil)))
		lines = append(lines, categoryRows(m.loc, m.budget.Income, m.budget.IncomeTotal, w, true)...)
	}
	lines = append(lines, "")
	lines = append(lines, wrapped(m.loc.T("budget_targets_soon", nil), w, dim)...)
	return lines
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
