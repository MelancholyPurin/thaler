package importer

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// A CAMT.053 file is the ISO 20022 account statement (BankToCustomerStatement) that
// European banks offer beside or instead of MT940: XML, a <Document> holding
// <BkToCstmrStmt>, one <Stmt> per statement and one <Ntry> per booking. The versions
// (camt.053.001.02 to .13) differ in their namespace and in a few wrappings that are
// read alike here: a status is <Sts>BOOK</Sts> or <Sts><Cd>BOOK</Cd></Sts>, a party
// is <Cdtr><Nm> or <Cdtr><Pty><Nm>. Elements are matched by their local name, so any
// namespace and any prefix read the same.
//
// An entry is one row, as the bank booked it, so the rows add up to the statement's
// balance; a batch booking (several <TxDtls> in one entry) stays one row.

// isCAMT says whether the text is a CAMT.053 statement.
func isCAMT(text string) bool {
	head := strings.TrimSpace(text)
	return strings.HasPrefix(head, "<") && strings.Contains(head, "BkToCstmrStmt")
}

// camtEntry is the part of an <Ntry> that makes a row.
type camtEntry struct {
	Amt struct {
		Value string `xml:",chardata"`
		Ccy   string `xml:"Ccy,attr"`
	} `xml:"Amt"`
	CdtDbtInd string `xml:"CdtDbtInd"`
	Sts       struct {
		Text string `xml:",chardata"`
		Cd   string `xml:"Cd"`
	} `xml:"Sts"`
	BookgDt      camtDate `xml:"BookgDt"`
	ValDt        camtDate `xml:"ValDt"`
	AddtlNtryInf string   `xml:"AddtlNtryInf"`
	BkTxCd       struct {
		Prtry struct {
			Cd string `xml:"Cd"`
		} `xml:"Prtry"`
	} `xml:"BkTxCd"`
	Txs []camtTx `xml:"NtryDtls>TxDtls"`
}

type camtDate struct {
	Dt   string `xml:"Dt"`
	DtTm string `xml:"DtTm"`
}

// camtTx is a transaction of an entry: who was on the other side, and what for.
type camtTx struct {
	Parties struct {
		Dbtr      camtParty   `xml:"Dbtr"`
		DbtrAcct  camtAccount `xml:"DbtrAcct"`
		UltmtDbtr camtParty   `xml:"UltmtDbtr"`
		Cdtr      camtParty   `xml:"Cdtr"`
		CdtrAcct  camtAccount `xml:"CdtrAcct"`
		UltmtCdtr camtParty   `xml:"UltmtCdtr"`
	} `xml:"RltdPties"`
	Ustrd      []string `xml:"RmtInf>Ustrd"`
	AddtlTxInf string   `xml:"AddtlTxInf"`
}

// camtParty is a debtor or creditor: its name directly (up to version 7) or in <Pty>.
type camtParty struct {
	Nm    string `xml:"Nm"`
	PtyNm string `xml:"Pty>Nm"`
}

func (p camtParty) name() string {
	if p.Nm != "" {
		return p.Nm
	}
	return p.PtyNm
}

// camtAccount is an account: by IBAN, or by another identifier.
type camtAccount struct {
	IBAN string `xml:"Id>IBAN"`
	Othr string `xml:"Id>Othr>Id"`
	Ccy  string `xml:"Ccy"`
}

func (a camtAccount) id() string {
	if a.IBAN != "" {
		return strings.ToUpper(strings.Join(strings.Fields(a.IBAN), ""))
	}
	return strings.TrimSpace(a.Othr)
}

// parseCAMT reads the booked entries of a CAMT.053 file. A pending or informational
// entry is counted as skipped, like a pending row of a CSV export. A file may hold
// several statements of one account (a month of daily ones); a file with the
// statements of several accounts is refused, since its rows go into one account.
func parseCAMT(text, encoding string, fallback ledger.Currency) (*ParseResult, error) {
	if !isCAMT(text) {
		return nil, coded.New("camt_not_camt", nil)
	}
	res := &ParseResult{Encoding: encoding, Format: FormatCAMT}
	dec := xml.NewDecoder(strings.NewReader(text))
	// The text is UTF-8 by now, whatever its declaration says.
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	accounts := map[string]bool{}
	cur := fallback // the currency of the statement's account
	var path []string
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line, _ := dec.InputPos()
			return nil, fmt.Errorf("%w: %w", coded.New("camt_unreadable", map[string]any{"Line": line}), err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		case xml.StartElement:
			parent := ""
			if len(path) > 0 {
				parent = path[len(path)-1]
			}
			line, _ := dec.InputPos()
			switch {
			case t.Name.Local == "Stmt":
				cur = fallback
			case t.Name.Local == "Acct" && parent == "Stmt":
				var a camtAccount
				if err := dec.DecodeElement(&a, &t); err != nil {
					return nil, fmt.Errorf("%w: %w", coded.New("camt_unreadable", map[string]any{"Line": line}), err)
				}
				if id := a.id(); id != "" {
					accounts[id] = true
				}
				if a.Ccy != "" {
					c, err := ledger.ParseCurrency(a.Ccy)
					if err != nil {
						return nil, err
					}
					cur = c
				}
				continue // DecodeElement read its end
			case t.Name.Local == "Ntry" && parent == "Stmt":
				var e camtEntry
				if err := dec.DecodeElement(&e, &t); err != nil {
					return nil, fmt.Errorf("%w: %w", coded.New("camt_unreadable", map[string]any{"Line": line}), err)
				}
				if !e.booked() {
					res.Skipped++
					continue
				}
				row, err := camtRow(line, e, cur)
				if err != nil {
					res.Errors = append(res.Errors, RowError{Line: line, Err: err})
				} else {
					res.Rows = append(res.Rows, row)
				}
				continue
			}
			path = append(path, t.Name.Local)
		}
	}
	if len(accounts) > 1 {
		return nil, coded.New("camt_several_accounts", map[string]any{"Count": len(accounts)})
	}
	return res, nil
}

