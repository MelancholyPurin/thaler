package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The split popup (s on a transaction, or ctrl+o in the form that adds or edits one)
// divides one amount between several categories: a shop bill that is groceries and
// household goods, a salary that is pay and a bonus. A split is one transaction with one
// posting on your own account and a posting for each category, which together add up to
// it. The lines are written as sizes, without a sign (the sign of the amount at the top
// says whether it is spending or income), and one line may be left without an amount to
// take what the others leave. Saving is refused until everything adds up; nothing is
// written before that.

// The fields in the order tab goes through them: five of the transaction, then a
// category and an amount for each line.
const (
	splitAccount = iota
	splitAmount
	splitPayee
	splitDate
	splitDescription
	splitHead // the number of fields before the lines
)

const (
	splitAmountW = 12 // the width of a line's amount
	splitItems   = 4  // the rows of categories offered under the lines
)

type splitLine struct {
	query  textinput.Model // what is typed to find a category
	chosen string          // the category of the line
	amount textinput.Model // empty takes what is left
}

type splitForm struct {
	editing int64       // id of the transaction being split or edited, 0 when new
	old     core.TxView // as it was before, for undo
	pos     int         // the field with the keyboard, see splitHead
	top     int         // the first line shown

	account                   int // index into Model.accounts
	amount, payee, date, desc textinput.Model
	lines                     []splitLine

	items   []pickItem // the categories offered for the line that has the keyboard
	itemCur int
	touched bool // the choice was moved by hand
	err     string
}

// splitSeed is what a split starts from.
type splitSeed struct {
	editing int64
	old     core.TxView
	account int
	amount  string // as typed in the form: a + for income
	payee   string
	date    string
	desc    string
	lines   [][2]string // category and amount
	focus   int
}

// splitOf reads a transaction as a split of two or more lines (see core.TxView.Split).
func splitOf(tx core.TxView) (core.Split, bool) {
	sp, ok := tx.Split()
	return sp, ok && len(sp.Lines) >= 2
}

// splittable says whether s works on the transaction: an ordinary one with a category of
// income or spending, or one that already is a split.
func splittable(r row) bool {
	_, ok := r.tx.Split()
	return ok
}

func (m *Model) isSplit(r row) bool {
	_, ok := splitOf(r.tx)
	return ok
}

// signedText is an amount as the form writes it: a + for money in, none for money out.
func (m *Model) signedText(a ledger.Money) string {
	text := m.loc.Amount(a)
	if a.Minor > 0 {
		return "+" + text
	}
	return strings.TrimPrefix(text, "-")
}

// openSplit splits the selected transaction, or opens the one that is a split.
func (m *Model) openSplit(focus int) tea.Cmd {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	if !splittable(r) {
		m.setStatus(m.loc.T("cannot_split", nil), true)
		return nil
	}
	sp, _ := r.tx.Split()
	if m.isClosed(sp.Account) {
		m.setStatus(m.errText(coded.New("account_closed", map[string]any{"Name": sp.Account})), true)
		return nil
	}
	seed := splitSeed{editing: r.tx.ID, old: r.tx, account: m.accountIndex(sp.Account), amount: m.signedText(sp.Amount),
		payee: r.tx.Payee, date: m.loc.StoredDate(r.tx.Date), desc: r.tx.Description, focus: focus}
	if len(sp.Lines) >= 2 {
		for _, l := range sp.Lines {
			seed.lines = append(seed.lines, [2]string{l.Category, m.loc.Amount(l.Amount)})
		}
	} else {
		seed.lines = [][2]string{{sp.Lines[0].Category, m.loc.Amount(sp.Lines[0].Amount)}, {"", ""}}
		if seed.focus < 0 {
			seed.focus = splitHead + 1 // the amount of the first line, to be made smaller
		}
	}
	if seed.focus < 0 {
		seed.focus = splitAmount
	}
	return m.startSplit(seed)
}

// splitFromForm carries what is typed in the form into the split popup.
func (m *Model) splitFromForm() tea.Cmd {
	f := m.form
	seed := splitSeed{editing: f.editing, old: f.old, account: f.account, amount: strings.TrimSpace(f.amount.Value()),
		payee: f.payee.Value(), date: f.date.Value(), desc: f.description.Value(), focus: splitHead + 1}
	category := ""
	if f.catCur < len(f.catItems) {
		category = f.catItems[f.catCur].name
	}
	seed.lines = [][2]string{{category, seed.amount}, {"", ""}}
	seed.lines[0][1] = strings.TrimLeft(seed.amount, "+-−")
	m.form = nil
	return m.startSplit(seed)
}

