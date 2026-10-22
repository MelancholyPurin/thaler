package main

import (
	"errors"
	"fmt"
	"math"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/ledger"
	"github.com/melancholypurin/thaler/internal/store"
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
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			acc, err := st.GetAccount(ctx, account)
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
			res, err := st.AddTransaction(ctx, store.NewTransaction{
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
func resolveCurrency(acc store.Account, flag string) (ledger.Currency, error) {
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

Cross-currency transfers are booked through Equity:Conversions so that every
currency balances on its own.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			src, err := st.GetAccount(ctx, from)
			if err != nil {
				return err
			}
			dst, err := st.GetAccount(ctx, to)
			if err != nil {
				return err
			}
			if src.Currency == "" || dst.Currency == "" {
				return errors.New("transfers work between accounts with a fixed currency (Assets or Liabilities)")
			}
			if src.ID == dst.ID {
				return errors.New("source and destination are the same account")
			}
			out, err := ledger.ParseAmount(amount, src.Currency, 0)
			if err != nil {
				return err
			}
			if out.Minor <= 0 {
				return errors.New("--amount must be greater than zero")
			}
			when, err := parseDateFlag(date)
			if err != nil {
				return err
			}

			postings := []ledger.Posting{{Account: src.Name, Amount: out.Neg()}}
			summary := fmt.Sprintf("Transferred %s from %s to %s", out, src.Name, dst.Name)
			if src.Currency == dst.Currency {
				if toAmount != "" {
					return fmt.Errorf("both accounts use %s; omit --to-amount", src.Currency)
				}
				postings = append(postings, ledger.Posting{Account: dst.Name, Amount: out})
			} else {
				if toAmount == "" {
					return fmt.Errorf("%s -> %s crosses currencies; pass --to-amount with the amount that arrives in %s",
						src.Currency, dst.Currency, dst.Currency)
				}
				in, err := ledger.ParseAmount(toAmount, dst.Currency, 0)
				if err != nil {
					return err
				}
				if in.Minor <= 0 {
					return errors.New("--to-amount must be greater than zero")
				}
				postings = append(postings,
					ledger.Posting{Account: ledger.ConversionAccount, Amount: out},
					ledger.Posting{Account: ledger.ConversionAccount, Amount: in.Neg()},
					ledger.Posting{Account: dst.Name, Amount: in},
				)
				// Display only: the stored amounts are exact integers.
				rate := (float64(in.Minor) / math.Pow10(dst.Currency.Exponent())) /
					(float64(out.Minor) / math.Pow10(src.Currency.Exponent()))
				summary += fmt.Sprintf(" (%s arrived, effective rate 1 %s = %.4f %s; record it with: thaler cli rate set %s %s %.4f)",
					in, src.Currency, rate, dst.Currency, src.Currency, dst.Currency, rate)
			}
			res, err := st.AddTransaction(ctx, store.NewTransaction{
				Date:        when,
				Description: memo,
				Postings:    postings,
			})
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "#%d  %s  %s\n", res.ID, when.Format(ledger.DateLayout), summary)
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