// booked says whether the entry is booked: BOOK, or no status at all, which an older
// file may leave out. PDNG (pending), INFO and FUTR are not booked yet, or never are.
func (e camtEntry) booked() bool {
	sts := strings.TrimSpace(e.Sts.Cd)
	if sts == "" {
		sts = strings.TrimSpace(e.Sts.Text)
	}
	return sts == "" || strings.EqualFold(sts, "BOOK")
}

// camtRow reads one booked entry: the booking day (else the value day), the amount
// with the sign of CdtDbtInd (an amount in CAMT has none), in the currency of the
// amount, the other side (the creditor of a debit, the debtor of a credit, with its
// account), the unstructured remittance lines as the purpose, and the entry's
// additional information as the booking text.
func camtRow(line int, e camtEntry, cur ledger.Currency) (Row, error) {
	date, err := camtDay(e.BookgDt)
	if err != nil {
		if errors.Is(err, errCAMTNoDate) {
			date, err = camtDay(e.ValDt)
		}
		if err != nil {
			return Row{}, err
		}
	}
	if c := strings.TrimSpace(e.Amt.Ccy); c != "" {
		if cur, err = ledger.ParseCurrency(c); err != nil {
			return Row{}, err
		}
	}
	text := strings.TrimSpace(e.Amt.Value)
	if text == "" {
		return Row{}, coded.New("camt_no_amount", nil)
	}
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
		return Row{}, coded.New("camt_signed_amount", map[string]any{"Text": text})
	}
	amount, err := ledger.ParseAmount(text, cur, '.')
	if err != nil {
		return Row{}, err
	}
	var debit bool
	switch strings.TrimSpace(e.CdtDbtInd) {
	case "DBIT":
		debit = true
	case "CRDT":
	default:
		return Row{}, coded.New("camt_no_direction", map[string]any{"Text": e.CdtDbtInd})
	}
	if debit {
		amount = amount.Neg()
	}
	payee, iban := camtOtherSide(e.Txs, debit)
	var purpose []string
	for _, tx := range e.Txs {
		purpose = append(purpose, tx.Ustrd...)
		if len(tx.Ustrd) == 0 && tx.AddtlTxInf != "" {
			purpose = append(purpose, tx.AddtlTxInf)
		}
	}
	booking := e.AddtlNtryInf
	if strings.TrimSpace(booking) == "" {
		booking = e.BkTxCd.Prtry.Cd
	}
	row := Row{
		Line:        line,
		Date:        date,
		Payee:       collapse(payee),
		Purpose:     collapse(strings.Join(purpose, " ")),
		IBAN:        iban,
		BookingText: collapse(booking),
		Amount:      amount,
	}
	if row.Payee == "" {
		row.Payee = row.BookingText
	}
	return row, nil
}

// camtOtherSide is the name and account of the other side of an entry: the creditor
// of a debit, the debtor of a credit, or the ultimate one when the direct one has no
// name. A batch whose transactions name different parties has none.
func camtOtherSide(txs []camtTx, debit bool) (name, account string) {
	for i, tx := range txs {
		party, ultimate, acct := tx.Parties.Dbtr, tx.Parties.UltmtDbtr, tx.Parties.DbtrAcct
		if debit {
			party, ultimate, acct = tx.Parties.Cdtr, tx.Parties.UltmtCdtr, tx.Parties.CdtrAcct
		}
		n := strings.TrimSpace(party.name())
		if n == "" {
			n = strings.TrimSpace(ultimate.name())
		}
		if i == 0 {
			name, account = n, acct.id()
			continue
		}
		if n != name || acct.id() != account {
			return "", ""
		}
	}
	return name, account
}

var errCAMTNoDate = coded.New("camt_no_date", nil)

// camtDay reads the day of a CAMT date: <Dt> is YYYY-MM-DD, <DtTm> an ISO 8601 time
// whose day, as written, is the bank's.
func camtDay(d camtDate) (time.Time, error) {
	s := strings.TrimSpace(d.Dt)
	if s == "" {
		s = strings.TrimSpace(d.DtTm)
	}
	if s == "" {
		return time.Time{}, errCAMTNoDate
	}
	if len(s) < 10 {
		return time.Time{}, coded.New("unrecognised_date", map[string]any{"Text": s})
	}
	day, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return time.Time{}, coded.New("unrecognised_date", map[string]any{"Text": s})
	}
	return day, nil
}
