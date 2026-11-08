package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// The target popup (e on the Budget screen) sets or clears what a category may
// spend in the month on show. The categories are listed with the target each has,
// the amount is typed under them, and an empty amount clears the target. tab
// switches whether the category carries what is left of it into the next month, from
// the month on show. z takes each change back, as for everything else.

type targetCat struct {
	name   string
	target ledger.Money
	has    bool
	// rollFrom is the month the category started carrying over, "" if it does not.
	// roll says it is carrying in the month on show: it started in it or before.
	rollFrom string
	roll     bool
}

type targetForm struct {
	month    string
	cats     []targetCat
	list     listState
	amount   textinput.Model
	rollover bool // whether the category under the cursor will carry over once saved
	err      string
}

// targetRows is how many categories the popup shows at once, and targetLabelW the width of
// the names of its two fields.
const (
	targetRows   = 8
	targetLabelW = 12
)

func (m *Model) targetSize() (w, h int) {
	return min(max(m.width-6, 30), 60), min(targetRows, len(m.tgt.cats)) + 9 // the list, a rule, the amount, the carry-over, two lines of hint, two of message, the border
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
	rollovers, err := m.svc.BudgetRollovers(m.ctx)
	if err != nil {
		m.setStatus(m.errText(err), true)
		return nil
	}
	from := make(map[string]string)
	for _, r := range rollovers {
		from[r.Category] = r.From
	}
	f := &targetForm{month: m.period, amount: newInput("")}
	for _, name := range m.categories {
		if typ, _ := ledger.TypeOf(name); typ != ledger.Expenses {
			continue
		}
		t, ok := have[name]
		start := from[name]
		f.cats = append(f.cats, targetCat{name: name, target: t, has: ok, rollFrom: start, roll: start != "" && start <= m.period})
	}
	if len(f.cats) == 0 {
		m.setStatus(m.loc.T("target_no_categories", nil), true)
		return nil
	}
	sort.Slice(f.cats, func(i, j int) bool { return f.cats[i].name < f.cats[j].name })
	m.tgt, m.mode = f, modeTarget
	w, _ := m.targetSize()
	f.amount.Width = max(8, w-2-targetLabelW-1-1) // minus the border, the label, a space and the cursor cell
	f.amount.Placeholder = m.loc.T("target_placeholder", nil)
	m.showTargetOfCursor()
	return f.amount.Focus()
}

