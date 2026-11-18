package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
)

// The imports popup lists the imports that were made, newest first, and takes any of
// them back as a whole. Taking one back deletes what it added, so it asks first and
// says what will go; `z` cannot bring it back (the file can be imported again).

type importList struct {
	items   []core.ImportRecord
	list    listState
	confirm bool // asking "really?" about the import under the cursor
	err     string
}

func (f *importList) selected() core.ImportRecord { return f.items[f.list.cursor] }

// openImports shows the popup, on the newest import.
func (m *Model) openImports() {
	items, err := m.svc.ListImports(m.ctx)
	if err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.ims = &importList{items: items}
	m.mode = modeImports
}

// importsRows is how many imports the popup shows at once.
func (m *Model) importsRows() int {
	_, h := m.importSize()
	return max(h-9, 1) // minus the border, the detail, the rule, the message line and the notes
}

func (m *Model) updateImports(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.ims
	key := msg.String()
	if f.confirm {
		switch key {
		case "enter", "y":
			m.takeBack()
		case "esc", "n":
			f.confirm = false
		}
		return m, nil
	}
	switch key {
	case "esc", "q":
		m.mode, m.ims = modeList, nil
	case "enter":
		if len(f.items) > 0 {
			f.confirm, f.err = true, ""
		}
	default:
		f.list.navigate(key, len(f.items), m.importsRows())
	}
	return m, nil
}

// takeBack takes the chosen import back and shows what is left.
func (m *Model) takeBack() {
	f := m.ims
	rec := f.selected()
	done, err := m.svc.UndoImport(m.ctx, rec.ID)
	if err != nil {
		f.confirm, f.err = false, m.errText(err)
		return
	}
	// The step that would take it back from `z` has nothing left to do.
	m.undo = slices.DeleteFunc(m.undo, func(op undoOp) bool { return op.importID == rec.ID })
	m.mode, m.ims = modeList, nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(m.loc.T("imports_undone", map[string]any{"Count": done.Record.Added, "File": m.importName(done.Record)}), false)
}

// importName is what to call an import: its file, or its number when the file is not known.
func (m *Model) importName(r core.ImportRecord) string {
	if r.Source != "" {
		return r.Source
	}
	return m.loc.T("imports_unnamed", map[string]any{"ID": r.ID})
}

// importFacts is what an import did, as a list of parts: what it added, took over, paired and
// answered.
func (m *Model) importFacts(r core.ImportRecord) []string {
	parts := []string{m.loc.T("import_sum_new", map[string]any{"Count": r.Added})}
	if r.Adopted > 0 {
		parts = append(parts, m.loc.T("import_sum_matched", map[string]any{"Count": r.Adopted}))
	}
	if r.Paired > 0 {
		parts = append(parts, m.loc.T("import_sum_paired", map[string]any{"Count": r.Paired}))
	}
	if r.Settled > 0 {
		parts = append(parts, m.loc.T("imports_sum_settled", map[string]any{"Count": r.Settled}))
	}
	if r.Profile != "" {
		parts = append(parts, m.loc.T("import_sum_profile", map[string]any{"Name": r.Profile}))
	}
	return parts
}

func (m *Model) importsPopup() string {
	f := m.ims
	w, h := m.importSize()
	iw := w - 2
	var lines []string
	footer := m.loc.T("footer_imports", nil)
	switch {
	case f.confirm:
		lines = m.importsConfirmLines(iw)
		footer = m.loc.T("footer_imports_confirm", nil)
	case len(f.items) == 0:
		lines = wrapped(m.loc.T("imports_none", nil), iw, dim)
		footer = m.loc.T("footer_imports_none", nil)
	default:
		rows := m.importsRows()
		ls := f.list
		ls.clamp(len(f.items), rows)
		f.list = ls
		for i := ls.offset; i < min(ls.offset+rows, len(f.items)); i++ {
			r := f.items[i]
			row := m.loc.T("imports_row", map[string]any{
				"When": m.loc.DateTime(r.At.Local()), "Account": m.loc.ShortName(r.Account), "File": m.importName(r),
			})
			if i == ls.cursor {
				lines = append(lines, accent.Render(fit("▸ "+row, iw)))
			} else {
				lines = append(lines, dim.Render(fit("  "+row, iw)))
			}
		}
		for len(lines) < rows {
			lines = append(lines, "")
		}
		lines = append(lines, dim.Render(strings.Repeat("─", iw)))
		lines = append(lines, packed(m.importFacts(f.selected()), iw)...)
		lines = append(lines, wrapped(m.loc.T("imports_only_since", nil), iw, dim)...)
	}
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	lines = append(lines, errStyle.Render(fit(f.err, iw)))
	return box(m.loc.T("imports_title", nil), lines, w, h, true, footer)
}

// importsConfirmLines says what taking the import back would do, before it does it.
func (m *Model) importsConfirmLines(iw int) []string {
	r := m.ims.selected()
	lines := []string{
		fit(m.loc.T("imports_confirm_title", nil), iw),
		accent.Render(fit("  "+m.loc.T("imports_row", map[string]any{
			"When": m.loc.DateTime(r.At.Local()), "Account": m.loc.ShortName(r.Account), "File": m.importName(r),
		}), iw)),
		"",
		fit(m.loc.T("imports_confirm_deleted", map[string]any{"Count": r.Added}), iw),
	}
	if r.Adopted > 0 {
		lines = append(lines, fit(m.loc.T("imports_confirm_released", map[string]any{"Count": r.Adopted}), iw))
	}
	if r.Paired > 0 {
		lines = append(lines, fit(m.loc.T("imports_confirm_unpaired", map[string]any{"Count": r.Paired}), iw))
	}
	if r.Settled > 0 {
		lines = append(lines, fit(m.loc.T("imports_confirm_settled", map[string]any{"Count": r.Settled}), iw))
	}
	lines = append(lines, "")
	lines = append(lines, wrapped(m.loc.T("imports_confirm_edits", nil), iw, dim)...)
	lines = append(lines, wrapped(m.loc.T("imports_confirm_again", nil), iw, dim)...)
	return lines
}
