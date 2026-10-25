package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func (m *Model) openPicker() tea.Cmd {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	if !r.simple {
		m.setStatus("transfers and split transactions cannot be recategorized here", true)
		return nil
	}
	m.mode = modePick
	m.pickTarget = r.tx.ID
	m.pickPayee = r.tx.Payee
	m.pickIsIncome = r.source.Amount.Minor > 0
	m.pickCursor = 0
	m.picker.SetValue("")
	m.pickRule.SetValue("")
	m.pickRule.Blur()
	m.pickRuleOn, m.pickCfg, m.pickNote, m.pickNoteBad = false, nil, "", false
	m.refreshPicker()
	return m.picker.Focus()
}

// refreshPicker ranks the categories for the transaction being categorised.
func (m *Model) refreshPicker() {
	m.pickItems = m.rankCategories(strings.TrimSpace(m.picker.Value()), m.pickIsIncome, m.pickPayee, m.pickTarget)
	m.clampPickCursor()
}

// rankCategories lists the categories matching what has been typed, best
// first. With nothing typed, the category last used for the same payee comes
// first, then categories of the matching kind (income for money in, expenses
// for money out), most used first. A full account name that does not exist yet
// is offered as a new category, and, with nothing typed, "uncategorized" closes
// the list so a decision can wait. skip is a transaction to ignore when looking
// up the payee's previous category (the one being edited).
func (m *Model) rankCategories(q string, isIncome bool, payee string, skip int64) []pickItem {
	type scored struct {
		pickItem
		score    int
		kindOK   bool
		sameCnt  int
		lastUsed bool
	}
	lastForPayee := ""
	if payee = strings.TrimSpace(payee); payee != "" {
		for _, r := range m.rows { // newest first
			if r.simple && !r.uncategorized && r.tx.ID != skip && strings.EqualFold(r.tx.Payee, payee) {
				lastForPayee = r.category.Account
				break
			}
		}
	}
	var items []scored
	exact := false
	for _, name := range m.categories {
		s, ok := fuzzyScore(q, name)
		if !ok {
			continue
		}
		if name == q {
			exact = true
		}
		it := scored{
			pickItem: pickItem{name: name},
			score:    s,
			kindOK:   strings.HasPrefix(name, "Income:") == isIncome,
			sameCnt:  m.counts[name],
			lastUsed: name == lastForPayee,
		}
		if it.lastUsed {
			it.hint = "same payee"
		} else if it.sameCnt > 0 {
			it.hint = fmt.Sprintf("%d×", it.sameCnt)
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if q != "" && a.score != b.score {
			return a.score > b.score
		}
		if a.lastUsed != b.lastUsed {
			return a.lastUsed
		}
		if a.kindOK != b.kindOK {
			return a.kindOK
		}
		if a.sameCnt != b.sameCnt {
			return a.sameCnt > b.sameCnt
		}
		return a.name < b.name
	})
	out := make([]pickItem, 0, len(items)+2)
	for _, it := range items {
		out = append(out, it.pickItem)
	}
	if q != "" && !exact {
		if err := ledger.ValidateAccountName(q); err == nil {
			if typ, _ := ledger.TypeOf(q); !typ.NeedsCurrency() {
				out = append(out, pickItem{name: q, hint: "new category", create: true})
			}
		}
	}
	if q == "" {
		unc := ledger.UncategorizedExpense
		if isIncome {
			unc = ledger.UncategorizedIncome
		}
		out = append(out, pickItem{name: unc, hint: "decide later"})
	}
	return out
}

func (m *Model) clampPickCursor() {
	if m.pickCursor >= len(m.pickItems) {
		m.pickCursor = len(m.pickItems) - 1
	}
	if m.pickCursor < 0 {
		m.pickCursor = 0
	}
}

func (m *Model) updatePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closePicker()
		return m, nil
	case "tab", "shift+tab":
		return m, m.toggleRuleField()
	case "up", "ctrl+p":
		m.pickCursor--
		m.clampPickCursor()
		m.refreshRuleNote()
		return m, nil
	case "down", "ctrl+n":
		m.pickCursor++
		m.clampPickCursor()
		m.refreshRuleNote()
		return m, nil
	case "enter":
		if m.pickCursor < len(m.pickItems) {
			m.applyCategory(m.pickItems[m.pickCursor].name)
		}
		return m, nil
	}
	var cmd tea.Cmd
	if m.pickRuleOn {
		m.pickRule, cmd = m.pickRule.Update(msg)
	} else {
		m.picker, cmd = m.picker.Update(msg)
		m.pickCursor = 0
		m.refreshPicker()
	}
	m.refreshRuleNote()
	return m, cmd
}

