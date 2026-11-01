package i18n

import (
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func eur(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "EUR"} }

func Test_Localizer_Amount(t *testing.T) {
	tcs := []struct {
		name string
		tag  language.Tag
		m    ledger.Money
		want string
	}{
		{name: "English, thousands and cents", tag: language.English, m: eur(123456), want: "1,234.56"},
		{name: "German", tag: language.German, m: eur(123456), want: "1.234,56"},
		{name: "Spanish", tag: language.Spanish, m: eur(123456), want: "1.234,56"},
		{name: "Spanish, five digits", tag: language.Spanish, m: eur(1234567), want: "12.345,67"},
		{name: "a million in English", tag: language.English, m: eur(123456789), want: "1,234,567.89"},
		{name: "a million in German", tag: language.German, m: eur(123456789), want: "1.234.567,89"},
		{name: "negative", tag: language.German, m: eur(-123456), want: "-1.234,56"},
		{name: "zero", tag: language.English, m: eur(0), want: "0.00"},
		{name: "cents only", tag: language.German, m: eur(5), want: "0,05"},
		{name: "cents only, negative", tag: language.English, m: eur(-7), want: "-0.07"},
		{name: "a currency without minor units", tag: language.English, m: ledger.Money{Minor: 1234567, Currency: "JPY"}, want: "1,234,567"},
		{name: "three decimals", tag: language.German, m: ledger.Money{Minor: 1234567, Currency: "KWD"}, want: "1.234,567"},
		{name: "every digit of the largest amount", tag: language.English, m: eur(math.MaxInt64), want: "92,233,720,368,547,758.07"},
		{name: "every digit of the smallest amount", tag: language.English, m: eur(math.MinInt64), want: "-92,233,720,368,547,758.08"},
		{name: "a region of German", tag: language.MustParse("de-AT"), m: eur(123456), want: "1.234,56"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got := loc.Amount(tc.m)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Localizer_Money(t *testing.T) {
	tcs := []struct {
		name       string
		tag        language.Tag
		m          ledger.Money
		wantMoney  string
		wantSigned string
	}{
		{name: "income in English", tag: language.English, m: eur(245000), wantMoney: "2,450.00 EUR", wantSigned: "+2,450.00 EUR"},
		{name: "an expense in German", tag: language.German, m: eur(-1250), wantMoney: "-12,50 EUR", wantSigned: "-12,50 EUR"},
		{name: "zero has no sign", tag: language.English, m: eur(0), wantMoney: "0.00 EUR", wantSigned: "0.00 EUR"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			money, signed := loc.Money(tc.m), loc.Signed(tc.m)

			// then
			if diff := cmp.Diff([]string{tc.wantMoney, tc.wantSigned}, []string{money, signed}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Localizer_Date(t *testing.T) {
	day := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	tcs := []struct {
		name       string
		tag        language.Tag
		wantDate   string
		wantStored string // StoredDate("2026-10-01")
	}{
		{name: "English stays ISO", tag: language.English, wantDate: "2026-10-01", wantStored: "2026-10-01"},
		{name: "Spanish, day first with slashes", tag: language.Spanish, wantDate: "01/10/2026", wantStored: "01/10/2026"},
		{name: "German, day first with dots", tag: language.German, wantDate: "01.10.2026", wantStored: "01.10.2026"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			date, stored := loc.Date(day), loc.StoredDate("2026-10-01")

			// then
			if diff := cmp.Diff([]string{tc.wantDate, tc.wantStored}, []string{date, stored}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Localizer_StoredDate(t *testing.T) {
	tcs := []struct {
		name string
		in   string
		want string
	}{
		{name: "a stored date", in: "2026-10-01", want: "01.10.2026"},
		{name: "text that is no date is left alone", in: "yesterday", want: "yesterday"},
		{name: "nothing", in: "", want: ""},
		{name: "a month is not a day", in: "2026-10", want: "2026-10"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(language.German)

			// when
			got := loc.StoredDate(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Localizer_ParseDate(t *testing.T) {
	tcs := []struct {
		name    string
		tag     language.Tag
		in      string
		want    string // YYYY-MM-DD
		wantErr string
	}{
		{name: "ISO in English", tag: language.English, in: "2026-10-01", want: "2026-10-01"},
		{name: "DD.MM.YYYY in English", tag: language.English, in: "01.10.2026", want: "2026-10-01"},
		{name: "slashes are ambiguous in English", tag: language.English, in: "01/10/2026", wantErr: `invalid date "01/10/2026": use YYYY-MM-DD or DD.MM.YYYY`},
		{name: "a year first with slashes is refused too", tag: language.English, in: "2026/10/01", wantErr: `invalid date "2026/10/01": use YYYY-MM-DD or DD.MM.YYYY`},
		{name: "Spanish reads day first", tag: language.Spanish, in: "01/10/2026", want: "2026-10-01"},
		{name: "Spanish with one-digit day and month", tag: language.Spanish, in: "1/2/2026", want: "2026-02-01"},
		{name: "Spanish still reads ISO", tag: language.Spanish, in: "2026-10-01", want: "2026-10-01"},
		{name: "Spanish still reads dots", tag: language.Spanish, in: "01.10.2026", want: "2026-10-01"},
		{name: "a day that does not exist", tag: language.Spanish, in: "31/02/2026", wantErr: `fecha inválida "31/02/2026": usa DD/MM/YYYY, YYYY-MM-DD o DD.MM.YYYY`},
		{name: "German with one-digit day and month", tag: language.German, in: "1.2.2026", want: "2026-02-01"},
		{name: "German does not read slashes", tag: language.German, in: "01/10/2026", wantErr: `ungültiges Datum "01/10/2026": verwende DD.MM.YYYY oder YYYY-MM-DD`},
		{name: "spaces around it", tag: language.German, in: "  01.10.2026 ", want: "2026-10-01"},
		{name: "nothing", tag: language.English, in: "", wantErr: `invalid date "": use YYYY-MM-DD or DD.MM.YYYY`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got, err := loc.ParseDate(tc.in)

			// then
			gotErr, gotDay := "", ""
			if err != nil {
				gotErr = loc.Error(err)
			} else {
				gotDay = got.Format("2006-01-02")
			}
			if diff := cmp.Diff(tc.wantErr, gotErr); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, gotDay); diff != "" {
				t.Errorf("date mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A date with its time, a figure with decimals, a month with its year and a file
// size, each the way the language writes it.
func Test_Localizer_DateTime_Decimal_MonthYear_Size(t *testing.T) {
	at := time.Date(2026, time.October, 1, 14, 5, 0, 0, time.UTC)
	tcs := []struct {
		name          string
		tag           language.Tag
		wantDateTime  string
		wantDecimal   string // Decimal(1.085, 4)
		wantMonthYear string
		wantSize      string // Size(12700)
	}{
		{name: "English", tag: language.English, wantDateTime: "2026-10-01 14:05", wantDecimal: "1.0850", wantMonthYear: "Oct 2026", wantSize: "12.4 KB"},
		{name: "Spanish", tag: language.Spanish, wantDateTime: "01/10/2026 14:05", wantDecimal: "1,0850", wantMonthYear: "oct 2026", wantSize: "12,4 KB"},
		{name: "German", tag: language.German, wantDateTime: "01.10.2026 14:05", wantDecimal: "1,0850", wantMonthYear: "Okt 2026", wantSize: "12,4 KB"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got := []string{loc.DateTime(at), loc.Decimal(1.085, 4), loc.MonthYear(at), loc.Size(12700)}

			// then
			want := []string{tc.wantDateTime, tc.wantDecimal, tc.wantMonthYear, tc.wantSize}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Every month has a name in English, so that none comes out as its id.
func Test_Localizer_MonthYear_everyMonth(t *testing.T) {
	tcs := []struct {
		name  string
		month time.Month
		want  string
	}{
		{name: "January", month: time.January, want: "Jan 2026"},
		{name: "February", month: time.February, want: "Feb 2026"},
		{name: "March", month: time.March, want: "Mar 2026"},
		{name: "April", month: time.April, want: "Apr 2026"},
		{name: "May", month: time.May, want: "May 2026"},
		{name: "June", month: time.June, want: "Jun 2026"},
		{name: "July", month: time.July, want: "Jul 2026"},
		{name: "August", month: time.August, want: "Aug 2026"},
		{name: "September", month: time.September, want: "Sep 2026"},
		{name: "October", month: time.October, want: "Oct 2026"},
		{name: "November", month: time.November, want: "Nov 2026"},
		{name: "December", month: time.December, want: "Dec 2026"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(language.English)

			// when
			got := loc.MonthYear(time.Date(2026, tc.month, 15, 0, 0, 0, 0, time.UTC))

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
