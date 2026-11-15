package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The rules popup (M) lists the categorization rules of config.toml in the order they are
// checked, the first that matches winning, and edits the one under the cursor: its text and
// category, its place in the order, or deleting it. The file is rewritten by the config
// package, which leaves the comments and the rest of the file alone and refuses a file it
// cannot rewrite safely. Every change leaves a way to take it back, with z, also from the
// popup. Transactions a rule has already categorized are not touched by any of that; a
// applies the rules to the ones still uncategorized, after saying how many it would be.

type ruleStep int

const (
	ruleChoose ruleStep = iota // choosing the rule
	ruleEdit                   // editing its text and category
	ruleDelete                 // being asked before it is deleted
	ruleApply                  // being asked before the rules are applied to the uncategorized
)

type rulesForm struct {
	rules      []importer.Rule
	list       listState
	step       ruleStep
	text       textinput.Model // what to look for
	category   textinput.Model
	onCategory bool // the cursor of the form is in the category
	err, note  string
	apply      core.RecategorizeResult // what applying the rules would do, while it is asked about
}

// ruleRows is how many rules the popup shows at once, and ruleLabelW the width of the names
// of the fields.
const (
	ruleRows   = 8
	ruleLabelW = 12
)

func (m *Model) rulesSize() (w, h int) {
	return min(max(m.width-6, 30), 72), min(ruleRows, len(m.ruleForm.rules)) + 11 // the list, a rule, two fields or the question, hints, two lines of message, the border
}

// openRules lists the rules of the config file.
func (m *Model) openRules() tea.Cmd {
	rules, err := m.readRules()
	if err != nil {
		m.setStatus(m.errText(err), true)
		return nil
	}
	if len(rules) == 0 {
		m.setStatus(m.loc.T("rules_none", nil), true)
		return nil
	}
	m.ruleForm = &rulesForm{rules: rules, text: newInput(""), category: newInput("")}
	m.mode = modeRules
	w, _ := m.rulesSize()
	for _, in := range []*textinput.Model{&m.ruleForm.text, &m.ruleForm.category} {
		in.Width = max(8, w-2-ruleLabelW-1-1) // minus the border, the label, a space and the cursor cell
		in.CharLimit = 0
	}
	return nil
}

// readRules is the rules of the config file as it is now.
func (m *Model) readRules() ([]importer.Rule, error) {
	if m.opts.ConfigPath == "" || m.loadConfig == nil {
		return nil, coded.New("rules_no_config", nil)
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return nil, err
	}
	return cfg.Rules, nil
}

// reloadRules reads the file again after a change, and closes the popup if it cannot.
func (m *Model) reloadRules() {
	f := m.ruleForm
	rules, err := m.readRules()
	if err != nil {
		m.mode, m.ruleForm = modeList, nil
		m.setStatus(m.loc.T("config_problem", map[string]any{"Error": m.errText(err)}), true)
		return
	}
	f.rules = rules
	if len(rules) == 0 {
		m.mode, m.ruleForm = modeList, nil
		m.setStatus(m.loc.T("rules_none", nil), false)
		return
	}
	f.list.clamp(len(rules), ruleRows)
}

func (m *Model) updateRules(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.ruleForm
	key := msg.String()
	switch f.step {
	case ruleEdit:
		switch key {
		case "esc":
			f.step, f.err = ruleChoose, ""
			f.text.Blur()
			f.category.Blur()
		case "enter":
			m.saveRule()
		case "tab", "shift+tab", "up", "down":
			f.onCategory = !f.onCategory
			return m, m.focusRuleField()
		default:
			var cmd tea.Cmd
			if f.onCategory {
				f.category, cmd = f.category.Update(msg)
			} else {
				f.text, cmd = f.text.Update(msg)
			}
			f.err = ""
			return m, cmd
		}
		return m, nil
	case ruleDelete:
		switch key {
		case "esc", "n":
			f.step = ruleChoose
		case "enter", "y":
			m.deleteRule()
		}
		return m, nil
	case ruleApply:
		switch key {
		case "esc", "n":
			f.step = ruleChoose
		case "enter", "y":
			m.applyRules()
		}
		return m, nil
	}
	f.note = ""
	switch key {
	case "esc":
		m.mode, m.ruleForm = modeList, nil
	case "e", "enter":
		r := f.rules[f.list.cursor]
		f.step, f.err, f.onCategory = ruleEdit, "", false
		f.text.SetValue(ruleText(r))
		f.text.CursorEnd()
		f.category.SetValue(r.Category)
		f.category.CursorEnd()
		return m, m.focusRuleField()
	case "d":
		f.step, f.err = ruleDelete, ""
	case "a":
		m.askApply()
	case "<":
		m.moveRule(-1)
	case ">":
		m.moveRule(1)
	case "z":
		m.undoRule()
	default:
		f.err = ""
		f.list.navigate(key, len(f.rules), ruleRows)
		f.list.clamp(len(f.rules), ruleRows)
	}
	return m, nil
}

