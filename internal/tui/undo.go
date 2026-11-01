package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
)

// undoOp reverses one change made in this session.
type undoOp struct {
	label string
	run   func() error
}

// undoLast reverses the most recent change.
func (m *Model) undoLast() {
	if len(m.undo) == 0 {
		m.setStatus("nothing to undo", true)
		return
	}
	op := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	if err := op.run(); err != nil {
		m.setStatus("undo failed: "+m.errText(err), true)
		return
	}
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus("Undid "+op.label, false)
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
		label: fmt.Sprintf("deleting #%d", tx.ID),
		run:   func() error { return m.svc.RestoreTransaction(m.ctx, tx.ID, old) },
	})
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(fmt.Sprintf("Deleted #%d  (z to undo)", tx.ID), false)
}
