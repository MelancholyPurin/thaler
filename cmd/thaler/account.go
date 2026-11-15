package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func (a *app) accountCmd() *cobra.Command {
	root := &cobra.Command{Use: "account", Short: "Create, list, rename and close accounts"}

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
			if wantsJSON(cmd) {
				return printJSON(cmd, accs)
			}
			out := cmd.OutOrStdout()
			if len(accs) == 0 {
				fmt.Fprintln(out, a.cli().T("no_accounts_yet", nil))
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

	addJSONFlag(list)

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
			when, err := a.parseDateFlag(asOf)
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

	var clearMinimum bool
	var minimumAmount string
	minimum := &cobra.Command{
		Use:   "minimum [ACCOUNT [AMOUNT]]",
		Short: "Set the lowest an Assets account should fall, or list them",
		Long: `Say how low an Assets account may fall. "thaler cli forecast" warns about the
day its projected balance would be under AMOUNT, in the account's currency (a
minimum can be negative, for an agreed overdraft: write it as
--amount=-300). An account with no minimum is never warned about.
Without arguments the minimums set are listed; --clear takes one away.`,
		Example: "  thaler cli account minimum Assets:Bank 500",
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			if minimumAmount != "" {
				if len(args) != 1 {
					return fmt.Errorf("--amount goes with the account and no amount")
				}
				args = append(args, minimumAmount)
			}
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			switch {
			case len(args) == 0 && !clearMinimum:
				mins, err := svc.MinimumBalances(ctx)
				if err != nil {
					return err
				}
				if wantsJSON(cmd) {
					return printJSON(cmd, mins)
				}
				if len(mins) == 0 {
					fmt.Fprintln(out, "No minimum balances. Set one with: thaler cli account minimum Assets:Bank 500")
					return nil
				}
				lines := make([]column, 0, len(mins))
				for _, m := range mins {
					lines = append(lines, column{label: m.Account, amount: m.Amount.String()})
				}
				printColumns(out, "", lines)
				return nil
			case len(args) == 0:
				return fmt.Errorf("--clear needs the account")
			}
			acc, err := svc.GetAccount(ctx, args[0])
			if err != nil {
				return err
			}
			if clearMinimum {
				if len(args) > 1 {
					return fmt.Errorf("--clear takes the account only, not an amount")
				}
				if err := svc.ClearMinimumBalance(ctx, acc.Name); err != nil {
					return err
				}
				fmt.Fprintf(out, "%s has no minimum balance\n", acc.Name)
				return nil
			}
			if len(args) < 2 {
				return fmt.Errorf("give the amount, or --clear to take the minimum away")
			}
			amount, err := ledger.ParseAmount(args[1], acc.Currency, 0)
			if err != nil {
				return err
			}
			if err := svc.SetMinimumBalance(ctx, acc.Name, amount); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s: warn when the balance would fall below %s\n", acc.Name, amount)
			return nil
		},
	}
	addJSONFlag(minimum)
	minimum.Flags().BoolVar(&clearMinimum, "clear", false, "take the minimum of the account away")
	minimum.Flags().StringVar(&minimumAmount, "amount", "", "the minimum, instead of the second argument (for a negative one: --amount=-300)")

	rename := &cobra.Command{
		Use:   "rename OLD NEW",
		Short: "Give an Assets or Liabilities account a new name, e.g. Assets:Bank Assets:Sparkasse",
		Long: `Rename one of your accounts. Its transactions, schedules, minimum balance and
history stay with it; only the name changes. The new name keeps the type (an
Assets account stays an Assets account) and must not be taken. To rename a
category use the interface (K).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			acc, err := svc.RenameAccount(ctx, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed %s to %s\n", args[0], acc.Name)
			return nil
		},
	}

	closeCmd := &cobra.Command{
		Use:   "close ACCOUNT",
		Short: "Set an account aside, keeping its history",
		Long: `Close an Assets or Liabilities account you no longer use. It has to hold nothing
(move what is in it to another account first) and no schedule may book to it. Its
transactions stay, and the reports of the months it was in use still count it, but it
takes no new money, and a transaction of it cannot be edited or deleted, until it is
reopened. The list of accounts marks it as closed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			acc, err := svc.CloseAccount(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Closed %s\n", acc.Name)
			return nil
		},
	}

	reopen := &cobra.Command{
		Use:   "reopen ACCOUNT",
		Short: "Take a closed account back into use",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			acc, err := svc.ReopenAccount(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Reopened %s\n", acc.Name)
			return nil
		},
	}

	root.AddCommand(add, list, balance, minimum, rename, closeCmd, reopen)
	return root
}

func describeAccount(a core.Account) string {
	text := string(a.Type) + ", " + string(a.Currency)
	if a.Currency == "" {
		text = string(a.Type) + ", any currency"
	}
	if a.Closed {
		text += ", closed"
	}
	return text
}
