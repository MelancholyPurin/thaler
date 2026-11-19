package importer

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// ofxV1 is a statement in OFX version 1 (SGML: values are not closed), made up for the
// tests: a card payment, a salary with an empty memo and its payee in an aggregate, and a
// fee with no name.
const ofxV1 = `OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:USASCII
CHARSET:1252
COMPRESSION:NONE
OLDFILEUID:NONE
NEWFILEUID:NONE

<OFX>
<SIGNONMSGSRSV1><SONRS><STATUS><CODE>0<SEVERITY>INFO</STATUS><DTSERVER>20261031<LANGUAGE>ENG</SONRS></SIGNONMSGSRSV1>
<BANKMSGSRSV1>
<STMTTRNRS>
<TRNUID>1
<STATUS><CODE>0<SEVERITY>INFO</STATUS>
<STMTRS>
<CURDEF>EUR
<BANKACCTFROM><BANKID>12345678<ACCTID>0000000001<ACCTTYPE>CHECKING</BANKACCTFROM>
<BANKTRANLIST>
<DTSTART>20261001<DTEND>20261031
<STMTTRN>
<TRNTYPE>POS
<DTPOSTED>20261004120000.000[+1:CET]
<TRNAMT>-23.47
<FITID>A1
<NAME>REWE Markt
<MEMO>Einkauf &amp; Pfand
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT
<DTPOSTED>20261001
<TRNAMT>2450.00
<FITID>A2
<MEMO>
<PAYEE><NAME>Muster GmbH<ADDR1>Hauptstr. 1<CITY>Hamburg</PAYEE>
</STMTTRN>
<STMTTRN>
<TRNTYPE>FEE
<DTPOSTED>20261007
<TRNAMT>-4,90
<FITID>A3
</STMTTRN>
</BANKTRANLIST>
<LEDGERBAL><BALAMT>2421.63<DTASOF>20261031</LEDGERBAL>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>
`

// ofxV2 is the same card payment in OFX version 2 (XML), with a transaction in dollars.
const ofxV2 = `<?xml version="1.0" encoding="UTF-8"?>
<?OFX OFXHEADER="200" VERSION="220" SECURITY="NONE" OLDFILEUID="NONE" NEWFILEUID="NONE"?>
<OFX>
  <CREDITCARDMSGSRSV1>
    <CCSTMTTRNRS>
      <CCSTMTRS>
        <CURDEF>EUR</CURDEF>
        <BANKTRANLIST>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20261004</DTPOSTED>
            <TRNAMT>-23.47</TRNAMT>
            <FITID>B1</FITID>
            <NAME>REWE Markt</NAME>
            <MEMO>Einkauf</MEMO>
          </STMTTRN>
          <STMTTRN>
            <TRNTYPE>DEBIT</TRNTYPE>
            <DTPOSTED>20261005</DTPOSTED>
            <TRNAMT>-19.99</TRNAMT>
            <FITID>B2</FITID>
            <NAME>Shop</NAME>
            <CURRENCY><CURSYM>USD</CURSYM></CURRENCY>
          </STMTTRN>
        </BANKTRANLIST>
      </CCSTMTRS>
    </CCSTMTTRNRS>
  </CREDITCARDMSGSRSV1>
</OFX>
`

