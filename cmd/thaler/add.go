package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

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
			when, err := a.parseDateFlag(date)
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
			when, err := a.parseDateFlag(date)
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
	cmd.AddCommand(a.transferEditCmd(), a.transferDeleteCmd(), a.transferLinkCmd(), a.transferUnlinkCmd())
	return cmd
}

// transferID reads the number of either half of a transfer.
func transferID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%q is not a transaction number", s)
	}
	return id, nil
}

// printHalves lists the two halves of a transfer, one line each.
func printHalves(w io.Writer, v core.TransferView) {
	for _, half := range []core.TxView{v.Out, v.In} {
		source, _, _ := half.Legs()
		fmt.Fprintf(w, "#%d  %s  %s  %s  %s\n", half.ID, half.Date, source.Account, signed(source.Amount), half.Payee)
	}
}

func (a *app) transferEditCmd() *cobra.Command {
	var from, to, amount, toAmount, date, inDate, memo string
	cmd := &cobra.Command{
		Use:   "edit ID",
		Short: "Change both halves of a transfer: only the flags you give are changed",
		Long: `Change a transfer, given the number of either of its halves. Only the flags you
give change; the rest stays. Both halves change together and keep their numbers,
so the transfer stays one. --date moves both halves to one day, --in-date gives
the half that arrives a day of its own (the two bank statements of a transfer can
be days apart). Without either, each half keeps its day. A payee that Thaler wrote
("Transfer to Savings") follows a new account; one from the bank stays.`,
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
			cur, err := svc.GetTransfer(ctx, id)
			if err != nil {
				return err
			}
			f := cmd.Flags()
			changes := 0
			for _, flag := range []string{"from", "to", "amount", "to-amount", "date", "in-date", "memo"} {
				if f.Changed(flag) {
					changes++
				}
			}
			if changes == 0 {
				return errors.New("nothing to change: give at least one of --from, --to, --amount, --to-amount, --date, --in-date, --memo")
			}
			e := cur.TransferEdit
			if f.Changed("from") {
				e.From = from
			}
			if f.Changed("to") {
				e.To = to
			}
			src, err := svc.GetAccount(ctx, e.From)
			if err != nil {
				return err
			}
			dst, err := svc.GetAccount(ctx, e.To)
			if err != nil {
				return err
			}
			if f.Changed("amount") {
				if e.Amount, err = ledger.ParseAmount(amount, src.Currency, 0); err != nil {
					return fmt.Errorf("--amount: %w", err)
				}
			}
			switch {
			case f.Changed("to-amount"):
				if e.ToAmount, err = ledger.ParseAmount(toAmount, dst.Currency, 0); err != nil {
					return fmt.Errorf("--to-amount: %w", err)
				}
			case src.Currency == dst.Currency:
				e.ToAmount = ledger.Money{}
			}
			if f.Changed("date") {
				if e.Date, err = a.parseDateFlag(date); err != nil {
					return err
				}
				e.InDate = e.Date
			}
			if f.Changed("in-date") {
				if e.InDate, err = a.parseDateFlag(inDate); err != nil {
					return err
				}
			}
			if f.Changed("memo") {
				e.Memo = memo
			}
			if _, err := svc.ReplaceTransfer(ctx, id, e); err != nil {
				return err
			}
			now, err := svc.GetTransfer(ctx, id)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "Changed the transfer:")
			printHalves(w, now)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the account the money leaves")
	cmd.Flags().StringVar(&to, "to", "", "the account the money arrives in")
	cmd.Flags().StringVar(&amount, "amount", "", "what leaves, in the source account's currency")
	cmd.Flags().StringVar(&toAmount, "to-amount", "", "what arrives, when the currencies differ")
	cmd.Flags().StringVarP(&date, "date", "d", "", "the day of both halves")
	cmd.Flags().StringVar(&inDate, "in-date", "", "the day of the half that arrives")
	cmd.Flags().StringVarP(&memo, "memo", "m", "", "the description of both halves")
	return cmd
}

func (a *app) transferDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete both halves of a transfer, with --yes",
		Long: `Delete a transfer, given the number of either of its halves: both halves go,
or neither. Without --yes nothing is deleted: the two halves are listed.`,
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
			w := cmd.OutOrStdout()
			if !yes {
				v, err := svc.GetTransfer(ctx, id)
				if err != nil {
					return err
				}
				printHalves(w, v)
				fmt.Fprintln(w, "\nNothing was deleted. Run it again with --yes to delete both halves.")
				return nil
			}
			v, err := svc.DeleteTransfer(ctx, id)
			if err != nil {
				return err
			}
			fmt.Fprintln(w, "Deleted the transfer:")
			printHalves(w, v)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "delete both halves, instead of only listing them")
	return cmd
}

func (a *app) transferLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link A B",
		Short: "Make two transactions you already have the two halves of a transfer",
		Long: `Make two stored transactions the two halves of one transfer: money that left one
of your accounts (A or B) and arrived in another, entered or imported separately.
Both are booked to Equity:Transfers (Equity:Conversions when the currencies
differ) and linked, so that the move is neither spending nor income. Within one
currency they must move the same amount, one out and one in; each must be on one
account and one category, and neither may be a half already.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			first, err := transferID(args[0])
			if err != nil {
				return err
			}
			second, err := transferID(args[1])
			if err != nil {
				return err
			}
			res, err := svc.LinkTransfer(ctx, first, second)
			if err != nil {
				return err
			}
			v, err := svc.GetTransfer(ctx, res.OutID)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, "Linked as a transfer:")
			printHalves(w, v)
			for _, name := range res.Created {
				fmt.Fprintf(w, "Created new account %s\n", name)
			}
			return nil
		},
	}
}

func (a *app) transferUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink ID",
		Short: "Make the two halves of a transfer ordinary transactions again",
		Long: `Take the link between the two halves of a transfer away, given either half's
number. Both stay, each in Expenses:Uncategorized or Income:Uncategorized, so that
they can be categorized.`,
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
			v, err := svc.UnlinkTransfer(ctx, id)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unlinked #%d and #%d: both are uncategorized now.\n", v.Out.ID, v.In.ID)
			return nil
		},
	}
}
