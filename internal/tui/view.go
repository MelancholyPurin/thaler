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
	case modeAccount:
		screen = m.centerPopup(screen, m.accountPopup())
	case modeTransfer:
		screen = m.centerPopup(screen, m.transferPopup())
	case modeBackup:
		screen = m.centerPopup(screen, m.backupPopup())
	case modeRestore:
		screen = m.centerPopup(screen, m.restorePopup())
	case modePrune:
		screen = m.centerPopup(screen, m.prunePopup())
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
	for i := range numScreens {
		name := m.screenName(i)
		label := " " + name + " "
		if i == m.screen {
			label = selFocused.Render(label)
		} else {
			label = dim.Render(label)
		}
		tabs = append(tabs, label)
	}
	period := dim.Render("[ ") + bold.Render(m.periodLabel(m.period)) + dim.Render(" ]")
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
		label := m.periodLabel(items[i])
		if items[i] == m.now().Format(monthLayout) {
			label += dim.Render("  " + m.loc.T("this_month", nil))
		}
		line := fit(label, iw)
		if i == cursor {
			line = selFocused.Render(fit(m.periodLabel(items[i]), iw))
		}
		lines = append(lines, line)
	}
	left := box(m.loc.T("panel_months", nil), lines, l.leftW, l.avail, true, m.loc.T("position_of", map[string]any{"N": cursor + 1, "Total": len(items)}))
	if !l.two {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, m.rightPane(l))
}

// plannedLines say what a screen that is not built yet will hold.
func (m *Model) plannedLines(s screen) []string {
	const bullet = "  • "
	switch s {
	case screenForecast:
		return []string{
			m.loc.T("planned_forecast_title", nil),
			"",
			bullet + m.loc.T("planned_forecast_recurring", nil),
			bullet + m.loc.T("planned_forecast_projected", nil),
			bullet + m.loc.T("planned_forecast_dip", nil),
		}
	case screenReports:
		return []string{
			m.loc.T("planned_reports_title", nil),
			"",
			bullet + m.loc.T("planned_reports_categories", nil),
			bullet + m.loc.T("planned_reports_net_worth", nil),
			bullet + m.loc.T("planned_reports_income", nil),
		}
	}
	return nil
}

func (m *Model) placeholderBody(l layout) string {
	lines := []string{"", dim.Render(m.loc.T("not_built_yet", nil)), ""}
	lines = append(lines, m.plannedLines(m.screen)...)
	return box(m.screenName(m.screen), lines, m.width, l.avail, true, "")
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
	text := appName + "  " + dim.Render(m.loc.T("home_credit", nil))
	if m.focus == paneHome {
		text = bold.Render(appName) + "  " + dim.Render(m.loc.T("home_credit", nil))
	}
	return box(m.loc.T("panel_home", nil), []string{text}, l.leftW, l.hHome, m.focus == paneHome, "")
}

func (m *Model) accountsPanel(l layout) string {
	iw := l.leftW - 2
	n := len(m.accounts) + 1
	lines := make([]string, 0, l.acctLines)
	for i := m.accts.offset; i < n && len(lines) < l.acctLines; i++ {
		label, value := m.loc.T("all_accounts", nil), m.moneyOrDash(m.netWorth.Total, len(m.netWorth.Missing) == 0)
		if i > 0 {
			e := m.accounts[i-1]
			label, value = m.loc.ShortName(e.account.Name), m.loc.Money(e.balance)
		}
		line := leftRight(label, value, iw)
		if i == m.accts.cursor {
			line = m.rowStyle(paneAccounts).Render(fit(line, iw))
		}
		lines = append(lines, line)
	}
	return box(m.loc.T("panel_accounts", nil), lines, l.leftW, l.hAccts, m.focus == paneAccounts, m.loc.T("position_of", map[string]any{"N": m.accts.cursor + 1, "Total": n}))
}

// moneyOrDash formats m, or "—" when it is not meaningful.
func (m *Model) moneyOrDash(v ledger.Money, ok bool) string {
	if !ok {
		return "≈ " + m.loc.Money(v)
	}
	return m.loc.Money(v)
}

