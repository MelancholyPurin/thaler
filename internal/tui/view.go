package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/ledger"
)

const dateWidth = 10

// View implements tea.Model.
func (m *Model) View() string {
	l := m.layout()
	var body string
	switch m.screen {
	case screenLedger:
		body = m.ledgerBody(l)
	case screenBudget:
		body = m.budgetBody(l)
	default:
		body = m.placeholderBody(l)
	}
	screen := m.tabStrip() + "\n" + body + "\n" + m.bottomBar()
	switch m.mode {
	case modePick:
		screen = m.centerPopup(screen, m.pickerPopup())
	case modeHelp:
		screen = m.centerPopup(screen, m.helpPopup())
	case modeForm:
		screen = m.centerPopup(screen, m.formPopup())
	case modeConfirm:
		screen = m.centerPopup(screen, m.confirmPopup())
	case modeImport:
		screen = m.centerPopup(screen, m.importPopup())
	}
	return screen
}

func (m *Model) ledgerBody(l layout) string {
	left := strings.Join(m.leftBlocks(l), "\n")
	if !l.two {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, m.rightPane(l))
}

// tabStrip is the top line: the screens on the left, the period on the right.
func (m *Model) tabStrip() string {
	var tabs []string
	for i, name := range screenNames {
		label := " " + name + " "
		if screen(i) == m.screen {
			label = selFocused.Render(label)
		} else {
			label = dim.Render(label)
		}
		tabs = append(tabs, label)
	}
	period := dim.Render("[ ") + bold.Render(periodLabel(m.period)) + dim.Render(" ]")
	return leftRight(strings.Join(tabs, " "), period, m.width)
}

// budgetBody is the Budget screen: months on the left, the figures on the right.
func (m *Model) budgetBody(l layout) string {
	items := m.budgetItems()
	cursor := m.budgetCursor()
	h := max(l.avail-2, 1)
	ls := listState{cursor: cursor, offset: m.budgetTop.offset}
	ls.clamp(len(items), h)

	iw := l.leftW - 2
	var lines []string
	for i := ls.offset; i < len(items) && len(lines) < h; i++ {
		label := periodLabel(items[i])
		if items[i] == m.now().Format(monthLayout) {
			label += dim.Render("  this month")
		}
		line := fit(label, iw)
		if i == cursor {
			line = selFocused.Render(fit(periodLabel(items[i]), iw))
		}
		lines = append(lines, line)
	}
	left := box("[1] Months", lines, l.leftW, l.avail, true, fmt.Sprintf("%d of %d", cursor+1, len(items)))
	if !l.two {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, m.rightPane(l))
}

var plannedScreens = map[screen][]string{
	screenForecast: {
		"Where the balance is heading.",
		"",
		"  • recurring income and bills (rent, salary, subscriptions)",
		"  • projected balance per account, week by week",
		"  • the day you would dip below a chosen amount",
	},
	screenReports: {
		"Where the money went, over time.",
		"",
		"  • spending by category across months",
		"  • net worth history",
		"  • income against expenses",
	},
}

func (m *Model) placeholderBody(l layout) string {
	lines := []string{"", dim.Render("Not built yet."), ""}
	lines = append(lines, plannedScreens[m.screen]...)
	return box(screenNames[m.screen], lines, m.width, l.avail, true, "")
}

func (m *Model) leftBlocks(l layout) []string {
	var blocks []string
	if l.hHome > 0 {
		blocks = append(blocks, m.homePanel(l))
	}
	if l.hAccts > 0 {
		blocks = append(blocks, m.accountsPanel(l))
	}
	if l.hTxs > 0 {
		blocks = append(blocks, m.transactionsPanel(l))
	}
	return blocks
}

func (m *Model) rowStyle(p pane) lipgloss.Style {
	if m.focus == p {
		return selFocused
	}
	return selUnfocused
}

// ---- left column ---------------------------------------------------------------

