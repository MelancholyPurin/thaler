package core

import (
	"sort"
	"strings"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// Categories group by their names: Expenses:Housing:Rent and Expenses:Housing:Power are in
// the group Expenses:Housing, as RenameCategory and MergeCategory already treat what is
// "below" a category. A group is not stored and has no target of its own: its target is
// what its categories' targets add up to, so that setting, clearing and copying targets
// work as they do and nothing is counted twice. A category named like the group
// (Expenses:Housing itself) is one of its members when the group exists. Only the first
// level below the type is a group: Expenses:Housing:Utilities:Power belongs to
// Expenses:Housing, and has no group of its own.

// GroupOf is the group an expense category is in: Expenses:Housing for
// Expenses:Housing:Rent, and "" for a category that is not below another (Expenses:Rent).
func GroupOf(category string) string {
	parts := strings.Split(category, ":")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:2], ":")
}

// MonthBudgetGroup is the categories of one group in a month, added up. Everything is in
// the base currency, and the figures are those of MonthBudgetLine summed over Members:
// Left only counts the members with money set aside (Planned), and what the others spent
// is Unbudgeted, as MonthBudget does for the whole month, so that the groups and the
// categories that are in none add up to the month's totals.
type MonthBudgetGroup struct {
	Name    string
	Members []string // the names of the lines in the group, in the order of Lines
	Spent   ledger.Money
	// Target is the sum of the members' targets, HasTarget says one of them has one.
	Target     ledger.Money
	HasTarget  bool
	Carried    ledger.Money
	Left       ledger.Money
	Unbudgeted ledger.Money
	// Planned says a member has money set aside: a target, or something carried in.
	Planned bool
}

// GroupsOf maps each of the category names to its group, "" for one that is in none, the
// way MonthBudget.Groups does: by GroupOf, and a name that a group is named after is one of
// its members when the group exists among the names.
func GroupsOf(names []string) map[string]string {
	exists := make(map[string]bool)
	for _, n := range names {
		if g := GroupOf(n); g != "" {
			exists[g] = true
		}
	}
	out := make(map[string]string, len(names))
	for _, n := range names {
		g := GroupOf(n)
		if g == "" && exists[n] {
			g = n
		}
		out[n] = g
	}
	return out
}

// budgetGroups adds the lines up by group, the largest spending first.
func budgetGroups(lines []MonthBudgetLine, base ledger.Currency) []MonthBudgetGroup {
	names := make([]string, len(lines))
	for i, l := range lines {
		names[i] = l.Name
	}
	groupOf := GroupsOf(names)
	zero := ledger.Money{Currency: base}
	byName := make(map[string]*MonthBudgetGroup)
	for _, l := range lines {
		g := groupOf[l.Name]
		if g == "" {
			continue
		}
		grp := byName[g]
		if grp == nil {
			grp = &MonthBudgetGroup{Name: g, Spent: zero, Target: zero, Carried: zero, Left: zero, Unbudgeted: zero}
			byName[g] = grp
		}
		grp.Members = append(grp.Members, l.Name)
		grp.Spent.Minor += l.Spent.Minor
		grp.Target.Minor += l.Target.Minor
		grp.Carried.Minor += l.Carried.Minor
		grp.HasTarget = grp.HasTarget || l.HasTarget
		if l.Planned() {
			grp.Planned = true
			grp.Left.Minor += l.Left.Minor
		} else {
			grp.Unbudgeted.Minor += l.Spent.Minor
		}
	}
	var out []MonthBudgetGroup
	for _, g := range byName {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Spent.Minor != out[j].Spent.Minor {
			return out[i].Spent.Minor > out[j].Spent.Minor
		}
		return out[i].Name < out[j].Name
	})
	return out
}
