package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
)

func (a *app) exportCmd() *cobra.Command {
	var account, from, to string
	cmd := &cobra.Command{
		Use:   "export [DEST]",
		Short: "Write the transactions as CSV, for a spreadsheet",
		Long: `Write the stored transactions as a CSV file, oldest first, with a line for each
posting: the transaction's id, date, payee and description, the account, and the
amount with its currency. A transaction has at least two lines, which add up to
nothing in each currency; the one on a category is the spending or the income.
Without DEST the CSV goes to standard output, to redirect or pipe. With DEST it
goes to a new file readable by you alone, and a file that is already there is
never overwritten.

The file is plain on purpose, whatever the language setting: English column
names, dates as YYYY-MM-DD, amounts as decimals with a point and no thousands
separator. A name that a spreadsheet would read as a formula gets a ' before it.
--account, --from and --to choose which transactions: those touching the account
or one below it, and those between the days, both included.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			filter := core.TxFilter{Account: account}
			if filter.From, err = isoDateFlag(from); err != nil {
				return err
			}
			if filter.To, err = isoDateFlag(to); err != nil {
				return err
			}
			txs, err := svc.ListTransactions(ctx, filter)
			if err != nil {
				return err
			}
			data, err := core.TransactionsCSV(txs)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}
			if err := core.WriteNewFile(args[0], data); err != nil {
				return err
			}
			noun := "transactions"
			if len(txs) == 1 {
				noun = "transaction"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d %s to %s\n", len(txs), noun, args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&account, "account", "a", "", "only transactions touching this account or its sub-accounts")
	cmd.Flags().StringVar(&from, "from", "", "first day to include (YYYY-MM-DD)")
	cmd.Flags().StringVar(&to, "to", "", "last day to include (YYYY-MM-DD)")
	return cmd
}
