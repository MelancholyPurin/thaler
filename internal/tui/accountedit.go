package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// In the Accounts panel, r renames the account under the cursor, c closes it (or reopens it,
// when it is a closed one) and v shows the closed accounts after the others, dimmed, or hides
// them again. A closed account keeps its history, so its transactions can still be read and
// their categories changed; it is out of every form, which offer the open accounts only.

// panelLen is how many lines the Accounts panel has: "All accounts", the open accounts and,
// when they are shown, the closed ones.
func (m *Model) panelLen() int {
	n := 1 + len(m.accounts)
	if m.showClosed {
		n += len(m.closed)
	}
	return n
}

// panelEntry is the account on line i of the Accounts panel; line 0 is "All accounts", which
// is none.
func (m *Model) panelEntry(i int) (acctEntry, bool) {
	switch {
	case i < 1:
		return acctEntry{}, false
	case i <= len(m.accounts):
		return m.accounts[i-1], true
	case m.showClosed && i-1-len(m.accounts) < len(m.closed):
		return m.closed[i-1-len(m.accounts)], true
	}
	return acctEntry{}, false
}

// isClosed says whether the account called name is closed.
func (m *Model) isClosed(name string) bool {
	for _, e := range m.closed {
		if e.account.Name == name {
			return true
		}
	}
	return false
}

// selectAccount puts the cursor of the Accounts panel on the account called name, if it is
// shown.
func (m *Model) selectAccount(name string) {
	for i := 1; i < m.panelLen(); i++ {
		if e, _ := m.panelEntry(i); e.account.Name == name {
			m.accts.cursor = i
		}
	}
}

// accountsChanged reloads what an account change touches, keeping the cursor on name (the
// account the change was about) when it is still shown.
func (m *Model) accountsChanged(name string) {
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.selectAccount(name)
	m.clampAll()
	m.applyFilter(false)
}

func (m *Model) toggleShowClosed() {
	if !m.showClosed && len(m.closed) == 0 {
		m.setStatus(m.loc.T("accounts_none_closed", nil), false)
		return
	}
	m.showClosed = !m.showClosed
	m.clampAll()
	m.applyFilter(false)
	if m.showClosed {
		m.setStatus(m.loc.T("accounts_closed_shown", nil), false)
		return
	}
	m.setStatus(m.loc.T("accounts_closed_hidden", nil), false)
}

// toggleClosed closes the account under the cursor, or reopens it if it is closed.
func (m *Model) toggleClosed() {
	e, ok := m.panelEntry(m.accts.cursor)
	if !ok {
		m.setStatus(m.loc.T("select_account_first", nil), true)
		return
	}
	name := e.account.Name
	if e.account.Closed {
		if _, err := m.svc.ReopenAccount(m.ctx, name); err != nil {
			m.setStatus(m.errText(err), true)
			return
		}
		m.undo = append(m.undo, undoOp{
			account: name,
			undone:  func() string { return m.loc.T("undid_reopen", map[string]any{"Name": m.loc.ShortName(name)}) },
			run: func() error {
				_, err := m.svc.CloseAccount(m.ctx, name)
				return err
			},
		})
		m.accountsChanged(name)
		m.setStatus(m.statusLine([]string{m.loc.T("account_reopened", map[string]any{"Name": m.loc.ShortName(name)}), m.loc.T("hint_z_undoes", nil)}), false)
		return
	}
	if _, err := m.svc.CloseAccount(m.ctx, name); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.undo = append(m.undo, undoOp{
		account: name,
		undone:  func() string { return m.loc.T("undid_close", map[string]any{"Name": m.loc.ShortName(name)}) },
		run: func() error {
			_, err := m.svc.ReopenAccount(m.ctx, name)
			return err
		},
	})
	m.accountsChanged(name)
	parts := []string{m.loc.T("account_closed_status", map[string]any{"Name": m.loc.ShortName(name)}), m.loc.T("hint_z_undoes", nil)}
	if !m.showClosed {
		parts = append(parts, m.loc.T("hint_v_shows_closed", nil))
	}
	m.setStatus(m.statusLine(parts), false)
}

