package main

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// payeesListed is how many payees "thaler cli report payees" lists unless --limit says.
const payeesListed = 20

func (a *app) reportCmd() *cobra.Command {
	var from, to string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Spending, income, payees and net worth over the months",
		Long: `Show the reports of the Reports screen as tables: where the money went by
category, what came in against what went out, which payees took the most, and
what the accounts were worth at the end of each month.

Every report covers a range of months, the 12 up to this one unless you say:
  thaler cli report spending --from 2026-01 --to 2026-06
--from and --to are months (YYYY-MM), both included, at most 120 of them; an empty
--to is this month and an empty --from the 11 months before --to. Amounts are in
the base currency ("thaler cli currency"), each at the exchange rate of its day;
a currency with no rate at all is named in a warning and left out.`,
	}
	cmd.PersistentFlags().StringVar(&from, "from", "", "the first month, YYYY-MM (default: 11 months before --to)")
	cmd.PersistentFlags().StringVar(&to, "to", "", "the last month, YYYY-MM (default: this month)")

	run := func(show func(cmd *cobra.Command, svc core.Service, from, to string) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			svc, err := a.service(cmd.Context())
			if err != nil {
				return err
			}
			return show(cmd, svc, from, to)
		}
	}

	var byMonth bool
	spending := &cobra.Command{
		Use:   "spending",
		Short: "What each expense category spent, the most first",
		Long: `Show what each expense category spent over the months: its total and the average
per month, the biggest first, and the total of all of them. A refund counts
against its category. --months gives a column for each month instead of the
average.`,
		Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, svc core.Service, from, to string) error {
			r, err := svc.SpendingReport(cmd.Context(), core.SpendingRequest{From: from, To: to})
			if err != nil {
				return err
			}
			printSpending(cmd.OutOrStdout(), r, byMonth)
			return nil
		}),
	}
	spending.Flags().BoolVar(&byMonth, "months", false, "a column for each month, instead of the average per month")

	cashflow := &cobra.Command{
		Use:   "cashflow",
		Short: "What came in against what went out, month by month",
		Long: `Show for each month what came in (Income categories), what went out (Expenses
categories), what was kept and the share of the income that is: negative when
more went out than came in, a dash for a month with no income. A transfer
between your own accounts is neither. The last line is all the months.`,
		Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, svc core.Service, from, to string) error {
			r, err := svc.CashFlowReport(cmd.Context(), core.CashFlowRequest{From: from, To: to})
			if err != nil {
				return err
			}
			printCashFlow(cmd.OutOrStdout(), r)
			return nil
		}),
	}

	var limit int
	payees := &cobra.Command{
		Use:   "payees",
		Short: "The payees that took the most, the biggest first",
		Long: `Show what each payee took from your expense categories, with its share of all
the spending and how many transactions that was, the biggest first. Payees are
told apart without regard to case or spacing, transactions with no payee are one
line, and a refund counts against its payee. It lists 20 payees and adds up the
others in one line; --limit changes that, and --limit 0 lists every payee.`,
		Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, svc core.Service, from, to string) error {
			if limit < 0 {
				return fmt.Errorf("--limit cannot be negative")
			}
			r, err := svc.PayeeReport(cmd.Context(), core.PayeeRequest{From: from, To: to, Limit: limit})
			if err != nil {
				return err
			}
			printPayees(cmd.OutOrStdout(), r)
			return nil
		}),
	}
	payees.Flags().IntVar(&limit, "limit", payeesListed, "how many payees to list, the rest in one line; 0 lists them all")

	networth := &cobra.Command{
		Use:   "networth",
		Short: "What the accounts were worth at the end of each month",
		Long: `Show for each month what the asset and liability accounts held on its last day,
everything booked up to then, and how much that changed from the month before.
Each account's balance is counted at the exchange rate of that day, so a rate
entered later does not move a month that is past; a month before a currency's
first rate uses the rate at the end of the range. "thaler cli balance --as-of"
gives the same figure for one day.`,
		Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, svc core.Service, from, to string) error {
			r, err := svc.NetWorthReport(cmd.Context(), core.NetWorthRequest{From: from, To: to})
			if err != nil {
				return err
			}
			printNetWorth(cmd.OutOrStdout(), r)
			return nil
		}),
	}
	cmd.AddCommand(spending, cashflow, payees, networth)
	return cmd
}

// percentText is a share in basis points with two decimals and a %, or a dash.
func percentText(s core.SavingsRate) string {
	if !s.Known {
		return "-"
	}
	bp, sign := s.BasisPoints, ""
	if bp < 0 {
		sign, bp = "-", -bp
	}
	return fmt.Sprintf("%s%d.%02d%%", sign, bp/100, bp%100)
}

