package importer

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// fuzzProfiles are the ways Fuzz_Parse reads a file: the built-in profiles, and CSV
// profiles of the kinds a person writes, with a point, a comma or neither fixed as the
// decimal separator.
var fuzzProfiles = []Profile{
	Sparkasse, OFX, QIF, CAMT,
	{Name: "point", Delimiter: ",", DecimalSep: ".", Columns: Columns{Date: []string{"Date"}, Payee: []string{"Name"}, Amount: []string{"Amount"}, Currency: []string{"Currency"}}},
	{Name: "comma", DecimalSep: ",", DateFormats: []string{"DD.MM.YY"}, Columns: Columns{Date: []string{"Datum"}, Amount: []string{"Betrag"}}, SkipColumn: "Status", SkipContains: "offen"},
	{Name: "guess", Columns: Columns{Date: []string{"Date"}, Amount: []string{"Amount"}}},
	{Name: "qif-de", Format: FormatQIF, DecimalSep: ",", DateFormats: []string{"DD.MM.YYYY"}},
}

// Fuzz_Parse feeds every profile any bytes. Whatever a file holds, Parse returns rows or
// an error, never a panic, and what it returns holds together: each row is on a line of
// the file, has a day and is in a currency, each problem names a line, and reading the
// same bytes again gives the same result. Detect names only a profile there is.
//
// make fuzz runs it for longer; go test runs the seeds.
func Fuzz_Parse(f *testing.F) {
	raw, err := os.ReadFile("testdata/sparkasse_camt_sample.csv")
	if err != nil {
		f.Fatal(err)
	}
	sample := string(raw)
	seeds := []struct {
		file    string
		profile uint8 // index into fuzzProfiles
	}{
		{file: sample, profile: 0},
		{file: strings.ReplaceAll(sample, ";", "\t"), profile: 0},
		{file: utf16Text(sample, false, true), profile: 0},
		{file: ofxV1, profile: 1},
		{file: ofxV2, profile: 1},
		{file: qifBank, profile: 2},
		{file: "Date,Name,Amount,Currency\n2026-10-04,REWE,-23.47,eur\n2026-10-05,\"A, B\",1.234.567,EUR\n", profile: 4},
		{file: "Datum;Betrag;Status\n04.10.26;-23,47;\n05.10.26;-1,00;offen\n", profile: 5},
		{file: "Date|Amount\n2026-10-04|-1\n\n2026-10-05|\"2\n", profile: 6},
		{file: "!Type:Bank\nD04.10.2026\nT-1.234,56\n^\n", profile: 7},
		{file: "", profile: 0},
		{file: "\xff\xfe\x00", profile: 1},
		{file: "<OFX><STMTTRN><DTPOSTED>2026<TRNAMT>1</STMTTRN>", profile: 1},
		{file: camtV2, profile: 3},
		{file: camtV8, profile: 3},
		{file: "<Document><BkToCstmrStmt><Stmt><Ntry><Amt>1</Amt><CdtDbtInd>DBIT</CdtDbtInd><ValDt><DtTm>2026-10-04", profile: 3},
	}
	for _, s := range seeds {
		f.Add([]byte(s.file), s.profile)
	}
	f.Fuzz(func(t *testing.T, file []byte, profile uint8) {
		// given
		p := fuzzProfiles[int(profile)%len(fuzzProfiles)]

		// when
		got, err := Parse(bytes.NewReader(file), p, "EUR")
		again, errAgain := Parse(bytes.NewReader(file), p, "EUR")
		detected, found := Detect(bytes.NewReader(file), fuzzProfiles[4:])

		// then
		if diff := cmp.Diff(errText(err), errText(errAgain)); diff != "" {
			t.Fatalf("the same bytes failed differently (-first +again):\n%s", diff)
		}
		if found {
			if _, err := FindProfile(detected, fuzzProfiles[4:]); err != nil {
				t.Fatalf("Detect named %q, which FindProfile does not know: %v", detected, err)
			}
		}
		if err != nil {
			return
		}
		if diff := cmp.Diff(got, again, rowErrors); diff != "" {
			t.Fatalf("the same bytes read differently (-first +again):\n%s", diff)
		}
		// A line ends in a \n, which every encoding Parse reads has as a byte of its own
		// (UTF-16 as half of a pair), so a file has at most this many lines.
		lines := bytes.Count(file, []byte{'\n'}) + 1
		for _, r := range got.Rows {
			if r.Line < 1 || r.Line > lines {
				t.Fatalf("a row is on line %d of a file of %d lines: %+v", r.Line, lines, r)
			}
			if r.Date.IsZero() {
				t.Fatalf("a row has no day: %+v", r)
			}
			if _, err := ledger.ParseCurrency(string(r.Amount.Currency)); err != nil {
				t.Fatalf("a row is in no currency: %+v", r)
			}
		}
		for _, e := range got.Errors {
			if e.Line < 1 || e.Line > lines || e.Err == nil {
				t.Fatalf("a problem is on line %d of a file of %d lines: %v", e.Line, lines, e.Err)
			}
		}
	})
}
