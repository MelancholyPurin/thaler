package importer

import (
	"html"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// An OFX file is a header (key:value lines in version 1, an XML declaration and
// processing instruction in version 2) and then tags under <OFX>. Version 1 is SGML:
// an aggregate such as <STMTTRN> is closed (</STMTTRN>), a value such as <TRNAMT>-23.47
// is not. Version 2 is XML and closes both. Reading the tags in order and taking the text
// after a tag as its value reads both alike.

// isOFX says whether the text is an OFX file.
func isOFX(text string) bool {
	head := strings.ToUpper(strings.TrimSpace(text))
	return strings.HasPrefix(head, "OFXHEADER:") || strings.Contains(head, "<OFX>")
}

// ofxTag is one tag of the file: its name in upper case, whether it closes, the text
// that follows it up to the next tag, and the line it is on.
type ofxTag struct {
	name    string
	closing bool
	text    string
	line    int
}

// ofxTags lists the tags of the file from <OFX> on, skipping the header.
func ofxTags(text string) []ofxTag {
	start := strings.Index(strings.ToUpper(text), "<OFX>")
	if start < 0 {
		return nil
	}
	line := 1 + strings.Count(text[:start], "\n")
	var tags []ofxTag
	rest := text[start:]
	for {
		open := strings.IndexByte(rest, '<')
		if open < 0 {
			return tags
		}
		line += strings.Count(rest[:open], "\n")
		end := strings.IndexByte(rest[open:], '>')
		if end < 0 {
			return tags
		}
		name := strings.TrimSpace(rest[open+1 : open+end])
		rest = rest[open+end+1:]
		next := strings.IndexByte(rest, '<')
		if next < 0 {
			next = len(rest)
		}
		t := ofxTag{name: strings.ToUpper(name), text: strings.TrimSpace(html.UnescapeString(rest[:next])), line: line}
		if strings.HasPrefix(t.name, "/") {
			t.name, t.closing = t.name[1:], true
		}
		tags = append(tags, t)
	}
}

// ofxStatements are the aggregates of a statement of an account: a bank account's, or
// a credit card's.
var ofxStatements = map[string]bool{"STMTRS": true, "CCSTMTRS": true}

// parseOFX reads the transactions of an OFX statement. A file with the statements of
// several accounts is refused, since its rows go into one account.
func parseOFX(text, encoding string, fallback ledger.Currency) (*ParseResult, error) {
	if !isOFX(text) {
		return nil, coded.New("ofx_not_ofx", nil)
	}
	res := &ParseResult{Encoding: encoding, Format: FormatOFX}
	tags := ofxTags(text)
	statements := 0
	for _, t := range tags {
		if !t.closing && ofxStatements[t.name] {
			statements++
		}
	}
	if statements > 1 {
		return nil, coded.New("ofx_several_statements", map[string]any{"Count": statements})
	}
	cur := fallback
	for i := 0; i < len(tags); i++ {
		t := tags[i]
		switch {
		case t.closing:
		case t.name == "CURDEF" && t.text != "":
			c, err := ledger.ParseCurrency(t.text)
			if err != nil {
				return nil, err
			}
			cur = c
		case t.name == "STMTTRN":
			end := ofxEnd(tags, i)
			row, err := ofxRow(t.line, tags[i+1:end], cur)
			if err != nil {
				res.Errors = append(res.Errors, RowError{Line: t.line, Err: err})
			} else {
				res.Rows = append(res.Rows, row)
			}
			i = end - 1
		}
	}
	return res, nil
}

// ofxEnd is where the transaction that opens at tags[i] ends: at its </STMTTRN>, or
// where the next one or the end of the list begins, should a file leave it open.
func ofxEnd(tags []ofxTag, i int) int {
	for end := i + 1; end < len(tags); end++ {
		t := tags[end]
		switch {
		case t.name == "STMTTRN" && t.closing:
			return end + 1
		case t.name == "STMTTRN", t.name == "BANKTRANLIST" && t.closing:
			return end
		}
	}
	return len(tags)
}

// ofxAggregates are the tags within a transaction that hold other tags rather than a
// value. An empty value (<MEMO> with nothing after it, in version 1) is not one.
var ofxAggregates = map[string]bool{"PAYEE": true, "BANKACCTTO": true, "CCACCTTO": true, "CURRENCY": true, "ORIGCURRENCY": true, "IMAGEDATA": true}

// ofxRow reads one <STMTTRN>: the booking date (DTPOSTED), the amount (TRNAMT, in the
// currency of the statement unless the transaction names its own in <CURRENCY>), the
// name of the other side (NAME, or the NAME in <PAYEE>), the memo and the type, which
// stands in for a booking text.
func ofxRow(line int, tags []ofxTag, cur ledger.Currency) (Row, error) {
	fields := map[string]string{}
	var in []string // the aggregates the tag is in, within the transaction
	for _, t := range tags {
		if t.closing {
			if n := len(in); n > 0 && in[n-1] == t.name {
				in = in[:n-1]
			}
			continue
		}
		if ofxAggregates[t.name] { // what follows is inside it
			in = append(in, t.name)
			continue
		}
		if t.text == "" {
			continue
		}
		key := strings.Join(append(append([]string(nil), in...), t.name), ".")
		if _, seen := fields[key]; !seen {
			fields[key] = t.text
		}
	}
	date, err := ofxDate(fields["DTPOSTED"])
	if err != nil {
		return Row{}, err
	}
	if sym := fields["CURRENCY.CURSYM"]; sym != "" {
		if cur, err = ledger.ParseCurrency(sym); err != nil {
			return Row{}, err
		}
	}
	text := fields["TRNAMT"]
	if text == "" {
		return Row{}, coded.New("ofx_no_amount", nil)
	}
	// OFX writes a point; a few banks write a comma. Neither uses thousands separators.
	sep := byte('.')
	if !strings.Contains(text, ".") && strings.Contains(text, ",") {
		sep = ','
	}
	amount, err := ledger.ParseAmount(text, cur, sep)
	if err != nil {
		return Row{}, err
	}
	payee := fields["NAME"]
	if payee == "" {
		payee = fields["PAYEE.NAME"]
	}
	row := Row{
		Line:        line,
		Date:        date,
		Payee:       collapse(payee),
		Purpose:     collapse(fields["MEMO"]),
		BookingText: collapse(fields["TRNTYPE"]),
		Amount:      amount,
	}
	if row.Payee == "" {
		row.Payee = row.BookingText
	}
	return row, nil
}

// ofxDate reads the day of an OFX date: YYYYMMDD, and maybe a time and a time zone
// after it (20261004120000.000[+1:CET]), which are left aside: the day is the bank's.
func ofxDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, coded.New("ofx_no_date", nil)
	}
	if len(s) < 8 {
		return time.Time{}, coded.New("unrecognised_date", map[string]any{"Text": s})
	}
	d, err := time.Parse("20060102", s[:8])
	if err != nil {
		return time.Time{}, coded.New("unrecognised_date", map[string]any{"Text": s})
	}
	return d, nil
}