func (m *Model) startSplit(seed splitSeed) tea.Cmd {
	f := &splitForm{editing: seed.editing, old: seed.old, account: seed.account}
	f.amount, f.payee, f.date, f.desc = newInput(seed.amount), newInput(seed.payee), newInput(seed.date), newInput(seed.desc)
	f.desc.CharLimit = 0
	for _, l := range seed.lines {
		f.lines = append(f.lines, m.newSplitLine(l[0], l[1]))
	}
	f.pos = min(seed.focus, splitHead+2*len(f.lines)-1)
	m.split, m.mode = f, modeSplit
	m.sizeSplit(f)
	return m.focusSplit(f.pos)
}

func (m *Model) newSplitLine(category, amount string) splitLine {
	l := splitLine{query: newInput(""), chosen: category, amount: newInput(amount)}
	l.query.Placeholder = category
	l.amount.Placeholder = m.loc.T("split_rest", nil)
	return l
}

func (m *Model) splitWidth() int { return min(max(m.width-4, 30), 76) }

// sizeSplit sets the widths of the inputs for the terminal.
func (m *Model) sizeSplit(f *splitForm) {
	iw := m.splitWidth() - 2
	for _, in := range []*textinput.Model{&f.payee, &f.date, &f.desc} {
		in.Width = max(8, iw-formLabelWidth-1-1)
	}
	f.amount.Width = max(4, iw-formLabelWidth-1-1-max(lipgloss.Width(m.loc.T("form_expense", nil)), lipgloss.Width(m.loc.T("form_income", nil)))-1)
	for i := range f.lines {
		f.lines[i].query.Width = max(8, iw-2-1-splitAmountW-1)
		f.lines[i].amount.Width = splitAmountW - 1
	}
}

// splitInput is the input that has the keyboard, or nil on the account.
func (f *splitForm) splitInput() *textinput.Model {
	switch {
	case f.pos == splitAmount:
		return &f.amount
	case f.pos == splitPayee:
		return &f.payee
	case f.pos == splitDate:
		return &f.date
	case f.pos == splitDescription:
		return &f.desc
	case f.pos < splitHead:
		return nil
	case (f.pos-splitHead)%2 == 0:
		return &f.lines[(f.pos-splitHead)/2].query
	}
	return &f.lines[(f.pos-splitHead)/2].amount
}

func (f *splitForm) line() int        { return (f.pos - splitHead) / 2 }
func (f *splitForm) onCategory() bool { return f.pos >= splitHead && (f.pos-splitHead)%2 == 0 }
func (f *splitForm) fields() int      { return splitHead + 2*len(f.lines) }
func (f *splitForm) income() bool     { return strings.HasPrefix(strings.TrimSpace(f.amount.Value()), "+") }

// focusSplit moves the keyboard to a field. Leaving a category field takes the category
// that was chosen in it.
func (m *Model) focusSplit(pos int) tea.Cmd {
	f := m.split
	f.takeChoice()
	f.pos = (pos + f.fields()) % f.fields()
	for _, in := range []*textinput.Model{&f.amount, &f.payee, &f.date, &f.desc} {
		in.Blur()
	}
	for i := range f.lines {
		f.lines[i].query.Blur()
		f.lines[i].amount.Blur()
	}
	// keep the line with the keyboard in the window
	if f.pos >= splitHead {
		rows := m.splitRows()
		f.top = max(min(f.top, f.line()), f.line()-rows+1)
	}
	m.refreshSplit()
	if in := f.splitInput(); in != nil {
		return in.Focus()
	}
	return nil
}

// takeChoice makes the category highlighted in the field with the keyboard the category
// of its line.
func (f *splitForm) takeChoice() {
	if f.onCategory() && len(f.items) > 0 && f.itemCur < len(f.items) && f.line() < len(f.lines) {
		l := &f.lines[f.line()]
		l.chosen = f.items[f.itemCur].name
		l.query.SetValue("")
		l.query.Placeholder = l.chosen
	}
}

// splitRows is how many lines the popup has room for.
// The rest of the popup takes 14 rows: the border, the five fields and the rule, what is
// left to give out, the categories offered and the line for a problem.
func (m *Model) splitRows() int { return min(6, max(2, m.splitHeight()-14)) }

func (m *Model) splitHeight() int { return min(22, max(m.height-2, 14)) }

