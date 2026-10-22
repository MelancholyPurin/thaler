package importer

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// Row is one booked transaction from a bank export.
type Row struct {
	Line        int // 1-based line of the record in the file
	Date        time.Time
	Payee       string // counterparty; falls back to the booking text
	Purpose     string // free-text reference, whitespace collapsed
	IBAN        string // counterparty account
	BookingText string // bank's transaction type, e.g. KARTENZAHLUNG
	Amount      ledger.Money
}

// RowError describes a record that could not be read.
type RowError struct {
	Line int
	Err  error
}

func (e RowError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }

// ParseResult is the outcome of reading one export.
type ParseResult struct {
	Rows    []Row
	Skipped int // rows ignored by the profile's skip rule (pending bookings)
	Errors  []RowError
}

// Parse reads a CSV export according to profile p. fallback is the currency
// used when the file has no currency column or the cell is empty.
func Parse(r io.Reader, p Profile, fallback ledger.Currency) (*ParseResult, error) {
	cp, err := p.compile()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	text, err := decodeToUTF8(raw, cp.encoding)
	if err != nil {
		return nil, err
	}

	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = cp.delim
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1

	var header []string
	for header == nil {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("file is empty: no header row found")
		}
		if err != nil {
			return nil, fmt.Errorf("reading header: %w", err)
		}
		if !blank(rec) {
			header = rec
		}
	}
	index := make(map[string]int, len(header))
	for i, h := range header {
		key := normHeader(h)
		if _, dup := index[key]; !dup {
			index[key] = i
		}
	}
	find := func(names []string) int {
		for _, n := range names {
			if i, ok := index[normHeader(n)]; ok {
				return i
			}
		}
		return -1
	}
	cols := struct{ date, payee, purpose, iban, booking, amount, currency, skip int }{
		date:     find(p.Columns.Date),
		payee:    find(p.Columns.Payee),
		purpose:  find(p.Columns.Purpose),
		iban:     find(p.Columns.IBAN),
		booking:  find(p.Columns.BookingText),
		amount:   find(p.Columns.Amount),
		currency: find(p.Columns.Currency),
		skip:     -1,
	}
	if p.SkipColumn != "" {
		cols.skip = find([]string{p.SkipColumn})
	}
	if cols.date < 0 || cols.amount < 0 {
		return nil, fmt.Errorf("profile %q does not match this file: need a date column (%s) and an amount column (%s), but the header is: %s",
			p.Name, strings.Join(p.Columns.Date, " / "), strings.Join(p.Columns.Amount, " / "), strings.Join(header, "; "))
	}
	skipNeedle := strings.ToLower(strings.TrimSpace(p.SkipContains))

	res := &ParseResult{}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if blank(rec) {
			continue
		}
		line, _ := cr.FieldPos(0)
		get := func(i int) string {
			if i < 0 || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		fail := func(err error) { res.Errors = append(res.Errors, RowError{Line: line, Err: err}) }

		if cols.skip >= 0 && skipNeedle != "" && strings.Contains(strings.ToLower(get(cols.skip)), skipNeedle) {
			res.Skipped++
			continue
		}
		date, err := parseDate(get(cols.date), cp.layouts)
		if err != nil {
			fail(err)
			continue
		}
		cur := fallback
		if v := get(cols.currency); v != "" {
			if cur, err = ledger.ParseCurrency(v); err != nil {
				fail(err)
				continue
			}
		}
		amount, err := ledger.ParseAmount(get(cols.amount), cur, cp.sep)
		if err != nil {
			fail(err)
			continue
		}
		row := Row{
			Line:        line,
			Date:        date,
			Payee:       collapse(get(cols.payee)),
			Purpose:     collapse(get(cols.purpose)),
			IBAN:        collapse(get(cols.iban)),
			BookingText: collapse(get(cols.booking)),
			Amount:      amount,
		}
		if row.Payee == "" {
			row.Payee = row.BookingText
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

func parseDate(s string, layouts []string) (time.Time, error) {
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised date %q", s)
}

func normHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(s, "\ufeff")))
	return strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(s)
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func blank(rec []string) bool {
	for _, f := range rec {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}
