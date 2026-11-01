package i18n

import (
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
	"github.com/melancholypurin/thaler/internal/units"
)

// decimalSeparator is the character a language puts between the whole part of a
// number and the rest. x/text has no call for it, so a number is formatted and the
// separator read from it.
func decimalSeparator(tag language.Tag) string {
	s := message.NewPrinter(tag).Sprint(number.Decimal(1.5, number.Scale(1)))
	return strings.TrimSuffix(strings.TrimPrefix(s, "1"), "5")
}

// Amount is m written without its currency the way the language writes numbers:
// -1,234.50 in English and -1.234,50 in German and Spanish. The digits are exact: the whole part and the
// fraction are formatted apart, because a float would not hold every amount.
func (l *Localizer) Amount(m ledger.Money) string {
	exp := m.Currency.Exponent()
	u := uint64(m.Minor) //nolint:gosec // reinterpreting the bits is the point: see the negation below
	neg := m.Minor < 0
	if neg {
		u = -u // two's complement negation; also correct for math.MinInt64
	}
	div := uint64(1)
	for range exp {
		div *= 10
	}
	s := message.NewPrinter(l.lang.Tag).Sprint(number.Decimal(u / div))
	if exp > 0 {
		frac := strconv.FormatUint(u%div, 10)
		s += l.decimal + strings.Repeat("0", exp-len(frac)) + frac
	}
	if neg {
		s = "-" + s
	}
	return s
}

// Money is Amount with the currency code after it, as -1,234.50 EUR.
func (l *Localizer) Money(m ledger.Money) string {
	return l.Amount(m) + " " + string(m.Currency)
}

// Signed is Money with a + in front of a positive amount.
func (l *Localizer) Signed(m ledger.Money) string {
	if m.Minor > 0 {
		return "+" + l.Money(m)
	}
	return l.Money(m)
}

// Date is t written the way the language writes dates.
func (l *Localizer) Date(t time.Time) string { return t.Format(l.lang.DateLayout) }

// Decimal is f with exactly places digits after the decimal separator of the
// language (1.0850 or 1,0850), for a figure that is not an amount, such as an
// exchange rate.
func (l *Localizer) Decimal(f float64, places int) string {
	return message.NewPrinter(l.lang.Tag).Sprint(number.Decimal(f, number.Scale(places)))
}

// Size is a file size for reading (12.4 KB, 12,4 KB), with the decimal separator of
// the language.
func (l *Localizer) Size(n int64) string {
	return strings.Replace(units.HumanSize(n), ".", l.decimal, 1)
}

// DateTime is t with its time of day, the date written the way the language writes it
// (2026-10-01 14:30).
func (l *Localizer) DateTime(t time.Time) string { return t.Format(l.lang.DateLayout + " 15:04") }

// StoredDate is a date as the ledger stores it (YYYY-MM-DD) written the way the
// language writes dates. Text that is not a stored date is returned as it is.
func (l *Localizer) StoredDate(s string) string {
	t, err := time.Parse(ledger.DateLayout, s)
	if err != nil {
		return s
	}
	return l.Date(t)
}

// ParseDate reads a typed date: ISO (YYYY-MM-DD) and DD.MM.YYYY in every language,
// and the layout of the language besides. A date such as 01/10/2026 is read only in
// a language that says which of the numbers is the day, so it is refused in English
// instead of being read as the wrong month.
func (l *Localizer) ParseDate(s string) (time.Time, error) {
	if t, err := ledger.ParseDate(s); err == nil {
		return t, nil
	}
	s = strings.TrimSpace(s)
	for _, layout := range l.lang.DateInput {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, coded.New("invalid_date", map[string]any{"Text": s, "Formats": l.lang.DateFormats})
}
