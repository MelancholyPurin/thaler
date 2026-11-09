package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func (a *app) budgetCmd() *cobra.Command {
	var month string
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Show a month's spending against its targets, and set or clear targets",
		Long: `Show what each expense category spent in a month against its target, and what
is left. A category carries over what is left of it into the next month when it
is switched on in the interface (the Carried column); the Left total counts the
categories with a target or something carried in, and says what the others
spent. Amounts are in the base currency ("thaler cli currency").

A target belongs to one month and one category, in the base currency:
  thaler cli budget set Expenses:Groceries 400 --month 2026-10
  thaler cli budget clear Expenses:Groceries --month 2026-10`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			month := a.monthFlag(month)
			b, err := svc.MonthBudget(ctx, core.MonthBudgetRequest{Month: month, RateDate: todayISO()})
			if err != nil {
				return err
			}
			printMonthBudget(cmd.OutOrStdout(), b)
			return nil
		},
	}
	cmd.PersistentFlags().StringVarP(&month, "month", "m", "", "the month, YYYY-MM (default: this month)")

	set := &cobra.Command{
		Use:   "set CATEGORY AMOUNT",
		Short: "Set what a category may spend in a month, e.g. Expenses:Groceries 400",
		Long: `Set the target of an expense category for a month, in the base currency. A
target that is already there is replaced.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			base, err := svc.BaseCurrency(ctx)
			if err != nil {
				return err
			}
			amount, err := ledger.ParseAmount(args[1], base, 0)
			if err != nil {
				return err
			}
			month := a.monthFlag(month)
			if err := svc.SetBudgetTarget(ctx, month, args[0], amount); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Target for %s in %s: %s\n", args[0], month, amount)
			return nil
		},
	}

	clear := &cobra.Command{
		Use:   "clear CATEGORY",
		Short: "Remove a category's target for a month",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			month := a.monthFlag(month)
			targets, err := svc.BudgetTargets(ctx, month)
			if err != nil {
				return err
			}
			had := false
			for _, t := range targets {
				had = had || t.Category == args[0]
			}
			if err := svc.ClearBudgetTarget(ctx, month, args[0]); err != nil {
				return err
			}
			if had {
				fmt.Fprintf(cmd.OutOrStdout(), "Cleared the target for %s in %s\n", args[0], month)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s has no target in %s\n", args[0], month)
			}
			return nil
		},
	}
	cmd.AddCommand(set, clear)
	return cmd
}

// monthFlag is the month asked for, or this one.
func (a *app) monthFlag(month string) string {
	if month == "" {
		return a.thisMonth()
	}
	return month
}

// printMonthBudget writes a month as a table: the categories with money set aside
// first, then the ones that spent without, and a total.
func printMonthBudget(w io.Writer, b core.MonthBudget) {
	if len(b.Lines) == 0 {
		fmt.Fprintf(w, "Nothing spent and no targets in %s. Set one with: thaler cli budget set Expenses:Groceries 400 --month %s\n", b.Month, b.Month)
		return
	}
	carried := b.CarriedTotal.Minor != 0
	for _, l := range b.Lines {
		carried = carried || l.Carried.Minor != 0
	}
	planned := false
	for _, l := range b.Lines {
		planned = planned || l.Planned()
	}

	header := []string{"Category", "Spent"}
	if planned {
		header = append(header, "Target")
		if carried {
			header = append(header, "Carried")
		}
		header = append(header, "Left")
	}
	row := func(name string, spent, target, carry, left *ledger.Money) []string {
		cells := []string{name, spent.Decimal()}
		if !planned {
			return cells
		}
		cells = append(cells, cell(target))
		if carried {
			cells = append(cells, cell(carry))
		}
		return append(cells, cell(left))
	}
	var rows [][]string
	for _, pass := range []bool{true, false} { // those with money set aside, then the others
		for _, l := range b.Lines {
			if l.Planned() != pass {
				continue
			}
			var target, carry, left *ledger.Money
			if l.Planned() {
				target, carry, left = &l.Target, &l.Carried, &l.Left
				if !l.HasTarget {
					target = nil
				}
				if l.Carried.Minor == 0 {
					carry = nil
				}
			}
			rows = append(rows, row(l.Name, &l.Spent, target, carry, left))
		}
	}
	rows = append(rows, row("Total", &b.SpentTotal, &b.TargetTotal, &b.CarriedTotal, &b.LeftTotal))

	fmt.Fprintf(w, "Budget for %s, in %s\n\n", b.Month, b.Base)
	printTable(w, header, rows)
	if planned && b.UnbudgetedTotal.Minor != 0 {
		fmt.Fprintf(w, "\nLeft counts the categories with a target or something carried in; %s was spent on the others.\n", b.UnbudgetedTotal.Decimal())
	}
	for _, c := range b.Missing {
		fmt.Fprintf(w, "warning: no exchange rate %s -> %s, so %s is left out; add one with: thaler cli rate set %s %s <rate>\n", c, b.Base, c, c, b.Base)
	}
}

// cell writes an amount without its currency (the title has it), or nothing.
func cell(m *ledger.Money) string {
	if m == nil {
		return ""
	}
	return m.Decimal()
}