// focusRuleField puts the cursor in the field the form is on.
func (m *Model) focusRuleField() tea.Cmd {
	f := m.ruleForm
	if f.onCategory {
		f.text.Blur()
		return f.category.Focus()
	}
	f.category.Blur()
	return f.text.Focus()
}

// ruleText is what a rule looks for, whichever way it does.
func ruleText(r importer.Rule) string {
	if r.Regex != "" {
		return r.Regex
	}
	return r.Contains
}

// ruleChanged records a change to the file so that z can take it back.
func (m *Model) ruleChanged(c config.Change) {
	path := m.opts.ConfigPath
	m.undo = append(m.undo, undoOp{
		rules:  true,
		undone: func() string { return m.loc.T("undid_rules", nil) },
		run:    func() error { return config.Revert(path, c) },
	})
}

// saveRule writes the text and category typed into the rule under the cursor.
func (m *Model) saveRule() {
	f := m.ruleForm
	i := f.list.cursor
	old := f.rules[i]
	r := importer.Rule{Category: strings.TrimSpace(f.category.Value())}
	if old.Regex != "" {
		r.Regex = f.text.Value()
	} else {
		r.Contains = f.text.Value()
	}
	switch {
	case r.Category == ledger.UncategorizedExpense || r.Category == ledger.UncategorizedIncome:
		f.err = m.loc.T("rule_pick_real", nil)
		return
	case r.Contains != "" && len([]rune(strings.TrimSpace(r.Contains))) < 2:
		f.err = m.loc.T("rule_too_short", nil)
		return
	}
	r.Contains, r.Regex = strings.TrimSpace(r.Contains), strings.TrimSpace(r.Regex)
	if r == old {
		f.step = ruleChoose
		return
	}
	change, err := config.EditRule(m.opts.ConfigPath, i, r)
	if err != nil {
		f.err = m.errText(err)
		return
	}
	m.ruleChanged(change)
	f.step, f.err = ruleChoose, ""
	f.text.Blur()
	f.category.Blur()
	m.reloadRules()
	m.noteRule(m.loc.T("rule_edited", map[string]any{"Rule": i + 1}))
}

// moveRule makes the rule under the cursor checked one place earlier (-1) or later (1).
func (m *Model) moveRule(delta int) {
	f := m.ruleForm
	i := f.list.cursor
	if i+delta < 0 || i+delta >= len(f.rules) {
		return
	}
	change, err := config.MoveRule(m.opts.ConfigPath, i, delta)
	if err != nil {
		f.err = m.errText(err)
		return
	}
	m.ruleChanged(change)
	f.list.cursor = i + delta
	m.reloadRules()
	args := map[string]any{"From": i + 1, "To": i + delta + 1}
	if delta > 0 {
		m.noteRule(m.loc.T("rule_moved_down", args))
	} else {
		m.noteRule(m.loc.T("rule_moved_up", args))
	}
}

// deleteRule removes the rule under the cursor, once it has been asked about.
func (m *Model) deleteRule() {
	f := m.ruleForm
	i := f.list.cursor
	change, err := config.DeleteRule(m.opts.ConfigPath, i)
	f.step = ruleChoose
	if err != nil {
		f.err = m.errText(err)
		return
	}
	m.ruleChanged(change)
	m.reloadRules()
	m.noteRule(m.loc.T("rule_deleted", map[string]any{"Rule": i + 1}))
}

// askApply works out what the rules would do to the uncategorized transactions, and asks
// before doing it, or says there is nothing to do.
func (m *Model) askApply() {
	f := m.ruleForm
	res, err := m.svc.Recategorize(m.ctx, core.RecategorizeRequest{Rules: f.rules, DryRun: true})
	switch {
	case err != nil:
		f.err = m.errText(err)
	case res.Uncategorized == 0:
		f.note, f.err = m.loc.T("rules_apply_nothing", nil), ""
	case len(res.Moves) == 0:
		f.note, f.err = m.loc.T("rules_apply_none", map[string]any{"Total": res.Uncategorized}), ""
	default:
		f.apply, f.step, f.err = res, ruleApply, ""
	}
}