func (m *Model) homePanel(l layout) string {
	text := appName + "  " + dim.Render("From Purin • https://github.com/MelancholyPurin")
	if m.focus == paneHome {
		text = bold.Render(appName) + "  " + dim.Render("From Purin • https://github.com/MelancholyPurin")
	}
	return box("[1] Home", []string{text}, l.leftW, l.hHome, m.focus == paneHome, "")
}

func (m *Model) accountsPanel(l layout) string {
	iw := l.leftW - 2
	n := len(m.accounts) + 1
	lines := make([]string, 0, l.acctLines)
	for i := m.accts.offset; i < n && len(lines) < l.acctLines; i++ {
		label, value := "All accounts", m.moneyOrDash(m.netWorth.Total, len(m.netWorth.Missing) == 0)
		if i > 0 {
			e := m.accounts[i-1]
			label, value = shortName(e.account.Name), e.balance.String()
		}
		line := leftRight(label, value, iw)
		if i == m.accts.cursor {
			line = m.rowStyle(paneAccounts).Render(fit(line, iw))
		}
		lines = append(lines, line)
	}
	return box("[2] Accounts", lines, l.leftW, l.hAccts, m.focus == paneAccounts, fmt.Sprintf("%d of %d", m.accts.cursor+1, n))
}

// moneyOrDash formats m, or "—" when it is not meaningful.
func (m *Model) moneyOrDash(v ledger.Money, ok bool) string {
	if !ok {
		return "≈ " + v.String()
	}
	return v.String()
}

func (m *Model) transactionsPanel(l layout) string {
	iw := l.leftW - 2
	title := "[3] Transactions"
	switch {
	case m.scopeAccount() != "":
		title += " · " + shortName(m.scopeAccount())
	}
	if m.onlyUncat {
		title += " · uncategorized"
	}
	if q := strings.TrimSpace(m.filter.Value()); q != "" {
		title += " · /" + q
	}

	var lines []string
	if len(m.view) == 0 {
		switch {
		case len(m.rows) == 0:
			lines = []string{dim.Render("No transactions yet."), dim.Render("i imports a bank export · a adds one by hand")}
		case m.period != "" && !m.onlyUncat && m.filter.Value() == "" && m.scopeAccount() == "":
			lines = []string{dim.Render("No transactions in " + periodLabel(m.period) + "."), dim.Render("[ ] change month · T all time")}
		case m.onlyUncat && m.filter.Value() == "" && m.scopeAccount() == "":
			lines = []string{dim.Render("Nothing left to categorize."), dim.Render("Press u to see all transactions.")}
		default:
			lines = []string{dim.Render("No transaction matches.")}
		}
	}
	amtW := 0
	for i := m.txs.offset; i < len(m.view) && i < m.txs.offset+l.txLines; i++ {
		amtW = max(amtW, lipgloss.Width(m.listAmount(m.rows[m.view[i]])))
	}
	for i := m.txs.offset; i < len(m.view) && len(lines) < l.txLines; i++ {
		r := m.rows[m.view[i]]
		marker := " "
		switch {
		case r.uncategorized:
			marker = "●"
		case !r.simple:
			marker = "»"
		}
		payeeW := max(4, iw-dateWidth-amtW-4)
		plain := r.tx.Date + " " + marker + " " + fit(clean(r.tx.Payee), payeeW) + " " + fitRight(m.listAmount(r), amtW)
		if i == m.txs.cursor {
			lines = append(lines, m.rowStyle(paneTransactions).Render(fit(plain, iw)))
			continue
		}
		switch {
		case r.uncategorized:
			marker = warn.Render(marker)
		case !r.simple:
			marker = dim.Render(marker)
		}
		amount := fitRight(m.listAmount(r), amtW)
		if r.source.Amount.Minor > 0 {
			amount = accent.Render(amount)
		}
		lines = append(lines, r.tx.Date+" "+marker+" "+fit(clean(r.tx.Payee), payeeW)+" "+amount)
	}
	footer := ""
	if len(m.view) > 0 {
		footer = fmt.Sprintf("%d of %d", m.txs.cursor+1, len(m.view))
	}
	return box(title, lines, l.leftW, l.hTxs, m.focus == paneTransactions, footer)
}

