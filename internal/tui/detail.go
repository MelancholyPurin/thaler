package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// appName and banner are the only places the product name appears in the
// interface.
const appName = "thaler"

var banner = []string{
	"╺┳╸ ╻ ╻ ┏━┓ ╻   ┏━╸ ┏━┓",
	" ┃  ┣━┫ ┣━┫ ┃   ┣╸  ┣┳┛",
	" ╹  ╹ ╹ ╹ ╹ ┗━╸ ┗━╸ ╹┗╸",
}

// rightPane shows details for whatever is selected in the focused panel.
func (m *Model) rightPane(l layout) string {
	iw := l.rightW - 2
	var title string
	var lines []string
	switch {
	case m.screen == screenBudget:
		title, lines = "Budget · "+periodLabel(m.period), m.budgetDetail(iw)
	case m.focus == paneAccounts:
		title, lines = m.accountDetail(iw)
	case m.focus == paneTransactions:
		title, lines = m.transactionDetail(iw)
	default:
		title, lines = appName, m.homeDetail(iw)
	}
	if ih := l.avail - 2; ih > 1 && len(lines) > ih {
		lines = append(lines[:ih-1:ih-1], dim.Render(fmt.Sprintf("… %d more lines", len(lines)-ih+1)))
	}
	return box(title, lines, l.rightW, l.avail, false, "")
}

// kv renders a dim key and a value, with the value wrapped under itself.
func kv(key, value string, keyW, w int) []string {
	valueW := max(w-keyW-1, 8)
	var out []string
	for i, part := range strings.Split(ansi.Wrap(value, valueW, ""), "\n") {
		k := strings.Repeat(" ", keyW)
		if i == 0 {
			k = dim.Render(fit(key, keyW))
		}
		out = append(out, k+" "+part)
	}
	return out
}

// ---- home -------------------------------------------------------------------------

func (m *Model) homeDetail(w int) []string {
	var lines []string
	lines = append(lines, "")
	for _, b := range banner {
		lines = append(lines, centered(accent.Render(b), w))
	}
	lines = append(lines, "", centered(dim.Render("local-first personal finance"), w), "")

	netText := m.netWorth.Total.String()
	if len(m.netWorth.Missing) > 0 {
		netText = "≈ " + netText
	}
	uncat := m.uncategorizedCount()
	uncatText := fmt.Sprint(uncat)
	if uncat > 0 {
		uncatText = warn.Render(uncatText + "  ●  press 3, then u to review")
	}
	span := "—"
	if n := len(m.rows); n > 0 {
		span = m.rows[n-1].tx.Date + " → " + m.rows[0].tx.Date
	}
	const keyW = 15
	pad := strings.Repeat(" ", max(0, (w-44)/2))
	add := func(key, value string) {
		for _, line := range kv(key, value, keyW, w-len(pad)) {
			lines = append(lines, pad+line)
		}
	}
	add("Net worth", bold.Render(netText))
	if len(m.netWorth.Missing) > 0 {
		codes := make([]string, len(m.netWorth.Missing))
		for i, c := range m.netWorth.Missing {
			codes[i] = string(c)
		}
		add("", warn.Render("no rate for "+strings.Join(codes, ", ")+"; add one with: thaler cli rate set "+codes[0]+" "+string(m.netWorth.Base)+" RATE"))
	}
	add("Accounts", fmt.Sprint(len(m.accounts)))
	add("Transactions", fmt.Sprint(len(m.rows)))
	add("Period", span)
	add("Uncategorized", uncatText)
	add("Base currency", string(m.netWorth.Base))
	lines = append(lines, "", centered(dim.Render("? for keys · 2 accounts · 3 transactions"), w))
	return lines
}

// ---- accounts ----------------------------------------------------------------------

