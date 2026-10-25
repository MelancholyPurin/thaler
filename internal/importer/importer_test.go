package importer

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func day(s string) time.Time {
	d, err := ledger.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func eur(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "EUR"} }

// rowErrors compares a RowError by its line and message: the error inside is
// an opaque value.
var rowErrors = cmp.Transformer("RowError", func(e RowError) struct {
	Line    int
	Message string
} {
	return struct {
		Line    int
		Message string
	}{e.Line, errText(e.Err)}
})

func readSample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/sparkasse_camt_sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// cp1252 encodes s the way an old banking export would.
func cp1252(t *testing.T, s string) string {
	t.Helper()
	var out []byte
	for _, r := range s {
		switch {
		case r < 0x80, r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		case r == '€':
			out = append(out, 0x80)
		default:
			t.Fatalf("test helper cannot encode %q", r)
		}
	}
	return string(out)
}

// sampleRows is what testdata/sparkasse_camt_sample.csv holds: five booked rows
// (the sixth is pending). Two of them are identical bakery bookings.
func sampleRows() []Row {
	bakery := Row{
		Date: day("2026-10-02"), Payee: "Bäckerei Schmidt", BookingText: "KARTENZAHLUNG", Amount: eur(-320),
		Purpose: "Baeckerei Schmidt//Hamburg/DE 2026-10-01T07:45:10 KFN 1 VJ 2912", // whitespace collapsed
	}
	salary := Row{Line: 2, Date: day("2026-10-01"), Payee: "Muster GmbH", Purpose: "Gehalt Oktober 2026", IBAN: "DE00123456780000000000", BookingText: "GEHALT/RENTE", Amount: eur(245000)}
	first, second := bakery, bakery
	first.Line, second.Line = 3, 4
	return []Row{
		salary, first, second,
		{Line: 5, Date: day("2026-10-05"), Payee: "Hausverwaltung Nord", Purpose: "Miete Oktober", IBAN: "DE00987654320000000000", BookingText: "LASTSCHRIFT", Amount: eur(-115000)}, // 1.150,00: a thousands separator
		{Line: 7, Date: day("2026-10-07"), Payee: "ENTGELTABSCHLUSS", Purpose: "Abrechnung 30.09.2026 Entgelte", BookingText: "ENTGELTABSCHLUSS", Amount: eur(-490)},                 // no payee: the booking text; four-digit year
	}
}