func (m *Model) closePicker() {
	m.mode = modeList
	m.picker.Blur()
	m.pickRule.Blur()
}

// ---- rules made while categorizing ----------------------------------------------

// toggleRuleField moves between the category and the rule field. The first time
// the rule field is entered it reads the config and offers the payee as the
// text to look for, to be trimmed down to the part that matters ("REWE").
func (m *Model) toggleRuleField() tea.Cmd {
	if m.pickRuleOn {
		m.pickRuleOn = false
		m.pickRule.Blur()
		return m.picker.Focus()
	}
	if m.pickCfg == nil {
		cfg := &config.Config{}
		if m.loadConfig != nil {
			var err error
			if cfg, err = m.loadConfig(); err != nil {
				m.pickNote, m.pickNoteBad = "fix the config first (esc, then C): "+err.Error(), true
				return nil
			}
		}
		m.pickCfg = cfg
	}
	m.picker.Blur()
	m.pickRuleOn = true
	if strings.TrimSpace(m.pickRule.Value()) == "" {
		m.pickRule.SetValue(clean(m.pickPayee))
		m.pickRule.CursorEnd()
	}
	m.refreshRuleNote()
	return m.pickRule.Focus()
}

// ruleCheck is what adding a rule would do.
type ruleCheck struct {
	problem string // why it cannot be added; empty when it can
	covered int    // 1-based number of an existing rule that already does this, else 0
	others  int    // other uncategorized transactions it would categorize
}

// checkRule judges a rule that looks for match and sends to category. Rules are
// tried top to bottom and the first match wins, so a rule added at the end that
// an earlier rule overrules would never fire: that is refused and the earlier
// rule named.
func (m *Model) checkRule(match, category string) ruleCheck {
	var c ruleCheck
	row, ok := m.findRow(m.pickTarget)
	switch {
	case m.opts.ConfigPath == "" || m.pickCfg == nil:
		c.problem = "no config file to add the rule to"
	case len([]rune(match)) < 2:
		c.problem = "a rule needs at least two characters to look for"
	case category == ledger.UncategorizedExpense || category == ledger.UncategorizedIncome:
		c.problem = "pick a real category for a rule, not \"decide later\""
	case !ok:
		c.problem = "that transaction is gone"
	case !strings.Contains(strings.ToLower(row.tx.Payee+" "+row.tx.Description), strings.ToLower(match)):
		c.problem = "\"" + match + "\" does not appear in this transaction's payee or description"
	}
	if c.problem != "" {
		return c
	}
	rules, err := importer.NewRuleset(m.pickCfg.Rules)
	if err != nil {
		c.problem = "the config's rules are invalid: " + err.Error()
		return c
	}
	if i, existing, found := rules.Find(row.tx.Payee, row.tx.Description); found {
		if existing != category {
			c.problem = fmt.Sprintf("rule %d already sends this to %s and is checked first; change it with C", i+1, existing)
			return c
		}
		c.covered = i + 1
	}
	res, err := m.svc.Recategorize(m.ctx, core.RecategorizeRequest{Rules: []importer.Rule{{Contains: match, Category: category}}, DryRun: true})
	if err != nil {
		c.problem = err.Error()
		return c
	}
	for _, mv := range res.Moves {
		if mv.TransactionID != m.pickTarget {
			c.others++
		}
	}
	return c
}

// refreshRuleNote explains the rule that is typed, if there is one.
func (m *Model) refreshRuleNote() {
	match := clean(m.pickRule.Value())
	if match == "" || m.pickCfg == nil {
		if m.pickCfg != nil {
			m.pickNote, m.pickNoteBad = "", false
		}
		return
	}
	category := ""
	if m.pickCursor < len(m.pickItems) {
		category = m.pickItems[m.pickCursor].name
	}
	c := m.checkRule(match, category)
	others := "no other uncategorized match"
	switch c.others {
	case 0:
	case 1:
		others = "also categorizes 1 other"
	default:
		others = fmt.Sprintf("also categorizes %d others", c.others)
	}
	switch {
	case c.problem != "":
		m.pickNote, m.pickNoteBad = c.problem, true
	case c.covered > 0:
		m.pickNote, m.pickNoteBad = fmt.Sprintf("rule %d already covers this · %s", c.covered, others), false
	default:
		m.pickNote, m.pickNoteBad = "adds a rule to config.toml · "+others, false
	}
}
