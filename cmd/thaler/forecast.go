package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// forecastDays is how far ahead "thaler cli forecast" looks when --until is not given.
const forecastDays = 90

func (a *app) forecastCmd() *cobra.Command {
	var until string
	cmd := &cobra.Command{
		Use:   "forecast",
		Short: "Project the account balances from the schedules",
		Long: `Show what each asset and liability account would hold on the days its
schedules move it, if every date were confirmed: the balance today, then each
day with what changes and the balance after it. A date that came and nobody
answered yet is counted on today. It looks 90 days ahead, or up to --until.

Only the schedules count: a purchase you have not made yet is not in it.
Nothing is booked.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			var upTo string
			if until != "" {
				d, err := ledger.ParseDate(until)
				if err != nil {
					return err
				}
				upTo = d.Format(ledger.DateLayout)
			} else {
				now, err := ledger.ParseDate(a.today())
				if err != nil {
					return err
				}
				upTo = now.AddDate(0, 0, forecastDays).Format(ledger.DateLayout)
			}
			p, err := svc.Projection(ctx, upTo)
			if err != nil {
				return err
			}
			low, err := svc.LowBalances(ctx, upTo)
			if err != nil {
				return err
			}
			printProjection(cmd, p, low)
			return nil
		},
	}
	cmd.Flags().StringVar(&until, "until", "", "look up to this date instead of 90 days ahead, YYYY-MM-DD or DD.MM.YYYY")
	return cmd
}

func printProjection(cmd *cobra.Command, p core.Projection, low []core.LowBalance) {
	out := cmd.OutOrStdout()
	moved := false
	for _, acc := range p.Accounts {
		moved = moved || len(acc.Days) > 0
	}
	if !moved {
		fmt.Fprintf(out, "No schedule moves an account up to %s. Add one with: thaler cli schedule add\n", p.To)
		printLow(out, low)
		return
	}
	fmt.Fprintf(out, "Projected balances from %s up to %s\n", p.From, p.To)
	for _, acc := range p.Accounts {
		if len(acc.Days) == 0 {
			continue
		}
		fmt.Fprintf(out, "\n%s, today %s\n", acc.Name, acc.Start)
		var rows [][]string
		for _, d := range acc.Days {
			names := make([]string, 0, len(d.Moves))
			for _, m := range d.Moves {
				name := m.Schedule
				if m.Overdue {
					name += " (not answered yet)"
				}
				names = append(names, name)
			}
			rows = append(rows, []string{d.Date, d.Change.String(), d.Balance.String(), strings.Join(names, ", ")})
		}
		printTable(out, []string{"Date", "Change", "Balance", "Schedules"}, rows)
	}
	printLow(out, low)
}

// printLow warns about the accounts that fall below their minimum.
func printLow(out io.Writer, low []core.LowBalance) {
	if len(low) == 0 {
		return
	}
	fmt.Fprintln(out)
	for _, l := range low {
		switch {
		case l.Now:
			fmt.Fprintf(out, "warning: %s holds %s, below its minimum of %s, already today\n", l.Account, l.Balance, l.Minimum)
		case l.Schedule != "":
			fmt.Fprintf(out, "warning: %s would fall to %s on %s (%s takes the most), below its minimum of %s\n", l.Account, l.Balance, l.Date, l.Schedule, l.Minimum)
		default:
			fmt.Fprintf(out, "warning: %s would fall to %s on %s, below its minimum of %s\n", l.Account, l.Balance, l.Date, l.Minimum)
		}
	}
}
