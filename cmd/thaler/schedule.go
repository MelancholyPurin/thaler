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
	if s.Transfer && len(s.Postings) == 2 {
		return s.Postings[0].Amount.String()
	}
	if src, _, ok := (core.TxView{Postings: s.Postings}).Legs(); ok {
		if s.Tolerance > 0 {
			return "~" + src.Amount.String() // an estimate
		}
		return src.Amount.String()
	}
	return "split"
}

// transferPostings are the postings of a scheduled transfer of amount (its sign
// does not matter) from source to dest, with what arrives in toAmount when the two
// accounts hold different currencies.
func transferPostings(cmd *cobra.Command, svc core.Service, source string, amount ledger.Money, dest, toAmount string) ([]ledger.Posting, error) {
	dst, err := svc.GetAccount(cmd.Context(), dest)
	if err != nil {
		return nil, err
	}
	out := ledger.Money{Minor: amount.Minor, Currency: amount.Currency}
	if out.Minor > 0 {
		out = out.Neg()
	}
	in := out.Neg()
	if dst.Currency != "" && dst.Currency != out.Currency {
		if toAmount == "" {
			return nil, fmt.Errorf("%s holds %s, not %s: say how much arrives with --to-amount", dest, dst.Currency, out.Currency)
		}
		if in, err = ledger.ParseAmount(toAmount, dst.Currency, 0); err != nil {
			return nil, fmt.Errorf("--to-amount: %w", err)
		}
		if in.Minor < 0 {
			in = in.Neg()
		}
	} else if toAmount != "" {
		return nil, errors.New("both accounts hold the same currency: there is no separate --to-amount")
	}
	return []ledger.Posting{{Account: source, Amount: out}, {Account: dest, Amount: in}}, nil
}

// arrives says what arrives when a scheduled transfer changes currency.
func arrives(p []ledger.Posting) string {
	if len(p) == 2 && p[0].Amount.Currency != p[1].Amount.Currency {
		return " (arrives " + p[1].Amount.String() + ")"
	}
	return ""
}

