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

	var asOf string
	balance := &cobra.Command{
		Use:   "balance ACCOUNT AMOUNT",
		Short: "Set an account's balance to what your bank shows",
		Long: `Make an Assets or Liabilities account hold AMOUNT at the end of a day, as your
bank shows it. The difference between that and the transactions already booked
becomes the account's opening balance, dated before its first transaction, so
the whole history adds up. For a Liabilities account give the amount you owe
(a credit card showing 500.00 owed is 500). Run it again with a new reading to
correct the same opening balance.`,
		Example: "  thaler cli account balance Assets:Sparkasse 1234.56 --as-of 2026-10-22",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			acc, err := svc.GetAccount(ctx, args[0])
			if err != nil {
				return err
			}
			if acc.Currency == "" {
				return fmt.Errorf("%s has no fixed currency; only Assets and Liabilities accounts have a balance", acc.Name)
			}
			shown, err := ledger.ParseAmount(args[1], acc.Currency, 0)
			if err != nil {
				return err
			}
			when, err := parseDateFlag(asOf)
			if err != nil {
				return err
			}
			res, err := svc.SetOpeningBalance(ctx, core.OpeningBalanceRequest{Account: acc.Name, Balance: acc.LedgerBalance(shown), AsOf: when})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if res.ID == 0 {
				fmt.Fprintf(out, "Nothing to book: the transactions already add up to %s on %s.\n", shown, when.Format(ledger.DateLayout))
				return nil
			}
			fmt.Fprintf(out, "Booked an opening balance of %s on %s (#%d) so that %s holds %s on %s.\n",
				res.Amount, res.Date, res.ID, acc.Name, shown, when.Format(ledger.DateLayout))
			return nil
		},
	}
	balance.Flags().StringVar(&asOf, "as-of", "", "the day the bank showed this balance, YYYY-MM-DD or DD.MM.YYYY (default: today)")

	root.AddCommand(add, list, balance)
	return root
}

func describeAccount(a core.Account) string {
	if a.Currency == "" {
		return string(a.Type) + ", any currency"
	}
	return string(a.Type) + ", " + string(a.Currency)
}
