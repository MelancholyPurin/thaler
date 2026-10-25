package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func (a *app) accountCmd() *cobra.Command {
	root := &cobra.Command{Use: "account", Short: "Create and list accounts"}

	var currency string
	add := &cobra.Command{
		Use:   "add NAME",
		Short: "Create an account, e.g. Assets:Sparkasse --currency EUR",
		Long: `Create an account. The first segment of the name is its type:
Assets, Liabilities, Income, Expenses or Equity (e.g. Assets:Sparkasse,
Expenses:Groceries). Assets and Liabilities need a currency; income, expense
and equity accounts accept any currency unless you give one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			var cur ledger.Currency
			if currency != "" {
				if cur, err = ledger.ParseCurrency(currency); err != nil {
					return err
				}
			}
			acc, err := svc.AddAccount(ctx, args[0], cur)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s (%s)\n", acc.Name, describeAccount(acc))
			return nil
		},
	}
	add.Flags().StringVarP(&currency, "currency", "c", "", "currency code, e.g. EUR, USD or MXN (required for Assets and Liabilities)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List all accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			accs, err := svc.ListAccounts(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(accs) == 0 {
				fmt.Fprintln(out, "No accounts yet. Create one with: thaler cli account add Assets:Sparkasse --currency EUR")
				return nil
			}
			lines := make([]column, 0, len(accs))
			for _, acc := range accs {
				lines = append(lines, column{label: acc.Name, amount: describeAccount(acc)})
			}
			printColumns(out, "", lines)
			return nil
		},
	}

	root.AddCommand(add, list)
	return root
}

func describeAccount(a core.Account) string {
	if a.Currency == "" {
		return string(a.Type) + ", any currency"
	}
	return string(a.Type) + ", " + string(a.Currency)
}
