package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
)

func (a *app) categoryCmd() *cobra.Command {
	root := &cobra.Command{Use: "category", Short: "Change the category of stored transactions"}
	root.AddCommand(&cobra.Command{
		Use:   "set ID CATEGORY",
		Short: "Move transaction ID to CATEGORY, e.g. 42 Expenses:Groceries",
		Long: `Move a transaction to another category. The category is created if it does not
exist yet. Find transaction ids with: thaler cli list`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("invalid transaction id %q", args[0])
			}
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			created, err := svc.SetCategories(ctx, []core.CategoryChange{{TransactionID: id, Category: args[1]}})
			if err != nil {
				return err
			}
			for _, name := range created {
				fmt.Fprintf(cmd.OutOrStdout(), "Created new account %s\n", name)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Transaction #%d is now in %s\n", id, args[1])
			return nil
		},
	})
	return root
}

func (a *app) recategorizeCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "recategorize",
		Short: "Apply the config.toml rules to transactions that are still uncategorized",
		Long: `Run the categorisation rules from config.toml over every transaction that sits
in Expenses:Uncategorized or Income:Uncategorized and move the ones that match.
Transactions you already categorised are never touched.

Rules see the payee and the stored description (the bank's purpose text, or its
booking text when there is no purpose), so a rule that only matched the booking
text of a row with a purpose at import time may not match here.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}
			res, err := svc.Recategorize(ctx, core.RecategorizeRequest{Rules: cfg.Rules, DryRun: dryRun})
			if err != nil {
				return err
			}

			if len(res.Moves) == 0 {
				fmt.Fprintf(out, "No rule matches any of the %d uncategorized transaction(s).\n", res.Uncategorized)
				return nil
			}
			lines := make([]column, 0, len(res.Moves))
			for _, m := range res.Moves {
				lines = append(lines, column{
					label:  fmt.Sprintf("%s  #%-5d %-28s  %s", m.Date, m.TransactionID, truncate(m.Category, 28), truncate(m.Payee, 40)),
					amount: signed(m.Amount),
				})
			}
			printColumns(out, "", lines)
			if dryRun {
				fmt.Fprintf(out, "\nDry run: %d of %d uncategorized transaction(s) would move. Nothing was written.\n", len(res.Moves), res.Uncategorized)
				return nil
			}
			for _, name := range res.Created {
				fmt.Fprintf(out, "Created new account %s\n", name)
			}
			fmt.Fprintf(out, "\nMoved %d of %d uncategorized transaction(s); %d still uncategorized.\n",
				len(res.Moves), res.Uncategorized, res.Uncategorized-len(res.Moves))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing anything")
	return cmd
}
