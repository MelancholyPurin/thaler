package importer

import (
	"os"
	"strings"
	"testing"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func openSample(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open("testdata/sparkasse_camt_sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestParseSparkasseSample(t *testing.T) {
	res, err := Parse(openSample(t), Sparkasse, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected row errors: %v", res.Errors)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (the pending REWE booking)", res.Skipped)
	}
	if len(res.Rows) != 5 {
		t.Fatalf("got %d rows, want 5", len(res.Rows))
	}

	salary := res.Rows[0]
	if salary.Amount.Minor != 245000 || salary.Amount.Currency != "EUR" {
		t.Errorf("salary amount = %v", salary.Amount)
	}
	if salary.Date.Format(ledger.DateLayout) != "2026-10-01" {
		t.Errorf("two-digit year parsed as %s", salary.Date.Format(ledger.DateLayout))
	}
	if salary.Payee != "Muster GmbH" || salary.IBAN != "DE00123456780000000000" {
		t.Errorf("salary payee/iban = %q / %q", salary.Payee, salary.IBAN)
	}

	bakery := res.Rows[1]
	if bakery.Payee != "Bäckerei Schmidt" || bakery.Amount.Minor != -320 {
		t.Errorf("bakery = %+v", bakery)
	}
	if strings.Contains(bakery.Purpose, "  ") {
		t.Errorf("purpose whitespace not collapsed: %q", bakery.Purpose)
	}

	rent := res.Rows[3]
	if rent.Amount.Minor != -115000 {
		t.Errorf("rent amount = %v, want -1150.00 (thousands separator)", rent.Amount)
	}

	fee := res.Rows[4]
	if fee.Date.Format(ledger.DateLayout) != "2026-10-07" {
		t.Errorf("four-digit year parsed as %s", fee.Date.Format(ledger.DateLayout))
	}
	if fee.Payee != "ENTGELTABSCHLUSS" {
		t.Errorf("empty payee should fall back to booking text, got %q", fee.Payee)
	}

	var sum int64
	for _, r := range res.Rows {
		sum += r.Amount.Minor
	}
	if sum != 128870 {
		t.Errorf("sum of rows = %d, want 128870", sum)
	}
}

// cp1252 encodes s the way an old banking export would.
func cp1252(t *testing.T, s string) []byte {
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
	return out
}

func TestParseWindows1252(t *testing.T) {
	raw, err := os.ReadFile("testdata/sparkasse_camt_sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	encoded := cp1252(t, string(raw))
	if string(encoded) == string(raw) {
		t.Fatal("test file should contain non-ASCII characters")
	}
	res, err := Parse(strings.NewReader(string(encoded)), Sparkasse, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 5 || res.Rows[1].Payee != "Bäckerei Schmidt" {
		t.Errorf("Windows-1252 file decoded wrongly: %d rows, payee %q", len(res.Rows), res.Rows[1].Payee)
	}

	// An explicit utf-8 profile must reject the same bytes instead of guessing.
	p := Sparkasse
	p.Encoding = "utf-8"
	if _, err := Parse(strings.NewReader(string(encoded)), p, "EUR"); err == nil {
		t.Error("expected an error for invalid UTF-8 when encoding is utf-8")
	}
}

func TestParseHandlesBOMAndMissingColumns(t *testing.T) {
	withBOM := "\ufeff" + `"Buchungstag";"Beguenstigter/Zahlungspflichtiger";"Betrag"` + "\n" + `"01.02.26";"Test";"1,50"` + "\n"
	res, err := Parse(strings.NewReader(withBOM), Sparkasse, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0].Amount.Minor != 150 || res.Rows[0].Amount.Currency != "EUR" {
		t.Errorf("rows = %+v (fallback currency should apply without a Waehrung column)", res.Rows)
	}

	_, err = Parse(strings.NewReader(`"Datum";"Text"`+"\n"), Sparkasse, "EUR")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("expected a profile mismatch error naming the header, got %v", err)
	}
	if _, err := Parse(strings.NewReader(""), Sparkasse, "EUR"); err == nil {
		t.Error("expected an error for an empty file")
	}
}

func TestParseReportsBadRows(t *testing.T) {
	csv := `"Buchungstag";"Betrag";"Waehrung"` + "\n" +
		`"01.02.26";"1,50";"EUR"` + "\n" +
		`"31.02.26";"1,50";"EUR"` + "\n" +
		`"03.02.26";"abc";"EUR"` + "\n" +
		`"04.02.26";"2,00";"usd"` + "\n"
	res, err := Parse(strings.NewReader(csv), Sparkasse, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || len(res.Errors) != 2 {
		t.Fatalf("rows=%d errors=%d, want 2 and 2: %v", len(res.Rows), len(res.Errors), res.Errors)
	}
	if res.Rows[1].Amount.Currency != "USD" {
		t.Errorf("currency column should win and be upper-cased, got %s", res.Rows[1].Amount.Currency)
	}
	if res.Errors[0].Line != 3 || res.Errors[1].Line != 4 {
		t.Errorf("error lines = %d, %d, want 3 and 4", res.Errors[0].Line, res.Errors[1].Line)
	}
}

func TestCustomProfile(t *testing.T) {
	p := Profile{
		Name:        "mybank",
		Delimiter:   ",",
		Encoding:    "utf-8",
		DecimalSep:  ".",
		DateFormats: []string{"YYYY-MM-DD"},
		Columns: Columns{
			Date:   []string{"Date"},
			Payee:  []string{"Description"},
			Amount: []string{"Amount"},
		},
		SkipColumn:   "Status",
		SkipContains: "pending",
	}
	csv := "Date,Description,Amount,Status\n2026-03-04,Coffee,\"-3.50\",posted\n2026-03-05,Lunch,-9.00,PENDING\n"
	res, err := Parse(strings.NewReader(csv), p, "USD")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Skipped != 1 || res.Rows[0].Amount.Minor != -350 || res.Rows[0].Amount.Currency != "USD" {
		t.Errorf("rows=%+v skipped=%d", res.Rows, res.Skipped)
	}

	found, err := FindProfile("MyBank", []Profile{p})
	if err != nil || found.Name != "mybank" {
		t.Errorf("FindProfile custom: %v, %v", found.Name, err)
	}
	if _, err := FindProfile("sparkasse", nil); err != nil {
		t.Errorf("FindProfile builtin: %v", err)
	}
	if _, err := FindProfile("nope", []Profile{p}); err == nil || !strings.Contains(err.Error(), "mybank") {
		t.Errorf("unknown profile error should list the available ones, got %v", err)
	}
	if _, err := Parse(strings.NewReader(csv), Profile{Name: "broken"}, "EUR"); err == nil {
		t.Error("profile without date/amount columns must be rejected")
	}
}

func TestDeduper(t *testing.T) {
	res, err := Parse(openSample(t), Sparkasse, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	keys := func(account string) []string {
		d := NewDeduper(account)
		var out []string
		for _, r := range res.Rows {
			out = append(out, d.Key(r))
		}
		return out
	}

	first, again := keys("1"), keys("1")
	for i := range first {
		if first[i] != again[i] {
			t.Errorf("key %d is not stable across runs", i)
		}
	}
	seen := map[string]bool{}
	for i, k := range first {
		if seen[k] {
			t.Errorf("key %d collides: identical bookings must still be distinguishable", i)
		}
		seen[k] = true
	}
	// Rows 1 and 2 are the two identical bakery bookings.
	if first[1] == first[2] {
		t.Error("identical rows in one file must get different keys")
	}
	if keys("2")[0] == first[0] {
		t.Error("the account must be part of the key")
	}

	// Re-importing only the second half of the file (as an overlapping export
	// would) must reproduce the keys of rows that were already imported when
	// the overlap starts on a day boundary.
	d := NewDeduper("1")
	if got := d.Key(res.Rows[3]); got != first[3] {
		t.Error("key of a row must not depend on rows from other days")
	}
}

func TestRuleset(t *testing.T) {
	rs, err := NewRuleset([]Rule{
		{Contains: "rewe", Category: "Expenses:Groceries"},
		{Regex: `bäckerei|baeckerei`, Category: "Expenses:Groceries:Bakery"},
		{Contains: "gehalt", Category: "Income:Salary"},
		{Regex: `^Hausverwaltung`, Category: "Expenses:Rent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		fields []string
		want   string
		ok     bool
	}{
		{[]string{"REWE Markt GmbH", "", "KARTENZAHLUNG"}, "Expenses:Groceries", true},
		{[]string{"Bäckerei Schmidt", "", ""}, "Expenses:Groceries:Bakery", true},
		{[]string{"Muster GmbH", "Gehalt Oktober", "GEHALT/RENTE"}, "Income:Salary", true},
		{[]string{"Hausverwaltung Nord", "Miete", ""}, "Expenses:Rent", true},
		{[]string{"Unknown Shop", "", ""}, "", false},
	}
	for _, tc := range cases {
		got, ok := rs.Match(tc.fields...)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Match(%q) = %q, %v; want %q, %v", tc.fields, got, ok, tc.want, tc.ok)
		}
	}

	// First match wins: a payee matching two rules takes the earlier one.
	first, _ := rs.Match("REWE Bäckerei")
	if first != "Expenses:Groceries" {
		t.Errorf("rule order not respected, got %q", first)
	}

	invalid := [][]Rule{
		{{Category: "Expenses:Food"}},
		{{Contains: "a", Regex: "b", Category: "Expenses:Food"}},
		{{Contains: "a", Category: "Food"}},
		{{Regex: "(", Category: "Expenses:Food"}},
	}
	for i, rules := range invalid {
		if _, err := NewRuleset(rules); err == nil {
			t.Errorf("invalid[%d]: expected error", i)
		}
	}
}

func TestRulesetFindSaysWhichRule(t *testing.T) {
	rs, err := NewRuleset([]Rule{
		{Contains: "rewe", Category: "Expenses:Groceries"},
		{Regex: "b(ä|ae)ckerei", Category: "Expenses:Bakery"},
		{Contains: "markt", Category: "Expenses:Other"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		fields []string
		index  int
		cat    string
		ok     bool
	}{
		{[]string{"REWE Markt", ""}, 0, "Expenses:Groceries", true}, // rule 0 beats rule 2, which also matches
		{[]string{"Bäckerei Schulz"}, 1, "Expenses:Bakery", true},
		{[]string{"Wochenmarkt"}, 2, "Expenses:Other", true},
		{[]string{"Kino"}, 0, "", false},
	} {
		i, cat, ok := rs.Find(tc.fields...)
		if ok != tc.ok || cat != tc.cat || (ok && i != tc.index) {
			t.Errorf("Find(%q) = %d, %q, %v; want %d, %q, %v", tc.fields, i, cat, ok, tc.index, tc.cat, tc.ok)
		}
		if mc, mok := rs.Match(tc.fields...); mc != tc.cat || mok != tc.ok {
			t.Errorf("Match(%q) = %q, %v; must agree with Find", tc.fields, mc, mok)
		}
	}
}