func Test_Parse_ofx(t *testing.T) {
	usd := func(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "USD"} }
	tcs := []struct {
		name     string
		input    string
		fallback ledger.Currency
		want     ParseResult
		wantErr  string
	}{
		{
			name: "version 1: the booking day, the name, the memo and the type; a comma for a point", input: ofxV1, fallback: "USD",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatOFX, Rows: []Row{
				{Line: 22, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf & Pfand", BookingText: "POS", Amount: eur(-2347)},
				{Line: 30, Date: day("2026-10-01"), Payee: "Muster GmbH", BookingText: "CREDIT", Amount: eur(245000)},
				{Line: 38, Date: day("2026-10-07"), Payee: "FEE", BookingText: "FEE", Amount: eur(-490)},
			}},
		},
		{
			name: "version 2, and a transaction in a currency of its own", input: ofxV2, fallback: "USD",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatOFX, Rows: []Row{
				{Line: 9, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf", BookingText: "DEBIT", Amount: eur(-2347)},
				{Line: 17, Date: day("2026-10-05"), Payee: "Shop", BookingText: "DEBIT", Amount: usd(-1999)},
			}},
		},
		{
			name:  "a statement without a currency takes the account's",
			input: strings.Replace(ofxV2, "<CURDEF>EUR</CURDEF>", "", 1), fallback: "USD",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatOFX, Rows: []Row{
				{Line: 9, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf", BookingText: "DEBIT", Amount: usd(-2347)},
				{Line: 17, Date: day("2026-10-05"), Payee: "Shop", BookingText: "DEBIT", Amount: usd(-1999)},
			}},
		},
		{
			name:  "a transaction that cannot be read is reported, and the others are read",
			input: strings.Replace(strings.Replace(ofxV1, "<DTPOSTED>20261001\n", "", 1), "<TRNAMT>-4,90\n", "<TRNAMT>abc\n", 1), fallback: "EUR",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatOFX,
				Rows: []Row{{Line: 22, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf & Pfand", BookingText: "POS", Amount: eur(-2347)}},
				Errors: []RowError{
					{Line: 30, Err: errors.New("the transaction has no booking date (DTPOSTED)")},
					{Line: 37, Err: errors.New(`invalid amount "abc"`)},
				},
			},
		},
		{
			name: "a transaction left open ends where the next begins", fallback: "EUR",
			input: strings.Replace(ofxV1, "<MEMO>Einkauf &amp; Pfand\n</STMTTRN>", "<MEMO>Einkauf &amp; Pfand\n", 1),
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatOFX, Rows: []Row{
				{Line: 22, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf & Pfand", BookingText: "POS", Amount: eur(-2347)},
				{Line: 30, Date: day("2026-10-01"), Payee: "Muster GmbH", BookingText: "CREDIT", Amount: eur(245000)},
				{Line: 38, Date: day("2026-10-07"), Payee: "FEE", BookingText: "FEE", Amount: eur(-490)},
			}},
		},
		{
			name: "a bad date", fallback: "EUR",
			input: strings.Replace(ofxV2, "<DTPOSTED>20261005</DTPOSTED>", "<DTPOSTED>2026-10</DTPOSTED>", 1),
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatOFX,
				Rows:   []Row{{Line: 9, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf", BookingText: "DEBIT", Amount: eur(-2347)}},
				Errors: []RowError{{Line: 17, Err: errors.New(`unrecognised date "2026-10"`)}},
			},
		},
		{
			name: "the statements of two accounts", fallback: "EUR",
			input:   strings.Replace(ofxV1, "</STMTTRNRS>", "</STMTTRNRS><STMTTRNRS><STMTRS><CURDEF>EUR</STMTRS></STMTTRNRS>", 1),
			wantErr: "the file has the statements of 2 accounts; import one account's at a time",
		},
		{name: "a CSV file", input: "Date;Amount\n2026-10-04;-1\n", fallback: "EUR", wantErr: "this is not an OFX file: it has no <OFX>"},
		{name: "a statement with no transactions", input: "<OFX><BANKMSGSRSV1></BANKMSGSRSV1></OFX>", fallback: "EUR", want: ParseResult{Encoding: EncodingUTF8, Format: FormatOFX}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.input

			// when
			got, err := Parse(strings.NewReader(tc.input), OFX, tc.fallback)

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

// qifBank is a QIF file of a bank account, made up for the tests, with the dates as
// Quicken writes them. The U of the salary differs from its T, which wins.
const qifBank = `!Type:Bank
D10/ 4'26
T-23.47
PREWE Markt
MEinkauf
LGroceries
^
D10/1'26
T2,450.00
U2,450.99
PMuster GmbH
^
D10/7'26
T-4.90
N1001
^
`

func Test_Parse_qif(t *testing.T) {
	german := Profile{Name: "meine", Format: FormatQIF, DecimalSep: ",", DateFormats: []string{"DD.MM.YYYY"}}
	tcs := []struct {
		name    string
		input   string
		profile Profile
		want    ParseResult
		wantErr string
	}{
		{
			name: "Quicken's dates and amounts; the number stands in for a booking text", input: qifBank, profile: QIF,
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatQIF, Rows: []Row{
				{Line: 2, Date: day("2026-10-04"), Payee: "REWE Markt", Purpose: "Einkauf", Amount: eur(-2347)},
				{Line: 8, Date: day("2026-10-01"), Payee: "Muster GmbH", Amount: eur(245000)},
				{Line: 13, Date: day("2026-10-07"), Payee: "1001", BookingText: "1001", Amount: eur(-490)},
			}},
		},
		{
			name: "one order for the whole file: 03/04 is the 4th of March because 03/13 is in it", profile: QIF,
			input: "!Type:CCard\nD03/04/2026\nT-1.00\n^\nD03/13/2026\nT-2.00\n^\n",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatQIF, Rows: []Row{
				{Line: 2, Date: day("2026-03-04"), Amount: eur(-100)}, {Line: 5, Date: day("2026-03-13"), Amount: eur(-200)},
			}},
		},
		{
			name: "and 13/03 makes it the 3rd of April", profile: QIF,
			input: "!Type:Bank\nD03/04/2026\nT-1.00\n^\nD13/03/2026\nT-2.00\n^\n",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatQIF, Rows: []Row{
				{Line: 2, Date: day("2026-04-03"), Amount: eur(-100)}, {Line: 5, Date: day("2026-03-13"), Amount: eur(-200)},
			}},
		},
		{
			name: "a profile of yours with German dates and a decimal comma", profile: german,
			input: "!Type:Bank\nD04.10.2026\nT-1.234,56\nPMiete\n^\n",
			want:  ParseResult{Encoding: EncodingUTF8, Format: FormatQIF, Rows: []Row{{Line: 2, Date: day("2026-10-04"), Payee: "Miete", Amount: eur(-123456)}}},
		},
		{
			name: "the account's own record and a list of categories are not transactions", profile: QIF,
			input: "!Option:AutoSwitch\n!Account\nNGiro\nTBank\n^\n!Clear:AutoSwitch\n!Type:Cat\nNGroceries\nE\n^\n!Type:Bank\nD10/04/26\nT-1.00\n^\n",
			want:  ParseResult{Encoding: EncodingUTF8, Format: FormatQIF, Rows: []Row{{Line: 12, Date: day("2026-10-04"), Amount: eur(-100)}}},
		},
		{
			name: "a last record without its ^, and Windows line ends", profile: QIF,
			input: "!Type:Bank\r\nD10/04/26\r\nT-1.00\r\nPKino\r\n",
			want:  ParseResult{Encoding: EncodingUTF8, Format: FormatQIF, Rows: []Row{{Line: 2, Date: day("2026-10-04"), Payee: "Kino", Amount: eur(-100)}}},
		},
		{
			name: "records that cannot be read are reported, and the others are read", profile: QIF,
			input: "!Type:Bank\nT-1.00\n^\nD10/04/26\n^\nD10/05/26\nTabc\n^\nD10/40/26\nT-1.00\n^\nD10/06/26\nT-2.00\n^\n",
			want: ParseResult{Encoding: EncodingUTF8, Format: FormatQIF,
				Rows: []Row{{Line: 12, Date: day("2026-10-06"), Amount: eur(-200)}},
				Errors: []RowError{
					{Line: 2, Err: errors.New("the record has no date (D)")},
					{Line: 4, Err: errors.New("the record has no amount (T)")},
					{Line: 6, Err: errors.New(`invalid amount "abc"`)},
					{Line: 9, Err: errors.New(`unrecognised date "10/40/26"`)},
				},
			},
		},
		{name: "an investment account", input: "!Type:Invst\nD10/04/26\nNBuy\n^\n", profile: QIF, wantErr: "this QIF file is of an investment account, which Thaler does not import"},
		{name: "a CSV file", input: "Date;Amount\n", profile: QIF, wantErr: "this is not a QIF file: it does not start with a line such as !Type:Bank"},
		{name: "an empty file", input: "", profile: QIF, wantErr: "this is not a QIF file: it does not start with a line such as !Type:Bank"},
		{name: "a profile of a format there is not", input: qifBank, profile: Profile{Name: "x", Format: "mt940"}, wantErr: `profile "x": format "mt940" is none of csv, ofx and qif`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.input

			// when
			got, err := Parse(strings.NewReader(tc.input), tc.profile, "EUR")

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

func Test_qifDate(t *testing.T) {
	tcs := []struct {
		in   string
		want string
	}{
		{in: "10/ 4'26", want: "10/04/26"},
		{in: "1/5'2026", want: "01/05/2026"},
		{in: "4.10.2026", want: "04.10.2026"},
		{in: "2026-1-5", want: "2026-01-05"},
		{in: "", want: ""},
	}
	for _, tc := range tcs {
		t.Run(tc.in, func(t *testing.T) {
			// given tc.in

			// when
			got := qifDate(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
