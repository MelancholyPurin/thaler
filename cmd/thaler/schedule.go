package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func (a *app) scheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage recurring transactions and confirm what is due",
		Long: `A schedule is a transaction that repeats: rent every month, a salary on the
28th, an insurance every year. Thaler never books one by itself. "schedule due"
lists the dates that have come, and each one is confirmed (booked, optionally for
another amount) or skipped:

  thaler cli schedule add Rent --unit month -a Assets:Bank --amount=-900 -t Expenses:Rent --start 2026-11-28
  thaler cli schedule due
  thaler cli schedule confirm 1 2026-11-28
  thaler cli schedule skip 1 2026-12-28

Without a subcommand it lists the schedules.`,
		Args: cobra.NoArgs,
		RunE: a.listSchedules,
	}
	cmd.AddCommand(a.scheduleAddCmd(), a.scheduleListCmd(), a.scheduleEditCmd(), a.scheduleDeleteCmd(), a.schedulePauseCmd(), a.scheduleResumeCmd(), a.scheduleDueCmd(), a.scheduleConfirmCmd(), a.scheduleSkipCmd())
	return cmd
}

// today is the date "due" is counted up to: the clock of the app.
func (a *app) today() string {
	if a.now != nil {
		return a.now().Format(ledger.DateLayout)
	}
	return todayISO()
}

// scheduleUnit reads "month" or "months", and so on.
func scheduleUnit(s string) (core.ScheduleUnit, error) {
	switch strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "s") {
	case "week":
		return core.EveryWeek, nil
	case "month":
		return core.EveryMonth, nil
	case "year":
		return core.EveryYear, nil
	}
	return "", fmt.Errorf("--unit must be week, month or year, not %q", s)
}

func scheduleID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%q is not a schedule number; see thaler cli schedule", s)
	}
	return id, nil
}

// repeats says how often, in words: "every month", "every 2 weeks".
func repeats(s core.Schedule) string {
	if s.Every == 1 {
		return "every " + string(s.Unit)
	}
	return fmt.Sprintf("every %d %ss", s.Every, s.Unit)
}

// amountOf is the amount on the account of a schedule, or "split" for one with
// more than two postings.
func amountOf(s core.Schedule) string {
	if src, _, ok := (core.TxView{Postings: s.Postings}).Legs(); ok {
		return src.Amount.String()
	}
	return "split"
}

