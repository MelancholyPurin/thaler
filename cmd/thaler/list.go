package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func (a *app) listCmd() *cobra.Command {
	var account, from, to string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List transactions, oldest first (the newest is printed last)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			filter := core.TxFilter{Account: account, Limit: limit}
			if from != "" {
				d, err := ledger.ParseDate(from)
				if err != nil {
					return err
				}
				filter.From = d.Format(ledger.DateLayout)
			}
			if to != "" {
				d, err := ledger.ParseDate(to)
				if err != nil {
					return err
				}
				filter.To = d.Format(ledger.DateLayout)
			}
			txs, err := svc.ListTransactions(ctx, filter)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(txs) == 0 {
				fmt.Fprintln(out, "No transactions found.")
				return nil
			}

			nameW, amountW := 0, 0
			for _, tx := range txs {
				for _, p := range tx.Postings {
					if n := len([]rune(p.Account)); n > nameW {
						nameW = n
					}
					if n := len(signed(p.Amount)); n > amountW {
						amountW = n
					}
				}
			}
			for _, tx := range txs {
				title := tx.Payee
				if tx.Description != "" {
					if title != "" {
						title += " - "
					}
					title += truncate(tx.Description, 70)
				}
				fmt.Fprintf(out, "%s  #%-5d %s\n", tx.Date, tx.ID, title)
				for _, p := range tx.Postings {
					fmt.Fprintf(out, "    %-*s  %*s\n", nameW, p.Account, amountW, signed(p.Amount))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&account, "account", "a", "", "only transactions touching this account or its sub-accounts")
	cmd.Flags().StringVar(&from, "from", "", "first day to include (YYYY-MM-DD)")
	cmd.Flags().StringVar(&to, "to", "", "last day to include (YYYY-MM-DD)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 30, "show only the newest N transactions (0 = all)")
	return cmd
}