// listAmount is the compact amount shown in the transaction list: without the
// currency code while everything is in one currency.
func (m *Model) listAmount(r row) string {
	a := r.source.Amount
	s := a.Decimal()
	if a.Minor > 0 {
		s = "+" + s
	}
	if m.multiCurrency {
		s += " " + string(a.Currency)
	}
	return s
}

// ---- bottom bar and popups ---------------------------------------------------------

func (m *Model) bottomBar() string {
	if m.mode == modeFilter {
		return fit(m.filter.View(), m.width)
	}
	if m.status != "" {
		if m.statusIsError {
			return fit(errStyle.Render(m.status), m.width)
		}
		return fit(accent.Render(m.status), m.width)
	}
	var hint string
	switch {
	case m.screen == screenBudget:
		hint = "↑/↓ month · [ ] step · t this month · T all time · ? help · q quit"
	case m.screen != screenLedger:
		hint = "L ledger · B budget · [ ] month · ? help · q quit"
	default:
		hint = m.ledgerHint()
	}
	return fit(dim.Render(hint), m.width)
}

func (m *Model) ledgerHint() string {
	var hint string
	switch m.focus {
	case paneHome:
		hint = "a add · i import · enter transactions · 2 accounts · ? help · q quit"
	case paneAccounts:
		hint = "↑/↓ pick account · a add to it · enter transactions · ? help · q quit"
	default:
		hint = "a add · i import · c categorize · e edit · d delete · z undo · / search · ? help · q quit"
	}
	return hint
}

func (m *Model) centerPopup(screen, popup string) string {
	lines := strings.Split(popup, "\n")
	pw := lipgloss.Width(lines[0])
	x := max(0, (m.width-pw)/2)
	y := max(0, (m.height-1-len(lines))/2)
	return overlay(screen, popup, x, y)
}

func (m *Model) pickerPopup() string {
	r, _ := m.findRow(m.pickTarget)
	w := min(max(m.width-6, 20), 66)
	h := min(max(m.height-4, 5), 14, max(5, 3+len(m.pickItems)))
	title := fmt.Sprintf("Categorize · %s %s", clean(r.tx.Payee), signed(r.source.Amount))

	lines := []string{m.picker.View()}
	listH := h - 2 - 1
	start := 0
	if m.pickCursor >= listH {
		start = m.pickCursor - listH + 1
	}
	iw := w - 2
	for i := start; i < len(m.pickItems) && len(lines)-1 < listH; i++ {
		it := m.pickItems[i]
		line := leftRight(it.name, dim.Render(it.hint), iw)
		if it.hint == "" {
			line = fit(it.name, iw)
		}
		if i == m.pickCursor {
			line = selFocused.Render(fit(it.name+"  "+it.hint, iw))
		}
		lines = append(lines, line)
	}
	if len(m.pickItems) == 0 {
		lines = append(lines, dim.Render("No match. Type a full name like Expenses:Gifts to create one."))
	}
	return box(title, lines, w, h, true, "enter apply · esc cancel")
}

func (m *Model) helpPopup() string {
	rows := [][2]string{
		{"L B F R", "screens: Ledger, Budget, Forecast, Reports (also F1–F4)"},
		{"[  ]", "previous / next month"},
		{"t  T", "this month / all time"},
		{"1 2 3, tab", "Ledger: focus Home, Accounts, Transactions"},
		{"↑ ↓  j k", "move in the focused panel"},
		{"PgUp PgDn  g G", "page, top, bottom"},
		{"enter", "open the next panel / categorize"},
		{"a", "add a transaction (to the selected account)"},
		{"i", "import a bank export (CSV) into an account"},
		{"c  e  d", "categorize / edit / delete the selected transaction"},
		{"z", "undo the last change"},
		{"/", "fuzzy search the transactions (esc clears)"},
		{"u", "only uncategorized / all"},
		{"?", "this help"},
		{"q  ctrl+c", "quit"},
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, accent.Render(fit(r[0], 16))+" "+r[1])
	}
	w := min(max(m.width-6, 20), 64)
	return box("Keys", lines, w, min(len(lines)+2, max(m.height-2, 3)), true, "any key closes")
}

