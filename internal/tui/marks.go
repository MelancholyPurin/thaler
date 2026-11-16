package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
)

// Several transactions at once. space marks the one under the cursor (and moves down, so
// that a run of them is space space space), ctrl+a marks everything on show, and
// c and d then act on the marked ones instead of the one under the cursor; esc lets go
// of the marks. A mark is only ever on a transaction that is on show: when a search, a
// filter, an account or another month hides one, its mark goes with it, so that what is
// categorized or deleted is always what the person can see.

// toggleMark marks the transaction under the cursor, or lets go of it, and moves on.
func (m *Model) toggleMark() {
	r, ok := m.selected()
	if !ok {
		return
	}
	if m.marked[r.tx.ID] {
		delete(m.marked, r.tx.ID)
	} else {
		if m.marked == nil {
			m.marked = map[int64]bool{}
		}
		m.marked[r.tx.ID] = true
	}
	m.txs.cursor++
	m.txs.clamp(len(m.view), m.layout().txLines)
}

// markAll marks every transaction on show, or lets go of them when all are marked.
func (m *Model) markAll() {
	if len(m.view) == 0 {
		return
	}
	all := len(m.markedRows()) == len(m.view)
	m.marked = map[int64]bool{}
	if all {
		return
	}
	for _, i := range m.view {
		m.marked[m.rows[i].tx.ID] = true
	}
}

// markedRows are the marked transactions, in the order of the list.
func (m *Model) markedRows() []row {
	var out []row
	for _, i := range m.view {
		if m.marked[m.rows[i].tx.ID] {
			out = append(out, m.rows[i])
		}
	}
	return out
}

// dropHiddenMarks lets go of the marks on transactions that are no longer on show.
func (m *Model) dropHiddenMarks() {
	if len(m.marked) == 0 {
		return
	}
	shown := make(map[int64]bool, len(m.view))
	for _, i := range m.view {
		shown[m.rows[i].tx.ID] = true
	}
	for id := range m.marked {
		if !shown[id] {
			delete(m.marked, id)
		}
	}
}

// openPickerMany asks for the category of the marked transactions. A rule is made from
// one payee, so it is left out here. Transfers and split transactions cannot be
// categorized, and one among the marked stops the whole: nothing is left half done.
func (m *Model) openPickerMany(rows []row) tea.Cmd {
	m.pickMany = m.pickMany[:0]
	income := true
	for _, r := range rows {
		if !r.simple {
			m.pickMany = nil
			m.setStatus(m.loc.T("cannot_recategorize_marked", nil), true)
			return nil
		}
		m.pickMany = append(m.pickMany, r.tx.ID)
		income = income && r.source.Amount.Minor > 0
	}
	m.mode = modePick
	m.pickTarget = rows[0].tx.ID
	m.pickPayee = rows[0].tx.Payee // ranks the categories only when they all share it
	for _, r := range rows {
		if !strings.EqualFold(strings.TrimSpace(r.tx.Payee), strings.TrimSpace(m.pickPayee)) {
			m.pickPayee = ""
			break
		}
	}
	m.pickIsIncome = income
	m.pickCursor = 0
	m.picker.SetValue("")
	m.pickRule.SetValue("")
	m.pickRule.Blur()
	m.pickRuleOn, m.pickCfg, m.pickNote, m.pickNoteBad = false, nil, "", false
	m.refreshPicker()
	return m.picker.Focus()
}

// applyCategoryMany moves the marked transactions to the category, all or none, and z
// puts each back where it was.
func (m *Model) applyCategoryMany(category string) {
	ids := append([]int64(nil), m.pickMany...)
	m.closePicker()
	var changes, back []core.CategoryChange
	for _, id := range ids {
		changes = append(changes, core.CategoryChange{TransactionID: id, Category: category})
		if r, ok := m.findRow(id); ok {
			back = append(back, core.CategoryChange{TransactionID: id, Category: r.category.Account})
		}
	}
	if _, err := m.svc.SetCategories(m.ctx, changes); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.undo = append(m.undo, undoOp{
		undone: func() string { return m.loc.T("undid_categorize_many", map[string]any{"Count": len(ids)}) },
		run: func() error {
			_, err := m.svc.SetCategories(m.ctx, back)
			return err
		},
	})
	m.marked = nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(m.loc.T("categorized_many", map[string]any{"Count": len(ids), "Category": category}), false)
}

// confirmDeleteMany asks before deleting the marked transactions.
func (m *Model) confirmDeleteMany(rows []row) {
	m.confirmMany = m.confirmMany[:0]
	for _, r := range rows {
		m.confirmMany = append(m.confirmMany, r.tx)
	}
	m.mode = modeConfirm
}

// deleteMany deletes the transactions, all or none, and z brings them all back.
func (m *Model) deleteMany(txs []core.TxView) {
	var ids []int64
	var restore []core.RestoredTransaction
	for _, tx := range txs {
		nt, err := tx.NewTransaction()
		if err != nil {
			m.setStatus(m.errText(err), true)
			return
		}
		ids = append(ids, tx.ID)
		restore = append(restore, core.RestoredTransaction{ID: tx.ID, Transaction: nt})
	}
	if err := m.svc.DeleteTransactions(m.ctx, ids); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.undo = append(m.undo, undoOp{
		undone: func() string { return m.loc.T("undid_delete_many", map[string]any{"Count": len(ids)}) },
		run:    func() error { return m.svc.RestoreTransactions(m.ctx, restore) },
	})
	if err := m.reload(); err != nil { // the marks of what is gone go with the rows
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(m.loc.T("deleted_many", map[string]any{"Count": len(ids)}), false)
}

// confirmManyPopup lists what is about to go: the first few, and how many more.
func (m *Model) confirmManyPopup() string {
	const shown = 5
	txs := m.confirmMany
	w := min(max(m.width-6, 30), 60)
	lines := []string{""}
	for i, tx := range txs {
		if i == shown {
			lines = append(lines, dim.Render(fit("  "+m.loc.T("delete_and_more", map[string]any{"Count": len(txs) - shown}), w-2)))
			break
		}
		amount := ""
		if len(tx.Postings) > 0 {
			amount = m.loc.Signed(tx.Postings[0].Amount)
		}
		lines = append(lines, fit(fmt.Sprintf("  %s  %s  %s", m.loc.StoredDate(tx.Date), clean(tx.Payee), amount), w-2))
	}
	lines = append(lines, "", fit("  "+warn.Render(m.loc.T("delete_many_question", nil))+dim.Render("  "+m.loc.T("delete_undo_hint", nil)), w-2))
	return box(m.loc.T("delete_many_title", map[string]any{"Count": len(txs)}), lines, w, len(lines)+2, true, m.loc.T("footer_delete", nil))
}

// markedLabel is the count of marked transactions for the title of the list.
func (m *Model) markedLabel() string {
	return m.loc.T("panel_marked", map[string]any{"Count": len(m.marked)})
}
