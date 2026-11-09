package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// catsFocused says the categories of the Budget have the focus. With one column there is
// no pane of them, so the months keep it, whatever was asked before the terminal shrank.
func (m *Model) catsFocused() bool { return m.budgetCats && m.layout().two }

// monthsStyle is how the month under the cursor is drawn: bright while the months have
// the focus, quiet while the categories do.
func (m *Model) monthsStyle() lipgloss.Style {
	if m.catsFocused() {
		return selUnfocused
	}
	return selFocused
}

// budgetEntries are the categories the Budget lists, in the order it lists them: the
// ones with money set aside, the others that spent, then income.
func (m *Model) budgetEntries() []string {
	var out []string
	if m.hasPlan() {
		for _, l := range m.monthly.Lines {
			if l.Planned() {
				out = append(out, l.Name)
			}
		}
		for _, l := range m.unplanned() {
			out = append(out, l.Name)
		}
	} else {
		for _, l := range m.budget.Expenses {
			out = append(out, l.Name)
		}
	}
	for _, l := range m.budget.Income {
		out = append(out, l.Name)
	}
	return out
}

// budgetIndex is where the selected category is among the entries: the first when it
// is not there (another month does not have it).
func (m *Model) budgetIndex() int {
	for i, name := range m.budgetEntries() {
		if name == m.budgetSel {
			return i
		}
	}
	return 0
}

// budgetSelected is the category selected, or "" if the Budget lists none.
func (m *Model) budgetSelected() string {
	entries := m.budgetEntries()
	if len(entries) == 0 {
		return ""
	}
	return entries[m.budgetIndex()]
}

// budgetWindow shows the lines of the categories pane that fit in h, scrolled so that
// the selected category is in view.
func (m *Model) budgetWindow(lines []string, h int, sel span) []string {
	if h < 1 || len(lines) <= h {
		m.budgetScroll = 0
		return lines
	}
	off := max(0, min(m.budgetScroll, len(lines)-h))
	if sel.from >= 0 {
		if sel.from < off {
			off = sel.from
		}
		if sel.to > off+h {
			off = sel.to - h
		}
	}
	m.budgetScroll = max(0, min(off, len(lines)-h))
	return lines[m.budgetScroll : m.budgetScroll+h]
}

// updateBudget moves through the months list, or, with tab, through the categories; the
// selected month is the period. e sets a target for the month, c copies the month
// before's, and z takes back the last change. enter on a category shows its transactions.
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
	case "tab", "shift+tab":
		if m.layout().two { // with one column there is no pane of categories to move into
			m.budgetCats = !m.budgetCats
			m.budgetSel = m.budgetSelected()
		}
		return nil
	}
	l := m.layout()
	if m.catsFocused() {
		entries := m.budgetEntries()
		switch key {
		case "esc":
			m.budgetCats = false
		case "enter":
			m.openCategory()
		default:
			ls := listState{cursor: m.budgetIndex()}
			if ls.navigate(key, len(entries), max(l.avail-2, 1)) {
				m.budgetSel = entries[ls.cursor]
			}
		}
		return nil
	}
	items := m.budgetItems()
	ls := listState{cursor: m.budgetCursor(), offset: m.budgetTop.offset}
	if ls.navigate(key, len(items), max(l.avail-2, 1)) {
		m.budgetTop.offset = ls.offset
		m.setPeriod(items[ls.cursor])
	}
	return nil
}

// openCategory shows the transactions behind a number of the Budget: the Ledger, narrowed
// to the category selected, in the month on show.
func (m *Model) openCategory() {
	name := m.budgetSelected()
	if name == "" {
		return
	}
	m.onlyCategory, m.onlyUncat = name, false
	m.filter.SetValue("")
	m.accts.cursor = 0
	m.setScreen(screenLedger)
	m.setFocus(paneTransactions)
	m.applyFilter(false)
	m.setStatus(m.loc.T("category_shown", map[string]any{"Count": len(m.view), "Name": m.loc.ShortName(name)}), false)
}

// span is a run of lines of a pane: from is the first, to the one after the last.
type span struct{ from, to int }

var noSpan = span{-1, -1}

