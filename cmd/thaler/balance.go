package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
)

func (a *app) balanceCmd() *cobra.Command {
	var asOf string
	var all bool
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "Show account balances and net worth in the base currency",
		Long: `Show the balance of every asset and liability account in its own currency,
and the net worth converted into the base currency (see "thaler cli currency").
Conversion uses the most recent exchange rate on or before the report date
("thaler cli rate set"); accounts in a currency without a usable rate are listed in a
warning and left out of the total.

Liabilities are shown as negative numbers. With --all, income, expense and
equity accounts are listed too (income and equity show as negative credits).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			report, err := svc.BalanceReport(ctx, core.BalanceRequest{AsOf: asOf, IncludeAll: all})
			if err != nil {
				return err
			}
			if len(report.Lines) == 0 {
				fmt.Fprintln(out, "No balances yet. Create an account and add or import transactions (see README).")
				return nil
			}
			lines := make([]column, 0, len(report.Lines))
			for _, b := range report.Lines {
				lines = append(lines, column{label: b.Account, amount: b.Amount.String()})
			}
			fmt.Fprintf(out, "Balances as of %s\n\n", report.Date)
			printColumns(out, "  ", lines)
			fmt.Fprintf(out, "\nNet worth: %s\n", report.NetWorth.Total)

			base := report.NetWorth.Base
			for _, c := range report.NetWorth.Missing {
				fmt.Fprintf(out, "warning: no exchange rate %s -> %s, so %s accounts are not in the net worth; add one with: thaler cli rate set %s %s <rate>\n",
					c, base, c, c, base)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&asOf, "as-of", "", "report balances as of this day (YYYY-MM-DD, default: today)")
	cmd.Flags().BoolVar(&all, "all", false, "also list income, expense and equity accounts")
	return cmd
}
