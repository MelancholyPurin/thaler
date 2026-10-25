package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

type formField int

const (
	fieldAmount formField = iota
	fieldPayee
	fieldCategory
	fieldDate
	fieldAccount
	numFields
)

const maxSuggestions = 6

// form is the popup for adding or editing one transaction.
type form struct {
	editing int64       // id of the transaction being edited, 0 when adding
	old     core.TxView // as it was before the edit, for undo
	field   formField

	amount, payee, category, date textinput.Model
	account                       int // index into Model.accounts

	payeeItems []payeeStat
	payeeCur   int // highlighted payee suggestion, -1 for none
	catItems   []pickItem
	catCur     int
	keepCat    string // when editing: the category the transaction already has
	catTouched bool   // the user moved the category selection by hand
	err        string
}

func newInput(value string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 80
	in.SetValue(value)
	return in
}

func (m *Model) inputWidth() int { return max(8, min(max(m.width-6, 30), 72)-2-12-1) } // minus the cursor cell

// openForm starts adding a transaction, or editing the selected one. The
// account defaults to the one selected in the Accounts panel, or else the one
// used last: in this session, or failing that by the most recent transaction
// in the database.
func (m *Model) openForm(edit bool) tea.Cmd {
	if len(m.accounts) == 0 {
		m.setStatus("no accounts yet: press n to create one", true)
		return nil
	}
	f := &form{payeeCur: -1}
	f.account = m.defaultAccount()
	f.amount, f.payee, f.category = newInput(""), newInput(""), newInput("")
	f.date = newInput(m.now().Format(ledger.DateLayout))

	if edit {
		r, ok := m.selected()
		if !ok {
			return nil
		}
		if !r.simple {
			m.setStatus("transfers and split transactions cannot be edited here", true)
			return nil
		}
		f.editing, f.old = r.tx.ID, r.tx
		for i, e := range m.accounts {
			if e.account.Name == r.source.Account {
				f.account = i
			}
		}
		amount := r.source.Amount
		text := amount.Decimal()
		if amount.Minor > 0 {
			text = "+" + text
		} else {
			text = strings.TrimPrefix(text, "-")
		}
		f.amount.SetValue(text)
		f.payee.SetValue(r.tx.Payee)
		f.date.SetValue(r.tx.Date)
		f.keepCat = r.category.Account
	}
	for _, in := range []*textinput.Model{&f.amount, &f.payee, &f.category, &f.date} {
		in.Width = m.inputWidth()
	}
	m.form = f
	m.mode = modeForm
	m.refreshForm()
	return m.focusField(fieldAmount)
}

func (m *Model) defaultAccount() int {
	for _, name := range []string{m.scopeAccount(), m.lastAccount, m.lastEntered} {
		for i, e := range m.accounts {
			if name != "" && e.account.Name == name {
				return i
			}
		}
	}
	return 0
}

func (m *Model) focusField(field formField) tea.Cmd {
	f := m.form
	f.field = field
	f.amount.Blur()
	f.payee.Blur()
	f.category.Blur()
	f.date.Blur()
	switch field {
	case fieldAmount:
		return f.amount.Focus()
	case fieldPayee:
		return f.payee.Focus()
	case fieldCategory:
		return f.category.Focus()
	case fieldDate:
		return f.date.Focus()
	}
	return nil
}

func (f *form) isIncome() bool { return strings.HasPrefix(strings.TrimSpace(f.amount.Value()), "+") }

// refreshForm recomputes the suggestions from what has been typed.
func (m *Model) refreshForm() {
	f := m.form
	q := strings.TrimSpace(f.payee.Value())
	type scored struct {
		payeeStat
		score, recency int
	}
	var items []scored
	for i, p := range m.payees {
		if s, ok := fuzzyScore(q, p.name); ok {
			items = append(items, scored{p, s, i})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if q != "" && items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].recency < items[j].recency
	})
	f.payeeItems = f.payeeItems[:0]
	for _, it := range items {
		if len(f.payeeItems) == maxSuggestions {
			break
		}
		f.payeeItems = append(f.payeeItems, it.payeeStat)
	}
	// Typing the start of a known payee highlights it, so Enter completes it.
	f.payeeCur = -1
	if q != "" && len(f.payeeItems) > 0 && strings.HasPrefix(strings.ToLower(f.payeeItems[0].name), strings.ToLower(q)) {
		f.payeeCur = 0
	}

	typed := strings.TrimSpace(f.category.Value())
	f.catItems = m.rankCategories(typed, f.isIncome(), q, f.editing)
	switch {
	case typed == "" && f.keepCat != "" && !f.catTouched:
		// An edited transaction keeps its category until another is chosen.
		f.catCur = 0
		for i, it := range f.catItems {
			if it.name == f.keepCat {
				f.catCur = i
			}
		}
	case !f.catTouched || typed != "":
		f.catCur = 0
	}
	f.catCur = max(0, min(f.catCur, len(f.catItems)-1))
	f.err = ""
}