func (a *app) scheduleAddCmd() *cobra.Command {
	var every int
	var unit, start, end, account, amount, to, payee, memo, currency string
	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Define a schedule, e.g. Rent, monthly",
		Long: `Define a schedule: a transaction like "thaler cli add" makes, repeated every
--every weeks, months or years (--unit) from --start. A monthly schedule keeps
the day of its start, and falls on the last day of a month that is shorter. The
schedule books nothing by itself; see "thaler cli schedule due". --end is the last
day it may fall on.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			u, err := scheduleUnit(unit)
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
			when, err := a.parseDateFlag(start)
			if err != nil {
				return err
			}
			var last time.Time
			if end != "" {
				if last, err = a.parseDateFlag(end); err != nil {
					return err
				}
			}
			counter := to
			if counter == "" {
				counter = ledger.UncategorizedExpense
				if amt.Minor > 0 {
					counter = ledger.UncategorizedIncome
				}
			}
			id, err := svc.CreateSchedule(ctx, core.NewSchedule{
				Name: args[0], Every: every, Unit: u, Start: when, End: last, Payee: payee, Description: memo,
				Postings: []ledger.Posting{{Account: acc.Name, Amount: amt}, {Account: counter, Amount: amt.Neg()}},
			})
			if err != nil {
				return err
			}
			s := core.Schedule{Every: every, Unit: u}
			until := ""
			if !last.IsZero() {
				until = " until " + last.Format(ledger.DateLayout)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Schedule #%d %s: %s from %s%s, %s  %s -> %s\n", id, strings.TrimSpace(args[0]), repeats(s), when.Format(ledger.DateLayout), until, signed(amt), acc.Name, counter)
			return nil
		},
	}
	cmd.Flags().IntVar(&every, "every", 1, "repeat every this many units")
	cmd.Flags().StringVar(&unit, "unit", "", "week, month or year (required)")
	cmd.Flags().StringVarP(&start, "start", "d", "", "the first date, YYYY-MM-DD or DD.MM.YYYY (default: today)")
	cmd.Flags().StringVar(&end, "end", "", "the last date it may fall on, YYYY-MM-DD or DD.MM.YYYY (default: no end)")
	cmd.Flags().StringVarP(&account, "account", "a", "", "account the money moves in or out of, e.g. Assets:Sparkasse (required)")
	cmd.Flags().StringVar(&amount, "amount", "", "signed amount, e.g. -900 or 2450,00 (required)")
	cmd.Flags().StringVarP(&to, "to", "t", "", "category or other account on the other side (default: Expenses:Uncategorized / Income:Uncategorized)")
	cmd.Flags().StringVarP(&payee, "payee", "p", "", "who is paid or who pays you")
	cmd.Flags().StringVarP(&memo, "memo", "m", "", "free-text description")
	cmd.Flags().StringVarP(&currency, "currency", "c", "", "only needed when --account has no fixed currency")
	_ = cmd.MarkFlagRequired("unit")
	_ = cmd.MarkFlagRequired("account")
	_ = cmd.MarkFlagRequired("amount")
	return cmd
}

func (a *app) scheduleListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List the schedules", Args: cobra.NoArgs, RunE: a.listSchedules}
}

func (a *app) listSchedules(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	svc, err := a.service(ctx)
	if err != nil {
		return err
	}
	all, err := svc.Schedules(ctx)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(all) == 0 {
		fmt.Fprintln(out, "No schedules. Add one with: thaler cli schedule add Rent --unit month -a Assets:Bank --amount=-900 -t Expenses:Rent --start 2026-11-28")
		return nil
	}
	header := []string{"Schedule", "ID", "Repeats", "Start", "Amount"}
	var rows [][]string
	notes := false
	for _, s := range all {
		note := scheduleNote(s)
		notes = notes || note != ""
		rows = append(rows, []string{s.Name, strconv.FormatInt(s.ID, 10), repeats(s), s.Start, amountOf(s), note})
	}
	if !notes { // most schedules run for ever: no column for what they do not have
		for i := range rows {
			rows[i] = rows[i][:len(header)]
		}
	} else {
		header = append(header, "Note")
	}
	printTable(out, header, rows)
	return nil
}

// scheduleNote says what is out of the ordinary about s: paused, or ending.
func scheduleNote(s core.Schedule) string {
	var notes []string
	if s.Paused() {
		notes = append(notes, "paused")
	}
	if s.End != "" {
		notes = append(notes, "ends "+s.End)
	}
	return strings.Join(notes, ", ")
}

// scheduleNamed finds schedule id, for the words of a message.
func scheduleNamed(cmd *cobra.Command, svc core.Service, id int64) (core.Schedule, bool) {
	all, err := svc.Schedules(cmd.Context())
	if err != nil {
		return core.Schedule{}, false
	}
	for _, s := range all {
		if s.ID == id {
			return s, true
		}
	}
	return core.Schedule{}, false
}

func (a *app) scheduleEditCmd() *cobra.Command {
	var name, unit, start, end, amount, payee, memo string
	var every int
	cmd := &cobra.Command{
		Use:   "edit ID",
		Short: "Change a schedule: only the flags you give are changed",
		Long: `Change a schedule. Only the flags you give change; the rest stays. --amount is
the amount on the account, signed as in "thaler cli add", and works on a schedule
with one account and one category. To move a schedule to other accounts, delete
it and add it again. Occurrences already confirmed or skipped stay as they are.
Pauses are not changed here; see "schedule pause".`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			id, err := scheduleID(args[0])
			if err != nil {
				return err
			}
			cur, ok := scheduleNamed(cmd, svc, id)
			if !ok {
				return fmt.Errorf("no schedule number %d; see thaler cli schedule", id)
			}
			ns := core.NewSchedule{Name: cur.Name, Every: cur.Every, Unit: cur.Unit, Payee: cur.Payee, Description: cur.Description}
			if ns.Start, err = ledger.ParseDate(cur.Start); err != nil {
				return err
			}
			if cur.End != "" {
				if ns.End, err = ledger.ParseDate(cur.End); err != nil {
					return err
				}
			}
			for _, p := range cur.Postings {
				ns.Postings = append(ns.Postings, ledger.Posting{Account: p.Account, Amount: p.Amount})
			}
			f := cmd.Flags()
			changes := 0
			for _, flag := range []string{"name", "every", "unit", "start", "end", "amount", "payee", "memo"} {
				if f.Changed(flag) {
					changes++
				}
			}
			if changes == 0 {
				return errors.New("nothing to change: give at least one of --name, --every, --unit, --start, --end, --amount, --payee, --memo")
			}
			if f.Changed("name") {
				ns.Name = name
			}
			if f.Changed("every") {
				ns.Every = every
			}
			if f.Changed("unit") {
				if ns.Unit, err = scheduleUnit(unit); err != nil {
					return err
				}
			}
			if f.Changed("start") {
				if ns.Start, err = a.parseDateFlag(start); err != nil {
					return err
				}
			}
			if f.Changed("end") {
				ns.End = time.Time{}
				if strings.ToLower(strings.TrimSpace(end)) != "none" {
					if ns.End, err = a.parseDateFlag(end); err != nil {
						return err
					}
				}
			}
			if f.Changed("payee") {
				ns.Payee = payee
			}
			if f.Changed("memo") {
				ns.Description = memo
			}
			if f.Changed("amount") {
				if ns.Postings, err = withAmount(cur, amount); err != nil {
					return err
				}
			}
			if err := svc.ReplaceSchedule(ctx, id, ns); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Changed schedule #%d %s\n", id, strings.TrimSpace(ns.Name))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "a new name")
	cmd.Flags().IntVar(&every, "every", 1, "repeat every this many units")
	cmd.Flags().StringVar(&unit, "unit", "", "week, month or year")
	cmd.Flags().StringVarP(&start, "start", "d", "", "a new first date, YYYY-MM-DD or DD.MM.YYYY")
	cmd.Flags().StringVar(&end, "end", "", "a new last date, or none for no end")
	cmd.Flags().StringVar(&amount, "amount", "", "a new signed amount on the account")
	cmd.Flags().StringVarP(&payee, "payee", "p", "", "a new payee")
	cmd.Flags().StringVarP(&memo, "memo", "m", "", "a new description")
	return cmd
}

// withAmount is the postings of s with the amount on its account changed to the
// one typed; the category side follows. It needs one account and one category.
func withAmount(s core.Schedule, typed string) ([]ledger.Posting, error) {
	src, _, ok := (core.TxView{Postings: s.Postings}).Legs()
	if !ok {
		return nil, errors.New("--amount works on a schedule with one account and one category; delete this one and add it again")
	}
	amt, err := ledger.ParseAmount(typed, src.Amount.Currency, 0)
	if err != nil {
		return nil, err
	}
	if amt.IsZero() {
		return nil, errors.New("amount must not be zero")
	}
	var out []ledger.Posting
	for _, p := range s.Postings {
		if p.Account == src.Account {
			out = append(out, ledger.Posting{Account: p.Account, Amount: amt})
		} else {
			out = append(out, ledger.Posting{Account: p.Account, Amount: amt.Neg()})
		}
	}
	return out, nil
}

func (a *app) scheduleDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete ID",
		Short: "Remove a schedule; what it booked stays",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			id, err := scheduleID(args[0])
			if err != nil {
				return err
			}
			s, _ := scheduleNamed(cmd, svc, id)
			if err := svc.DeleteSchedule(ctx, id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted schedule #%d %s\n", id, s.Name)
			return nil
		},
	}
}

func (a *app) schedulePauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause ID",
		Short: "Stop a schedule proposing anything from today",
		Long: `Pause a schedule from today: it proposes nothing until it is resumed, and the
dates that fall in between are dropped, not held back for later. Dates before the
pause that were not answered stay due. To end a schedule for good, set its --end.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.pauseOrResume(cmd, args[0], true)
		},
	}
}