func (m *Model) transactionsPanel(l layout) string {
	iw := l.leftW - 2
	title := m.loc.T("panel_transactions", nil)
	if m.scopeAccount() != "" {
		title += " · " + m.loc.ShortName(m.scopeAccount())
	}
	if m.onlyUncat {
		title += " · " + m.loc.T("panel_uncategorized", nil)
	}
	if q := strings.TrimSpace(m.filter.Value()); q != "" {
		title += " · /" + q
	}

	var lines []string
	if len(m.view) == 0 {
		switch {
		case len(m.rows) == 0:
			lines = []string{dim.Render(m.loc.T("tx_none_yet", nil)), dim.Render(m.loc.T("tx_none_hint", nil))}
		case m.period != "" && !m.onlyUncat && m.filter.Value() == "" && m.scopeAccount() == "":
			lines = []string{dim.Render(m.loc.T("tx_none_period", map[string]any{"Period": m.periodLabel(m.period)})), dim.Render(m.loc.T("tx_none_period_hint", nil))}
		case m.onlyUncat && m.filter.Value() == "" && m.scopeAccount() == "":
			lines = []string{dim.Render(m.loc.T("tx_nothing_left", nil)), dim.Render(m.loc.T("tx_see_all", nil))}
		default:
			lines = []string{dim.Render(m.loc.T("tx_no_match", nil))}
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
		case r.transfer:
			marker = "⇄"
		}
		payeeW := max(4, iw-dateWidth-amtW-4)
		plain := m.loc.StoredDate(r.tx.Date) + " " + marker + " " + fit(clean(r.tx.Payee), payeeW) + " " + fitRight(m.listAmount(r), amtW)
		if i == m.txs.cursor {
			lines = append(lines, m.rowStyle(paneTransactions).Render(fit(plain, iw)))
			continue
		}
		switch {
		case r.uncategorized:
			marker = warn.Render(marker)
		case !r.simple, r.transfer:
			marker = dim.Render(marker)
		}
		amount := fitRight(m.listAmount(r), amtW)
		if r.source.Amount.Minor > 0 {
			amount = accent.Render(amount)
		}
		lines = append(lines, m.loc.StoredDate(r.tx.Date)+" "+marker+" "+fit(clean(r.tx.Payee), payeeW)+" "+amount)
	}
	footer := ""
	if len(m.view) > 0 {
		footer = m.loc.T("position_of", map[string]any{"N": m.txs.cursor + 1, "Total": len(m.view)})
	}
	return box(title, lines, l.leftW, l.hTxs, m.focus == paneTransactions, footer)
}

// listAmount is the compact amount shown in the transaction list: without the
// currency code while everything is in one currency.
func (m *Model) listAmount(r row) string {
	a := r.source.Amount
	s := m.loc.Amount(a)
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
		hint = m.loc.T("hint_budget", nil)
	case m.screen != screenLedger:
		hint = m.loc.T("hint_other_screens", nil)
	default:
		hint = m.ledgerHint()
	}
	if notes := m.backupNotes(); len(notes) > 0 {
		return fit(warn.Render(strings.Join(notes, " · "))+dim.Render(" · "+hint), m.width)
	}
	return fit(dim.Render(hint), m.width)
}

func (m *Model) ledgerHint() string {
	var hint string
	switch m.focus {
	case paneHome:
		hint = m.loc.T("hint_home", nil)
	case paneAccounts:
		hint = m.loc.T("hint_accounts", nil)
	default:
		hint = m.loc.T("hint_transactions", nil)
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
	h := min(max(m.height-4, 8), 17, max(8, 6+len(m.pickItems)))
	title := m.loc.T("categorize_title", map[string]any{"Payee": clean(r.tx.Payee), "Amount": m.loc.Signed(r.source.Amount)})

	lines := []string{m.picker.View()}
	listH := h - 2 - 1 - 3 // minus the border, the input and the rule section
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
		lines = append(lines, dim.Render(m.loc.T("picker_no_match", nil)))
	}
	for len(lines) < 1+listH {
		lines = append(lines, "")
	}

	// The rule section sits at the bottom: tab goes there from the category.
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	footer := m.loc.T("footer_pick", nil)
	switch {
	case m.pickRuleOn:
		lines = append(lines, accent.Render(fit("▸ "+m.loc.T("label_rule", nil), 7))+" "+m.pickRule.View())
		footer = m.loc.T("footer_pick_rule", nil)
	case clean(m.pickRule.Value()) != "":
		lines = append(lines, dim.Render(fit("  "+m.loc.T("label_rule", nil), 8))+m.pickRule.View())
	default:
		lines = append(lines, dim.Render(fit("  "+fit(m.loc.T("label_rule", nil), 6)+" "+m.loc.T("rule_hint", nil), iw)))
	}
	note := dim.Render(fit("  "+m.pickNote, iw))
	if m.pickNoteBad {
		note = errStyle.Render(fit("  "+m.pickNote, iw))
	}
	lines = append(lines, note)
	return box(title, lines, w, h, true, footer)
}