// showTargetOfCursor puts the target of the category under the cursor in the amount,
// and its carry-over beside it.
func (m *Model) showTargetOfCursor() {
	f := m.tgt
	c := f.cats[f.list.cursor]
	f.amount.SetValue("")
	f.rollover = c.roll
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
	case "tab":
		f.rollover = !f.rollover
		f.err = ""
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
// empty, and switches the carry-over as asked. Each change leaves a way to take it back.
func (m *Model) saveTarget() {
	f := m.tgt
	c := f.cats[f.list.cursor]
	text := strings.TrimSpace(f.amount.Value())
	rollChanged := f.rollover != c.roll

	// Everything is checked before anything is written.
	var set ledger.Money
	targetChanged, clearing := false, false
	switch {
	case text == "" && !c.has:
		if !rollChanged {
			f.err = m.loc.T("target_type_amount", nil)
			return
		}
	case text == "":
		targetChanged, clearing = true, true
	default:
		var err error
		if set, err = ledger.ParseAmount(text, m.monthly.Base, 0); err != nil {
			f.err = m.loc.T("error_amount", map[string]any{"Error": m.errText(err)})
			return
		}
		targetChanged = !c.has || set != c.target
	}

	month, name, before := f.month, c.name, c
	args := map[string]any{"Name": m.loc.ShortName(name), "Period": m.periodLabel(month), "Amount": m.loc.Money(set)}
	var said []string
	if targetChanged {
		var err error
		if clearing {
			err = m.svc.ClearBudgetTarget(m.ctx, month, name)
		} else {
			err = m.svc.SetBudgetTarget(m.ctx, month, name, set)
		}
		if err != nil {
			f.err = m.errText(err)
			return
		}
		m.undo = append(m.undo, undoOp{
			undone: func() string { return m.loc.T("undid_target", args) },
			run: func() error {
				if before.has {
					return m.svc.SetBudgetTarget(m.ctx, month, name, before.target)
				}
				return m.svc.ClearBudgetTarget(m.ctx, month, name)
			},
		})
		said = append(said, m.loc.T("target_set", args))
		if clearing {
			said[0] = m.loc.T("target_cleared", args)
		}
	}
	if rollChanged {
		var err error
		if f.rollover {
			err = m.svc.SetBudgetRollover(m.ctx, name, month)
		} else {
			err = m.svc.ClearBudgetRollover(m.ctx, name)
		}
		if err != nil {
			f.err = m.errText(err)
			return
		}
		m.undo = append(m.undo, undoOp{
			undone: func() string { return m.loc.T("undid_rollover", args) },
			run: func() error {
				if before.rollFrom != "" {
					return m.svc.SetBudgetRollover(m.ctx, name, before.rollFrom)
				}
				return m.svc.ClearBudgetRollover(m.ctx, name)
			},
		})
		if f.rollover {
			said = append(said, m.loc.T("rollover_on", args))
		} else {
			said = append(said, m.loc.T("rollover_off", args))
		}
	}

	m.mode, m.tgt = modeList, nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	if targetChanged && rollChanged { // one line that fits, with the hint still in it
		parts := []string{m.loc.T("target_part_set", args), m.loc.T("rollover_part_off", nil)}
		if clearing {
			parts[0] = m.loc.T("target_part_cleared", nil)
		}
		if f.rollover {
			parts[1] = m.loc.T("rollover_part_on", args)
		}
		args["Changes"] = strings.Join(parts, " · ")
		said = []string{m.loc.T("changes_for", args)}
	}
	if len(said) > 0 {
		m.setStatus(m.statusLine(append(said, m.loc.T("hint_z_undoes", nil))), false)
	}
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
		name := m.loc.ShortName(c.name)
		if c.roll {
			name += " ↻"
		}
		line := " " + leftRight(name, amount, iw-2) + " "
		if i == f.list.cursor {
			line = selFocused.Render(fit(line, iw))
		} else {
			line = fit(line, iw)
		}
		lines = append(lines, line)
	}
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	lines = append(lines, accent.Render(fit("▸ "+m.loc.T("label_target", nil), targetLabelW))+" "+f.amount.View())
	mark := "[ ]"
	if f.rollover {
		mark = "[x]"
	}
	lines = append(lines, dim.Render(fit("  "+m.loc.T("label_carry", nil), targetLabelW))+" "+mark+dim.Render("  "+m.loc.T("carry_hint", nil)))
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

// copyTargets starts the month on show from the targets of the month before. It adds
// what the month lacks and keeps what it has, so nothing is overwritten and there is
// nothing to ask; z takes back what it added.
func (m *Model) copyTargets() {
	first, err := time.Parse(monthLayout, m.period)
	if err != nil { // all time
		m.setStatus(m.loc.T("target_pick_month", nil), true)
		return
	}
	from, to := first.AddDate(0, -1, 0).Format(monthLayout), m.period
	res, err := m.svc.CopyBudgetTargets(m.ctx, from, to)
	if err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	args := map[string]any{"From": m.periodLabel(from), "To": m.periodLabel(to), "Count": len(res.Copied)}
	switch {
	case len(res.Copied) == 0 && res.Kept == 0:
		m.setStatus(m.loc.T("targets_copy_none", args), true)
		return
	case len(res.Copied) == 0:
		m.setStatus(m.loc.T("targets_copy_nothing_new", args), true)
		return
	}
	added := res.Copied
	m.undo = append(m.undo, undoOp{
		undone: func() string { return m.loc.T("undid_targets_copy", args) },
		run: func() error {
			for _, t := range added {
				if err := m.svc.ClearBudgetTarget(m.ctx, to, t.Category); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	parts := []string{m.loc.T("targets_copied", args)}
	if res.Kept > 0 {
		parts = append(parts, m.loc.T("targets_kept", map[string]any{"Count": res.Kept}))
	}
	m.setStatus(m.statusLine(append(parts, m.loc.T("hint_z_undoes", nil))), false)
}
