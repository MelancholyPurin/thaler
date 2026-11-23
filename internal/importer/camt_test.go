package importer

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// camtV2 is a statement in CAMT.053 version 2, the one most German banks send, made up
// for the tests: a card payment of two remittance lines, a salary, a pending entry and
// a fee with no details. Its <Ntry> are on lines 13, 34, 54 and 60.
const camtV2 = `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:camt.053.001.02">
  <BkToCstmrStmt>
    <GrpHdr><MsgId>1</MsgId></GrpHdr>
    <Stmt>
      <Id>1</Id>
      <Acct>
        <Id><IBAN>DE02 1203 0000 0000 2020 51</IBAN></Id>
        <Ccy>EUR</Ccy>
      </Acct>
      <Bal><Amt Ccy="EUR">100.00</Amt><CdtDbtInd>CRDT</CdtDbtInd></Bal>
      <!-- line 12: the entries follow -->
      <Ntry>
        <Amt Ccy="EUR">23.47</Amt>
        <CdtDbtInd>DBIT</CdtDbtInd>
        <Sts>BOOK</Sts>
        <BookgDt><Dt>2026-10-04</Dt></BookgDt>
        <ValDt><Dt>2026-10-05</Dt></ValDt>
        <BkTxCd><Prtry><Cd>NMSC+005</Cd></Prtry></BkTxCd>
        <NtryDtls>
          <TxDtls>
            <RltdPties>
              <Cdtr><Nm>REWE Markt</Nm></Cdtr>
              <CdtrAcct><Id><IBAN>DE89370400440532013000</IBAN></Id></CdtrAcct>
            </RltdPties>
            <RmtInf>
              <Ustrd>Einkauf &amp; Pfand</Ustrd>
              <Ustrd>Karte 1234</Ustrd>
            </RmtInf>
          </TxDtls>
        </NtryDtls>
        <AddtlNtryInf>KARTENZAHLUNG</AddtlNtryInf>
      </Ntry>
      <Ntry>
        <Amt Ccy="EUR">2450.00</Amt>
        <CdtDbtInd>CRDT</CdtDbtInd>
        <Sts>BOOK</Sts>
        <BookgDt><Dt>2026-10-01</Dt></BookgDt>
        <ValDt><Dt>2026-10-01</Dt></ValDt>
        <NtryDtls>
          <TxDtls>
            <RltdPties>
              <Dbtr><Nm>Muster GmbH</Nm></Dbtr>
              <DbtrAcct><Id><IBAN>DE44500105175407324931</IBAN></Id></DbtrAcct>
              <Cdtr><Nm>Erika Mustermann</Nm></Cdtr>
            </RltdPties>
            <RmtInf>
              <Ustrd>Gehalt Oktober</Ustrd>
            </RmtInf>
          </TxDtls>
        </NtryDtls>
        <AddtlNtryInf>LOHN GEHALT</AddtlNtryInf>
      </Ntry>
      <Ntry>
        <Amt Ccy="EUR">9.99</Amt>
        <CdtDbtInd>DBIT</CdtDbtInd>
        <Sts>PDNG</Sts>
        <ValDt><Dt>2026-10-08</Dt></ValDt>
      </Ntry>
      <Ntry>
        <Amt Ccy="EUR">4.90</Amt>
        <CdtDbtInd>DBIT</CdtDbtInd>
        <Sts>BOOK</Sts>
        <BookgDt><Dt>2026-10-07</Dt></BookgDt>
        <AddtlNtryInf>ENTGELT</AddtlNtryInf>
      </Ntry>
    </Stmt>
  </BkToCstmrStmt>
</Document>
`

