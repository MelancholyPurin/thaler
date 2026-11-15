package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// The categories popup (K) lists every Income and Expenses category, with how many
// transactions each has, and renames or merges the one under the cursor. A rename takes the
// categories below it along, and z takes it back. A merge moves everything of the category
// into another of its kind and removes it, so it asks first, and z cannot take it back.
// The rules in config.toml that name the category are not changed: they are counted and
// said, so that they can be.

type catStep int

const (
	catChoose  catStep = iota // choosing the category
	catRename                 // typing its new name
	catInto                   // choosing the category to merge it into
	catConfirm                // being asked before the merge
)

type catRow struct {
	name   string
	income bool
	used   int // transactions with a line on it
}

type categoryForm struct {
	rows  []catRow
	list  listState
	step  catStep
	name  textinput.Model
	into  []catRow  // where the category under the cursor can be merged: the others of its kind
	where listState // the cursor among them
	err   string
}

// catRows is how many categories the popup shows at once, and catLabelW the width of the
// name of its field.
const (
	catRows   = 8
	catLabelW = 14
)

func (m *Model) categoriesSize() (w, h int) {
	return min(max(m.width-6, 30), 64), min(catRows, len(m.catForm.rows)) + 11 // the list, a rule, the field, four lines of hint, two of message, the border
}

// openCategories lists the categories, the cursor on the one the screen is on: the
// category of the transaction selected in the Ledger, or the one selected on the Budget.
func (m *Model) openCategories() tea.Cmd {
	used := make(map[string]int)
	for _, r := range m.rows {
		seen := make(map[string]bool)
		for _, p := range r.tx.Postings {
			if !seen[p.Account] {
				seen[p.Account] = true
				used[p.Account]++
			}
		}
	}
	f := &categoryForm{name: newInput("")}
	for _, name := range m.categories {
		typ, _ := ledger.TypeOf(name)
		f.rows = append(f.rows, catRow{name: name, income: typ == ledger.Income, used: used[name]})
	}
	if len(f.rows) == 0 {
		m.setStatus(m.loc.T("categories_none", nil), true)
		return nil
	}
	// Spending first, then income; each in the order of the names.
	spending := make([]catRow, 0, len(f.rows))
	income := make([]catRow, 0, len(f.rows))
	for _, r := range f.rows {
		if r.income {
			income = append(income, r)
		} else {
			spending = append(spending, r)
		}
	}
	f.rows = slices.Concat(spending, income)
	focus := m.budgetSel
	if r, ok := m.selected(); ok && m.screen == screenLedger {
		focus = r.category.Account
	}
	for i, r := range f.rows {
		if r.name == focus {
			f.list.cursor = i
		}
	}
	m.catForm, m.mode = f, modeCategories
	w, _ := m.categoriesSize()
	f.name.Width = max(8, w-2-catLabelW-1-1) // minus the border, the label, a space and the cursor cell
	f.name.CharLimit = 0
	f.list.clamp(len(f.rows), catRows)
	return nil
}

func (m *Model) updateCategories(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.catForm
	key := msg.String()
	switch f.step {
	case catRename:
		switch key {
		case "esc":
			f.step, f.err = catChoose, ""
			f.name.Blur()
			return m, nil
		case "enter":
			m.renameCategory()
			return m, nil
		}
		var cmd tea.Cmd
		f.name, cmd = f.name.Update(msg)
		f.err = ""
		return m, cmd
	case catInto:
		switch key {
		case "esc":
			f.step, f.err = catChoose, ""
		case "enter":
			f.step, f.err = catConfirm, ""
		default:
			f.where.navigate(key, len(f.into), catRows)
			f.where.clamp(len(f.into), catRows)
		}
		return m, nil
	case catConfirm:
		switch key {
		case "esc", "n":
			f.step, f.err = catInto, ""
		case "enter", "y":
			m.mergeCategory()
		}
		return m, nil
	}
	switch key {
	case "esc":
		m.mode, m.catForm = modeList, nil
	case "r", "enter":
		f.step, f.err = catRename, ""
		f.name.SetValue(f.rows[f.list.cursor].name)
		f.name.CursorEnd()
		return m, f.name.Focus()
	case "m":
		m.chooseMergeTarget()
	default:
		f.list.navigate(key, len(f.rows), catRows)
		f.list.clamp(len(f.rows), catRows)
	}
	return m, nil
}

// chooseMergeTarget lists where the category under the cursor can be merged: the other
// categories of its kind.
func (m *Model) chooseMergeTarget() {
	f := m.catForm
	from := f.rows[f.list.cursor]
	f.into, f.where = nil, listState{}
	for _, r := range f.rows {
		if r.income == from.income && r.name != from.name {
			f.into = append(f.into, r)
		}
	}
	if len(f.into) == 0 {
		f.err = m.loc.T("category_no_other", nil)
		return
	}
	f.step, f.err = catInto, ""
}

// rulesNaming is how many rules of config.toml send to the category or to one below it. They
// are not changed here.
func (m *Model) rulesNaming(name string) int {
	if m.loadConfig == nil {
		return 0
	}
	cfg, err := m.loadConfig()
	if err != nil || cfg == nil {
		return 0
	}
	n := 0
	for _, r := range cfg.Rules {
		if r.Category == name || strings.HasPrefix(r.Category, name+":") {
			n++
		}
	}
	return n
}