func (a *app) scheduleAddCmd() *cobra.Command {
	var every, tolerance int
	var unit, start, end, account, amount, toAmount, to, payee, memo, currency string
	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Define a schedule, e.g. Rent, monthly",
		Long: `Define a schedule: a transaction like "thaler cli add" makes, repeated every
--every weeks, months or years (--unit) from --start. A monthly schedule keeps
the day of its start, and falls on the last day of a month that is shorter. The
schedule books nothing by itself; see "thaler cli schedule due". --end is the last
day it may fall on.

When --to is one of your own accounts (Assets or Liabilities) the schedule is a
transfer, like "thaler cli transfer": -a is the account the money leaves, --to the
one it arrives in, and the amount is how much leaves (its sign does not matter).
Between accounts in different currencies --to-amount says how much arrives:

  thaler cli schedule add Saving --unit month -a Assets:Bank --amount 200 --to Assets:Savings --start 2026-11-01

For a bill that changes (electricity), give the amount it usually is and a
--tolerance in percent: a bank row within that much of it, on either side, still
settles the date. Without one the amount has to be exact.

  thaler cli schedule add Electricity --unit month -a Assets:Bank --amount=-80 -t Expenses:Power --tolerance 25`,
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
			ns := core.NewSchedule{Name: args[0], Every: every, Unit: u, Start: when, End: last, Tolerance: tolerance, Payee: payee, Description: memo}
			transfer := false
			if typ, err := ledger.TypeOf(counter); err == nil && typ.NeedsCurrency() {
				transfer = true
				if ns.Postings, err = transferPostings(cmd, svc, acc.Name, amt, counter, toAmount); err != nil {
					return err
				}
				ns.Transfer = true
				ns.Payee = "" // the transfer names its own halves
			} else {
				if cmd.Flags().Changed("to-amount") {
					return errors.New("--to-amount is for a transfer between your own accounts")
				}
				ns.Postings = []ledger.Posting{{Account: acc.Name, Amount: amt}, {Account: counter, Amount: amt.Neg()}}
			}
			id, err := svc.CreateSchedule(ctx, ns)
			if err != nil {
				return err
			}
			s := core.Schedule{Every: every, Unit: u}
			until := ""
			if !last.IsZero() {
				until = " until " + last.Format(ledger.DateLayout)
			}
			if transfer {
				fmt.Fprintf(cmd.OutOrStdout(), "Schedule #%d %s: %s from %s%s, transfer %s  %s -> %s%s\n", id, strings.TrimSpace(args[0]), repeats(s), when.Format(ledger.DateLayout), until,
					ns.Postings[0].Amount.Neg(), acc.Name, counter, arrives(ns.Postings))
				return nil
			}
			estimate := ""
			if tolerance > 0 {
				estimate = fmt.Sprintf(" (an estimate, ±%d%%)", tolerance)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Schedule #%d %s: %s from %s%s, %s%s  %s -> %s\n", id, strings.TrimSpace(args[0]), repeats(s), when.Format(ledger.DateLayout), until, signed(amt), estimate, acc.Name, counter)
			return nil
		},
	}
	cmd.Flags().IntVar(&every, "every", 1, "repeat every this many units")
	cmd.Flags().StringVar(&unit, "unit", "", "week, month or year (required)")
	cmd.Flags().StringVarP(&start, "start", "d", "", "the first date, YYYY-MM-DD or DD.MM.YYYY (default: today)")
	cmd.Flags().StringVar(&end, "end", "", "the last date it may fall on, YYYY-MM-DD or DD.MM.YYYY (default: no end)")
	cmd.Flags().StringVar(&toAmount, "to-amount", "", "a transfer between currencies: how much arrives")
	cmd.Flags().IntVar(&tolerance, "tolerance", 0, "percent the amount may differ from a bank row's and still match it, 0 to 100 (default: exact)")
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
	if s.Transfer && len(s.Postings) == 2 {
		notes = append(notes, "transfer to "+s.Postings[1].Account)
	}
	if s.Tolerance > 0 {
		notes = append(notes, fmt.Sprintf("estimate, ±%d%%", s.Tolerance))
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
	var name, unit, start, end, amount, toAmount, payee, memo string
	var every, tolerance int
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
			ns := core.NewSchedule{Name: cur.Name, Every: cur.Every, Unit: cur.Unit, Payee: cur.Payee, Description: cur.Description, Transfer: cur.Transfer, Tolerance: cur.Tolerance}
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
			for _, flag := range []string{"name", "every", "unit", "start", "end", "amount", "to-amount", "tolerance", "payee", "memo"} {
				if f.Changed(flag) {
					changes++
				}
			}
			if changes == 0 {
				return errors.New("nothing to change: give at least one of --name, --every, --unit, --start, --end, --amount, --to-amount, --tolerance, --payee, --memo")
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
			if f.Changed("tolerance") {
				ns.Tolerance = tolerance
			}
			if f.Changed("payee") {
				ns.Payee = payee
			}
			if f.Changed("memo") {
				ns.Description = memo
			}
			if f.Changed("amount") || f.Changed("to-amount") {
				if ns.Postings, err = withAmount(cur, amount, toAmount); err != nil {
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
	cmd.Flags().StringVar(&amount, "amount", "", "a new signed amount on the account (for a transfer: how much leaves)")
	cmd.Flags().StringVar(&toAmount, "to-amount", "", "a transfer between currencies: how much arrives")
	cmd.Flags().IntVar(&tolerance, "tolerance", 0, "percent the amount may differ from a bank row's and still match it; 0 for exact")
	cmd.Flags().StringVarP(&payee, "payee", "p", "", "a new payee")
	cmd.Flags().StringVarP(&memo, "memo", "m", "", "a new description")
	return cmd
}

// withAmount is the postings of s with the amount on its account changed to the
// one typed; the category side follows. It needs one account and one category.
// For a scheduled transfer the amount typed is how much leaves (its sign does not
// matter) and typedTo how much arrives, which only a transfer between currencies
// has: either may be left empty to keep what the schedule has.
func withAmount(s core.Schedule, typed, typedTo string) ([]ledger.Posting, error) {
	if s.Transfer {
		return withTransferAmount(s, typed, typedTo)
	}
	if typedTo != "" {
		return nil, errors.New("--to-amount is for a transfer between your own accounts")
	}
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

func withTransferAmount(s core.Schedule, typed, typedTo string) ([]ledger.Posting, error) {
	out, in := s.Postings[0], s.Postings[1]
	cross := out.Amount.Currency != in.Amount.Currency
	if typed != "" {
		amt, err := ledger.ParseAmount(typed, out.Amount.Currency, 0)
		if err != nil {
			return nil, err
		}
		if amt.IsZero() {
			return nil, errors.New("amount must not be zero")
		}
		if amt.Minor > 0 {
			amt = amt.Neg()
		}
		out.Amount = amt
		if cross && typedTo == "" {
			return nil, fmt.Errorf("%s holds %s, not %s: say how much arrives with --to-amount", in.Account, in.Amount.Currency, out.Amount.Currency)
		}
	}
	switch {
	case typedTo != "" && !cross:
		return nil, errors.New("both accounts hold the same currency: there is no separate --to-amount")
	case typedTo != "":
		amt, err := ledger.ParseAmount(typedTo, in.Amount.Currency, 0)
		if err != nil {
			return nil, fmt.Errorf("--to-amount: %w", err)
		}
		if amt.IsZero() {
			return nil, errors.New("amount must not be zero")
		}
		if amt.Minor < 0 {
			amt = amt.Neg()
		}
		in.Amount = amt
	case !cross:
		in.Amount = out.Amount.Neg()
	}
	return []ledger.Posting{{Account: out.Account, Amount: out.Amount}, {Account: in.Account, Amount: in.Amount}}, nil
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
	var amount, toAmount string
	cmd := &cobra.Command{
		Use:   "confirm ID DATE",
		Short: "Book one due occurrence, as it is or for another amount",
		Long: `Book the occurrence of schedule ID on DATE (a date "schedule due" lists) as a
transaction dated that day. --amount books another amount than the schedule's,
signed as in "thaler cli add", for a schedule with one account and one category.
A scheduled transfer is booked as a transfer; --amount is then how much leaves
and, between currencies, --to-amount how much arrives.`,
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
			if cmd.Flags().Changed("amount") || cmd.Flags().Changed("to-amount") {
				s, ok := scheduleNamed(cmd, svc, id)
				if !ok {
					return fmt.Errorf("no schedule number %d; see thaler cli schedule", id)
				}
				if postings, err = withAmount(s, amount, toAmount); err != nil {
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
			if res.InID != 0 {
				in, err := svc.GetTransaction(ctx, res.InID)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Transferred #%d and #%d  %s  %s  %s -> %s%s\n", tx.ID, in.ID, tx.Date, tx.Postings[0].Amount.Neg(), tx.Postings[0].Account, in.Postings[0].Account, arrives([]ledger.Posting{{Amount: tx.Postings[0].Amount}, {Amount: in.Postings[0].Amount}}))
			} else if src, cat, ok := tx.Legs(); ok {
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
	cmd.Flags().StringVar(&amount, "amount", "", "another signed amount on the account (for a transfer: how much leaves)")
	cmd.Flags().StringVar(&toAmount, "to-amount", "", "a transfer between currencies: how much arrives")
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
