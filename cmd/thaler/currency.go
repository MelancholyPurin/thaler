package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func (a *app) currencyCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "currency",
		Short: "Show or change the base (reporting) currency",
		Long: `The base currency is the one reports are converted into. Changing it does not
touch any stored amount: accounts keep their own currency, and conversions only
happen when reports are produced. Any three-letter code works (EUR, USD, MXN, ...).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			base, err := svc.BaseCurrency(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Base currency: %s\n", base)

			accs, err := svc.ListAccounts(ctx)
			if err != nil {
				return err
			}
			used := make(map[ledger.Currency]bool)
			for _, acc := range accs {
				if acc.Currency != "" && acc.Currency != base {
					used[acc.Currency] = true
				}
			}
			if len(used) == 0 {
				return nil
			}
			rates, err := svc.Rates(ctx)
			if err != nil {
				return err
			}
			book := ledger.NewRateBook(rates)
			names := make([]string, 0, len(used))
			for c := range used {
				names = append(names, string(c))
			}
			sort.Strings(names)
			fmt.Fprintln(out, "\nOther currencies in use:")
			for _, n := range names {
				c := ledger.Currency(n)
				if r, ok := book.Lookup(c, base, todayISO()); ok {
					fmt.Fprintf(out, "  1 %s = %s %s\n", c, r.FloatString(4), base)
				} else {
					fmt.Fprintf(out, "  %s: no exchange rate yet; add one with: thaler cli rate set %s %s <rate>\n", c, c, base)
				}
			}
			return nil
		},
	}

	set := &cobra.Command{
		Use:   "set CODE",
		Short: "Change the base currency, e.g. thaler cli currency set USD",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := ledger.ParseCurrency(args[0])
			if err != nil {
				return err
			}
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			if err := svc.SetBaseCurrency(ctx, c); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Base currency is now %s\n", c)
			return nil
		},
	}
	root.AddCommand(set)
	return root
}

func (a *app) rateCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rate",
		Short: "Record and list exchange rates used for conversions in reports",
	}

	var date string
	set := &cobra.Command{
		Use:   "set BASE QUOTE RATE",
		Short: "Record that 1 BASE is worth RATE QUOTE, e.g. thaler cli rate set USD EUR 0.92",
		Long: `Record an exchange rate. "thaler cli rate set USD EUR 0.92" means 1 USD = 0.92 EUR.
You only need one direction per pair: the inverse is derived, and when two
currencies have no direct rate they are converted through a third one that has
rates to both (USD -> MXN via EUR, say). For any report date the most recent
rate on or before that date is used.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			base, err := ledger.ParseCurrency(args[0])
			if err != nil {
				return err
			}
			quote, err := ledger.ParseCurrency(args[1])
			if err != nil {
				return err
			}
			_, text, err := ledger.ParseRate(args[2])
			if err != nil {
				return err
			}
			when, err := a.parseDateFlag(date)
			if err != nil {
				return err
			}
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			day := when.Format(ledger.DateLayout)
			if err := svc.SetRate(ctx, day, base, quote, text); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "On %s: 1 %s = %s %s\n", day, base, text, quote)
			return nil
		},
	}
	set.Flags().StringVarP(&date, "date", "d", "", "day the rate applies from (default: today)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List all recorded exchange rates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			rates, err := svc.Rates(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(rates) == 0 {
				fmt.Fprintln(out, "No exchange rates yet. Add one with: thaler cli rate set USD EUR 0.92")
				return nil
			}
			for _, r := range rates {
				fmt.Fprintf(out, "%s  1 %s = %s %s\n", r.Date, r.Base, r.Rate.FloatString(6), r.Quote)
			}
			return nil
		},
	}

	root.AddCommand(set, list)
	return root
}
