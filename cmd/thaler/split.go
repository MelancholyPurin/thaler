package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func (a *app) splitCmd() *cobra.Command {
	var lines []string
	var amount, date, payee, memo string
	cmd := &cobra.Command{
		Use:   "split ID --line CATEGORY[=AMOUNT]...",
		Short: "Divide a transaction between categories, or change how it is divided",
		Long: `Divide one transaction between several categories, or change one that already
is: a shop bill that is groceries and household goods, a salary that is pay and a
bonus. Each --line is a category and its share, without a sign; one line may leave
the share out and gets what the others leave. The lines given replace all of them,
and must add up to the amount, or nothing changes. One line makes it an ordinary
transaction again. Only the flags you give change; the rest stays, the number and
the bank's import hash too.

  thaler cli split 12 --line Expenses:Groceries=23.10 --line Expenses:Household
  thaler cli split 12 --amount=-35.00 --line Expenses:Groceries`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			id, err := transferID(args[0])
			if err != nil {
				return err
			}
			tx, err := svc.GetTransaction(ctx, id)
			if err != nil {
				return err
			}
			sp, ok := tx.Split()
			if !ok {
				return fmt.Errorf("transaction #%d cannot be split: only one on an account of yours with income or expense categories can", id)
			}
			f := cmd.Flags()
			if !f.Changed("line") && !f.Changed("amount") && !f.Changed("date") && !f.Changed("payee") && !f.Changed("memo") {
				return errors.New("nothing to change: give --line, --amount, --date, --payee or --memo")
			}
			cur := sp.Amount.Currency
			if f.Changed("amount") {
				if sp.Amount, err = ledger.ParseAmount(amount, cur, 0); err != nil {
					return fmt.Errorf("--amount: %w", err)
				}
			}
			if f.Changed("line") {
				sp.Lines = nil
				for _, text := range lines {
					l, err := splitLine(text, cur)
					if err != nil {
						return err
					}
					sp.Lines = append(sp.Lines, l)
				}
			}
			if f.Changed("date") {
				if sp.Date, err = a.parseDateFlag(date); err != nil {
					return err
				}
			}
			if f.Changed("payee") {
				sp.Payee = payee
			}
			if f.Changed("memo") {
				sp.Description = memo
			}
			created, err := svc.ReplaceSplit(ctx, id, sp)
			if err != nil {
				return err
			}
			now, err := svc.GetTransaction(ctx, id)
			if err != nil {
				return err
			}
			printSplit(cmd, now)
			for _, name := range created {
				fmt.Fprintf(cmd.OutOrStdout(), "Created new account %s\n", name)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&lines, "line", nil, "a category and its share, Expenses:Food=12.50; leave out the share for the rest (repeat for each line)")
	cmd.Flags().StringVar(&amount, "amount", "", "the signed amount on the account, e.g. -35.00")
	cmd.Flags().StringVarP(&date, "date", "d", "", "the booking date")
	cmd.Flags().StringVarP(&payee, "payee", "p", "", "who was paid or who paid you")
	cmd.Flags().StringVarP(&memo, "memo", "m", "", "free-text description")
	return cmd
}

// splitLine reads CATEGORY=AMOUNT, or CATEGORY alone for the line that takes the rest.
func splitLine(text string, cur ledger.Currency) (core.SplitLine, error) {
	category, share, found := strings.Cut(text, "=")
	l := core.SplitLine{Category: strings.TrimSpace(category)}
	if !found || strings.TrimSpace(share) == "" {
		return l, nil
	}
	m, err := ledger.ParseAmount(share, cur, 0)
	if err != nil {
		return core.SplitLine{}, fmt.Errorf("--line %s: %w", text, err)
	}
	if m.IsZero() {
		return core.SplitLine{}, fmt.Errorf("--line %s: a share of nothing; leave it out for the line that takes the rest", text)
	}
	l.Amount = m
	return l, nil
}

// printSplit shows the transaction on its account, then each category with its share.
func printSplit(cmd *cobra.Command, tx core.TxView) {
	w := cmd.OutOrStdout()
	sp, _ := tx.Split()
	fmt.Fprintf(w, "#%d  %s  %s  %s  %s\n", tx.ID, tx.Date, sp.Account, signed(sp.Amount), tx.Payee)
	for _, l := range sp.Lines {
		fmt.Fprintf(w, "  %s  %s\n", l.Category, l.Amount)
	}
}
