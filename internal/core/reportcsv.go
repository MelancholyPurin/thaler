package core

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The reports as CSV, for a spreadsheet. The text is not translated and the numbers
// are not formatted for a language: the first line names the columns in English, an
// amount is a plain decimal with a point and no thousands separator, in the base
// currency (named in a column), a month is YYYY-MM, a share is a percentage with two
// decimals. What a report left out for want of an exchange rate (Missing) is left out
// of the file too, and the file does not say so.
// Where a column holds names, a name that a spreadsheet would read as a formula is
// written with a ' before it.

// csvText is a name as a cell: one that starts with a character that makes a
// spreadsheet read a formula gets a ' in front, which is how they take it as text.
func csvText(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@' || s[0] == '\t' || s[0] == '\r') {
		return "'" + s
	}
	return s
}

// percentText writes basis points as a percentage with two decimals: 4167 is "41.67".
func percentText(bp int64) string {
	sign := ""
	if bp < 0 {
		sign, bp = "-", -bp
	}
	return fmt.Sprintf("%s%d.%02d", sign, bp/100, bp%100)
}

// encodeCSV writes the rows as CSV, with a line feed after each.
func encodeCSV(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(rows); err != nil { // WriteAll flushes
		return nil, err
	}
	return buf.Bytes(), nil
}

// SpendingCSV is a SpendingReport as CSV: a line for each category with its currency,
// a column for each month and its total, and, last, the line of all of them.
func SpendingCSV(r SpendingReport) ([]byte, error) {
	header := append([]string{"category", "currency"}, r.Months...)
	rows := [][]string{append(header, "total")}
	line := func(name string, months []string, total string) []string {
		return append(append([]string{name, string(r.Base)}, months...), total)
	}
	for _, c := range r.Categories {
		rows = append(rows, line(csvText(c.Name), moneyTexts(c.Months), c.Total.Decimal()))
	}
	rows = append(rows, line("Total", moneyTexts(r.Totals), r.Total.Decimal()))
	return encodeCSV(rows)
}

// CashFlowCSV is a CashFlowReport as CSV: a line for each month with what came in, what
// went out, what was kept and the share of the income that is (empty for a month with
// no income), and, last, the line of all of them.
func CashFlowCSV(r CashFlowReport) ([]byte, error) {
	rows := [][]string{{"month", "currency", "income", "spending", "net", "saved_percent"}}
	line := func(name string, m CashFlowMonth) []string {
		saved := ""
		if m.Savings.Known {
			saved = percentText(m.Savings.BasisPoints)
		}
		return []string{name, string(r.Base), m.Income.Decimal(), m.Spending.Decimal(), m.Net.Decimal(), saved}
	}
	for _, m := range r.Months {
		rows = append(rows, line(m.Month, m))
	}
	rows = append(rows, line("Total", r.Total))
	return encodeCSV(rows)
}

// PayeesCSV is a PayeeReport as CSV: a line for each payee, the one that took most
// first, with its currency, what it took, its share of all the spending (empty when
// that is nothing or less) and its transactions. A payee with no name has an empty
// name. The payees a Limit left out are one line with an empty name too, so ask for
// every payee.
func PayeesCSV(r PayeeReport) ([]byte, error) {
	rows := [][]string{{"payee", "currency", "amount", "share_percent", "transactions"}}
	for _, p := range r.Payees {
		share := ""
		if s := r.Share(p); s.Known {
			share = percentText(s.BasisPoints)
		}
		rows = append(rows, []string{csvText(p.Name), string(r.Base), p.Amount.Decimal(), share, strconv.Itoa(p.Transactions)})
	}
	return encodeCSV(rows)
}

// NetWorthCSV is a NetWorthReport as CSV: a line for each month with its currency and
// the net worth at the end of it.
func NetWorthCSV(r NetWorthReport) ([]byte, error) {
	rows := [][]string{{"month", "currency", "net_worth"}}
	for _, m := range r.Months {
		rows = append(rows, []string{m.Month, string(r.Base), m.Total.Decimal()})
	}
	return encodeCSV(rows)
}

func moneyTexts(ms []ledger.Money) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Decimal()
	}
	return out
}

// ReportCSV is the report asked for as a CSV file, for the months asked for. The payees
// file holds every payee, not a limited few. A report that is not one of ReportKinds is
// refused with unknown_report.
func (s *Local) ReportCSV(ctx context.Context, req ReportCSVRequest) (ReportCSV, error) {
	switch req.Report {
	case ReportSpending:
		r, err := s.SpendingReport(ctx, SpendingRequest{From: req.From, To: req.To, RateDate: req.RateDate})
		return csvFile(r.Missing, err, func() ([]byte, error) { return SpendingCSV(r) })
	case ReportCashFlow:
		r, err := s.CashFlowReport(ctx, CashFlowRequest{From: req.From, To: req.To, RateDate: req.RateDate})
		return csvFile(r.Missing, err, func() ([]byte, error) { return CashFlowCSV(r) })
	case ReportPayees:
		r, err := s.PayeeReport(ctx, PayeeRequest{From: req.From, To: req.To, RateDate: req.RateDate})
		return csvFile(r.Missing, err, func() ([]byte, error) { return PayeesCSV(r) })
	case ReportNetWorth:
		r, err := s.NetWorthReport(ctx, NetWorthRequest{From: req.From, To: req.To, RateDate: req.RateDate})
		return csvFile(r.Missing, err, func() ([]byte, error) { return NetWorthCSV(r) })
	}
	return ReportCSV{}, coded.New("unknown_report", map[string]any{"Report": string(req.Report)})
}

// csvFile is a report's CSV once the report is made: err is what making it said.
func csvFile(missing []ledger.Currency, err error, encode func() ([]byte, error)) (ReportCSV, error) {
	if err != nil {
		return ReportCSV{}, err
	}
	data, err := encode()
	if err != nil {
		return ReportCSV{}, err
	}
	return ReportCSV{Data: data, Missing: missing}, nil
}
