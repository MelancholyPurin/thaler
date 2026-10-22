package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
	case "up", "ctrl+p":
		m.pickCursor--
		m.clampPickCursor()
		return m, nil
	case "down", "ctrl+n":
		m.pickCursor++
		m.clampPickCursor()
		return m, nil
	case "enter":
		if m.pickCursor < len(m.pickItems) {
			m.applyCategory(m.pickItems[m.pickCursor].name)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	m.pickCursor = 0
	m.refreshPicker()
	return m, cmd
}

func (m *Model) closePicker() {
	m.mode = modeList
	m.picker.Blur()
}
