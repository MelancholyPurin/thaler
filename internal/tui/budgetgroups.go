package tui

import (
	"sort"
	"strings"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// Category groups on the Budget (see core.GroupOf): categories below one another sit
// under a row of their group, which adds them up and can be folded so that only it shows.
// space on a group, or on one of its categories, folds and unfolds it; ← folds it, → unfolds
// it, and on a category ← goes to the group. enter on the group opens the Ledger with the
// transactions of all of its categories. A folded group stays folded while Thaler runs.

// groupMark starts the key of a selected group, which no category name can: it is the
// group's name, which a category of its own can have as well.
const groupMark = "#"

func groupKey(group string) string { return groupMark + group }

func isGroupKey(key string) bool { return strings.HasPrefix(key, groupMark) }

// keyName is the category or group a selection is about.
func keyName(key string) string { return strings.TrimPrefix(key, groupMark) }

// budgetRow is a line of the categories on the Budget: a category, or a group.
type budgetRow struct {
	key    string // what is selected: a category's name, or groupKey of a group's
	label  string
	lead   string // what comes before the label: the fold sign of a group, or the indent of a member
	group  string // the group it is, or is in
	header bool   // the row of a group
	amount ledger.Money
	// line has the figures of a row with money set aside, and planned says it has (a group
	// with a member that has).
	line    core.MonthBudgetLine
	planned bool
}

// folded says the group is shown as one row.
func (m *Model) folded(group string) bool { return m.folds[group] }

func (m *Model) leadOf(group string, header bool) string {
	switch {
	case !header:
		return "  "
	case m.folded(group):
		return "+ "
	}
	return "- "
}

// memberLabel is a category of a group as it is written under it: without the group's name.
func (m *Model) memberLabel(group, name string) string {
	if name == group {
		return m.loc.T("budget_group_direct", map[string]any{"Name": m.loc.ShortName(name)})
	}
	return strings.TrimPrefix(name, group+":")
}

// rowGroup is a category's row, or a group's row with the rows of its categories.
type rowGroup struct {
	amount int64
	name   string
	rows   []budgetRow
}

// arrange puts the groups of rows in order, the largest first and then by name, and joins
// them, leaving out the categories of a group that is folded.
func (m *Model) arrange(items []rowGroup) []budgetRow {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].amount != items[j].amount {
			return items[i].amount > items[j].amount
		}
		return items[i].name < items[j].name
	})
	var out []budgetRow
	for _, it := range items {
		for i, r := range it.rows {
			switch {
			case i == 0 && r.header:
				r.lead = m.leadOf(r.group, true)
			case r.group != "":
				if m.folded(r.group) {
					continue
				}
				r.lead = m.leadOf(r.group, false)
			}
			out = append(out, r)
		}
	}
	return out
}

// spendRows lists what was spent in each category, the largest first, with the categories
// of a group under a row for it that adds them up.
func (m *Model) spendRows(list []core.BudgetLine) []budgetRow {
	names := make([]string, len(list))
	for i, l := range list {
		names[i] = l.Name
	}
	groupOf := core.GroupsOf(names)
	var items []rowGroup
	at := make(map[string]int) // where the group's item is
	for _, l := range list {
		g := groupOf[l.Name]
		if g == "" {
			items = append(items, rowGroup{l.Amount.Minor, l.Name, []budgetRow{{key: l.Name, label: m.loc.ShortName(l.Name), amount: l.Amount}}})
			continue
		}
		i, ok := at[g]
		if !ok {
			i = len(items)
			at[g] = i
			items = append(items, rowGroup{name: g, rows: []budgetRow{{key: groupKey(g), label: m.loc.ShortName(g), group: g, header: true, amount: ledger.Money{Currency: l.Amount.Currency}}}})
		}
		items[i].amount += l.Amount.Minor
		items[i].rows[0].amount.Minor += l.Amount.Minor
		items[i].rows = append(items[i].rows, budgetRow{key: l.Name, label: m.memberLabel(g, l.Name), group: g, amount: l.Amount})
	}
	return m.arrange(items)
}

// planRows are the categories of the month with money set aside, and with them the groups
// that have a category with some, with all of their categories under them; and the others
// that spent.
func (m *Model) planRows() (planned, other []budgetRow) {
	grouped := make(map[string]bool)      // in a group
	groupPlanned := make(map[string]bool) // in a group with money set aside
	for _, g := range m.monthly.Groups {
		for _, n := range g.Members {
			grouped[n] = true
			groupPlanned[n] = g.Planned
		}
	}
	lineOf := make(map[string]core.MonthBudgetLine)
	var items []rowGroup
	var rest []core.BudgetLine
	for _, l := range m.monthly.Lines {
		lineOf[l.Name] = l
		switch {
		case grouped[l.Name] && groupPlanned[l.Name]: // under its group
		case grouped[l.Name] || !l.Planned():
			if l.Spent.Minor != 0 {
				rest = append(rest, core.BudgetLine{Name: l.Name, Amount: l.Spent})
			}
		default:
			items = append(items, rowGroup{l.Spent.Minor, l.Name, []budgetRow{{key: l.Name, label: m.loc.ShortName(l.Name), amount: l.Spent, line: l, planned: true}}})
		}
	}
	for _, g := range m.monthly.Groups {
		if !g.Planned {
			continue
		}
		head := core.MonthBudgetLine{Name: g.Name, Spent: g.Spent, HasTarget: g.HasTarget, Target: g.Target, Carried: g.Carried, Left: g.Left}
		rows := []budgetRow{{key: groupKey(g.Name), label: m.loc.ShortName(g.Name), group: g.Name, header: true, amount: g.Spent, line: head, planned: true}}
		for _, n := range g.Members {
			l := lineOf[n]
			rows = append(rows, budgetRow{key: n, label: m.memberLabel(g.Name, n), group: g.Name, amount: l.Spent, line: l, planned: l.Planned()})
		}
		items = append(items, rowGroup{g.Spent.Minor, g.Name, rows})
	}
	return m.arrange(items), m.spendRows(rest)
}

// groupTouches says an account is the category or, for a group, one below it.
func groupTouches(r row, name string, group bool) bool {
	if !group {
		return r.touches(name)
	}
	for _, p := range r.tx.Postings {
		if p.Account == name || strings.HasPrefix(p.Account, name+":") {
			return true
		}
	}
	return false
}

// fold folds or unfolds the group of the category selected on the Budget, and moves the
// selection to the group when it is folded. It does nothing for a category in none.
func (m *Model) fold(on bool) {
	sel := m.budgetSelected()
	group := ""
	for _, r := range m.budgetRows() {
		if r.key == sel {
			group = r.group
		}
	}
	if group == "" {
		return
	}
	if m.folds == nil {
		m.folds = map[string]bool{}
	}
	m.folds[group] = on
	if on {
		m.budgetSel = groupKey(group)
	}
}

// toggleFold folds the group of the selection, or unfolds it when it is folded.
func (m *Model) toggleFold() {
	sel := m.budgetSelected()
	for _, r := range m.budgetRows() {
		if r.key == sel && r.group != "" {
			m.fold(!m.folded(r.group))
			return
		}
	}
}

// parentGroup is where ← goes: from a category to its group.
func (m *Model) goToGroup() {
	sel := m.budgetSelected()
	for _, r := range m.budgetRows() {
		if r.key == sel && r.group != "" && !r.header {
			m.budgetSel = groupKey(r.group)
			return
		}
	}
	m.fold(true)
}

// hasGroups says the Budget lists a group.
func (m *Model) hasGroups() bool {
	for _, r := range m.budgetRows() {
		if r.header {
			return true
		}
	}
	return false
}
