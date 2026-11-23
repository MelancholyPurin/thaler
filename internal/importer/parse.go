package importer

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
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
	// Encoding and Delimiter are how the file was read: the profile's, or what was
	// found when the profile says "auto". Encoding is one of the Encoding constants.
	Encoding  string
	Delimiter rune   // 0 for a file that is not CSV
	Format    string // FormatCSV, FormatOFX, FormatQIF or FormatCAMT
}

// Parse reads an export according to profile p: a CSV file, or an OFX, QIF or
// CAMT.053 file. fallback is the currency used when the file does not say one: a CSV
// file with no currency column or an empty cell, an OFX or CAMT statement without
// one, any QIF file.
func Parse(r io.Reader, p Profile, fallback ledger.Currency) (*ParseResult, error) {
	cp, err := p.compile()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if cp.format != FormatCSV {
		text, encoding, err := decodeToUTF8(raw, cp.encoding)
		if err != nil {
			return nil, err
		}
		switch cp.format {
		case FormatOFX:
			return parseOFX(text, encoding, fallback)
		case FormatCAMT:
			return parseCAMT(text, encoding, fallback)
		}
		return parseQIF(text, encoding, cp, fallback)
	}
	tab, err := openTable(raw, p, cp)
	if err != nil {
		return nil, err
	}
	cr, header := tab.cr, tab.header
	index := headerIndex(header)
	find := func(names []string) int { return findColumn(index, names) }
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
		return nil, coded.New("profile_mismatch", map[string]any{
			"Name": p.Name, "Date": strings.Join(p.Columns.Date, " / "), "Amount": strings.Join(p.Columns.Amount, " / "), "Header": strings.Join(header, "; "),
		})
	}
	skipNeedle := strings.ToLower(strings.TrimSpace(p.SkipContains))

	res := &ParseResult{Encoding: tab.encoding, Delimiter: tab.delim, Format: FormatCSV}
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

// table is a decoded export ready to be read, its header read already.
type table struct {
	cr       *csv.Reader
	header   []string
	encoding string
	delim    rune
}

// openTable decodes raw and reads its header: the first line that is not blank.
// With an "auto" delimiter it tries each of Delimiters and keeps the one under which
// the header has the profile's date and amount columns; when none does, the one that
// splits the header into the most columns, so that an error can show it.
func openTable(raw []byte, p Profile, cp compiled) (table, error) {
	text, encoding, err := decodeToUTF8(raw, cp.encoding)
	if err != nil {
		return table{}, err
	}
	if cp.delim != 0 {
		return readHeader(text, encoding, cp.delim)
	}
	var (
		best               table
		bestFits, bestCols = false, 0
		firstErr           error
	)
	for _, d := range Delimiters {
		tab, err := readHeader(text, encoding, d)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		index := headerIndex(tab.header)
		fits := findColumn(index, p.Columns.Date) >= 0 && findColumn(index, p.Columns.Amount) >= 0
		if (fits && !bestFits) || (fits == bestFits && len(tab.header) > bestCols) {
			best, bestFits, bestCols = tab, fits, len(tab.header)
		}
	}
	if best.cr == nil {
		return table{}, firstErr
	}
	return best, nil
}

// readHeader starts reading text with the delimiter d, up to and including the header.
func readHeader(text, encoding string, d rune) (table, error) {
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = d
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return table{}, coded.New("file_empty", nil)
		}
		if err != nil {
			return table{}, fmt.Errorf("%w: %w", coded.New("reading_header", nil), err)
		}
		if !blank(rec) {
			return table{cr: cr, header: rec, encoding: encoding, delim: d}, nil
		}
	}
}

func parseDate(s string, layouts []string) (time.Time, error) {
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, coded.New("unrecognised_date", map[string]any{"Text": s})
}

// headerIndex maps each normalised header name to its first column.
func headerIndex(header []string) map[string]int {
	index := make(map[string]int, len(header))
	for i, h := range header {
		key := normHeader(h)
		if _, dup := index[key]; !dup {
			index[key] = i
		}
	}
	return index
}

// findColumn is the first of names that the header has, or -1.
func findColumn(index map[string]int, names []string) int {
	for _, n := range names {
		if i, ok := index[normHeader(n)]; ok {
			return i
		}
	}
	return -1
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