// camtV8 is a card payment in a later version, which wraps the status in <Cd> and the
// name in <Pty>, gives the booking day as a time and writes a prefix on every element.
// The amount is in dollars, in an account without a currency. Its <Ntry> is on line 6.
const camtV8 = `<?xml version="1.0" encoding="UTF-8"?>
<c:Document xmlns:c="urn:iso:std:iso:20022:tech:xsd:camt.053.001.08">
<c:BkToCstmrStmt>
<c:Stmt>
<c:Acct><c:Id><c:Othr><c:Id>123456</c:Id></c:Othr></c:Id></c:Acct>
<c:Ntry>
<c:Amt Ccy="USD">19.99</c:Amt><c:CdtDbtInd>DBIT</c:CdtDbtInd>
<c:Sts><c:Cd>BOOK</c:Cd></c:Sts>
<c:BookgDt><c:DtTm>2026-10-05T23:30:00-05:00</c:DtTm></c:BookgDt>
<c:NtryDtls><c:TxDtls><c:RltdPties>
<c:Cdtr><c:Pty><c:Nm>Shop</c:Nm></c:Pty></c:Cdtr>
</c:RltdPties><c:AddtlTxInf>Order 42</c:AddtlTxInf></c:TxDtls></c:NtryDtls>
</c:Ntry>
</c:Stmt>
</c:BkToCstmrStmt>
</c:Document>
`

// camtEntries is a statement of the given entries, in an account in euros. The first
// entry is on line 6, and every entry here is written on one line.
func camtEntries(entries ...string) string {
	return `<?xml version="1.0"?>
<Document>
<BkToCstmrStmt>
<Stmt>
<Acct><Id><IBAN>DE02120300000000202051</IBAN></Id><Ccy>EUR</Ccy></Acct>
` + strings.Join(entries, "\n") + `
</Stmt>
</BkToCstmrStmt>
</Document>
`
}