func (m *Model) accountDetail(w int) (string, []string) {
	const keyW = 15
	var lines []string
	if m.scopeAccount() == "" {
		lines = append(lines, bold.Render("All accounts"), "")
		for _, e := range m.accounts {
			lines = append(lines, leftRight(e.account.Name, e.balance.String(), w))
		}
		lines = append(lines, "")
		netText := m.netWorth.Total.String()
		if len(m.netWorth.Missing) > 0 {
			netText = "≈ " + netText
		}
		lines = append(lines, kv("Net worth", bold.Render(netText), keyW, w)...)
		for _, c := range m.netWorth.Missing {
			lines = append(lines, warn.Render(fmt.Sprintf("no exchange rate %s → %s: thaler cli rate set %s %s RATE", c, m.netWorth.Base, c, m.netWorth.Base)))
		}
		return "All accounts", lines
	}

	e := m.accounts[m.accts.cursor-1]
	lines = append(lines, bold.Render(e.account.Name), "")
	lines = append(lines, kv("Type", string(e.account.Type), keyW, w)...)
	lines = append(lines, kv("Currency", string(e.account.Currency), keyW, w)...)
	lines = append(lines, kv("Balance", bold.Render(e.balance.String()), keyW, w)...)
	lines = append(lines, kv("Transactions", fmt.Sprint(e.txCount), keyW, w)...)
	if e.uncat > 0 {
		lines = append(lines, kv("Uncategorized", warn.Render(fmt.Sprint(e.uncat)), keyW, w)...)
	}
	if e.last != "" {
		lines = append(lines, kv("Last activity", e.last, keyW, w)...)
	}

	in, out := m.periodFlow(e.account.Name)
	lines = append(lines, "", dim.Render("Money moved · "+periodLabel(m.period)))
	lines = append(lines, kv("In", accent.Render(in.String()), keyW, w)...)
	lines = append(lines, kv("Out", out.String(), keyW, w)...)
	return e.account.Name, lines
}

// periodFlow sums the money that moved into and out of an account in the
// selected period.
func (m *Model) periodFlow(account string) (in, out ledger.Money) {
	cur := ledger.Money{}
	for _, e := range m.accounts {
		if e.account.Name == account {
			cur = ledger.Money{Currency: e.account.Currency}
		}
	}
	in, out = cur, cur
	for _, r := range m.rows {
		if !strings.HasPrefix(r.tx.Date, m.period) {
			continue
		}
		for _, p := range r.tx.Postings {
			if p.Account != account {
				continue
			}
			if p.Amount.Minor > 0 {
				in.Minor += p.Amount.Minor
			} else {
				out.Minor += p.Amount.Minor
			}
		}
	}
	return in, out
}

// ---- transactions ----------------------------------------------------------------------

func (m *Model) transactionDetail(w int) (string, []string) {
	r, ok := m.selected()
	if !ok {
		return "Transaction", []string{dim.Render("Nothing selected.")}
	}
	const keyW = 10
	var lines []string
	lines = append(lines, bold.Render(r.tx.Date)+dim.Render(fmt.Sprintf("   #%d", r.tx.ID)), "")
	if p := clean(r.tx.Payee); p != "" {
		lines = append(lines, strings.Split(ansi.Wrap(bold.Render(p), w, ""), "\n")...)
	}
	if d := clean(r.tx.Description); d != "" {
		lines = append(lines, strings.Split(ansi.Wrap(d, w, ""), "\n")...)
	}

	lines = append(lines, "", dim.Render("Postings"))
	for _, p := range r.tx.Postings {
		name := p.Account
		amount := signed(p.Amount)
		if p.Account == ledger.UncategorizedExpense || p.Account == ledger.UncategorizedIncome {
			name, amount = warn.Render(name), warn.Render(amount)
		}
		lines = append(lines, "  "+leftRight(name, amount, w-2))
	}

	lines = append(lines, "")
	switch {
	case r.simple:
		lines = append(lines, kv("Account", r.source.Account, keyW, w)...)
		lines = append(lines, kv("Category", r.category.Account, keyW, w)...)
		if r.uncategorized {
			lines = append(lines, "", warn.Render("● Uncategorized")+dim.Render(" – press c to choose a category"))
		}
	default:
		lines = append(lines, dim.Render("Transfer or split: shown here, but it cannot be recategorized."))
	}
	return fmt.Sprintf("Transaction #%d", r.tx.ID), lines
}
