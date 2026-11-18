package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
)

// undoOp reverses one change made in this session.
type undoOp struct {
	undone func() string // the status shown once it is undone, written then so that it follows the language
	run    func() error
	rules  bool // a change made in the rules popup (to config.toml, or applying the rules), which it can undo
	// account is the account the change was about, which the Accounts panel is left on once it
	// is undone, if it is shown.
	account string
	// importID is the import this takes back, so that taking it back from the list of
	// imports leaves no step in the list that would look for it again.
	importID int64
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
	if op.account != "" {
		m.selectAccount(op.account)
		m.clampAll()
	}
	m.applyFilter(true)
	m.setStatus(op.undone(), false)
}

// confirmDelete asks before deleting the selected transaction.
func (m *Model) confirmDelete() {
	m.confirmMany = nil
	r, ok := m.selected()
	switch marked := m.markedRows(); len(marked) {
	case 0:
	case 1:
		r, ok = marked[0], true
	default:
		m.confirmDeleteMany(marked)
		return
	}
	if !ok {
		return
	}
	m.confirmTx, m.confirmXfer = r.tx, nil
	if r.tx.TransferWith != 0 { // both halves go
		v, err := m.svc.GetTransfer(m.ctx, r.tx.ID)
		if err != nil {
			m.setStatus(m.errText(err), true)
			return
		}
		m.confirmXfer = &v
	}
	m.mode = modeConfirm
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = modeList
		if len(m.confirmMany) > 0 {
			m.deleteMany(m.confirmMany)
			return m, nil
		}
		if m.confirmXfer != nil {
			m.deleteTransfer(*m.confirmXfer)
			return m, nil
		}
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

// deleteTransfer deletes both halves of a transfer, and z brings both back, linked.
func (m *Model) deleteTransfer(v core.TransferView) {
	var restore []core.RestoredTransaction
	for _, half := range []core.TxView{v.Out, v.In} {
		nt, err := half.NewTransaction()
		if err != nil {
			m.setStatus(m.errText(err), true)
			return
		}
		restore = append(restore, core.RestoredTransaction{ID: half.ID, Transaction: nt})
	}
	if _, err := m.svc.DeleteTransfer(m.ctx, v.Out.ID); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	outID, inID := v.Out.ID, v.In.ID
	m.undo = append(m.undo, undoOp{
		undone: func() string { return m.loc.T("undid_delete_transfer", map[string]any{"Out": outID, "In": inID}) },
		run:    func() error { return m.svc.RestoreTransactions(m.ctx, restore) },
	})
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(m.loc.T("deleted_transfer", map[string]any{"Out": outID, "In": inID}), false)
}