func (m *Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.form
	switch msg.String() {
	case "esc":
		m.mode, m.form = modeList, nil
		return m, nil
	case "ctrl+s":
		m.saveForm()
		return m, nil
	case "tab":
		return m, m.moveField(1)
	case "shift+tab":
		return m, m.moveField(-1)
	case "enter":
		switch f.field {
		case fieldAmount:
			return m, m.moveField(1)
		case fieldPayee:
			return m, m.moveField(1)
		default:
			m.saveForm()
			return m, nil
		}
	case "up", "down":
		d := 1
		if msg.String() == "up" {
			d = -1
		}
		switch f.field {
		case fieldPayee:
			f.payeeCur = max(-1, min(len(f.payeeItems)-1, f.payeeCur+d))
		case fieldCategory:
			f.catCur = max(0, min(len(f.catItems)-1, f.catCur+d))
			f.catTouched = true
		case fieldAccount:
			f.account = (f.account + d + len(m.accounts)) % len(m.accounts)
		}
		return m, nil
	case "left", "right":
		if f.field == fieldAccount {
			d := 1
			if msg.String() == "left" {
				d = -1
			}
			f.account = (f.account + d + len(m.accounts)) % len(m.accounts)
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch f.field {
	case fieldAmount:
		f.amount, cmd = f.amount.Update(msg)
	case fieldPayee:
		f.payee, cmd = f.payee.Update(msg)
	case fieldCategory:
		f.category, cmd = f.category.Update(msg)
		f.catTouched = false
	case fieldDate:
		f.date, cmd = f.date.Update(msg)
	}
	m.refreshForm()
	return m, cmd
}

// moveField goes to the next (or previous) field. Leaving the payee field
// completes a highlighted suggestion first.
func (m *Model) moveField(d int) tea.Cmd {
	f := m.form
	if f.field == fieldPayee && f.payeeCur >= 0 && f.payeeCur < len(f.payeeItems) {
		f.payee.SetValue(f.payeeItems[f.payeeCur].name)
		f.payee.CursorEnd()
		m.refreshForm()
	}
	return m.focusField(formField((int(f.field) + d + int(numFields)) % int(numFields)))
}

// parseFormDate understands "" and "t" (today), "y" (yesterday), "-3" (three
// days ago) and the usual date formats.
func parseFormDate(s string, now time.Time) (time.Time, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "" || s == "t" || s == "today":
		return today, nil
	case s == "y" || s == "yesterday":
		return today.AddDate(0, 0, -1), nil
	case strings.HasPrefix(s, "-"):
		if n, err := strconv.Atoi(s[1:]); err == nil && n >= 0 {
			return today.AddDate(0, 0, -n), nil
		}
	}
	return ledger.ParseDate(s)
}

// saveForm validates the form and stores the transaction. Problems are shown
// in the form and nothing is written.
func (m *Model) saveForm() {
	f := m.form
	fail := func(format string, args ...any) { f.err = fmt.Sprintf(format, args...) }
	acc := m.accounts[f.account].account

	text := strings.TrimSpace(f.amount.Value())
	income := strings.HasPrefix(text, "+")
	amount, err := ledger.ParseAmount(strings.TrimLeft(text, "+-−"), acc.Currency, 0)
	if err != nil {
		fail("amount: %v", err)
		return
	}
	// Banks book zero amounts (card verifications) and may correct a charge to
	// zero, so an edit may be zero; a new entry typed by hand may not.
	if amount.Minor == 0 && f.editing == 0 {
		fail("amount must be greater than zero")
		return
	}
	if !income {
		amount.Minor = -amount.Minor
	}

	when, err := parseFormDate(f.date.Value(), m.now())
	if err != nil {
		fail("date: %v", err)
		return
	}

	var category string
	switch {
	case f.catCur < len(f.catItems):
		category = f.catItems[f.catCur].name
	default:
		fail("no category matches; type a full name like Expenses:Gifts to create one")
		return
	}

	nt := core.NewTransaction{
		Date:        when,
		Payee:       clean(f.payee.Value()),
		Description: f.old.Description,
		Postings: []ledger.Posting{
			{Account: acc.Name, Amount: amount},
			{Account: category, Amount: amount.Neg()},
		},
	}

	var id int64
	var undo undoOp
	if f.editing != 0 {
		id = f.editing
		old, err := f.old.NewTransaction()
		if err != nil {
			fail("%v", err)
			return
		}
		if _, err := m.svc.ReplaceTransaction(m.ctx, id, nt); err != nil {
			fail("%v", err)
			return
		}
		undo = undoOp{
			label: fmt.Sprintf("edit of #%d", id),
			run:   func() error { _, err := m.svc.ReplaceTransaction(m.ctx, id, old); return err },
		}
	} else {
		res, err := m.svc.AddTransaction(m.ctx, nt)
		if err != nil {
			fail("%v", err)
			return
		}
		id = res.ID
		undo = undoOp{
			label: fmt.Sprintf("adding #%d", id),
			run:   func() error { return m.svc.DeleteTransaction(m.ctx, id) },
		}
	}
	m.undo = append(m.undo, undo)
	m.lastAccount = acc.Name
	m.mode, m.form = modeList, nil

	if err := m.reload(); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.applyFilter(true)
	visible := m.selectTx(id)

	verb := "Added"
	if f.editing != 0 {
		verb = "Updated"
	}
	msg := fmt.Sprintf("%s #%d  %s  %s → %s", verb, id, nt.Payee, signed(amount), category)
	if !visible {
		msg += "  (not shown: " + periodLabel(m.period) + ", filtered)"
	}
	m.setStatus(msg, false)
}

// selectTx moves the transaction list cursor to id and reports whether it is
// visible with the current filters.
func (m *Model) selectTx(id int64) bool {
	for vi, ri := range m.view {
		if m.rows[ri].tx.ID == id {
			m.txs.cursor = vi
			m.clampAll()
			return true
		}
	}
	return false
}
