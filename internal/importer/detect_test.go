package importer

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_Detect(t *testing.T) {
	type found struct {
		Name string
		OK   bool
	}
	sample := readSample(t)
	// A bank that writes its dates and amounts in English, commas between columns.
	ing := Profile{
		Name: "Ing", Delimiter: ",",
		Columns: Columns{Date: []string{"Date"}, Payee: []string{"Name"}, Amount: []string{"Amount"}, Purpose: []string{"Notes"}},
	}
	ingFile := "Date,Name,Amount,Notes\n2026-10-02,REWE,-23.47,food\n"
	// Two columns of what Sparkasse names, so it fits the Sparkasse export too, but worse.
	thin := Profile{Name: "thin", Columns: Columns{Date: []string{"Buchungstag"}, Amount: []string{"Betrag"}}}
	umlaut := Profile{
		Name: "Umlaut", Encoding: "windows-1252",
		Columns: Columns{Date: []string{"Datum"}, Payee: []string{"Begünstigter"}, Amount: []string{"Betrag"}},
	}
	tcs := []struct {
		name   string
		file   string
		custom []Profile
		want   found
	}{
		{name: "the Sparkasse export", file: sample, want: found{"sparkasse", true}},
		{name: "with a byte order mark", file: "\xef\xbb\xbf" + sample, want: found{"sparkasse", true}},
		{name: "in Windows-1252", file: cp1252(t, sample), want: found{"sparkasse", true}},
		{name: "a profile of yours for another bank", file: ingFile, custom: []Profile{ing}, want: found{"ing", true}},
		{name: "a file of another bank fits no profile", file: ingFile, want: found{}},
		{name: "the profile that names more of the columns wins", file: sample, custom: []Profile{thin}, want: found{"sparkasse", true}},
		{
			name: "of two that fit equally well the first written wins", file: ingFile,
			custom: []Profile{{Name: "second", Delimiter: ",", Columns: ing.Columns}, ing}, want: found{"second", true},
		},
		{
			name: "yours replaces the built-in one of the same name", file: sample,
			custom: []Profile{{Name: "Sparkasse", Columns: Columns{Date: []string{"Datum"}, Amount: []string{"Wert"}}}},
			want:   found{},
		},
		{
			name: "a column the profile skips by counts for it", file: sample,
			custom: []Profile{{Name: "plain", Columns: Sparkasse.Columns}, {Name: "skipping", Columns: Sparkasse.Columns, SkipColumn: "Info"}},
			want:   found{"skipping", true},
		},
		{
			name: "yours that is the same as the built-in one fits as well and comes first", file: sample,
			custom: []Profile{{Name: "mine", Delimiter: ";", Columns: Sparkasse.Columns, SkipColumn: "Info"}}, want: found{"mine", true},
		},
		{name: "a copy saved with commas is still found", file: strings.ReplaceAll(sample, ";", ","), want: found{"sparkasse", true}},
		{name: "or with tabs", file: strings.ReplaceAll(sample, ";", "\t"), want: found{"sparkasse", true}},
		{name: "or as UTF-16 with a byte order mark", file: utf16Text(sample, false, true), want: found{"sparkasse", true}},
		{name: "or as UTF-16 without one", file: utf16Text(sample, false, false), want: found{"sparkasse", true}},
		{name: "or as big-endian UTF-16", file: utf16Text(sample, true, false), want: found{"sparkasse", true}},
		{
			name: "a profile with a delimiter of its own reads no other", file: ingFile,
			custom: []Profile{{Name: "fixed", Delimiter: ";", Columns: ing.Columns}}, want: found{},
		},
		{
			name: "a profile with an encoding of its own reads no other", file: utf16Text(ingFile, false, true),
			custom: []Profile{{Name: "fixed", Encoding: "utf-8", Delimiter: ",", Columns: ing.Columns}}, want: found{},
		},
		{name: "an OFX statement, version 1", file: ofxV1, want: found{"ofx", true}},
		{name: "an OFX statement, version 2", file: ofxV2, want: found{"ofx", true}},
		{name: "an OFX statement as UTF-16", file: utf16Text(ofxV2, false, true), want: found{"ofx", true}},
		{name: "a QIF file", file: qifBank, want: found{"qif", true}},
		{name: "a CAMT.053 statement, version 2", file: camtV2, want: found{"camt", true}},
		{name: "a CAMT.053 statement with prefixes", file: camtV8, want: found{"camt", true}},
		{name: "a CAMT.053 statement as UTF-16", file: utf16Text(camtV2, false, true), want: found{"camt", true}},
		{name: "a CAMT.053 statement in Windows-1252", file: cp1252(t, strings.Replace(camtV2, "REWE Markt", "Bäckerei", 1)), want: found{"camt", true}},
		{name: "a CSV profile of yours does not fit a CAMT statement", file: camtV2, custom: []Profile{ing}, want: found{"camt", true}},
		{name: "an account report, camt.052, is no statement", file: "<Document><BkToCstmrAcctRpt></BkToCstmrAcctRpt></Document>", want: found{}},
		{name: "XML of something else", file: `<?xml version="1.0"?><settings><theme>dark</theme></settings>`, want: found{}},
		{name: "a QIF file after blank lines", file: "\n\n" + qifBank, want: found{"qif", true}},
		{
			name: "a QIF profile of yours fits as well as the built-in one and comes first", file: qifBank,
			custom: []Profile{{Name: "Meine", Format: FormatQIF, DateFormats: []string{"DD.MM.YYYY"}}}, want: found{"meine", true},
		},
		{name: "a CSV profile of yours does not fit an OFX statement", file: ofxV1, custom: []Profile{ing}, want: found{"ofx", true}},
		{
			name: "the format wins even over a CSV profile that would read its first line", file: qifBank,
			custom: []Profile{{Name: "odd", Columns: Columns{Date: []string{"!Type:Bank"}, Amount: []string{"!Type:Bank"}, Payee: []string{"!Type:Bank"}}}}, want: found{"qif", true},
		},
		{name: "a header with no date column", file: "Betrag;Name\n1;x\n", want: found{}},
		{name: "a header with no amount column", file: "Buchungstag;Name\n1;x\n", want: found{}},
		{name: "blank lines before the header", file: "\n\n" + sample, want: found{"sparkasse", true}},
		{name: "an empty file", file: "", want: found{}},
		{name: "a profile that cannot be used is passed over", file: sample, custom: []Profile{{Name: "bad", Encoding: "ebcdic", Columns: Sparkasse.Columns, SkipColumn: "Info"}}, want: found{"sparkasse", true}},
		{
			name: "a long file is read as far as its header", file: sample + strings.Repeat("02.10.26;;ÄÖÜ;;;;;-1,00;EUR;\n", 5000),
			want: found{"sparkasse", true},
		},
		{
			name: "a long file in UTF-16 is read as far as its header", file: utf16Text(sample+strings.Repeat("02.10.26;;ÄÖÜ;;;;;-1,00;EUR;\n", 5000), true, true),
			want: found{"sparkasse", true},
		},
		{
			name: "a long file in Windows-1252 is not cut in the middle of a letter", file: cp1252(t, "Datum;Begünstigter;Betrag\n"+strings.Repeat("02.10.26;Müller;-1,00\n", 5000)),
			custom: []Profile{umlaut}, want: found{"umlaut", true},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.file and the profiles of the person

			// when
			name, ok := Detect(strings.NewReader(tc.file), tc.custom)

			// then
			if diff := cmp.Diff(tc.want, found{name, ok}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
