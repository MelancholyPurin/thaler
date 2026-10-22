// Package ledger contains the pure domain logic of thaler: money, account
// naming, double-entry validation and currency conversion. It performs no I/O
// and imports only the standard library, so it is cheap to test.
package ledger

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Currency is an upper-case ISO 4217 style code such as EUR, USD or MXN.
type Currency string

// minorExponent lists currencies whose minor unit is not 1/100. Any currency
// not listed here is assumed to have two decimal places.
var minorExponent = map[Currency]int{
	"JPY": 0, "KRW": 0, "CLP": 0, "VND": 0, "ISK": 0, "UGX": 0, "PYG": 0,
	"BHD": 3, "KWD": 3, "JOD": 3, "OMR": 3, "TND": 3,
}

// Exponent returns the number of decimal places of the currency's minor unit.
func (c Currency) Exponent() int {
	if e, ok := minorExponent[c]; ok {
		return e
	}
	return 2
}

// ParseCurrency validates and normalises a currency code.
func ParseCurrency(s string) (Currency, error) {
	up := strings.ToUpper(strings.TrimSpace(s))
	ok := len(up) == 3
	for _, r := range up {
		if r < 'A' || r > 'Z' {
			ok = false
		}
	}
	if !ok {
		return "", fmt.Errorf("invalid currency code %q: want three letters such as EUR, USD or MXN", s)
	}
	return Currency(up), nil
}

// Money is an amount in the minor unit of a currency (cents for EUR/USD/MXN).
// Floating point is never used for money.
type Money struct {
	Minor    int64
	Currency Currency
}

// ErrCurrencyMismatch is returned when amounts of different currencies are mixed.
var ErrCurrencyMismatch = errors.New("currency mismatch")

// Add returns m+o. Both must have the same currency.
func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.Currency, o.Currency)
	}
	return Money{Minor: m.Minor + o.Minor, Currency: m.Currency}, nil
}

// Neg returns the amount with its sign flipped.
func (m Money) Neg() Money { return Money{Minor: -m.Minor, Currency: m.Currency} }

// IsZero reports whether the amount is zero.
func (m Money) IsZero() bool { return m.Minor == 0 }

// Decimal formats the amount without currency, e.g. "-1234.50".
func (m Money) Decimal() string {
	exp := m.Currency.Exponent()
	neg := m.Minor < 0
	u := uint64(m.Minor)
	if neg {
		u = -u // two's complement negation; also correct for math.MinInt64
	}
	s := strconv.FormatUint(u, 10)
	if exp > 0 {
		for len(s) <= exp {
			s = "0" + s
		}
		s = s[:len(s)-exp] + "." + s[len(s)-exp:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// String formats the amount with its currency, e.g. "-1234.50 EUR".
func (m Money) String() string { return m.Decimal() + " " + string(m.Currency) }

// ParseAmount parses a human-written decimal amount in currency c.
//
// decimalSep may be '.' or ',' to force that character as the decimal
// separator (the other one is then a thousands separator), or 0 to detect it:
// when both characters occur the last one is the decimal separator, and a
// single separator character is treated as the decimal separator. Ambiguous
// input such as "1,234" therefore fails for two-decimal currencies instead of
// being silently misread. Thousands separators must group digits in threes.
func ParseAmount(s string, c Currency, decimalSep byte) (Money, error) {
	clean := strings.NewReplacer(" ", "", " ", "", "'", "", "−", "-").Replace(strings.TrimSpace(s))
	neg := false
	if clean != "" {
		switch clean[0] {
		case '-':
			neg = true
			clean = clean[1:]
		case '+':
			clean = clean[1:]
		}
	}
	if clean == "" {
		return Money{}, fmt.Errorf("invalid amount %q", s)
	}

	sep := decimalSep
	if sep == 0 {
		sep = guessDecimalSep(clean)
	}

	var left, right string
	var thousands byte
	if sep == 0 {
		// No decimal part: any separators are thousands separators.
		left = clean
		if i := strings.IndexAny(left, ".,"); i >= 0 {
			thousands = left[i]
		}
	} else {
		left, right, _ = strings.Cut(clean, string(sep))
		if strings.IndexByte(right, sep) >= 0 {
			return Money{}, fmt.Errorf("invalid amount %q: more than one decimal separator", s)
		}
		other := byte('.')
		if sep == '.' {
			other = ','
		}
		if strings.IndexByte(right, other) >= 0 {
			return Money{}, fmt.Errorf("invalid amount %q: thousands separator after the decimal separator", s)
		}
		if strings.IndexByte(left, other) >= 0 {
			thousands = other
		}
	}
	if thousands != 0 {
		parts := strings.Split(left, string(thousands))
		if !validGroups(parts) {
			return Money{}, fmt.Errorf("invalid amount %q: inconsistent thousands separators", s)
		}
		left = strings.Join(parts, "")
	}
	if !allDigits(left) || !allDigits(right) || (left == "" && right == "") {
		return Money{}, fmt.Errorf("invalid amount %q", s)
	}

	exp := c.Exponent()
	for len(right) > exp && strings.HasSuffix(right, "0") {
		right = right[:len(right)-1]
	}
	if len(right) > exp {
		return Money{}, fmt.Errorf("invalid amount %q: %s has at most %d decimal places", s, c, exp)
	}
	right += strings.Repeat("0", exp-len(right))

	digits := strings.TrimLeft(left+right, "0")
	if digits == "" {
		digits = "0"
	}
	if len(digits) > 18 {
		return Money{}, fmt.Errorf("amount %q is too large", s)
	}
	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("invalid amount %q", s)
	}
	if neg {
		v = -v
	}
	return Money{Minor: v, Currency: c}, nil
}

func guessDecimalSep(s string) byte {
	dot, comma := strings.LastIndexByte(s, '.'), strings.LastIndexByte(s, ',')
	switch {
	case dot >= 0 && comma >= 0:
		if dot > comma {
			return '.'
		}
		return ','
	case dot >= 0 && strings.Count(s, ".") == 1:
		return '.'
	case comma >= 0 && strings.Count(s, ",") == 1:
		return ','
	}
	return 0
}

func validGroups(parts []string) bool {
	if len(parts[0]) < 1 || len(parts[0]) > 3 {
		return false
	}
	for _, p := range parts[1:] {
		if len(p) != 3 {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Convert converts m into currency `to`. rate is the number of `to` units that
// one unit of m.Currency is worth. The result is rounded to the minor unit of
// `to`, with halves rounded away from zero.
func Convert(m Money, to Currency, rate *big.Rat) Money {
	if m.Currency == to {
		return m
	}
	r := new(big.Rat).SetInt64(m.Minor)
	r.Mul(r, rate)
	r.Mul(r, new(big.Rat).SetFrac(pow10(to.Exponent()), pow10(m.Currency.Exponent())))
	return Money{Minor: roundHalfAwayFromZero(r), Currency: to}
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

func roundHalfAwayFromZero(r *big.Rat) int64 {
	num := new(big.Int).Set(r.Num())
	den := r.Denom()
	neg := num.Sign() < 0
	num.Abs(num)
	// floor((2*num + den) / (2*den)) == round(num/den) with halves rounded up.
	num.Lsh(num, 1)
	num.Add(num, den)
	q := num.Quo(num, new(big.Int).Lsh(den, 1))
	v := q.Int64()
	if neg {
		v = -v
	}
	return v
}