// ---- renaming ---------------------------------------------------------------------

type renameForm struct {
	account core.Account
	name    textinput.Model // what follows the type
	err     string
}

func (m *Model) renameSize() (w, h int) { return min(max(m.width-6, 30), 56), 9 }

// openRename asks for the new name of the account under the cursor.
func (m *Model) openRename() tea.Cmd {
	e, ok := m.panelEntry(m.accts.cursor)
	if !ok {
		m.setStatus(m.loc.T("select_account_first", nil), true)
		return nil
	}
	f := &renameForm{account: e.account, name: newInput("")}
	w, _ := m.renameSize()
	f.name.Width = max(8, w-2-12-1-1) // minus the border, the label, a space and the cursor cell
	f.name.SetValue(strings.TrimPrefix(e.account.Name, string(e.account.Type)+":"))
	f.name.CursorEnd()
	m.rename, m.mode = f, modeRename
	return f.name.Focus()
}

// fullAccountName is the name an account of type typ gets from what was typed: "Sparkasse" is
// Assets:Sparkasse. A name that starts with a type is taken as it is, and so refused by the
// service when it is another type, instead of silently becoming Assets:Expenses:Food.
func fullAccountName(typ ledger.AccountType, typed string) string {
	text := strings.TrimSpace(typed)
	if _, err := ledger.TypeOf(text); err == nil && strings.Contains(text, ":") {
		return text
	}
	return string(typ) + ":" + text
}

func (m *Model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.rename
	switch msg.String() {
	case "esc":
		m.mode, m.rename = modeList, nil
		return m, nil
	case "enter":
		m.saveRename()
		return m, nil
	}
	var cmd tea.Cmd
	f.name, cmd = f.name.Update(msg)
	f.err = ""
	return m, cmd
}

func (m *Model) saveRename() {
	f := m.rename
	from := f.account.Name
	to := fullAccountName(f.account.Type, f.name.Value())
	if to == from {
		m.mode, m.rename = modeList, nil
		return
	}
	if _, err := m.svc.RenameAccount(m.ctx, from, to); err != nil {
		f.err = m.errText(err)
		return
	}
	m.mode, m.rename = modeList, nil
	m.undo = append(m.undo, undoOp{
		account: from, // the name it has again
		undone: func() string {
			return m.loc.T("undid_rename_account", map[string]any{"From": m.loc.ShortName(from), "To": m.loc.ShortName(to)})
		},
		run: func() error {
			_, err := m.svc.RenameAccount(m.ctx, to, from)
			return err
		},
	})
	m.accountsChanged(to)
	m.setStatus(m.statusLine([]string{
		m.loc.T("account_renamed", map[string]any{"From": m.loc.ShortName(from), "To": m.loc.ShortName(to)}),
		m.loc.T("hint_z_undoes", nil),
	}), false)
}

func (m *Model) renamePopup() string {
	f := m.rename
	w, h := m.renameSize()
	iw := w - 2
	lines := []string{
		dim.Render(fit(" "+m.loc.T("rename_current", map[string]any{"Name": m.loc.AccountName(f.account.Name)}), iw)),
		"",
		accent.Render(fit("▸ "+m.loc.T("label_name", nil), 12)) + " " + f.name.View(),
		"",
	}
	lines = append(lines, wrapped(m.loc.T("rename_hint", map[string]any{"Type": m.loc.AccountName(string(f.account.Type))}), iw, dim)...)
	for len(lines) < h-4 {
		lines = append(lines, "")
	}
	msg := wrapped(f.err, iw, errStyle)
	for len(msg) < 2 {
		msg = append(msg, "")
	}
	lines = append(lines, msg...)
	return box(m.loc.T("rename_title", nil), lines, w, h, true, m.loc.T("rename_footer", nil))
}
