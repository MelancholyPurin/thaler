package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// budgetLine is the total of one category over the selected period, converted
// into the base currency.
type budgetLine struct {
	name   string
	amount ledger.Money
}

// budgetData is what the Budget screen shows: where the money came from and
// where it went in the selected period. Targets and envelopes come later.
type budgetData struct {
	income, expenses          []budgetLine // largest first
	incomeTotal, expenseTotal ledger.Money
	missing                   []ledger.Currency // no exchange rate, so left out
}

// refreshBudget recomputes the Budget screen's figures for the current period.
func (m *Model) refreshBudget() error {
	from, to := periodRange(m.period)
	totals, err := m.st.CategoryTotals(m.ctx, from, to)
	if err != nil {
		return err
	}
	book, err := m.st.RateBook(m.ctx)
	if err != nil {
		return err
	}
	base := m.netWorth.Base
	rateDate := m.now().Format(ledger.DateLayout)
	if to != "" && to < rateDate {
		rateDate = to
	}

	b := budgetData{
		incomeTotal:  ledger.Money{Currency: base},
		expenseTotal: ledger.Money{Currency: base},
	}
	byName := make(map[string]*budgetLine)
	missing := make(map[ledger.Currency]bool)
	for _, t := range totals {
		amount, ok := book.Convert(t.Amount, base, rateDate)
		if !ok {
			missing[t.Amount.Currency] = true
			continue
		}
		if t.Type == ledger.Income {
			amount = amount.Neg() // income is stored as credits
		}
		line := byName[t.Account]
		if line == nil {
			line = &budgetLine{name: t.Account, amount: ledger.Money{Currency: base}}
			byName[t.Account] = line
		}
		line.amount.Minor += amount.Minor
		if t.Type == ledger.Income {
			b.incomeTotal.Minor += amount.Minor
		} else {
			b.expenseTotal.Minor += amount.Minor
		}
	}
	for _, line := range byName {
		if typ, _ := ledger.TypeOf(line.name); typ == ledger.Income {
			b.income = append(b.income, *line)
		} else {
			b.expenses = append(b.expenses, *line)
		}
	}
	for _, list := range [][]budgetLine{b.income, b.expenses} {
		sort.Slice(list, func(i, j int) bool {
			if list[i].amount.Minor != list[j].amount.Minor {
				return list[i].amount.Minor > list[j].amount.Minor
			}
			return list[i].name < list[j].name
		})
	}
	for c := range missing {
		b.missing = append(b.missing, c)
	}
	sort.Slice(b.missing, func(i, j int) bool { return b.missing[i] < b.missing[j] })
	m.budget = b
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
	net := ledger.Money{Minor: m.budget.incomeTotal.Minor - m.budget.expenseTotal.Minor, Currency: m.netWorth.Base}
	lines = append(lines, bold.Render(periodLabel(m.period)), "")
	lines = append(lines, leftRight("Income", accent.Render(m.budget.incomeTotal.String()), w))
	lines = append(lines, leftRight("Spent", m.budget.expenseTotal.String(), w))
	netText := net.String()
	if net.Minor < 0 {
		netText = warn.Render(netText)
	}
	lines = append(lines, leftRight("Net", bold.Render(netText), w))
	for _, c := range m.budget.missing {
		lines = append(lines, warn.Render(fmt.Sprintf("no exchange rate %s → %s: left out. thaler cli rate set %s %s RATE", c, m.netWorth.Base, c, m.netWorth.Base)))
	}

	lines = append(lines, "", dim.Render("Spending"))
	lines = append(lines, categoryRows(m.budget.expenses, m.budget.expenseTotal, w, false)...)
	if len(m.budget.expenses) == 0 {
		lines = append(lines, dim.Render("  nothing spent in this period"))
	}
	if len(m.budget.income) > 0 {
		lines = append(lines, "", dim.Render("Income"))
		lines = append(lines, categoryRows(m.budget.income, m.budget.incomeTotal, w, true)...)
	}
	lines = append(lines, "", dim.Render("Targets and envelopes will appear here as columns next to these figures."))
	return lines
}

// categoryRows draws one line per category: name, amount, share bar.
func categoryRows(list []budgetLine, total ledger.Money, w int, isIncome bool) []string {
	const barW = 12
	amtW := 0
	for _, l := range list {
		amtW = max(amtW, len(l.amount.Decimal()))
	}
	nameW := max(8, w-amtW-barW-4)
	var out []string
	for _, l := range list {
		filled := 0
		if total.Minor > 0 {
			filled = int((l.amount.Minor*barW + total.Minor/2) / total.Minor)
		}
		filled = max(0, min(barW, filled))
		if filled == 0 && l.amount.Minor > 0 {
			filled = 1
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
		name := shortName(l.name)
		if l.name == ledger.UncategorizedExpense || l.name == ledger.UncategorizedIncome {
			name = warn.Render(name)
		}
		style := dim
		if isIncome {
			style = accent
		}
		out = append(out, "  "+fit(name, nameW)+" "+fitRight(l.amount.Decimal(), amtW)+" "+style.Render(bar))
	}
	return out
}