func Test_Parse(t *testing.T) {
	utf8Only := Sparkasse
	utf8Only.Encoding = "utf-8"
	mybank := Profile{
		Name: "mybank", Delimiter: ",", Encoding: "utf-8", DecimalSep: ".", DateFormats: []string{"YYYY-MM-DD"},
		Columns:    Columns{Date: []string{"Date"}, Payee: []string{"Description"}, Amount: []string{"Amount"}},
		SkipColumn: "Status", SkipContains: "pending",
	}
	text := func(s string) func(*testing.T) string { return func(*testing.T) string { return s } }

	tcs := []struct {
		name     string
		input    func(t *testing.T) string
		profile  Profile
		fallback ledger.Currency
		want     ParseResult
		wantErr  string
	}{
		{
			name:     "the sparkasse sample",
			input:    func(t *testing.T) string { return readSample(t) },
			profile:  Sparkasse,
			fallback: "EUR",
			want:     ParseResult{Rows: sampleRows(), Skipped: 1}, // the pending REWE booking
		},
		{
			name:     "the same sample as a windows-1252 file",
			input:    func(t *testing.T) string { return cp1252(t, readSample(t)) },
			profile:  Sparkasse,
			fallback: "EUR",
			want:     ParseResult{Rows: sampleRows(), Skipped: 1},
		},
		{
			name:     "an explicit utf-8 profile refuses windows-1252 bytes instead of guessing",
			input:    func(t *testing.T) string { return cp1252(t, readSample(t)) },
			profile:  utf8Only,
			fallback: "EUR",
			wantErr:  `file is not valid UTF-8 (try encoding = "windows-1252")`,
		},
		{
			name:     "a byte order mark, and no currency column so the fallback applies",
			input:    text("\ufeff" + `"Buchungstag";"Beguenstigter/Zahlungspflichtiger";"Betrag"` + "\n" + `"01.02.26";"Test";"1,50"` + "\n"),
			profile:  Sparkasse,
			fallback: "EUR",
			want:     ParseResult{Rows: []Row{{Line: 2, Date: day("2026-02-01"), Payee: "Test", Amount: eur(150)}}},
		},
		{
			name: "bad rows are reported and do not stop the others; the currency column wins and is upper-cased",
			input: text(`"Buchungstag";"Betrag";"Waehrung"` + "\n" +
				`"01.02.26";"1,50";"EUR"` + "\n" +
				`"31.02.26";"1,50";"EUR"` + "\n" +
				`"03.02.26";"abc";"EUR"` + "\n" +
				`"04.02.26";"2,00";"usd"` + "\n"),
			profile:  Sparkasse,
			fallback: "EUR",
			want: ParseResult{
				Rows: []Row{
					{Line: 2, Date: day("2026-02-01"), Amount: eur(150)},
					{Line: 5, Date: day("2026-02-04"), Amount: ledger.Money{Minor: 200, Currency: "USD"}},
				},
				Errors: []RowError{
					{Line: 3, Err: errors.New(`unrecognised date "31.02.26"`)},
					{Line: 4, Err: errors.New(`invalid amount "abc"`)},
				},
			},
		},
		{
			name:     "a custom profile with its own delimiter, decimal point, date format and skip rule",
			input:    text("Date,Description,Amount,Status\n2026-03-04,Coffee,\"-3.50\",posted\n2026-03-05,Lunch,-9.00,PENDING\n"),
			profile:  mybank,
			fallback: "USD",
			want: ParseResult{
				Rows:    []Row{{Line: 2, Date: day("2026-03-04"), Payee: "Coffee", Amount: ledger.Money{Minor: -350, Currency: "USD"}}},
				Skipped: 1,
			},
		},
		{
			name:     "a header that is not this bank's names the header it found",
			input:    text(`"Datum";"Text"` + "\n"),
			profile:  Sparkasse,
			fallback: "EUR",
			wantErr:  `profile "sparkasse" does not match this file: need a date column (Buchungstag) and an amount column (Betrag), but the header is: Datum; Text`,
		},
		{
			name:     "an empty file",
			input:    text(""),
			profile:  Sparkasse,
			fallback: "EUR",
			wantErr:  "file is empty: no header row found",
		},
		{
			name:     "a profile without date and amount columns",
			input:    text("a,b\n"),
			profile:  Profile{Name: "broken"},
			fallback: "EUR",
			wantErr:  `profile "broken": columns.date and columns.amount are required`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			input := tc.input(t)

			// when
			got, err := Parse(strings.NewReader(input), tc.profile, tc.fallback)

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

func Test_FindProfile(t *testing.T) {
	mybank := Profile{Name: "mybank", Delimiter: ","}
	shadow := Profile{Name: "Sparkasse", Delimiter: ","} // a custom profile takes precedence over the built-in one
	tcs := []struct {
		name    string
		find    string
		custom  []Profile
		want    Profile
		wantErr string
	}{
		{name: "custom, any case", find: "MyBank", custom: []Profile{mybank}, want: mybank},
		{name: "built-in", find: "sparkasse", want: Sparkasse},
		{name: "built-in, padded and upper-cased", find: "  SPARKASSE ", want: Sparkasse},
		{name: "custom shadows built-in", find: "sparkasse", custom: []Profile{shadow}, want: shadow},
		{name: "unknown lists what exists", find: "nope", custom: []Profile{mybank}, wantErr: `unknown import profile "nope" (available: mybank, sparkasse)`},
		{name: "unknown without custom profiles", find: "nope", wantErr: `unknown import profile "nope" (available: sparkasse)`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.find and tc.custom

			// when
			got, err := FindProfile(tc.find, tc.custom)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("profile mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ProfileNames(t *testing.T) {
	tcs := []struct {
		name   string
		custom []Profile
		want   []string
	}{
		{name: "built-in only", want: []string{"sparkasse"}},
		{name: "custom ones are added, lower-cased and sorted", custom: []Profile{{Name: "Zeta"}, {Name: "alpha"}}, want: []string{"alpha", "sparkasse", "zeta"}},
		{name: "a custom profile with a built-in name is listed once", custom: []Profile{{Name: "Sparkasse"}}, want: []string{"sparkasse"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.custom

			// when
			got := ProfileNames(tc.custom)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The keys are stored in the database, so their format must never change: a
// different key for the same bank row would make a re-import add it again.
func Test_Deduper_Key(t *testing.T) {
	bakery := Row{Date: day("2026-10-02"), Payee: "Bäckerei Schmidt", Purpose: "p", BookingText: "KARTENZAHLUNG", Amount: eur(-320)}
	rent := Row{Date: day("2026-10-05"), Payee: "Hausverwaltung Nord", Purpose: "Miete Oktober", IBAN: "DE00987654320000000000", BookingText: "LASTSCHRIFT", Amount: eur(-115000)}
	shouty := bakery
	shouty.Payee = "  BÄCKEREI   schmidt " // the same booking, spelled differently

	const (
		bakeryFirst  = "140c1875c2fd0cbfec692128c0130f22d3deb66dd3a29c710fa1a90790f4ab48"
		bakerySecond = "39de5435079ed1ec96403c8e53f0ac79f77e17ebbea393c4658c6cc85b302516"
		bakeryThird  = "3959a251bf2fbdef16325883375eccea841c1370dc732f7177d09e62e141f600"
		rentFirst    = "1faa529de8cc7e0e755ce8b8e304316ce8a74fd98acc5ba6a0a6776efc5d9b59"
		otherAccount = "279f52270b0e122e8b496d753728027813123181974ae43e9926b487c3e5e36a"
	)
	tcs := []struct {
		name    string
		account string
		rows    []Row
		want    []string
	}{
		{name: "a first booking", account: "1", rows: []Row{bakery}, want: []string{bakeryFirst}},
		{name: "identical bookings in one file are numbered, so they stay two", account: "1", rows: []Row{bakery, bakery, bakery}, want: []string{bakeryFirst, bakerySecond, bakeryThird}},
		{name: "a row's key does not depend on other days' rows", account: "1", rows: []Row{rent}, want: []string{rentFirst}},
		{name: "interleaved bookings are numbered independently", account: "1", rows: []Row{bakery, rent, bakery}, want: []string{bakeryFirst, rentFirst, bakerySecond}},
		{name: "the account is part of the key", account: "2", rows: []Row{bakery}, want: []string{otherAccount}},
		{name: "case and whitespace do not make a different booking", account: "1", rows: []Row{bakery, shouty}, want: []string{bakeryFirst, bakerySecond}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			d := NewDeduper(tc.account)

			// when
			var got []string
			for _, r := range tc.rows {
				got = append(got, d.Key(r))
			}

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_NewRuleset(t *testing.T) {
	tcs := []struct {
		name    string
		rules   []Rule
		wantErr string
	}{
		{name: "none"},
		{name: "contains and regex rules", rules: []Rule{{Contains: "rewe", Category: "Expenses:Groceries"}, {Regex: `^Hausverwaltung`, Category: "Expenses:Rent"}}},
		{name: "neither contains nor regex", rules: []Rule{{Category: "Expenses:Food"}}, wantErr: "rule 1: set exactly one of contains and regex"},
		{name: "both contains and regex", rules: []Rule{{Contains: "a", Regex: "b", Category: "Expenses:Food"}}, wantErr: "rule 1: set exactly one of contains and regex"},
		{name: "blank contains", rules: []Rule{{Contains: "ok", Category: "Expenses:Food"}, {Contains: "  ", Category: "Expenses:Food"}}, wantErr: "rule 2: set exactly one of contains and regex"},
		{name: "a category that is not an account", rules: []Rule{{Contains: "a", Category: "Food"}}, wantErr: `rule 1: category: account "Food" must start with one of Assets, Liabilities, Income, Expenses, Equity`},
		{name: "a regex that does not compile", rules: []Rule{{Regex: "(", Category: "Expenses:Food"}}, wantErr: "rule 1: regex: error parsing regexp: missing closing ): `(?i)(`"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.rules

			// when
			_, err := NewRuleset(tc.rules)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func testRules(t *testing.T) *Ruleset {
	t.Helper()
	rs, err := NewRuleset([]Rule{
		{Contains: "rewe", Category: "Expenses:Groceries"},
		{Regex: `b(ä|ae)ckerei`, Category: "Expenses:Bakery"},
		{Contains: "gehalt", Category: "Income:Salary"},
		{Regex: `^Hausverwaltung`, Category: "Expenses:Rent"},
		{Contains: "markt", Category: "Expenses:Other"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func Test_Ruleset_Match(t *testing.T) {
	type matched struct {
		Category string
		OK       bool
	}
	tcs := []struct {
		name   string
		fields []string
		want   matched
	}{
		{name: "contains, any case", fields: []string{"REWE Markt GmbH", "", "KARTENZAHLUNG"}, want: matched{"Expenses:Groceries", true}},
		{name: "a regex with an umlaut", fields: []string{"Bäckerei Schmidt", "", ""}, want: matched{"Expenses:Bakery", true}},
		{name: "any of the fields", fields: []string{"Muster GmbH", "Gehalt Oktober", "GEHALT/RENTE"}, want: matched{"Income:Salary", true}},
		{name: "an anchored regex", fields: []string{"Hausverwaltung Nord", "Miete", ""}, want: matched{"Expenses:Rent", true}},
		{name: "the first matching rule wins", fields: []string{"REWE Bäckerei"}, want: matched{"Expenses:Groceries", true}},
		{name: "no rule matches", fields: []string{"Unknown Shop", "", ""}, want: matched{}},
		{name: "no fields", want: matched{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			rs := testRules(t)

			// when
			category, ok := rs.Match(tc.fields...)

			// then
			if diff := cmp.Diff(tc.want, matched{category, ok}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Ruleset_Find(t *testing.T) {
	type found struct {
		Index    int
		Category string
		OK       bool
	}
	tcs := []struct {
		name   string
		fields []string
		want   found
	}{
		{name: "the first rule", fields: []string{"REWE Markt", ""}, want: found{0, "Expenses:Groceries", true}}, // rule 4 also matches
		{name: "a later rule", fields: []string{"Bäckerei Schulz"}, want: found{1, "Expenses:Bakery", true}},
		{name: "the last rule", fields: []string{"Wochenmarkt"}, want: found{4, "Expenses:Other", true}},
		{name: "no rule", fields: []string{"Kino"}, want: found{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			rs := testRules(t)

			// when
			index, category, ok := rs.Find(tc.fields...)

			// then
			if diff := cmp.Diff(tc.want, found{index, category, ok}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
