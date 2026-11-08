package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// The target popup (e on the Budget screen) sets or clears what a category may
// spend in the month on show. The categories are listed with the target each has,
// the amount is typed under them, and an empty amount clears the target. z takes
// the change back, as for everything else.

type targetCat struct {
	name   string
	target ledger.Money
	has    bool
}

type targetForm struct {
	month  string
	cats   []targetCat
	list   listState
	amount textinput.Model
	err    string
}

// targetRows is how many categories the popup shows at once.
const targetRows = 8

func (m *Model) targetSize() (w, h int) {
	return min(max(m.width-6, 30), 56), min(targetRows, len(m.tgt.cats)) + 8 // the list, a rule, the amount, two lines of hint, two of message, the border
}

// openTarget lists the expense categories with the month's target of each, with the
// cursor on the first one.
func (m *Model) openTarget() tea.Cmd {
	if m.period == "" {
		m.setStatus(m.loc.T("target_pick_month", nil), true)
		return nil
	}
	have := make(map[string]ledger.Money)
	for _, l := range m.monthly.Lines {
		if l.HasTarget {
			have[l.Name] = l.Target
		}
	}
	f := &targetForm{month: m.period, amount: newInput("")}
	for _, name := range m.categories {
		if typ, _ := ledger.TypeOf(name); typ != ledger.Expenses {
			continue
		}
		t, ok := have[name]
		f.cats = append(f.cats, targetCat{name: name, target: t, has: ok})
	}
	if len(f.cats) == 0 {
		m.setStatus(m.loc.T("target_no_categories", nil), true)
		return nil
	}
	sort.Slice(f.cats, func(i, j int) bool { return f.cats[i].name < f.cats[j].name })
	m.tgt, m.mode = f, modeTarget
	w, _ := m.targetSize()
	f.amount.Width = max(8, w-2-11-1-1) // minus the border, the label, a space and the cursor cell
	f.amount.Placeholder = m.loc.T("target_placeholder", nil)
	m.showTargetOfCursor()
	return f.amount.Focus()
}

// showTargetOfCursor puts the target of the category under the cursor in the amount.
func (m *Model) showTargetOfCursor() {
	f := m.tgt
	c := f.cats[f.list.cursor]
	f.amount.SetValue("")
	if c.has {
		f.amount.SetValue(m.loc.Amount(c.target))
	}
	f.err = ""
}

func (m *Model) updateTarget(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.tgt
	key := msg.String()
	switch key {
	case "esc":
		m.mode, m.tgt = modeList, nil
		return m, nil
	case "enter":
		m.saveTarget()
		return m, nil
	case "up", "down", "pgup", "pgdown":
		f.list.navigate(key, len(f.cats), targetRows)
		f.list.clamp(len(f.cats), targetRows)
		m.showTargetOfCursor()
		return m, nil
	}
	var cmd tea.Cmd
	f.amount, cmd = f.amount.Update(msg)
	f.err = ""
	return m, cmd
}

// saveTarget sets the target to the amount typed, or clears it when the amount is
// empty, and leaves a way to take that back.
func (m *Model) saveTarget() {
	f := m.tgt
	c := f.cats[f.list.cursor]
	text := strings.TrimSpace(f.amount.Value())
	base := m.monthly.Base

	var err error
	var set ledger.Money
	switch {
	case text == "" && !c.has:
		f.err = m.loc.T("target_type_amount", nil)
		return
	case text == "":
		err = m.svc.ClearBudgetTarget(m.ctx, f.month, c.name)
	default:
		if set, err = ledger.ParseAmount(text, base, 0); err != nil {
			f.err = m.loc.T("error_amount", map[string]any{"Error": m.errText(err)})
			return
		}
		err = m.svc.SetBudgetTarget(m.ctx, f.month, c.name, set)
	}
	if err != nil {
		f.err = m.errText(err)
		return
	}

	month, name, before := f.month, c.name, c
	m.undo = append(m.undo, undoOp{
		undone: func() string {
			return m.loc.T("undid_target", map[string]any{"Name": m.loc.ShortName(name), "Period": m.periodLabel(month)})
		},
		run: func() error {
			if before.has {
				return m.svc.SetBudgetTarget(m.ctx, month, name, before.target)
			}
			return m.svc.ClearBudgetTarget(m.ctx, month, name)
		},
	})
	m.mode, m.tgt = modeList, nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	args := map[string]any{"Name": m.loc.ShortName(name), "Period": m.periodLabel(month), "Amount": m.loc.Money(set)}
	said := m.loc.T("target_set", args)
	if text == "" {
		said = m.loc.T("target_cleared", args)
	}
	m.setStatus(m.statusLine([]string{said, m.loc.T("hint_z_undoes", nil)}), false)
}

func (m *Model) targetPopup() string {
	f := m.tgt
	w, h := m.targetSize()
	iw := w - 2

	var lines []string
	f.list.clamp(len(f.cats), targetRows)
	for i := f.list.offset; i < min(f.list.offset+targetRows, len(f.cats)); i++ {
		c := f.cats[i]
		amount := ""
		if c.has {
			amount = m.loc.Amount(c.target)
		}
		line := " " + leftRight(m.loc.ShortName(c.name), amount, iw-2) + " "
		if i == f.list.cursor {
			line = selFocused.Render(fit(line, iw))
		} else {
			line = fit(line, iw)
		}
		lines = append(lines, line)
	}
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	lines = append(lines, accent.Render(fit("▸ "+m.loc.T("label_target", nil), 11))+" "+f.amount.View())
	hint := wrapped(m.loc.T("target_hint", map[string]any{"Currency": m.monthly.Base}), iw, dim)
	lines = append(lines, hint...)
	for len(lines) < h-4 {
		lines = append(lines, "")
	}
	msg := wrapped(f.err, iw, errStyle)
	for len(msg) < 2 {
		msg = append(msg, "")
	}
	lines = append(lines, msg...)
	return box(m.loc.T("target_title", map[string]any{"Period": m.periodLabel(f.month)}), lines, w, h, true, m.loc.T("target_footer", nil))
}