// refreshSplit offers the categories for the line that has the keyboard.
func (m *Model) refreshSplit() {
	f := m.split
	f.err = ""
	if !f.onCategory() {
		f.items = nil
		return
	}
	l := &f.lines[f.line()]
	q := strings.TrimSpace(l.query.Value())
	f.items = m.rankCategories(q, f.income(), strings.TrimSpace(f.payee.Value()), f.editing)
	if q == "" && l.chosen != "" {
		kept := false
		for _, it := range f.items {
			kept = kept || it.name == l.chosen
		}
		if !kept {
			f.items = append([]pickItem{{name: l.chosen, hint: m.loc.T("pick_current", nil)}}, f.items...)
		}
		if !f.touched {
			f.itemCur = 0
			for i, it := range f.items {
				if it.name == l.chosen {
					f.itemCur = i
				}
			}
		}
	} else if !f.touched || q != "" {
		f.itemCur = 0
	}
	f.itemCur = max(0, min(f.itemCur, len(f.items)-1))
}

func (m *Model) updateSplit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.split
	key := msg.String()
	switch key {
	case "esc":
		m.mode, m.split = modeList, nil
		return m, nil
	case "ctrl+s":
		f.takeChoice()
		m.saveSplit()
		return m, nil
	case "tab":
		f.touched = false
		return m, m.focusSplit(f.pos + 1)
	case "shift+tab":
		f.touched = false
		return m, m.focusSplit(f.pos - 1)
	case "enter":
		if f.pos == f.fields()-1 {
			m.saveSplit()
			return m, nil
		}
		f.touched = false
		return m, m.focusSplit(f.pos + 1)
	case "ctrl+n": // a new line below the one with the keyboard
		at := len(f.lines)
		if f.pos >= splitHead {
			at = f.line() + 1
		}
		cmd := m.focusSplit(f.pos) // takes the category chosen so far
		f.lines = append(f.lines[:at], append([]splitLine{m.newSplitLine("", "")}, f.lines[at:]...)...)
		m.sizeSplit(f)
		f.touched = false
		return m, tea.Batch(cmd, m.focusSplit(splitHead+2*at))
	case "ctrl+x": // drop the line with the keyboard
		if f.pos < splitHead || len(f.lines) < 2 {
			return m, nil
		}
		i := f.line()
		f.items = nil // nothing to take when the field is left
		f.lines = append(f.lines[:i], f.lines[i+1:]...)
		f.touched = false
		return m, m.focusSplit(splitHead + 2*min(i, len(f.lines)-1))
	case "up", "down":
		d := 1
		if key == "up" {
			d = -1
		}
		switch {
		case f.pos == splitAccount:
			f.account = (f.account + d + len(m.accounts)) % len(m.accounts)
			return m, nil
		case f.onCategory() && len(f.items) > 0 && (d > 0 || f.itemCur > 0):
			f.itemCur = max(0, min(len(f.items)-1, f.itemCur+d))
			f.touched = true
			return m, nil
		case f.pos == 0 && d < 0:
			return m, nil
		}
		f.touched = false
		return m, m.focusSplit(f.pos + d)
	case "left", "right":
		if f.pos == splitAccount {
			d := 1
			if key == "left" {
				d = -1
			}
			f.account = (f.account + d + len(m.accounts)) % len(m.accounts)
			return m, nil
		}
	}
	in := f.splitInput()
	if in == nil {
		return m, nil
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	f.touched = false
	m.refreshSplit()
	return m, cmd
}

// splitTotals reads the amount and the lines typed so far: the amount, what the lines
// with an amount add up to, and how many lines have none. ok is false while the amount
// or one of the amounts is not a number.
func (m *Model) splitTotals() (total, given ledger.Money, empty int, ok bool) {
	f := m.split
	cur := m.accounts[f.account].account.Currency
	t, err := ledger.ParseAmount(strings.TrimLeft(strings.TrimSpace(f.amount.Value()), "+-−"), cur, 0)
	if err != nil {
		return total, given, 0, false
	}
	total, given = t, ledger.Money{Currency: t.Currency}
	for _, l := range f.lines {
		text := strings.TrimSpace(l.amount.Value())
		if text == "" {
			empty++
			continue
		}
		a, err := ledger.ParseAmount(strings.TrimLeft(text, "+-−"), cur, 0)
		if err != nil {
			return total, given, empty, false
		}
		given.Minor += a.Minor
	}
	return total, given, empty, true
}

