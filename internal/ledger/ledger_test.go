package ledger

import (
	"math/big"
	"strings"
	"testing"
)

func TestParseAmount(t *testing.T) {
	ok := []struct {
		in   string
		cur  Currency
		sep  byte
		want int64
	}{
		{"12.50", "EUR", 0, 1250},
		{"-12,50", "EUR", 0, -1250},
		{"1.234,56", "EUR", 0, 123456},
		{"1,234.56", "EUR", 0, 123456},
		{"1.234.567", "EUR", 0, 123456700},
		{"+3", "EUR", 0, 300},
		{"−5.5", "EUR", 0, -550},
		{".5", "EUR", 0, 50},
		{"12.500", "EUR", 0, 1250},
		{"1 000,50", "EUR", 0, 100050},
		{"1000", "JPY", 0, 1000},
		{"2.450,00", "EUR", ',', 245000},
		{"2,450.00", "MXN", '.', 245000},
		{"-0", "USD", 0, 0},
		{"1.5", "KWD", 0, 1500},
	}
	for _, tc := range ok {
		got, err := ParseAmount(tc.in, tc.cur, tc.sep)
		if err != nil {
			t.Errorf("ParseAmount(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got.Minor != tc.want || got.Currency != tc.cur {
			t.Errorf("ParseAmount(%q) = %+v, want %d %s", tc.in, got, tc.want, tc.cur)
		}
	}

	bad := []struct {
		in  string
		cur Currency
		sep byte
	}{
		{"", "EUR", 0},
		{"abc", "EUR", 0},
		{"-", "EUR", 0},
		{"--5", "EUR", 0},
		{"1,234", "EUR", 0},   // ambiguous: would be 3 decimals
		{"12.345", "EUR", 0},  // too many decimals
		{"1,2,3", "EUR", ','}, // two decimal separators
		{"12.5.5", "EUR", 0},  // inconsistent grouping
		{"12.5", "EUR", ','},  // '.' as thousands separator must group in threes
		{"1.2345,00", "EUR", ','},
		{"1.50", "JPY", 0},
		{"99999999999999999999", "EUR", 0},
	}
	for _, tc := range bad {
		if got, err := ParseAmount(tc.in, tc.cur, tc.sep); err == nil {
			t.Errorf("ParseAmount(%q, %s, %q) = %+v, want error", tc.in, tc.cur, tc.sep, got)
		}
	}
}

func TestMoneyDecimal(t *testing.T) {
	cases := []struct {
		m    Money
		want string
	}{
		{Money{-1250, "EUR"}, "-12.50"},
		{Money{5, "EUR"}, "0.05"},
		{Money{-5, "USD"}, "-0.05"},
		{Money{0, "MXN"}, "0.00"},
		{Money{1234, "JPY"}, "1234"},
		{Money{1500, "KWD"}, "1.500"},
	}
	for _, tc := range cases {
		if got := tc.m.Decimal(); got != tc.want {
			t.Errorf("%+v.Decimal() = %q, want %q", tc.m, got, tc.want)
		}
	}
	if got := (Money{-1250, "EUR"}).String(); got != "-12.50 EUR" {
		t.Errorf("String() = %q", got)
	}
}

func TestMoneyAdd(t *testing.T) {
	sum, err := Money{100, "EUR"}.Add(Money{-30, "EUR"})
	if err != nil || sum.Minor != 70 {
		t.Fatalf("Add = %+v, %v", sum, err)
	}
	if _, err := (Money{1, "EUR"}).Add(Money{1, "USD"}); err == nil {
		t.Fatal("expected currency mismatch error")
	}
}

func mustRate(t *testing.T, s string) *big.Rat {
	t.Helper()
	r, _, err := ParseRate(s)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r
}

func TestConvert(t *testing.T) {
	cases := []struct {
		m    Money
		to   Currency
		rate string
		want int64
	}{
		{Money{10000, "EUR"}, "USD", "1.0850", 10850},
		{Money{1, "EUR"}, "USD", "0.5", 1},   // half rounds away from zero
		{Money{-1, "EUR"}, "USD", "0.5", -1}, // ... in both directions
		{Money{1, "EUR"}, "USD", "0.49", 0},
		{Money{1000, "EUR"}, "JPY", "160", 1600}, // 10.00 EUR -> 1600 JPY
		{Money{1000, "USD"}, "MXN", "18.5", 18500},
	}
	for _, tc := range cases {
		got := Convert(tc.m, tc.to, mustRate(t, tc.rate))
		if got.Minor != tc.want || got.Currency != tc.to {
			t.Errorf("Convert(%v -> %s @ %s) = %v, want %d", tc.m, tc.to, tc.rate, got, tc.want)
		}
	}
	same := Convert(Money{42, "EUR"}, "EUR", mustRate(t, "2"))
	if same.Minor != 42 {
		t.Errorf("same-currency convert changed the amount: %v", same)
	}
}

func TestParseRate(t *testing.T) {
	if _, norm, err := ParseRate(" 1,0850 "); err != nil || norm != "1.0850" {
		t.Errorf("ParseRate comma: %q, %v", norm, err)
	}
	for _, in := range []string{"", "0", "-1", "abc", "1.2.3", "1e3", "."} {
		if _, _, err := ParseRate(in); err == nil {
			t.Errorf("ParseRate(%q): expected error", in)
		}
	}
}

func TestRateBook(t *testing.T) {
	book := NewRateBook([]RateEntry{
		{"2026-01-01", "USD", "EUR", mustRate(t, "0.90")},
		{"2026-10-01", "USD", "EUR", mustRate(t, "0.92")},
		{"2026-09-01", "MXN", "EUR", mustRate(t, "0.05")},
	})

	check := func(from, to Currency, date string, want *big.Rat) {
		t.Helper()
		got, ok := book.Lookup(from, to, date)
		if want == nil {
			if ok {
				t.Errorf("Lookup(%s,%s,%s) = %v, want not found", from, to, date, got)
			}
			return
		}
		if !ok || got.Cmp(want) != 0 {
			t.Errorf("Lookup(%s,%s,%s) = %v (%v), want %v", from, to, date, got, ok, want)
		}
	}

	check("USD", "EUR", "2026-10-21", big.NewRat(23, 25)) // latest entry on or before
	check("USD", "EUR", "2026-06-01", big.NewRat(9, 10))  // older entry applies earlier
	check("USD", "EUR", "2025-12-31", nil)                // before any entry
	check("EUR", "USD", "2026-10-21", big.NewRat(25, 23)) // inverse
	check("USD", "MXN", "2026-10-21", big.NewRat(92, 5))  // via EUR: 0.92 * (1/0.05)
	check("MXN", "USD", "2026-10-21", big.NewRat(5, 92))  // and back
	check("USD", "USD", "2020-01-01", big.NewRat(1, 1))   // identity needs no entry
	check("USD", "GBP", "2026-10-21", nil)                // unknown currency

	got, ok := book.Convert(Money{10000, "USD"}, "EUR", "2026-10-21")
	if !ok || got.Minor != 9200 {
		t.Errorf("Convert = %v, %v", got, ok)
	}
}

func TestCheckBalanced(t *testing.T) {
	good := [][]Posting{
		{{"Assets:Bank", Money{-1250, "EUR"}}, {"Expenses:Food", Money{1250, "EUR"}}},
		{{"Assets:Bank", Money{-10000, "EUR"}}, {"Equity:Conversions", Money{10000, "EUR"}},
			{"Equity:Conversions", Money{-10850, "USD"}}, {"Assets:Cash", Money{10850, "USD"}}},
		{{"Assets:Bank", Money{-1000, "EUR"}}, {"Expenses:A", Money{600, "EUR"}}, {"Expenses:B", Money{400, "EUR"}}},
		{{"Assets:Bank", Money{0, "EUR"}}, {"Expenses:Uncategorized", Money{0, "EUR"}}}, // bank verification charge
	}
	for i, ps := range good {
		if err := CheckBalanced(ps); err != nil {
			t.Errorf("good[%d]: %v", i, err)
		}
	}

	bad := map[string][]Posting{
		"one posting":                         {{"Assets:Bank", Money{100, "EUR"}}},
		"unbalanced":                          {{"Assets:Bank", Money{-1250, "EUR"}}, {"Expenses:Food", Money{1200, "EUR"}}},
		"mixed currencies without conversion": {{"Assets:Bank", Money{-1000, "EUR"}}, {"Assets:Cash", Money{1000, "USD"}}},
		"no currency":                         {{"Assets:Bank", Money{-1, ""}}, {"Expenses:Food", Money{1, ""}}},
		"no account":                          {{"", Money{-1, "EUR"}}, {"Expenses:Food", Money{1, "EUR"}}},
	}
	for name, ps := range bad {
		if err := CheckBalanced(ps); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}

	err := CheckBalanced(bad["unbalanced"])
	if err == nil || !strings.Contains(err.Error(), "EUR is off by -0.50") {
		t.Errorf("error should name the currency and the difference, got %v", err)
	}
}

func TestAccountNames(t *testing.T) {
	for _, n := range []string{"Assets:Sparkasse", "Assets:Cash-USD", "Expenses:Groceries:Bio_Markt", "Equity:Conversions", "Income:Gehalt", "Expenses:Bäckerei"} {
		if err := ValidateAccountName(n); err != nil {
			t.Errorf("ValidateAccountName(%q): %v", n, err)
		}
	}
	for _, n := range []string{"", "Sparkasse", "assets:bank", "Assets:", "Assets::Bank", "Assets:Bank Account", "Expenses:Food&Drink", "Assets:-Bank"} {
		if err := ValidateAccountName(n); err == nil {
			t.Errorf("ValidateAccountName(%q): expected error", n)
		}
	}
	typ, err := TypeOf("Liabilities:Visa")
	if err != nil || typ != Liabilities || !typ.NeedsCurrency() {
		t.Errorf("TypeOf = %v, %v", typ, err)
	}
	if Expenses.NeedsCurrency() {
		t.Error("expense accounts must be multi-currency")
	}
}

func TestParseDate(t *testing.T) {
	for _, in := range []string{"2026-10-21", "21.10.2026", " 2026-10-21 "} {
		d, err := ParseDate(in)
		if err != nil || d.Format(DateLayout) != "2026-10-21" {
			t.Errorf("ParseDate(%q) = %v, %v", in, d, err)
		}
	}
	for _, in := range []string{"", "21/10/2026", "2026-13-01", "yesterday"} {
		if _, err := ParseDate(in); err == nil {
			t.Errorf("ParseDate(%q): expected error", in)
		}
	}
}