// followCategory points what the screens keep a category's name in at its new name, for the
// category and those below it.
func (m *Model) followCategory(from, to string) {
	rename := func(name string) string {
		if name == from || strings.HasPrefix(name, from+":") {
			return to + strings.TrimPrefix(name, from)
		}
		return name
	}
	m.onlyCategory = rename(m.onlyCategory)
	m.budgetSel = rename(m.budgetSel)
}

// categoryDone closes the popup, loads what changed and says what was done.
func (m *Model) categoryDone(parts ...string) {
	m.mode, m.catForm = modeList, nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(m.statusLine(parts), false)
}

// renameCategory gives the category under the cursor the name typed. The change leaves a
// way to take it back.
func (m *Model) renameCategory() {
	f := m.catForm
	from := f.rows[f.list.cursor].name
	to := strings.TrimSpace(f.name.Value())
	if to == "" {
		f.err = m.loc.T("category_type_name", nil)
		return
	}
	if _, err := m.svc.RenameCategory(m.ctx, from, to); err != nil {
		f.err = m.errText(err)
		return
	}
	m.undo = append(m.undo, undoOp{
		undone: func() string {
			return m.loc.T("undid_rename", map[string]any{"From": m.loc.ShortName(from), "To": m.loc.ShortName(to)})
		},
		run: func() error {
			if _, err := m.svc.RenameCategory(m.ctx, to, from); err != nil {
				return err
			}
			m.followCategory(to, from)
			return nil
		},
	})
	m.followCategory(from, to)
	parts := []string{m.loc.T("category_renamed", map[string]any{"From": m.loc.ShortName(from), "To": m.loc.ShortName(to)})}
	if n := m.rulesNaming(from); n > 0 {
		parts = append(parts, m.loc.T("category_rules_left", map[string]any{"Count": n, "Name": from}))
	}
	m.categoryDone(append(parts, m.loc.T("hint_z_undoes", nil))...)
}

// mergeCategory moves the category under the cursor into the one chosen, and removes it.
func (m *Model) mergeCategory() {
	f := m.catForm
	from, into := f.rows[f.list.cursor].name, f.into[f.where.cursor].name
	if _, err := m.svc.MergeCategory(m.ctx, from, into); err != nil {
		f.step, f.err = catInto, m.errText(err)
		return
	}
	m.followCategory(from, into)
	parts := []string{m.loc.T("category_merged", map[string]any{"From": m.loc.ShortName(from), "Into": m.loc.ShortName(into)})}
	if n := m.rulesNaming(from); n > 0 {
		parts = append(parts, m.loc.T("category_rules_left", map[string]any{"Count": n, "Name": from}))
	}
	m.categoryDone(parts...)
}

// categoryLine is a category as a line of the list.
func (m *Model) categoryLine(r catRow, iw int, selected bool) string {
	name := m.loc.ShortName(r.name)
	if r.income {
		name += " +"
	}
	uses := m.loc.T("category_used", map[string]any{"Count": r.used})
	line := " " + leftRight(name, uses, iw-2) + " "
	if selected {
		return selFocused.Render(fit(line, iw))
	}
	return fit(line, iw)
}

func (m *Model) categoriesPopup() string {
	f := m.catForm
	w, h := m.categoriesSize()
	iw := w - 2
	cur := f.rows[f.list.cursor]

	var lines []string
	footer := m.loc.T("categories_footer", nil)
	switch f.step {
	case catInto, catConfirm:
		lines = append(lines, dim.Render(fit(" "+m.loc.T("category_merge_into", map[string]any{"Name": m.loc.ShortName(cur.name)}), iw)))
		f.where.clamp(len(f.into), catRows-1)
		for i := f.where.offset; i < min(f.where.offset+catRows-1, len(f.into)); i++ {
			lines = append(lines, m.categoryLine(f.into[i], iw, i == f.where.cursor))
		}
		lines = append(lines, dim.Render(strings.Repeat("─", iw)))
		footer = m.loc.T("category_merge_footer", nil)
		if f.step == catConfirm {
			into := f.into[f.where.cursor]
			lines = append(lines, wrapped(m.loc.T("category_merge_confirm", map[string]any{"From": m.loc.ShortName(cur.name), "Into": m.loc.ShortName(into.name), "Count": cur.used}), iw, bold)...)
			footer = m.loc.T("category_confirm_footer", nil)
		}
	default:
		f.list.clamp(len(f.rows), catRows)
		for i := f.list.offset; i < min(f.list.offset+catRows, len(f.rows)); i++ {
			lines = append(lines, m.categoryLine(f.rows[i], iw, i == f.list.cursor))
		}
		lines = append(lines, dim.Render(strings.Repeat("─", iw)))
		if f.step == catRename {
			lines = append(lines, accent.Render(fit("▸ "+m.loc.T("label_new_name", nil), catLabelW))+" "+f.name.View())
			lines = append(lines, wrapped(m.loc.T("category_rename_hint", nil), iw, dim)...)
			footer = m.loc.T("category_rename_footer", nil)
		}
	}
	for len(lines) < h-4 {
		lines = append(lines, "")
	}
	msg := wrapped(f.err, iw, errStyle)
	for len(msg) < 2 {
		msg = append(msg, "")
	}
	lines = append(lines, msg...)
	return box(m.loc.T("categories_title", nil), lines, w, h, true, footer)
}