func Test_Parse_camt(t *testing.T) {
	usd := func(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "USD"} }
	camt := func(r ...Row) ParseResult { return ParseResult{Encoding: EncodingUTF8, Format: FormatCAMT, Rows: r} }
	tcs := []struct {
		name     string
		input    string
		fallback ledger.Currency
		want     ParseResult
		wantErr  string
	}{
		{
			name: "version 2: the booking day, the other side and its IBAN, the remittance lines, the booking text; a pending entry is skipped", input: camtV2, fallback: "USD",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatCAMT, Skipped: 1, Rows: []Row{
				{Line: 13, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf & Pfand Karte 1234", IBAN: "DE89370400440532013000", BookingText: "KARTENZAHLUNG", Amount: eur(-2347)},
				{Line: 34, Date: day("2026-10-01"), Payee: "Muster GmbH", Purpose: "Gehalt Oktober", IBAN: "DE44500105175407324931", BookingText: "LOHN GEHALT", Amount: eur(245000)},
				{Line: 60, Date: day("2026-10-07"), Payee: "ENTGELT", BookingText: "ENTGELT", Amount: eur(-490)},
			}},
		},
		{
			name: "a later version, with prefixes, a time for a day and the amount's own currency", input: camtV8, fallback: "EUR",
			want: camt(Row{Line: 6, Date: day("2026-10-05"), Payee: "Shop", Purpose: "Order 42", Amount: usd(-1999)}),
		},
		{
			name:  "an amount without a currency is in the account's, and an account without one in the fallback",
			input: strings.Replace(strings.Replace(camtV2, `<Amt Ccy="EUR">23.47</Amt>`, `<Amt>23.47</Amt>`, 1), "<Ccy>EUR</Ccy>", "<Ccy>CHF</Ccy>", 1) + "\n", fallback: "USD",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatCAMT, Skipped: 1, Rows: []Row{
				{Line: 13, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf & Pfand Karte 1234", IBAN: "DE89370400440532013000", BookingText: "KARTENZAHLUNG", Amount: ledger.Money{Minor: -2347, Currency: "CHF"}},
				{Line: 34, Date: day("2026-10-01"), Payee: "Muster GmbH", Purpose: "Gehalt Oktober", IBAN: "DE44500105175407324931", BookingText: "LOHN GEHALT", Amount: eur(245000)},
				{Line: 60, Date: day("2026-10-07"), Payee: "ENTGELT", BookingText: "ENTGELT", Amount: eur(-490)},
			}},
		},
		{
			name: "no booking day takes the value day; no status is booked; no additional information takes the bank's code", fallback: "EUR",
			input: camtEntries(`<Ntry><Amt Ccy="EUR">1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><ValDt><Dt>2026-10-09</Dt></ValDt><BkTxCd><Prtry><Cd>NMSC+201</Cd></Prtry></BkTxCd></Ntry>`),
			want:  camt(Row{Line: 6, Date: day("2026-10-09"), Payee: "NMSC+201", BookingText: "NMSC+201", Amount: eur(-100)}),
		},
		{
			name: "a credit without a debtor's name takes the ultimate debtor's", fallback: "EUR",
			input: camtEntries(`<Ntry><Amt Ccy="EUR">5.00</Amt><CdtDbtInd>CRDT</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt><NtryDtls><TxDtls><RltdPties><UltmtDbtr><Nm>Oma</Nm></UltmtDbtr><Cdtr><Nm>Me</Nm></Cdtr></RltdPties></TxDtls></NtryDtls></Ntry>`),
			want:  camt(Row{Line: 6, Date: day("2026-10-09"), Payee: "Oma", Amount: eur(500)}),
		},
		{
			name: "a batch stays one row; its parties differ, so it has none, and its lines are joined", fallback: "EUR",
			input: camtEntries(`<Ntry><Amt Ccy="EUR">30.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt>` +
				`<NtryDtls><TxDtls><RltdPties><Cdtr><Nm>A</Nm></Cdtr></RltdPties><RmtInf><Ustrd>one</Ustrd></RmtInf></TxDtls>` +
				`<TxDtls><RltdPties><Cdtr><Nm>B</Nm></Cdtr></RltdPties><RmtInf><Ustrd>two</Ustrd></RmtInf></TxDtls></NtryDtls>` +
				`<AddtlNtryInf>SAMMLER</AddtlNtryInf></Ntry>`),
			want: camt(Row{Line: 6, Date: day("2026-10-09"), Payee: "SAMMLER", Purpose: "one two", BookingText: "SAMMLER", Amount: eur(-3000)}),
		},
		{
			name: "informational and future entries are skipped like pending ones", fallback: "EUR",
			input: camtEntries(
				`<Ntry><Amt Ccy="EUR">1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>INFO</Sts></Ntry>`,
				`<Ntry><Amt Ccy="EUR">1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts><Cd>FUTR</Cd></Sts></Ntry>`,
			),
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatCAMT, Skipped: 2},
		},
		{
			name: "entries that cannot be read are reported, and the others are read", fallback: "EUR",
			input: camtEntries(
				`<Ntry><Amt Ccy="EUR">1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd></Ntry>`,
				`<Ntry><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt></Ntry>`,
				`<Ntry><Amt Ccy="EUR">-1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt></Ntry>`,
				`<Ntry><Amt Ccy="EUR">1.00</Amt><CdtDbtInd>BOTH</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt></Ntry>`,
				`<Ntry><Amt Ccy="EUR">1,00</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt></Ntry>`,
				`<Ntry><Amt Ccy="EUR">1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>09.10.2026</Dt></BookgDt></Ntry>`,
				`<Ntry><Amt Ccy="EURO">1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt></Ntry>`,
				`<Ntry><Amt Ccy="EUR">2.00</Amt><CdtDbtInd>CRDT</CdtDbtInd><BookgDt><Dt>2026-10-10</Dt></BookgDt></Ntry>`,
			),
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatCAMT,
				Rows: []Row{{Line: 13, Date: day("2026-10-10"), Amount: eur(200)}},
				Errors: []RowError{
					{Line: 6, Err: errors.New("the entry has no booking date (BookgDt) and no value date (ValDt)")},
					{Line: 7, Err: errors.New("the entry has no amount (Amt)")},
					{Line: 8, Err: errors.New(`the amount "-1.00" has a sign, but CAMT gives the direction in CdtDbtInd`)},
					{Line: 9, Err: errors.New(`the entry is neither a credit nor a debit (CdtDbtInd "BOTH")`)},
					{Line: 10, Err: errors.New(`invalid amount "1,00": inconsistent thousands separators`)},
					{Line: 11, Err: errors.New(`unrecognised date "09.10.2026"`)},
					{Line: 12, Err: errors.New(`invalid currency code "EURO": want three letters such as EUR, USD or MXN`)},
				},
			},
		},
		{
			name: "several statements of one account, each in its own currency", fallback: "EUR",
			input: strings.Replace(camtEntries(`<Ntry><Amt>1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt></Ntry>`),
				"</Stmt>", "</Stmt><Stmt><Acct><Id><IBAN>de02 1203 0000 0000 2020 51</IBAN></Id></Acct>\n<Ntry><Amt>2.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-10-10</Dt></BookgDt></Ntry></Stmt>", 1),
			want: camt(Row{Line: 6, Date: day("2026-10-09"), Amount: eur(-100)}, Row{Line: 8, Date: day("2026-10-10"), Amount: eur(-200)}),
		},
		{
			name: "the statements of two accounts", fallback: "EUR",
			input:   strings.Replace(camtEntries(), "</Stmt>", "</Stmt><Stmt><Acct><Id><IBAN>DE89370400440532013000</IBAN></Id></Acct></Stmt>", 1),
			wantErr: "the file has the statements of 2 accounts; import one account's at a time",
		},
		{
			name: "a declared Latin-1 file is read as the bytes are", fallback: "EUR",
			input: cp1252(t, strings.Replace(camtEntries(`<Ntry><Amt Ccy="EUR">1.00</Amt><CdtDbtInd>DBIT</CdtDbtInd><BookgDt><Dt>2026-10-09</Dt></BookgDt><NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Bäckerei Müller</Nm></Cdtr></RltdPties></TxDtls></NtryDtls></Ntry>`),
				`<?xml version="1.0"?>`, `<?xml version="1.0" encoding="ISO-8859-1"?>`, 1)),
			want: ParseResult{Encoding: EncodingWindows1252, Format: FormatCAMT, Rows: []Row{{Line: 6, Date: day("2026-10-09"), Payee: "Bäckerei Müller", Amount: eur(-100)}}},
		},
		{name: "a statement with no entries", input: camtEntries(), fallback: "EUR", want: camt()},
		{
			name: "XML that breaks off", input: strings.TrimSuffix(camtV2, "</Document>\n") + "<Ntry", fallback: "EUR",
			wantErr: "the XML cannot be read near line 69: XML syntax error on line 69: unexpected EOF",
		},
		{name: "an account report, camt.052, is not a statement", input: `<Document><BkToCstmrAcctRpt><Rpt></Rpt></BkToCstmrAcctRpt></Document>`, fallback: "EUR", wantErr: "this is not a CAMT.053 statement: it has no <BkToCstmrStmt>"},
		{name: "an OFX statement", input: ofxV2, fallback: "EUR", wantErr: "this is not a CAMT.053 statement: it has no <BkToCstmrStmt>"},
		{name: "a CSV file", input: "Date;Amount\n2026-10-04;-1\n", fallback: "EUR", wantErr: "this is not a CAMT.053 statement: it has no <BkToCstmrStmt>"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.input

			// when
			got, err := Parse(strings.NewReader(tc.input), CAMT, tc.fallback)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, *got, rowErrors); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_camtDay(t *testing.T) {
	tcs := []struct {
		name    string
		in      camtDate
		want    string
		wantErr string
	}{
		{name: "a day", in: camtDate{Dt: "2026-10-04"}, want: "2026-10-04"},
		{name: "a time keeps the day written, whatever its zone", in: camtDate{DtTm: "2026-10-04T23:30:00-05:00"}, want: "2026-10-04"},
		{name: "the day wins over the time", in: camtDate{Dt: "2026-10-04", DtTm: "2026-10-05T00:00:00"}, want: "2026-10-04"},
		{name: "space around it", in: camtDate{Dt: " 2026-10-04\n"}, want: "2026-10-04"},
		{name: "nothing", in: camtDate{}, wantErr: "the entry has no booking date (BookgDt) and no value date (ValDt)"},
		{name: "too short", in: camtDate{Dt: "2026-10"}, wantErr: `unrecognised date "2026-10"`},
		{name: "not a day", in: camtDate{Dt: "2026-13-01"}, wantErr: `unrecognised date "2026-13-01"`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			got, err := camtDay(tc.in)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, got.Format("2006-01-02")); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
