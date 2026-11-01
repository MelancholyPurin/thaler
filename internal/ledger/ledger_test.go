package ledger

import (
	"math/big"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// errText is the message of err, or "" for nil, so that a failure is compared
// by what it says.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func mustRate(t *testing.T, s string) *big.Rat {
	t.Helper()
	r, _, err := ParseRate(s)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r
}

func Test_ParseAmount(t *testing.T) {
	tcs := []struct {
		name    string
		in      string
		cur     Currency
		sep     byte
		want    Money
		wantErr string
	}{
		{name: "plain", in: "12.50", cur: "EUR", want: Money{1250, "EUR"}},
		{name: "comma decimal and sign", in: "-12,50", cur: "EUR", want: Money{-1250, "EUR"}},
		{name: "german grouping", in: "1.234,56", cur: "EUR", want: Money{123456, "EUR"}},
		{name: "english grouping", in: "1,234.56", cur: "EUR", want: Money{123456, "EUR"}},
		{name: "grouping without decimals", in: "1.234.567", cur: "EUR", want: Money{123456700, "EUR"}},
		{name: "explicit plus", in: "+3", cur: "EUR", want: Money{300, "EUR"}},
		{name: "unicode minus", in: "−5.5", cur: "EUR", want: Money{-550, "EUR"}},
		{name: "no integer part", in: ".5", cur: "EUR", want: Money{50, "EUR"}},
		{name: "trailing zero beyond the exponent", in: "12.500", cur: "EUR", want: Money{1250, "EUR"}},
		{name: "space as thousands separator", in: "1 000,50", cur: "EUR", want: Money{100050, "EUR"}},
		{name: "currency without decimals", in: "1000", cur: "JPY", want: Money{1000, "JPY"}},
		{name: "forced comma decimal", in: "2.450,00", cur: "EUR", sep: ',', want: Money{245000, "EUR"}},
		{name: "forced dot decimal", in: "2,450.00", cur: "MXN", sep: '.', want: Money{245000, "MXN"}},
		{name: "negative zero", in: "-0", cur: "USD", want: Money{0, "USD"}},
		{name: "three decimals", in: "1.5", cur: "KWD", want: Money{1500, "KWD"}},

		{name: "empty", in: "", cur: "EUR", wantErr: `invalid_amount(Text=)`},
		{name: "letters", in: "abc", cur: "EUR", wantErr: `invalid_amount(Text=abc)`},
		{name: "only a sign", in: "-", cur: "EUR", wantErr: `invalid_amount(Text=-)`},
		{name: "two signs", in: "--5", cur: "EUR", wantErr: `invalid_amount(Text=--5)`},
		{name: "ambiguous comma", in: "1,234", cur: "EUR", wantErr: `invalid_amount_places(Currency=EUR, Max=2, Text=1,234)`},
		{name: "too many decimals", in: "12.345", cur: "EUR", wantErr: `invalid_amount_places(Currency=EUR, Max=2, Text=12.345)`},
		{name: "two decimal separators", in: "1,2,3", cur: "EUR", sep: ',', wantErr: `invalid_amount_decimal_separators(Text=1,2,3)`},
		{name: "inconsistent grouping", in: "12.5.5", cur: "EUR", wantErr: `invalid_amount_thousands_mixed(Text=12.5.5)`},
		{name: "dot as thousands must group in threes", in: "12.5", cur: "EUR", sep: ',', wantErr: `invalid_amount_thousands_mixed(Text=12.5)`},
		{name: "four digits after a thousands separator", in: "1.2345,00", cur: "EUR", sep: ',', wantErr: `invalid_amount_thousands_mixed(Text=1.2345,00)`},
		{name: "decimals in a currency without any", in: "1.50", cur: "JPY", wantErr: `invalid_amount_places(Currency=JPY, Max=0, Text=1.50)`},
		{name: "overflow", in: "99999999999999999999", cur: "EUR", wantErr: `amount_too_large(Text=99999999999999999999)`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in, tc.cur and tc.sep

			// when
			got, err := ParseAmount(tc.in, tc.cur, tc.sep)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("amount mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Money_Decimal(t *testing.T) {
	tcs := []struct {
		name string
		m    Money
		want string
	}{
		{name: "negative", m: Money{-1250, "EUR"}, want: "-12.50"},
		{name: "cents only", m: Money{5, "EUR"}, want: "0.05"},
		{name: "negative cents only", m: Money{-5, "USD"}, want: "-0.05"},
		{name: "zero", m: Money{0, "MXN"}, want: "0.00"},
		{name: "no decimals", m: Money{1234, "JPY"}, want: "1234"},
		{name: "three decimals", m: Money{1500, "KWD"}, want: "1.500"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.m

			// when
			got := tc.m.Decimal()

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Money_String(t *testing.T) {
	tcs := []struct {
		name string
		m    Money
		want string
	}{
		{name: "negative", m: Money{-1250, "EUR"}, want: "-12.50 EUR"},
		{name: "positive", m: Money{5, "USD"}, want: "0.05 USD"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.m

			// when
			got := tc.m.String()

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Money_Add(t *testing.T) {
	tcs := []struct {
		name    string
		a, b    Money
		want    Money
		wantErr string
	}{
		{name: "same currency", a: Money{100, "EUR"}, b: Money{-30, "EUR"}, want: Money{70, "EUR"}},
		{name: "different currencies", a: Money{1, "EUR"}, b: Money{1, "USD"}, wantErr: "currency_mismatch(First=EUR, Second=USD)"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.a and tc.b

			// when
			got, err := tc.a.Add(tc.b)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("sum mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Convert(t *testing.T) {
	tcs := []struct {
		name string
		m    Money
		to   Currency
		rate string
		want Money
	}{
		{name: "euro to dollar", m: Money{10000, "EUR"}, to: "USD", rate: "1.0850", want: Money{10850, "USD"}},
		{name: "half rounds away from zero", m: Money{1, "EUR"}, to: "USD", rate: "0.5", want: Money{1, "USD"}},
		{name: "half rounds away from zero when negative", m: Money{-1, "EUR"}, to: "USD", rate: "0.5", want: Money{-1, "USD"}},
		{name: "below half rounds down", m: Money{1, "EUR"}, to: "USD", rate: "0.49", want: Money{0, "USD"}},
		{name: "into a currency without decimals", m: Money{1000, "EUR"}, to: "JPY", rate: "160", want: Money{1600, "JPY"}},
		{name: "between two decimal currencies", m: Money{1000, "USD"}, to: "MXN", rate: "18.5", want: Money{18500, "MXN"}},
		{name: "same currency ignores the rate", m: Money{42, "EUR"}, to: "EUR", rate: "2", want: Money{42, "EUR"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			rate := mustRate(t, tc.rate)

			// when
			got := Convert(tc.m, tc.to, rate)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ParseRate(t *testing.T) {
	type parsed struct {
		Rat        string // the exact value, as a fraction
		Normalized string
	}
	tcs := []struct {
		name    string
		in      string
		want    parsed
		wantErr string
	}{
		{name: "comma and spaces", in: " 1,0850 ", want: parsed{"217/200", "1.0850"}},
		{name: "plain", in: "0.92", want: parsed{"23/25", "0.92"}},
		{name: "integer", in: "160", want: parsed{"160", "160"}},
		{name: "empty", in: "", wantErr: `invalid_rate(Text=)`},
		{name: "zero", in: "0", wantErr: `rate_not_positive(Text=0)`},
		{name: "negative", in: "-1", wantErr: `invalid_rate(Text=-1)`},
		{name: "letters", in: "abc", wantErr: `invalid_rate(Text=abc)`},
		{name: "two dots", in: "1.2.3", wantErr: `invalid_rate(Text=1.2.3)`},
		{name: "exponent", in: "1e3", wantErr: `invalid_rate(Text=1e3)`},
		{name: "only a dot", in: ".", wantErr: `invalid_rate(Text=.)`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			rat, normalized, err := ParseRate(tc.in)

			// then
			var got parsed
			if rat != nil {
				got = parsed{rat.RatString(), normalized}
			}
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("rate mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func testRateBook(t *testing.T) *RateBook {
	t.Helper()
	return NewRateBook([]RateEntry{
		{"2026-01-01", "USD", "EUR", mustRate(t, "0.90")},
		{"2026-10-01", "USD", "EUR", mustRate(t, "0.92")},
		{"2026-09-01", "MXN", "EUR", mustRate(t, "0.05")},
	})
}

func Test_RateBook_Lookup(t *testing.T) {
	type found struct {
		Rat string // as a fraction, "" when there is none
		OK  bool
	}
	tcs := []struct {
		name     string
		from, to Currency
		date     string
		want     found
	}{
		{name: "latest entry on or before", from: "USD", to: "EUR", date: "2026-10-21", want: found{"23/25", true}},
		{name: "an older entry applies earlier", from: "USD", to: "EUR", date: "2026-06-01", want: found{"9/10", true}},
		{name: "before any entry", from: "USD", to: "EUR", date: "2025-12-31", want: found{}},
		{name: "inverse", from: "EUR", to: "USD", date: "2026-10-21", want: found{"25/23", true}},
		{name: "via the euro", from: "USD", to: "MXN", date: "2026-10-21", want: found{"92/5", true}},
		{name: "and back", from: "MXN", to: "USD", date: "2026-10-21", want: found{"5/92", true}},
		{name: "identity needs no entry", from: "USD", to: "USD", date: "2020-01-01", want: found{"1", true}},
		{name: "unknown currency", from: "USD", to: "GBP", date: "2026-10-21", want: found{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			book := testRateBook(t)

			// when
			rat, ok := book.Lookup(tc.from, tc.to, tc.date)

			// then
			got := found{OK: ok}
			if ok {
				got.Rat = rat.RatString()
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_RateBook_Convert(t *testing.T) {
	type converted struct {
		Money Money
		OK    bool
	}
	tcs := []struct {
		name string
		m    Money
		to   Currency
		date string
		want converted
	}{
		{name: "with the latest rate", m: Money{10000, "USD"}, to: "EUR", date: "2026-10-21", want: converted{Money{9200, "EUR"}, true}},
		{name: "with an older rate", m: Money{10000, "USD"}, to: "EUR", date: "2026-06-01", want: converted{Money{9000, "EUR"}, true}},
		{name: "no rate known", m: Money{10000, "USD"}, to: "GBP", date: "2026-10-21", want: converted{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			book := testRateBook(t)

			// when
			m, ok := book.Convert(tc.m, tc.to, tc.date)

			// then
			if diff := cmp.Diff(tc.want, converted{m, ok}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_CheckBalanced(t *testing.T) {
	tcs := []struct {
		name     string
		postings []Posting
		wantErr  string
	}{
		{
			name:     "an expense",
			postings: []Posting{{"Assets:Bank", Money{-1250, "EUR"}}, {"Expenses:Food", Money{1250, "EUR"}}},
		},
		{
			name: "a conversion balances each currency on its own",
			postings: []Posting{
				{"Assets:Bank", Money{-10000, "EUR"}}, {"Equity:Conversions", Money{10000, "EUR"}},
				{"Equity:Conversions", Money{-10850, "USD"}}, {"Assets:Cash", Money{10850, "USD"}},
			},
		},
		{
			name:     "a split",
			postings: []Posting{{"Assets:Bank", Money{-1000, "EUR"}}, {"Expenses:A", Money{600, "EUR"}}, {"Expenses:B", Money{400, "EUR"}}},
		},
		{
			name:     "a bank verification charge of zero",
			postings: []Posting{{"Assets:Bank", Money{0, "EUR"}}, {"Expenses:Uncategorized", Money{0, "EUR"}}},
		},
		{
			name:     "one posting",
			postings: []Posting{{"Assets:Bank", Money{100, "EUR"}}},
			wantErr:  "too_few_postings",
		},
		{
			name:     "unbalanced",
			postings: []Posting{{"Assets:Bank", Money{-1250, "EUR"}}, {"Expenses:Food", Money{1200, "EUR"}}},
			wantErr:  "postings_unbalanced(Off=EUR -0.50)",
		},
		{
			name:     "mixed currencies without a conversion",
			postings: []Posting{{"Assets:Bank", Money{-1000, "EUR"}}, {"Assets:Cash", Money{1000, "USD"}}},
			wantErr:  "postings_unbalanced(Off=EUR -10.00, USD 10.00)",
		},
		{
			name:     "no currency",
			postings: []Posting{{"Assets:Bank", Money{-1, ""}}, {"Expenses:Food", Money{1, ""}}},
			wantErr:  "posting_no_currency(Account=Assets:Bank)",
		},
		{
			name:     "no account",
			postings: []Posting{{"", Money{-1, "EUR"}}, {"Expenses:Food", Money{1, "EUR"}}},
			wantErr:  "posting_without_account",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.postings

			// when
			err := CheckBalanced(tc.postings)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ValidateAccountName(t *testing.T) {
	tcs := []struct {
		name    string
		in      string
		wantErr string
	}{
		{name: "a bank", in: "Assets:Sparkasse"},
		{name: "a dash", in: "Assets:Cash-USD"},
		{name: "an underscore and a sub-account", in: "Expenses:Groceries:Bio_Markt"},
		{name: "equity", in: "Equity:Conversions"},
		{name: "income", in: "Income:Gehalt"},
		{name: "umlauts", in: "Expenses:Bäckerei"},

		{name: "empty", in: "", wantErr: "invalid_account_type(Name=)"},
		{name: "no type", in: "Sparkasse", wantErr: "invalid_account_type(Name=Sparkasse)"},
		{name: "lower case type", in: "assets:bank", wantErr: "invalid_account_type(Name=assets:bank)"},
		{name: "empty last segment", in: "Assets:", wantErr: `account_empty_segment(Name=Assets:)`},
		{name: "empty middle segment", in: "Assets::Bank", wantErr: `account_empty_segment(Name=Assets::Bank)`},
		{name: "a space", in: "Assets:Bank Account", wantErr: `account_invalid_char(Char= , Name=Assets:Bank Account)`},
		{name: "an ampersand", in: "Expenses:Food&Drink", wantErr: `account_invalid_char(Char=&, Name=Expenses:Food&Drink)`},
		{name: "a leading dash", in: "Assets:-Bank", wantErr: `account_invalid_char(Char=-, Name=Assets:-Bank)`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			err := ValidateAccountName(tc.in)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_TypeOf(t *testing.T) {
	tcs := []struct {
		name    string
		in      string
		want    AccountType
		wantErr string
	}{
		{name: "liabilities", in: "Liabilities:Visa", want: Liabilities},
		{name: "assets", in: "Assets:Bank", want: Assets},
		{name: "a bare type", in: "Expenses", want: Expenses},
		{name: "unknown type", in: "Visa:x", wantErr: `invalid_account_type(Name=Visa:x)`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			got, err := TypeOf(tc.in)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("type mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_AccountType_NeedsCurrency(t *testing.T) {
	tcs := []struct {
		typ  AccountType
		want bool
	}{
		{typ: Assets, want: true},
		{typ: Liabilities, want: true},
		{typ: Income, want: false},
		{typ: Expenses, want: false}, // expense accounts must be multi-currency
		{typ: Equity, want: false},
	}
	for _, tc := range tcs {
		t.Run(string(tc.typ), func(t *testing.T) {
			// given tc.typ

			// when
			got := tc.typ.NeedsCurrency()

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ParseDate(t *testing.T) {
	tcs := []struct {
		name    string
		in      string
		want    string // the day, as YYYY-MM-DD
		wantErr string
	}{
		{name: "iso", in: "2026-10-21", want: "2026-10-21"},
		{name: "german", in: "21.10.2026", want: "2026-10-21"},
		{name: "surrounding spaces", in: " 2026-10-21 ", want: "2026-10-21"},
		{name: "empty", in: "", wantErr: `invalid_date(Formats=YYYY-MM-DD or DD.MM.YYYY, Text=)`},
		{name: "slashes", in: "21/10/2026", wantErr: `invalid_date(Formats=YYYY-MM-DD or DD.MM.YYYY, Text=21/10/2026)`},
		{name: "month 13", in: "2026-13-01", wantErr: `invalid_date(Formats=YYYY-MM-DD or DD.MM.YYYY, Text=2026-13-01)`},
		{name: "a word", in: "yesterday", wantErr: `invalid_date(Formats=YYYY-MM-DD or DD.MM.YYYY, Text=yesterday)`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			d, err := ParseDate(tc.in)

			// then
			got := ""
			if err == nil {
				got = d.Format(DateLayout)
			}
			if diff := cmp.Diff(tc.wantErr, errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("date mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
