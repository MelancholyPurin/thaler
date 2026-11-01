package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
)

// undoOp reverses one change made in this session.
type undoOp struct {
	undone func() string // the status shown once it is undone, written then so that it follows the language
	run    func() error
}

// undoLast reverses the most recent change.
func (m *Model) undoLast() {
	if len(m.undo) == 0 {
		m.setStatus(m.loc.T("nothing_to_undo", nil), true)
		return
	}
	op := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	if err := op.run(); err != nil {
		m.setStatus(m.loc.T("undo_failed", map[string]any{"Error": m.errText(err)}), true)
		return
	}
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(op.undone(), false)
}

// confirmDelete asks before deleting the selected transaction.
func (m *Model) confirmDelete() {
	r, ok := m.selected()
	if !ok {
		return
	}
	m.confirmTx = r.tx
	m.mode = modeConfirm
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = modeList
		m.deleteTx(m.confirmTx)
	case "n", "N", "esc":
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) deleteTx(tx core.TxView) {
	old, err := tx.NewTransaction()
	if err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	if err := m.svc.DeleteTransaction(m.ctx, tx.ID); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.undo = append(m.undo, undoOp{
		undone: func() string { return m.loc.T("undid_delete", map[string]any{"ID": tx.ID}) },
		run:    func() error { return m.svc.RestoreTransaction(m.ctx, tx.ID, old) },
	})
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(m.loc.T("deleted_tx", map[string]any{"ID": tx.ID}), false)
}