func (m *Model) helpPopup() string {
	rows := [][2]string{
		{"L B F R", m.loc.T("help_screens", nil)},
		{"[  ]", m.loc.T("help_month", nil)},
		{"t  T", m.loc.T("help_this_month", nil)},
		{"1 2 3, tab", m.loc.T("help_focus", nil)},
		{"↑ ↓  j k", m.loc.T("help_move", nil)},
		{"PgUp PgDn  g G", m.loc.T("help_page", nil)},
		{"enter", m.loc.T("help_enter", nil)},
		{"a", m.loc.T("help_add", nil)},
		{"x", m.loc.T("help_transfer", nil)},
		{"i", m.loc.T("help_import", nil)},
		{"n  b", m.loc.T("help_new_account", nil)},
		{"c, then tab", m.loc.T("help_categorize_rule", nil)},
		{"c  e  d", m.loc.T("help_ced", nil)},
		{"r", m.loc.T("help_rename", nil)},
		{"z", m.loc.T("help_undo", nil)},
		{"/", m.loc.T("help_search", nil)},
		{"u", m.loc.T("help_uncategorized", nil)},
		{"S", m.loc.T("help_backup", nil)},
		{"ctrl+r", m.loc.T("help_restore", nil)},
		{"ctrl+p", m.loc.T("help_prune", nil)},
		{"C", m.loc.T("help_config", nil)},
		{"?", m.loc.T("help_help", nil)},
		{"q  ctrl+c", m.loc.T("help_quit", nil)},
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, accent.Render(fit(r[0], 16))+" "+r[1])
	}
	w := min(max(m.width-6, 20), 64)
	return box(m.loc.T("help_title", nil), lines, w, min(len(lines)+2, max(m.height-2, 3)), true, m.loc.T("help_footer", nil))
}

// ---- add / edit form --------------------------------------------------------------

func (m *Model) formPopup() string {
	f := m.form
	w := min(max(m.width-6, 30), 72)
	iw := w - 2
	h := min(16, max(m.height-2, 10))
	listH := max(h-10, 0)

	label := func(name string, field formField) string {
		if f.field == field {
			return accent.Render(fit("▸ "+name, formLabelWidth))
		}
		return dim.Render(fit("  "+name, formLabelWidth))
	}
	acc := m.accounts[f.account].account
	accText := fmt.Sprintf("%s · %s", acc.Name, acc.Currency)
	if f.field == fieldAccount {
		accText += dim.Render("   " + m.loc.T("change_hint", nil))
	}
	kind := m.loc.T("form_expense", nil)
	if f.isIncome() {
		kind = m.loc.T("form_income", nil)
	}

	lines := []string{
		label(m.loc.T("label_account", nil), fieldAccount) + " " + accText,
		leftRight(label(m.loc.T("label_amount", nil), fieldAmount)+" "+f.amount.View(), dim.Render(kind), iw),
		label(m.loc.T("label_payee", nil), fieldPayee) + " " + f.payee.View(),
		label(m.loc.T("label_category", nil), fieldCategory) + " " + f.category.View(),
		label(m.loc.T("label_date", nil), fieldDate) + " " + f.date.View(),
		label(m.loc.T("label_description", nil), fieldDescription) + " " + f.description.View(),
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

	title := m.loc.T("add_transaction_title", nil)
	if f.editing != 0 {
		title = m.loc.T("edit_transaction_title", map[string]any{"ID": f.editing})
	}
	return box(title, lines, w, h, true, m.formFooter())
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
			return []string{dim.Render(m.loc.T("payee_new", nil))}
		}
		var out []string
		for i, p := range f.payeeItems {
			if len(out) == h {
				break
			}
			hint := ""
			if p.category != "" {
				hint = "→ " + m.loc.ShortName(p.category)
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
			out = append(out, dim.Render(m.loc.T("category_no_match", nil)))
		}
		return out
	case fieldAmount:
		return []string{dim.Render(m.loc.T("amount_hint", nil))}
	case fieldDate:
		return []string{dim.Render(m.loc.T("date_hint", nil))}
	case fieldDescription:
		return []string{dim.Render(m.loc.T("description_hint", nil))}
	default:
		return []string{dim.Render(m.loc.T("form_account_hint", nil))}
	}
}

func (m *Model) confirmPopup() string {
	tx := m.confirmTx
	w := min(max(m.width-6, 30), 60)
	amount := ""
	if len(tx.Postings) > 0 {
		amount = m.loc.Signed(tx.Postings[0].Amount)
	}
	lines := []string{
		"",
		fit(fmt.Sprintf("  %s  %s", m.loc.StoredDate(tx.Date), clean(tx.Payee)), w-2),
		fit("  "+amount, w-2),
		"",
		fit("  "+warn.Render(m.loc.T("delete_question", nil))+dim.Render("  "+m.loc.T("delete_undo_hint", nil)), w-2),
	}
	return box(m.loc.T("delete_title", map[string]any{"ID": tx.ID}), lines, w, 7, true, m.loc.T("footer_delete", nil))
}