// saveSplit checks everything and writes the transaction. A problem is said in the popup
// and nothing is written.
func (m *Model) saveSplit() {
	f := m.split
	fail := func(text string) { f.err = text }
	acc := m.accounts[f.account].account

	text := strings.TrimSpace(f.amount.Value())
	income := strings.HasPrefix(text, "+")
	total, err := ledger.ParseAmount(strings.TrimLeft(text, "+-−"), acc.Currency, 0)
	if err != nil {
		fail(m.loc.T("error_amount", map[string]any{"Error": m.errText(err)}))
		return
	}
	if total.Minor == 0 {
		fail(m.loc.T("form_amount_positive", nil))
		return
	}
	when, err := parseFormDate(m.loc, f.date.Value(), m.now())
	if err != nil {
		fail(m.loc.T("error_date", map[string]any{"Error": m.errText(err)}))
		return
	}

	sizes := make([]ledger.Money, len(f.lines))
	rest, left := -1, total.Minor
	for i, l := range f.lines {
		n := map[string]any{"N": i + 1}
		if l.chosen == "" {
			fail(m.loc.T("split_no_category", n))
			return
		}
		text := strings.TrimSpace(l.amount.Value())
		if text == "" {
			if rest >= 0 {
				fail(m.loc.T("split_one_rest", nil))
				return
			}
			rest = i
			continue
		}
		a, err := ledger.ParseAmount(strings.TrimLeft(text, "+-−"), acc.Currency, 0)
		if err != nil {
			fail(m.loc.T("split_line_amount", map[string]any{"N": i + 1, "Error": m.errText(err)}))
			return
		}
		if a.Minor <= 0 {
			fail(m.loc.T("split_line_positive", n))
			return
		}
		sizes[i] = a
		left -= a.Minor
	}
	switch {
	case rest >= 0 && left <= 0:
		fail(m.loc.T("split_nothing_left", map[string]any{"N": rest + 1}))
		return
	case rest >= 0:
		sizes[rest] = ledger.Money{Minor: left, Currency: acc.Currency}
	case left != 0:
		over := ledger.Money{Minor: left, Currency: acc.Currency}
		if left < 0 {
			fail(m.loc.T("split_long", map[string]any{"Amount": m.loc.Amount(over.Neg())}))
		} else {
			fail(m.loc.T("split_short", map[string]any{"Amount": m.loc.Amount(over)}))
		}
		return
	}

	source := total
	if !income {
		source.Minor = -source.Minor
	}
	sp := core.Split{Date: when, Payee: clean(f.payee.Value()), Description: clean(f.desc.Value()), Account: acc.Name, Amount: source}
	for i, l := range f.lines {
		sp.Lines = append(sp.Lines, core.SplitLine{Category: l.chosen, Amount: sizes[i]})
	}

	var id int64
	var undo undoOp
	if f.editing != 0 {
		id = f.editing
		old, err := f.old.NewTransaction()
		if err != nil {
			fail(m.errText(err))
			return
		}
		if _, err := m.svc.ReplaceSplit(m.ctx, id, sp); err != nil {
			fail(m.errText(err))
			return
		}
		undo = undoOp{
			undone: func() string { return m.loc.T("undid_edit", map[string]any{"ID": id}) },
			run:    func() error { _, err := m.svc.ReplaceTransaction(m.ctx, id, old); return err },
		}
	} else {
		res, err := m.svc.AddSplit(m.ctx, sp)
		if err != nil {
			fail(m.errText(err))
			return
		}
		id = res.ID
		undo = undoOp{
			undone: func() string { return m.loc.T("undid_add", map[string]any{"ID": id}) },
			run:    func() error { return m.svc.DeleteTransaction(m.ctx, id) },
		}
	}
	m.undo = append(m.undo, undo)
	m.lastAccount = acc.Name
	editing := f.editing != 0
	m.mode, m.split = modeList, nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	visible := m.selectTx(id)

	args := map[string]any{"ID": id, "Payee": sp.Payee, "Amount": m.loc.Signed(source), "Count": len(f.lines)}
	parts := []string{m.loc.T("split_added", args)}
	if editing {
		parts[0] = m.loc.T("split_updated", args)
	}
	if !visible {
		parts = append(parts, m.loc.T("tx_not_shown", nil))
	}
	m.setStatus(m.statusLine(parts), false)
}

