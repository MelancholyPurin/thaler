package tui

import (
	"time"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The balances view of the Forecast (tab) shows, for each asset and liability
// account, what it holds today and what it would hold at the end of each of the next
// weeks if every schedule were confirmed. The figures are the service's Projection;
// the view only cuts it into weeks. A balance under the account's minimum is marked
// with a !, as a date nobody answered is on the schedules.

// balanceWeeks is how many weeks the view shows: 13, a little over the 90 days the
// Forecast looks ahead for a balance that falls under its minimum.
const balanceWeeks = 13

// Widths of the two amount columns, wide enough for -12,345.67 EUR, with the mark of a balance under the minimum.
const (
	balanceChangeW = 14
	balanceAmountW = 17
)

// refreshProjection loads the projected balances for the weeks of the balances view.
func (m *Model) refreshProjection() (err error) {
	upTo := m.now().AddDate(0, 0, 7*balanceWeeks).Format(ledger.DateLayout)
	m.projection, err = m.svc.Projection(m.ctx, upTo)
	return err
}

// weekRow is an account at the end of one week: the date, what the schedules moved in
// the week, and what it holds.
type weekRow struct {
	date    string
	change  ledger.Money
	balance ledger.Money
	moved   bool
}

// weekRows cuts an account's projection into weeks, the first ending a week after from.
func weekRows(a core.ProjectedAccount, from time.Time) []weekRow {
	rows := make([]weekRow, 0, balanceWeeks)
	prev := from.AddDate(0, 0, -1).Format(ledger.DateLayout) // a date nobody answered is on today, in the first week
	for k := 1; k <= balanceWeeks; k++ {
		end := from.AddDate(0, 0, 7*k).Format(ledger.DateLayout)
		row := weekRow{date: end, change: ledger.Money{Currency: a.Start.Currency}, balance: a.BalanceOn(end)}
		for _, d := range a.Days {
			if d.Date > prev && d.Date <= end {
				row.change.Minor += d.Change.Minor
				row.moved = true
			}
		}
		rows = append(rows, row)
		prev = end
	}
	return rows
}

// balanceLines are the lines of the view: for each account its name, what it holds
// today, and a line for each week. An account no schedule moves has one line saying so.
func (m *Model) balanceLines(iw int) []string {
	floor := make(map[string]ledger.Money, len(m.minimums))
	for _, x := range m.minimums {
		floor[x.Account] = x.Amount
	}
	from, err := ledger.ParseDate(m.projection.From)
	if err != nil {
		return nil
	}
	row := func(label, change, balance string) string {
		return "  " + leftRight(label, fitRight(change, balanceChangeW)+"  "+balance, iw-2)
	}
	var lines []string
	for _, a := range m.projection.Accounts {
		if m.fcAccount != "" && a.Name != m.fcAccount {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, accent.Render(fit(m.loc.ShortName(a.Name), iw)))
		lines = append(lines, row(m.loc.T("balances_today", nil), "", fitRight(m.loc.Money(a.Start), balanceAmountW)))
		if len(a.Days) == 0 {
			lines = append(lines, wrapped("  "+m.loc.T("balances_still", nil), iw, dim)...)
			continue
		}
		limit, hasLimit := floor[a.Name]
		for _, w := range weekRows(a, from) {
			change := ""
			if w.moved {
				change = m.loc.Signed(w.change)
			}
			balance := fitRight(m.loc.Money(w.balance), balanceAmountW)
			if hasLimit && w.balance.Minor < limit.Minor { // said in a mark as well as in colour
				balance = warn.Render(fitRight("! "+m.loc.Money(w.balance), balanceAmountW))
			}
			lines = append(lines, row(m.loc.StoredDate(w.date), change, balance))
		}
	}
	return lines
}

// balancesBody is the balances view: the warnings first, as on the schedules, then
// the accounts, scrolled by line.
func (m *Model) balancesBody(l layout) string {
	iw := m.width - 2
	head := m.lowLines(iw)
	if len(head) > 0 {
		head = append(head, "")
	}
	lines := m.balanceLines(iw)
	if len(lines) == 0 {
		lines = wrapped(" "+m.loc.T("balances_empty", nil), iw, dim)
		return box(m.scoped(m.loc.T("panel_balances", nil)), append(head, lines...), m.width, l.avail, true, "")
	}
	head = append(head, leftRight("", dim.Render(fitRight(m.loc.T("label_change", nil), balanceChangeW)+"  "+fitRight(m.loc.T("label_balance", nil), balanceAmountW)), iw))
	h := max(1, l.avail-2-len(head))
	m.balTop = max(0, min(m.balTop, len(lines)-h))
	shown := lines[m.balTop:min(m.balTop+h, len(lines))]
	footer := m.loc.T("balances_footer", nil)
	return box(m.scoped(m.loc.T("panel_balances", nil)), append(head, shown...), m.width, l.avail, true, footer)
}

// updateBalances handles the keys of the balances view: scrolling by line.
func (m *Model) updateBalances(key string) {
	page := max(1, m.layout().avail-4)
	switch key {
	case "up", "k":
		m.balTop--
	case "down", "j":
		m.balTop++
	case "pgup", "ctrl+u":
		m.balTop -= page
	case "pgdown", "ctrl+d":
		m.balTop += page
	case "home", "g":
		m.balTop = 0
	case "end", "G":
		m.balTop = 1 << 20 // balancesBody holds it at the end
	}
	m.balTop = max(m.balTop, 0)
}
