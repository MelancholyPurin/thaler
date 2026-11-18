package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
)

// l links two transactions you already have as the two halves of a transfer, or
// unlinks a transfer. On one transaction it lists the ones that could be its other
// half (another of your accounts, the opposite amount, the nearest in date first);
// with two marked it links those, which also works across currencies; on a half of
// a transfer it unlinks the two, which then wait in Uncategorized.

type linkPick struct {
	from   core.TxView
	items  []core.TxView
	cursor int
}

// link is the l key on the Transactions panel.
func (m *Model) link() {
	marked := m.markedRows()
	switch {
	case len(marked) == 2:
		m.linkPair(marked[0].tx.ID, marked[1].tx.ID)
		return
	case len(marked) > 2:
		m.setStatus(m.loc.T("link_mark_two", map[string]any{"Count": len(marked)}), true)
		return
	}
	r, ok := m.selected()
	if len(marked) == 1 {
		r, ok = marked[0], true
	}
	if !ok {
		return
	}
	if r.tx.TransferWith != 0 {
		m.unlink(r.tx.ID)
		return
	}
	items, err := m.svc.TransferCandidates(m.ctx, r.tx.ID)
	if err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	if len(items) == 0 {
		m.setStatus(m.loc.T("link_none", nil), true)
		return
	}
	m.lnk = &linkPick{from: r.tx, items: items}
	m.mode = modeLink
}

func (m *Model) updateLink(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.lnk
	switch msg.String() {
	case "esc":
		m.mode, m.lnk = modeList, nil
	case "up", "k":
		p.cursor = max(p.cursor-1, 0)
	case "down", "j":
		p.cursor = min(p.cursor+1, len(p.items)-1)
	case "enter":
		m.mode, m.lnk = modeList, nil
		m.linkPair(p.from.ID, p.items[p.cursor].ID)
	}
	return m, nil
}

// linkPair links a and b, and z puts them back as they were.
func (m *Model) linkPair(a, b int64) {
	res, err := m.svc.LinkTransfer(m.ctx, a, b)
	if err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	was := res.Was
	m.undo = append(m.undo, undoOp{
		undone: func() string { return m.loc.T("undid_link", map[string]any{"Out": res.OutID, "In": res.InID}) },
		run: func() error {
			if _, err := m.svc.UnlinkTransfer(m.ctx, res.OutID); err != nil {
				return err
			}
			_, err := m.svc.SetCategories(m.ctx, was)
			return err
		},
	})
	m.marked = nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(m.statusLine([]string{m.loc.T("linked", map[string]any{"Out": res.OutID, "In": res.InID}), m.loc.T("hint_z_undoes", nil)}), false)
}

// unlink makes the halves of a transfer ordinary transactions again, and z links them again.
func (m *Model) unlink(id int64) {
	v, err := m.svc.UnlinkTransfer(m.ctx, id)
	if err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	outID, inID := v.Out.ID, v.In.ID
	m.undo = append(m.undo, undoOp{
		undone: func() string { return m.loc.T("undid_unlink", map[string]any{"Out": outID, "In": inID}) },
		run: func() error {
			_, err := m.svc.LinkTransfer(m.ctx, outID, inID)
			return err
		},
	})
	m.marked = nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(m.statusLine([]string{m.loc.T("unlinked", map[string]any{"Out": outID, "In": inID}), m.loc.T("hint_z_undoes", nil)}), false)
}

func (m *Model) linkPopup() string {
	p := m.lnk
	w := min(max(m.width-6, 30), 66)
	iw := w - 2
	h := min(max(m.height-4, 8), 6+len(p.items))
	row := func(tx core.TxView) pickItem {
		source, _, _ := tx.Legs()
		return pickItem{
			name: m.loc.StoredDate(tx.Date) + "  " + m.loc.ShortName(source.Account) + "  " + clean(tx.Payee),
			hint: m.loc.Signed(source.Amount),
		}
	}
	from := row(p.from)
	lines := []string{
		leftRight(dim.Render(from.name), dim.Render(from.hint), iw),
		dim.Render(m.loc.T("link_with", nil)),
	}
	items := make([]pickItem, len(p.items))
	for i, tx := range p.items {
		items[i] = row(tx)
	}
	lines = append(lines, m.pickLines(items, p.cursor, h-2-len(lines), iw)...)
	return box(m.loc.T("link_title", map[string]any{"ID": p.from.ID}), lines, w, h, true, m.loc.T("footer_link", nil))
}
