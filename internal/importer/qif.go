package importer

import (
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// A QIF file is lines that each start with a letter saying what they hold (D the date,
// T the amount, P the payee, M the memo), records ending with a line "^", under a header
// such as "!Type:Bank" that says what the records are.

// isQIF says whether the text is a QIF file: its first line that is not blank is a
// header.
func isQIF(text string) bool {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		head := strings.ToLower(line)
		return strings.HasPrefix(head, "!type:") || strings.HasPrefix(head, "!account") || strings.HasPrefix(head, "!option:")
	}
	return false
}

// qifAccountTypes are the types of record that are transactions of an account like the
// ones Thaler keeps. Lists of categories, classes or memorised transactions are not
// transactions and are passed over; an investment account's are refused, since their
// amounts are not what moved on the account.
var qifAccountTypes = map[string]bool{"bank": true, "cash": true, "ccard": true, "oth a": true, "oth l": true}

// qifRecord is a record of the file as written, before its date is read.
type qifRecord struct {
	line    int
	date    string
	amount  string
	payee   string
	memo    string
	number  string
	present bool // it has a field at all
}

// parseQIF reads the transactions of a QIF file of an account. The date order is the
// first of the profile's formats that reads every date of the file, so that 03/04/26 is
// read the same way in every row; when none does, each date is read with the first that
// fits it.
func parseQIF(text, encoding string, cp compiled, fallback ledger.Currency) (*ParseResult, error) {
	if !isQIF(text) {
		return nil, coded.New("qif_not_qif", nil)
	}
	records, err := qifRecords(text)
	if err != nil {
		return nil, err
	}
	layouts := qifLayouts(records, cp.layouts)
	res := &ParseResult{Encoding: encoding, Format: FormatQIF}
	for _, r := range records {
		row, err := qifRow(r, layouts, cp.sep, fallback)
		if err != nil {
			res.Errors = append(res.Errors, RowError{Line: r.line, Err: err})
			continue
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

// qifRecords lists the records of the account sections of the file.
func qifRecords(text string) ([]qifRecord, error) {
	var out []qifRecord
	section := ""
	var r qifRecord
	n := 0
	for line := range strings.Lines(text) {
		n++
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] == '!' {
			head := strings.ToLower(strings.TrimSpace(line[1:]))
			switch {
			case strings.HasPrefix(head, "type:"):
				section = strings.TrimSpace(strings.TrimPrefix(head, "type:"))
				if section == "invst" {
					return nil, coded.New("qif_investment", nil)
				}
			case head == "account":
				section = "account" // the account's own record follows, not a transaction
			}
			r = qifRecord{}
			continue
		}
		if line[0] == '^' {
			if r.present && qifAccountTypes[section] {
				out = append(out, r)
			}
			r = qifRecord{}
			continue
		}
		if !r.present {
			r.line, r.present = n, true
		}
		value := strings.TrimSpace(line[1:])
		switch line[0] {
		case 'D':
			r.date = value
		case 'T':
			r.amount = value
		case 'U': // the same amount, which some programs write as well as T or instead of it
			if r.amount == "" {
				r.amount = value
			}
		case 'P':
			r.payee = value
		case 'M':
			r.memo = value
		case 'N':
			r.number = value
		}
	}
	if r.present && qifAccountTypes[section] { // a last record without its "^"
		out = append(out, r)
	}
	return out, nil
}

// qifLayouts is the date format of the file: the first of layouts that reads every date
// in it, alone; or all of them, in order, when none does.
func qifLayouts(records []qifRecord, layouts []string) []string {
	for _, l := range layouts {
		all := true
		for _, r := range records {
			if _, err := time.Parse(l, qifDate(r.date)); err != nil {
				all = false
				break
			}
		}
		if all && len(records) > 0 {
			return []string{l}
		}
	}
	return layouts
}

// qifDate writes a QIF date so that a layout reads it: Quicken's 1/ 5'26 is 01/05/26,
// each part of the day and the month two digits.
func qifDate(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "'", "/")
	var b strings.Builder
	part := 0
	for i := range len(s) {
		c := s[i]
		if c >= '0' && c <= '9' {
			part++
			continue
		}
		if part == 1 {
			b.WriteByte('0')
		}
		b.WriteString(s[i-part : i+1])
		part = 0
	}
	if part == 1 {
		b.WriteByte('0')
	}
	b.WriteString(s[len(s)-part:])
	return b.String()
}

// qifRow reads one record: its date, its amount in the currency of the account, the
// payee and the memo. The number of a cheque or a booking (N) stands in for a booking
// text.
func qifRow(r qifRecord, layouts []string, sep byte, cur ledger.Currency) (Row, error) {
	if r.date == "" {
		return Row{}, coded.New("qif_no_date", nil)
	}
	date, err := parseDate(qifDate(r.date), layouts)
	if err != nil {
		return Row{}, coded.New("unrecognised_date", map[string]any{"Text": r.date})
	}
	if r.amount == "" {
		return Row{}, coded.New("qif_no_amount", nil)
	}
	amount, err := ledger.ParseAmount(r.amount, cur, sep)
	if err != nil {
		return Row{}, err
	}
	row := Row{
		Line:        r.line,
		Date:        date,
		Payee:       collapse(r.payee),
		Purpose:     collapse(r.memo),
		BookingText: collapse(r.number),
		Amount:      amount,
	}
	if row.Payee == "" {
		row.Payee = row.BookingText
	}
	return row, nil
}
