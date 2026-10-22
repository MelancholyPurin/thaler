package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/ledger"
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
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			base, err := st.BaseCurrency(ctx)
			if err != nil {
				return err
			}
			book, err := st.RateBook(ctx)
			if err != nil {
				return err
			}

			date, asOfArg := todayISO(), ""
			if asOf != "" {
				d, err := ledger.ParseDate(asOf)
				if err != nil {
					return err
				}
				date = d.Format(ledger.DateLayout)
				asOfArg = date
			}
			balances, err := st.Balances(ctx, asOfArg)
			if err != nil {
				return err
			}

			net := ledger.Money{Currency: base}
			missing := make(map[ledger.Currency]bool)
			var lines []column
			for _, b := range balances {
				countsTowardNetWorth := b.Type == ledger.Assets || b.Type == ledger.Liabilities
				if !countsTowardNetWorth && !all {
					continue
				}
				lines = append(lines, column{label: b.Account, amount: b.Amount.String()})
				if !countsTowardNetWorth {
					continue
				}
				converted, ok := book.Convert(b.Amount, base, date)
				if !ok {
					missing[b.Amount.Currency] = true
					continue
				}
				if net, err = net.Add(converted); err != nil {
					return err
				}
			}

			if len(lines) == 0 {
				fmt.Fprintln(out, "No balances yet. Create an account and add or import transactions (see README).")
				return nil
			}
			fmt.Fprintf(out, "Balances as of %s\n\n", date)
			printColumns(out, "  ", lines)
			fmt.Fprintf(out, "\nNet worth: %s\n", net)

			if len(missing) > 0 {
				names := make([]string, 0, len(missing))
				for c := range missing {
					names = append(names, string(c))
				}
				sort.Strings(names)
				for _, c := range names {
					fmt.Fprintf(out, "warning: no exchange rate %s -> %s, so %s accounts are not in the net worth; add one with: thaler cli rate set %s %s <rate>\n",
						c, base, c, c, base)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&asOf, "as-of", "", "report balances as of this day (YYYY-MM-DD, default: today)")
	cmd.Flags().BoolVar(&all, "all", false, "also list income, expense and equity accounts")
	return cmd
}