func (m *Model) splitPopup() string {
	f := m.split
	w := m.splitWidth()
	iw := w - 2
	label := func(name string, pos int) string {
		if f.pos == pos {
			return accent.Render(fit("▸ "+name, formLabelWidth))
		}
		return dim.Render(fit("  "+name, formLabelWidth))
	}
	acc := m.accounts[f.account].account
	accText := fmt.Sprintf("%s · %s", acc.Name, acc.Currency)
	if f.pos == splitAccount {
		accText += dim.Render("   " + m.loc.T("change_hint", nil))
	}
	kind := m.loc.T("form_expense", nil)
	if f.income() {
		kind = m.loc.T("form_income", nil)
	}
	lines := []string{
		label(m.loc.T("label_account", nil), splitAccount) + " " + accText,
		leftRight(label(m.loc.T("label_amount", nil), splitAmount)+" "+f.amount.View(), dim.Render(kind), iw),
		label(m.loc.T("label_payee", nil), splitPayee) + " " + f.payee.View(),
		label(m.loc.T("label_date", nil), splitDate) + " " + f.date.View(),
		label(m.loc.T("label_description", nil), splitDescription) + " " + f.desc.View(),
		dim.Render(strings.Repeat("─", iw)),
	}

	rows := m.splitRows()
	for i := f.top; i < len(f.lines) && i < f.top+rows; i++ {
		l := f.lines[i]
		mark := "  "
		if f.line() == i && f.pos >= splitHead {
			mark = accent.Render("▸ ")
		}
		cat := l.query.View()
		if f.pos != splitHead+2*i && strings.TrimSpace(l.query.Value()) == "" {
			cat = fit(l.chosen, l.query.Width+1)
			if l.chosen == "" {
				cat = dim.Render(fit(m.loc.T("label_category", nil), l.query.Width+1))
			}
		}
		lines = append(lines, mark+cat+" "+l.amount.View())
	}
	for len(lines) < splitHead+1+rows {
		lines = append(lines, "")
	}

	// what is left to give out
	total, given, empty, ok := m.splitTotals()
	summary := ""
	switch {
	case !ok:
	case given.Minor == total.Minor && empty == 0:
		summary = dim.Render(m.loc.T("split_balanced", nil))
	case given.Minor > total.Minor || (empty == 0 && given.Minor < total.Minor):
		diff := total.Minor - given.Minor
		if diff < 0 {
			summary = warn.Render(m.loc.T("split_over", map[string]any{"Amount": m.loc.Amount(ledger.Money{Minor: -diff, Currency: total.Currency})}))
		} else {
			summary = warn.Render(m.loc.T("split_left", map[string]any{"Amount": m.loc.Amount(ledger.Money{Minor: diff, Currency: total.Currency})}))
		}
	default:
		summary = dim.Render(m.loc.T("split_rest_gets", map[string]any{"Amount": m.loc.Amount(ledger.Money{Minor: total.Minor - given.Minor, Currency: total.Currency})}))
	}
	lines = append(lines, fit(summary, iw))

	lines = append(lines, m.splitSuggestions(iw)...)
	if f.err != "" {
		lines = append(lines, errStyle.Render(f.err))
	} else {
		lines = append(lines, "")
	}

	title := m.loc.T("split_title_new", nil)
	if f.editing != 0 {
		title = m.loc.T("split_title", map[string]any{"ID": f.editing})
	}
	return box(title, lines, w, len(lines)+2, true, m.loc.T("footer_split", nil))
}

// splitSuggestions is the list under the lines: categories, or a hint for the field.
func (m *Model) splitSuggestions(w int) []string {
	f := m.split
	var out []string
	switch {
	case f.onCategory():
		start := 0
		if f.itemCur >= splitItems {
			start = f.itemCur - splitItems + 1
		}
		for i := start; i < len(f.items) && len(out) < splitItems; i++ {
			it := f.items[i]
			line := leftRight(it.name, dim.Render(it.hint), w)
			if i == f.itemCur {
				line = selFocused.Render(fit(it.name+"  "+it.hint, w))
			}
			out = append(out, line)
		}
		if len(f.items) == 0 {
			out = wrapped(m.loc.T("category_no_match", nil), w, dim)
		}
	case f.pos >= splitHead:
		out = wrapped(m.loc.T("split_amount_hint", nil), w, dim)
	case f.pos == splitAmount:
		out = wrapped(m.loc.T("amount_hint", nil), w, dim)
	case f.pos == splitDate:
		out = wrapped(m.loc.T("date_hint", nil), w, dim)
	case f.pos == splitDescription:
		out = wrapped(m.loc.T("description_hint", nil), w, dim)
	case f.pos == splitAccount:
		out = wrapped(m.loc.T("form_account_hint", nil), w, dim)
	default:
		out = wrapped(m.loc.T("split_payee_hint", nil), w, dim)
	}
	for len(out) < splitItems {
		out = append(out, "")
	}
	return out[:splitItems]
}