func (a *app) scheduleResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume ID",
		Short: "Start a paused schedule again from today",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.pauseOrResume(cmd, args[0], false)
		},
	}
}

func (a *app) pauseOrResume(cmd *cobra.Command, idArg string, pause bool) error {
	ctx := cmd.Context()
	svc, err := a.service(ctx)
	if err != nil {
		return err
	}
	id, err := scheduleID(idArg)
	if err != nil {
		return err
	}
	if pause {
		err = svc.PauseSchedule(ctx, id)
	} else {
		err = svc.ResumeSchedule(ctx, id)
	}
	if err != nil {
		return err
	}
	s, _ := scheduleNamed(cmd, svc, id)
	if pause {
		fmt.Fprintf(cmd.OutOrStdout(), "Paused schedule #%d %s: nothing is proposed until it is resumed\n", id, s.Name)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Resumed schedule #%d %s\n", id, s.Name)
	}
	return nil
}

func (a *app) scheduleDueCmd() *cobra.Command {
	var until string
	cmd := &cobra.Command{
		Use:   "due",
		Short: "List the occurrences that have come and are not answered yet",
		Long: `List every date of every schedule up to today (or --until) that has not been
confirmed or skipped. Nothing is booked: answer each with "schedule confirm"
or "schedule skip".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			upTo := a.today()
			if until != "" {
				d, err := ledger.ParseDate(until)
				if err != nil {
					return err
				}
				upTo = d.Format(ledger.DateLayout)
			}
			due, err := svc.Due(ctx, upTo)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(due) == 0 {
				fmt.Fprintf(out, "Nothing is due up to %s.\n", upTo)
				return nil
			}
			var rows [][]string
			for _, o := range due {
				rows = append(rows, []string{o.Date, strconv.FormatInt(o.Schedule.ID, 10), amountOf(o.Schedule), o.Schedule.Name})
			}
			printTable(out, []string{"Date", "ID", "Amount", "Schedule"}, rows)
			fmt.Fprintln(out, "\nAnswer each with: thaler cli schedule confirm ID DATE [--amount=N], or thaler cli schedule skip ID DATE")
			return nil
		},
	}
	cmd.Flags().StringVar(&until, "until", "", "count up to this date instead of today, YYYY-MM-DD or DD.MM.YYYY")
	return cmd
}

func (a *app) scheduleConfirmCmd() *cobra.Command {
	var amount string
	cmd := &cobra.Command{
		Use:   "confirm ID DATE",
		Short: "Book one due occurrence, as it is or for another amount",
		Long: `Book the occurrence of schedule ID on DATE (a date "schedule due" lists) as a
transaction dated that day. --amount books another amount than the schedule's,
signed as in "thaler cli add", for a schedule with one account and one category.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			id, err := scheduleID(args[0])
			if err != nil {
				return err
			}
			var postings []ledger.Posting
			if cmd.Flags().Changed("amount") {
				s, ok := scheduleNamed(cmd, svc, id)
				if !ok {
					return fmt.Errorf("no schedule number %d; see thaler cli schedule", id)
				}
				if postings, err = withAmount(s, amount); err != nil {
					return err
				}
			}
			res, err := svc.ConfirmOccurrence(ctx, id, args[1], postings)
			if err != nil {
				return err
			}
			tx, err := svc.GetTransaction(ctx, res.ID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if src, cat, ok := tx.Legs(); ok {
				fmt.Fprintf(out, "Booked #%d  %s  %s  %s -> %s\n", tx.ID, tx.Date, signed(src.Amount), src.Account, cat.Account)
			} else {
				fmt.Fprintf(out, "Booked #%d  %s\n", tx.ID, tx.Date)
			}
			for _, n := range res.Created {
				fmt.Fprintf(out, "Created new account %s\n", n)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&amount, "amount", "", "another signed amount on the account")
	return cmd
}

func (a *app) scheduleSkipCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "skip ID DATE",
		Short: "Pass on one due occurrence: nothing is booked",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			id, err := scheduleID(args[0])
			if err != nil {
				return err
			}
			if err := svc.SkipOccurrence(ctx, id, args[1]); err != nil {
				return err
			}
			s, _ := scheduleNamed(cmd, svc, id)
			d, _ := ledger.ParseDate(args[1])
			fmt.Fprintf(cmd.OutOrStdout(), "Skipped %s of %s\n", d.Format(ledger.DateLayout), s.Name)
			return nil
		},
	}
}
