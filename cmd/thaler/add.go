package main

import (
	"errors"
	"fmt"
	"math"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func (a *app) addCmd() *cobra.Command {
	var account, amount, to, date, payee, memo, currency string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Record a single income or expense",
		Long: `Record a transaction on one account. A negative amount is money leaving the
account (an expense), a positive amount is money arriving (income). The other
side defaults to Expenses:Uncategorized or Income:Uncategorized; pass --to to
choose a category. Categories that do not exist yet are created.

Write negative amounts with an equals sign so they are not read as flags:
  thaler cli add -a Assets:Sparkasse --amount=-12.50 -t Expenses:Groceries -p REWE`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			acc, err := svc.GetAccount(ctx, account)
			if err != nil {
				return err
			}
			cur, err := resolveCurrency(acc, currency)
			if err != nil {
				return err
			}
			amt, err := ledger.ParseAmount(amount, cur, 0)
			if err != nil {
				return err
			}
			if amt.IsZero() {
				return errors.New("amount must not be zero")
			}
			when, err := parseDateFlag(date)
			if err != nil {
				return err
			}
			counter := to
			if counter == "" {
				counter = ledger.UncategorizedExpense
				if amt.Minor > 0 {
					counter = ledger.UncategorizedIncome
				}
			}
			res, err := svc.AddTransaction(ctx, core.NewTransaction{
				Date:        when,
				Payee:       payee,
				Description: memo,
				Postings: []ledger.Posting{
					{Account: acc.Name, Amount: amt},
					{Account: counter, Amount: amt.Neg()},
				},
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Added #%d  %s  %s  %s -> %s\n", res.ID, when.Format(ledger.DateLayout), signed(amt), acc.Name, counter)
			for _, name := range res.Created {
				fmt.Fprintf(out, "Created new account %s\n", name)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&account, "account", "a", "", "account the money moves in or out of, e.g. Assets:Sparkasse (required)")
	cmd.Flags().StringVar(&amount, "amount", "", "signed amount, e.g. -12.50 or 2450,00 (required)")
	cmd.Flags().StringVarP(&to, "to", "t", "", "category or other account on the other side (default: Expenses:Uncategorized / Income:Uncategorized)")
	cmd.Flags().StringVarP(&date, "date", "d", "", "booking date, YYYY-MM-DD or DD.MM.YYYY (default: today)")
	cmd.Flags().StringVarP(&payee, "payee", "p", "", "who was paid or who paid you")
	cmd.Flags().StringVarP(&memo, "memo", "m", "", "free-text description")
	cmd.Flags().StringVarP(&currency, "currency", "c", "", "only needed when --account has no fixed currency")
	_ = cmd.MarkFlagRequired("account")
	_ = cmd.MarkFlagRequired("amount")
	return cmd
}

// resolveCurrency decides the currency of an amount entered for acc.
func resolveCurrency(acc core.Account, flag string) (ledger.Currency, error) {
	if flag == "" {
		if acc.Currency == "" {
			return "", fmt.Errorf("%s has no fixed currency; pass --currency", acc.Name)
		}
		return acc.Currency, nil
	}
	c, err := ledger.ParseCurrency(flag)
	if err != nil {
		return "", err
	}
	if acc.Currency != "" && acc.Currency != c {
		return "", fmt.Errorf("%s holds %s, not %s", acc.Name, acc.Currency, c)
	}
	return c, nil
}

func (a *app) transferCmd() *cobra.Command {
	var from, to, amount, toAmount, date, memo string
	cmd := &cobra.Command{
		Use:   "transfer",
		Short: "Move money between two of your accounts, optionally across currencies",
		Long: `Move money between two accounts that hold their own currency (Assets or
Liabilities). --amount is what leaves the source account. When the two accounts
use different currencies, --to-amount says how much arrives, which pins down the
exchange rate you actually got:

  thaler cli transfer --from Assets:Sparkasse --to Assets:Cash-USD --amount 100 --to-amount 108.50

A transfer is two linked transactions, one on each account, like the two bank
statements of a real transfer; they are booked to Equity:Transfers, or to
Equity:Conversions when the currencies differ, so that every currency balances
on its own and a transfer is neither income nor spending.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			// The accounts tell us which currency each typed amount is in.
			src, err := svc.GetAccount(ctx, from)
			if err != nil {
				return err
			}
			dst, err := svc.GetAccount(ctx, to)
			if err != nil {
				return err
			}
			if src.Currency == "" || dst.Currency == "" {
				return errors.New("transfers work between accounts with a fixed currency (Assets or Liabilities)")
			}
			out, err := ledger.ParseAmount(amount, src.Currency, 0)
			if err != nil {
				return fmt.Errorf("--amount: %w", err)
			}
			var in ledger.Money
			if toAmount != "" {
				if in, err = ledger.ParseAmount(toAmount, dst.Currency, 0); err != nil {
					return fmt.Errorf("--to-amount: %w", err)
				}
			}
			when, err := parseDateFlag(date)
			if err != nil {
				return err
			}
			res, err := svc.Transfer(ctx, core.TransferRequest{
				From: src.Name, To: dst.Name, Amount: out, ToAmount: in, Date: when, Memo: memo,
			})
			if err != nil {
				return err
			}

			summary := fmt.Sprintf("Transferred %s from %s to %s", out, src.Name, dst.Name)
			if res.CrossCurrency {
				// Display only: the stored amounts are exact integers.
				rate := (float64(res.In.Minor) / math.Pow10(dst.Currency.Exponent())) /
					(float64(out.Minor) / math.Pow10(src.Currency.Exponent()))
				summary += fmt.Sprintf(" (%s arrived, effective rate 1 %s = %.4f %s; record it with: thaler cli rate set %s %s %.4f)",
					res.In, src.Currency, rate, dst.Currency, src.Currency, dst.Currency, rate)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "#%d and #%d  %s  %s\n", res.ID, res.InID, when.Format(ledger.DateLayout), summary)
			for _, name := range res.Created {
				fmt.Fprintf(w, "Created new account %s\n", name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "source account (required)")
	cmd.Flags().StringVar(&to, "to", "", "destination account (required)")
	cmd.Flags().StringVar(&amount, "amount", "", "amount leaving the source account, in its currency (required)")
	cmd.Flags().StringVar(&toAmount, "to-amount", "", "amount arriving, in the destination currency (required when currencies differ)")
	cmd.Flags().StringVarP(&date, "date", "d", "", "booking date (default: today)")
	cmd.Flags().StringVarP(&memo, "memo", "m", "", "free-text description")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	_ = cmd.MarkFlagRequired("amount")
	return cmd
}