// ---- add / edit form --------------------------------------------------------------

func (m *Model) formPopup() string {
	f := m.form
	w := min(max(m.width-6, 30), 72)
	iw := w - 2
	h := min(15, max(m.height-2, 9))
	listH := max(h-9, 0)

	label := func(name string, field formField) string {
		if f.field == field {
			return accent.Render(fit("▸ "+name, 11))
		}
		return dim.Render(fit("  "+name, 11))
	}
	acc := m.accounts[f.account].account
	accText := fmt.Sprintf("%s · %s", acc.Name, acc.Currency)
	if f.field == fieldAccount {
		accText += dim.Render("   ◂ ▸ change")
	}
	kind := "Expense"
	if f.isIncome() {
		kind = "Income"
	}

	lines := []string{
		label("Account", fieldAccount) + " " + accText,
		leftRight(label("Amount", fieldAmount)+" "+f.amount.View(), dim.Render(kind), iw),
		label("Payee", fieldPayee) + " " + f.payee.View(),
		label("Category", fieldCategory) + " " + f.category.View(),
		label("Date", fieldDate) + " " + f.date.View(),
		dim.Render(strings.Repeat("─", iw)),
	}
	lines = append(lines, m.formSuggestions(iw, listH)...)
	for len(lines) < h-2-1 {
		lines = append(lines, "")
	}
	if f.err != "" {
		lines = append(lines, errStyle.Render(f.err))
	} else {
		lines = append(lines, "")
	}

	title := "Add transaction"
	if f.editing != 0 {
		title = fmt.Sprintf("Edit transaction #%d", f.editing)
	}
	return box(title, lines, w, h, true, "enter next · ctrl+s save · esc cancel")
}

// formSuggestions is the list under the fields: payees, categories, or a hint.
func (m *Model) formSuggestions(w, h int) []string {
	f := m.form
	if h == 0 {
		return nil
	}
	switch f.field {
	case fieldPayee:
		if len(f.payeeItems) == 0 {
			return []string{dim.Render("a new payee")}
		}
		var out []string
		for i, p := range f.payeeItems {
			if len(out) == h {
				break
			}
			hint := ""
			if p.category != "" {
				hint = "→ " + shortName(p.category)
			}
			line := leftRight(p.name, dim.Render(hint), w)
			if i == f.payeeCur {
				line = selFocused.Render(fit(p.name+"  "+hint, w))
			}
			out = append(out, line)
		}
		return out
	case fieldCategory:
		var out []string
		start := 0
		if f.catCur >= h {
			start = f.catCur - h + 1
		}
		for i := start; i < len(f.catItems) && len(out) < h; i++ {
			it := f.catItems[i]
			line := leftRight(it.name, dim.Render(it.hint), w)
			if i == f.catCur {
				line = selFocused.Render(fit(it.name+"  "+it.hint, w))
			}
			out = append(out, line)
		}
		if len(f.catItems) == 0 {
			out = append(out, dim.Render("no match: type a full name like Expenses:Gifts to create it"))
		}
		return out
	case fieldAmount:
		return []string{dim.Render("12.50 is an expense · +2450 is income")}
	case fieldDate:
		return []string{dim.Render("empty = today · y yesterday · -2 two days ago · 2026-10-20")}
	default:
		return []string{dim.Render("↑ ↓ or ◂ ▸ pick the account the money moves in or out of")}
	}
}

func (m *Model) confirmPopup() string {
	tx := m.confirmTx
	w := min(max(m.width-6, 30), 60)
	amount := ""
	if len(tx.Postings) > 0 {
		amount = signed(tx.Postings[0].Amount)
	}
	lines := []string{
		"",
		fit(fmt.Sprintf("  %s  %s", tx.Date, clean(tx.Payee)), w-2),
		fit("  "+amount, w-2),
		"",
		fit("  "+warn.Render("Delete this transaction?")+dim.Render("  (z undoes it)"), w-2),
	}
	return box(fmt.Sprintf("Delete #%d", tx.ID), lines, w, 7, true, "y delete · n cancel")
}