// budgetDetail is the right pane of the Budget screen, and the lines the selected
// category takes in it (none unless the categories have the focus).
func (m *Model) budgetDetail(w int) ([]string, span) {
	sel, at := "", noSpan
	if m.catsFocused() {
		sel = m.budgetSelected()
	}
	var lines []string
	// add appends the rows of a section; where holds the lines the selected one took in it.
	add := func(rows []string, where span) {
		if where.from >= 0 {
			at = span{len(lines) + where.from, len(lines) + where.to}
		}
		lines = append(lines, rows...)
	}
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
	if m.monthly.HasPlannedIncome {
		unassigned := m.loc.Money(m.monthly.Unassigned)
		if m.monthly.Unassigned.Minor < 0 {
			unassigned = warn.Render(unassigned)
		}
		lines = append(lines, leftRight(m.loc.T("budget_planned_income", nil), m.loc.Money(m.monthly.PlannedIncomeTotal), w))
		lines = append(lines, leftRight(m.loc.T("budget_unassigned", nil), bold.Render(unassigned), w))
	}

	switch {
	case m.hasPlan():
		lines = append(lines, "", dim.Render(m.loc.T("budget_section_targets", nil)))
		add(m.targetRows(w, sel))
		if rest := m.unplanned(); len(rest) > 0 {
			lines = append(lines, "", dim.Render(m.loc.T("budget_section_no_target", nil)))
			add(categoryRows(m.loc, rest, m.monthly.SpentTotal, w, false, sel))
		}
	default:
		lines = append(lines, "", dim.Render(m.loc.T("budget_spending", nil)))
		add(categoryRows(m.loc, m.budget.Expenses, m.budget.ExpenseTotal, w, false, sel))
		if len(m.budget.Expenses) == 0 {
			lines = append(lines, dim.Render("  "+m.loc.T("budget_nothing_spent", nil)))
		}
	}
	switch {
	case m.monthly.HasPlannedIncome:
		lines = append(lines, "", dim.Render(m.loc.T("budget_income", nil)))
		add(m.plannedIncomeRows(w, sel))
		var rest []core.BudgetLine
		for _, l := range m.monthly.Income {
			if !l.HasPlan {
				rest = append(rest, core.BudgetLine{Name: l.Name, Amount: l.Received})
			}
		}
		add(categoryRows(m.loc, rest, m.monthly.IncomeTotal, w, true, sel))
	case len(m.budget.Income) > 0:
		lines = append(lines, "", dim.Render(m.loc.T("budget_income", nil)))
		add(categoryRows(m.loc, m.budget.Income, m.budget.IncomeTotal, w, true, sel))
	}
	if m.period != "" && !m.hasPlan() {
		lines = append(lines, "")
		lines = append(lines, wrapped(m.loc.T("budget_targets_hint", nil), w, dim)...)
	}
	return lines, at
}

// hasPlan says whether the month on show has anything set aside to measure against: a
// target, or something carried in.
func (m *Model) hasPlan() bool { return hasPlan(m.monthly) }

func hasPlan(b core.MonthBudget) bool {
	for _, l := range b.Lines {
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
func (m *Model) targetRows(w int, sel string) ([]string, span) {
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
	at := noSpan
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
		first := len(out)
		out = append(out, marked(l.Name == sel)+fit(m.loc.ShortName(l.Name), nameW)+" "+fitRight(m.loc.Amount(l.Spent), amtW)+" "+bar)
		for _, line := range wrapped(strings.Join(parts, " · "), w-4, style) {
			out = append(out, "    "+line)
		}
		if l.Name == sel {
			at = span{first, len(out)}
		}
	}
	return out, at
}

// plannedIncomeRows draws the income categories with a plan, two lines each like the
// spending ones: the name, what has come in and a bar of how much of the plan that is,
// then the plan and what is still to come, or by how much it passed the plan.
func (m *Model) plannedIncomeRows(w int, sel string) ([]string, span) {
	const barW = 10
	var planned []core.MonthIncomeLine
	amtW := 0
	for _, l := range m.monthly.Income {
		if l.HasPlan {
			planned = append(planned, l)
			amtW = max(amtW, len(m.loc.Amount(l.Received)))
		}
	}
	nameW := max(8, w-amtW-barW-4)
	var out []string
	at := noSpan
	for _, l := range planned {
		filled := barW
		if l.Planned.Minor > 0 {
			filled = int((max(l.Received.Minor, 0)*barW + l.Planned.Minor/2) / l.Planned.Minor)
		}
		filled = max(0, min(barW, filled))
		if filled == 0 && l.Received.Minor > 0 {
			filled = 1
		}
		bar := accent.Render(strings.Repeat("█", filled) + strings.Repeat("░", barW-filled))
		parts := []string{m.loc.T("budget_of", map[string]any{"Target": m.loc.Amount(l.Planned)})}
		if rest := l.Planned.Minor - l.Received.Minor; rest > 0 {
			parts = append(parts, m.loc.T("budget_to_come", map[string]any{"Amount": m.loc.Amount(ledger.Money{Minor: rest, Currency: l.Planned.Currency})}))
		} else if rest < 0 {
			parts = append(parts, m.loc.T("budget_above_plan", map[string]any{"Amount": m.loc.Amount(ledger.Money{Minor: -rest, Currency: l.Planned.Currency})}))
		}
		first := len(out)
		out = append(out, marked(l.Name == sel)+fit(m.loc.ShortName(l.Name), nameW)+" "+fitRight(m.loc.Amount(l.Received), amtW)+" "+bar)
		for _, line := range wrapped(strings.Join(parts, " · "), w-4, dim) {
			out = append(out, "    "+line)
		}
		if l.Name == sel {
			at = span{first, len(out)}
		}
	}
	return out, at
}

// marked is the start of a category's row: two blanks, or the mark of the one selected.
func marked(selected bool) string {
	if selected {
		return accent.Render("▸") + " "
	}
	return "  "
}

// categoryRows draws one line per category: name, amount, share bar.
func categoryRows(loc *i18n.Localizer, list []core.BudgetLine, total ledger.Money, w int, isIncome bool, sel string) ([]string, span) {
	const barW = 10
	amtW := 0
	for _, l := range list {
		amtW = max(amtW, len(loc.Amount(l.Amount)))
	}
	nameW := max(8, w-amtW-barW-4)
	var out []string
	at := noSpan
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
		if l.Name == sel {
			at = span{len(out), len(out) + 1}
		}
		out = append(out, marked(l.Name == sel)+fit(name, nameW)+" "+fitRight(loc.Amount(l.Amount), amtW)+" "+style.Render(bar))
	}
	return out, at
}