// printMissing warns about the currencies a report left out for want of a rate.
func printMissing(w io.Writer, base ledger.Currency, missing []ledger.Currency) {
	for _, c := range missing {
		fmt.Fprintf(w, "warning: no exchange rate %s -> %s, so %s is left out; add one with: thaler cli rate set %s %s <rate>\n", c, base, c, c, base)
	}
}

func monthsText(months []string) string {
	return months[0] + " to " + months[len(months)-1]
}

func printSpending(w io.Writer, r core.SpendingReport, byMonth bool) {
	if len(r.Categories) == 0 {
		fmt.Fprintf(w, "Nothing was spent from %s.\n", monthsText(r.Months))
		printMissing(w, r.Base, r.Missing)
		return
	}
	header := []string{"Category"}
	if byMonth {
		header = append(header, r.Months...)
		header = append(header, "Total")
	} else {
		header = append(header, "Total", "Per month")
	}
	row := func(name string, months []ledger.Money, total ledger.Money) []string {
		cells := []string{name}
		if byMonth {
			for _, m := range months {
				cells = append(cells, m.Decimal())
			}
			return append(cells, total.Decimal())
		}
		average := ledger.Money{Minor: total.Minor / int64(len(r.Months)), Currency: total.Currency}
		return append(cells, total.Decimal(), average.Decimal())
	}
	var rows [][]string
	for _, c := range r.Categories {
		rows = append(rows, row(c.Name, c.Months, c.Total))
	}
	rows = append(rows, row("Total", r.Totals, r.Total))
	fmt.Fprintf(w, "Spending by category, %s, in %s\n\n", monthsText(r.Months), r.Base)
	printTable(w, header, rows)
	printMissing(w, r.Base, r.Missing)
}

func printCashFlow(w io.Writer, r core.CashFlowReport) {
	row := func(name string, m core.CashFlowMonth) []string {
		return []string{name, m.Income.Decimal(), m.Spending.Decimal(), m.Net.Decimal(), percentText(m.Savings)}
	}
	var rows [][]string
	var months []string
	for _, m := range r.Months {
		rows = append(rows, row(m.Month, m))
		months = append(months, m.Month)
	}
	rows = append(rows, row("Total", r.Total))
	fmt.Fprintf(w, "Income against spending, %s, in %s\n\n", monthsText(months), r.Base)
	printTable(w, []string{"Month", "Income", "Spent", "Kept", "Saved"}, rows)
	printMissing(w, r.Base, r.Missing)
}

func printPayees(w io.Writer, r core.PayeeReport) {
	if len(r.Payees) == 0 {
		fmt.Fprintf(w, "Nothing was spent from %s to %s.\n", r.From, r.To)
		printMissing(w, r.Base, r.Missing)
		return
	}
	var rows [][]string
	count := r.Others.Transactions
	line := func(name string, p core.PayeeSpending) []string {
		return []string{name, p.Amount.Decimal(), percentText(r.Share(p)), strconv.Itoa(p.Transactions)}
	}
	for _, p := range r.Payees {
		name := p.Name
		if name == "" {
			name = "(no payee)"
		}
		count += p.Transactions
		rows = append(rows, line(name, p))
	}
	if r.OtherPayees > 0 {
		noun := "payees"
		if r.OtherPayees == 1 {
			noun = "payee"
		}
		rows = append(rows, line(fmt.Sprintf("(%d other %s)", r.OtherPayees, noun), r.Others))
	}
	rows = append(rows, []string{"Total", r.Total.Decimal(), "", strconv.Itoa(count)})
	fmt.Fprintf(w, "Payees, %s to %s, in %s\n\n", r.From, r.To, r.Base)
	printTable(w, []string{"Payee", "Amount", "Share", "Transactions"}, rows)
	printMissing(w, r.Base, r.Missing)
}

func printNetWorth(w io.Writer, r core.NetWorthReport) {
	var rows [][]string
	var months []string
	for i, m := range r.Months {
		change := ""
		if i > 0 {
			change = ledger.Money{Minor: m.Total.Minor - r.Months[i-1].Total.Minor, Currency: m.Total.Currency}.Decimal()
		}
		rows = append(rows, []string{m.Month, m.Total.Decimal(), change})
		months = append(months, m.Month)
	}
	fmt.Fprintf(w, "Net worth at the end of each month, %s, in %s\n\n", monthsText(months), r.Base)
	printTable(w, []string{"Month", "Net worth", "Change"}, rows)
	printMissing(w, r.Base, r.Missing)
}