// applyRules categorizes what askApply found, and leaves a way to take it back.
func (m *Model) applyRules() {
	f := m.ruleForm
	f.step = ruleChoose
	res, err := m.svc.Recategorize(m.ctx, core.RecategorizeRequest{Rules: f.rules})
	if err != nil {
		f.err = m.errText(err)
		return
	}
	var back []core.CategoryChange
	for _, mv := range res.Moves {
		back = append(back, core.CategoryChange{TransactionID: mv.TransactionID, Category: uncategorizedOf(mv)})
	}
	n := len(res.Moves)
	if n > 0 {
		m.undo = append(m.undo, undoOp{
			rules:  true,
			undone: func() string { return m.loc.T("undid_rules_applied", map[string]any{"Count": n}) },
			run: func() error {
				_, err := m.svc.SetCategories(m.ctx, back)
				return err
			},
		})
	}
	if err := m.reload(); err != nil {
		f.err = m.errText(err)
		return
	}
	m.applyFilter(true)
	m.noteRule(m.loc.T("rules_applied", map[string]any{"Count": n}))
}

// undoRule takes back the last change to the rules, if the last thing undone is one.
func (m *Model) undoRule() {
	f := m.ruleForm
	if n := len(m.undo); n == 0 || !m.undo[n-1].rules {
		f.err = m.loc.T("nothing_to_undo", nil)
		return
	}
	m.undoLast()
	if m.ruleForm == nil {
		return
	}
	m.reloadRules()
	if m.ruleForm == nil {
		return
	}
	if m.statusIsError {
		f.err = m.status
	} else {
		f.note = m.status
	}
	m.status, m.statusIsError = "", false
}

// noteRule says what has been done in the popup.
func (m *Model) noteRule(s string) {
	if m.ruleForm != nil {
		m.ruleForm.note, m.ruleForm.err = s+" · "+m.loc.T("hint_z_undoes", nil), ""
	}
}

// ruleLine is a rule as a line of the list: its place, what it looks for and its category.
func (m *Model) ruleLine(n int, r importer.Rule, iw int, selected bool) string {
	pattern := `"` + r.Contains + `"`
	if r.Regex != "" {
		pattern = "/" + r.Regex + "/"
	}
	line := " " + leftRight(fmt.Sprintf("%2d %s", n, pattern), "→ "+r.Category, iw-2) + " "
	if selected {
		return selFocused.Render(fit(line, iw))
	}
	return fit(line, iw)
}

func (m *Model) rulesPopup() string {
	f := m.ruleForm
	w, h := m.rulesSize()
	iw := w - 2
	f.list.clamp(len(f.rules), ruleRows)

	var lines []string
	for i := f.list.offset; i < min(f.list.offset+ruleRows, len(f.rules)); i++ {
		lines = append(lines, m.ruleLine(i+1, f.rules[i], iw, i == f.list.cursor))
	}
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	footer := m.loc.T("rules_footer", nil)
	cur := f.rules[f.list.cursor]
	switch f.step {
	case ruleEdit:
		label := m.loc.T("label_contains", nil)
		if cur.Regex != "" {
			label = m.loc.T("label_regex", nil)
		}
		lines = append(lines,
			m.ruleField(label, f.text, !f.onCategory),
			m.ruleField(m.loc.T("label_category", nil), f.category, f.onCategory))
		lines = append(lines, wrapped(m.loc.T("rule_edit_hint", nil), iw, dim)...)
		footer = m.loc.T("rule_edit_footer", nil)
	case ruleApply:
		lines = append(lines, wrapped(m.loc.T("rules_apply_confirm", map[string]any{"Count": len(f.apply.Moves), "Total": f.apply.Uncategorized}), iw, bold)...)
		footer = m.loc.T("rules_apply_footer", nil)
	case ruleDelete:
		pattern := `"` + cur.Contains + `"`
		if cur.Regex != "" {
			pattern = "/" + cur.Regex + "/"
		}
		lines = append(lines, wrapped(m.loc.T("rule_delete_confirm", map[string]any{"Rule": f.list.cursor + 1, "Pattern": pattern, "Category": cur.Category}), iw, bold)...)
		footer = m.loc.T("rule_delete_footer", nil)
	default:
		lines = append(lines, wrapped(m.loc.T("rules_hint", nil), iw, dim)...)
	}
	for len(lines) < h-4 {
		lines = append(lines, "")
	}
	msg := wrapped(f.err, iw, errStyle)
	if f.err == "" {
		msg = wrapped(f.note, iw, dim)
	}
	for len(msg) < 2 {
		msg = append(msg, "")
	}
	lines = append(lines, msg...)
	return box(m.loc.T("rules_title", nil), lines, w, h, true, footer)
}

// ruleField is a field of the edit form: its name and what is typed in it.
func (m *Model) ruleField(label string, in textinput.Model, focused bool) string {
	if focused {
		return accent.Render(fit("▸ "+label, ruleLabelW)) + " " + in.View()
	}
	return dim.Render(fit("  "+label, ruleLabelW)) + " " + in.View()
}
