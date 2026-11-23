package ledger

import (
	"math/big"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// fuzzCurrencies are currencies with each number of decimal places there is: 2, 0 and 3.
var fuzzCurrencies = []Currency{"EUR", "JPY", "KWD"}

// fuzzSeparators are what ParseAmount takes as its decimal separator.
var fuzzSeparators = []byte{0, '.', ','}

// Fuzz_ParseAmount feeds ParseAmount any text. Whatever it accepts must be the amount it
// writes back: written plainly (Decimal), and with a comma for the point, it reads as the
// same amount, in the currency it was given.
//
// make fuzz runs it for longer; go test runs the seeds.
func Fuzz_ParseAmount(f *testing.F) {
	seeds := []struct {
		in       string
		currency uint8 // index into fuzzCurrencies
		sep      uint8 // index into fuzzSeparators
	}{
		{in: "12.50"}, {in: "-12,50"}, {in: "1.234,56"}, {in: "1,234.56"}, {in: "1.234.567"}, {in: "+3"},
		{in: "−5.5"}, {in: ".5"}, {in: "12.500"}, {in: "1 000,50"}, {in: "2.450,00", sep: 2}, {in: "2,450.00", sep: 1},
		{in: "-0"}, {in: ""}, {in: "-"}, {in: "--5"}, {in: "1,234"}, {in: "12.345", currency: 2}, {in: "1.5", currency: 1},
		{in: "999999999999999999"}, {in: "9999999999999999999"}, {in: "-92233720368547758.08"}, {in: "1'234.50"},
	}
	for _, s := range seeds {
		f.Add(s.in, s.currency, s.sep)
	}
	f.Fuzz(func(t *testing.T, in string, currency, sep uint8) {
		// given
		c := fuzzCurrencies[int(currency)%len(fuzzCurrencies)]
		decimalSep := fuzzSeparators[int(sep)%len(fuzzSeparators)]

		// when
		m, err := ParseAmount(in, c, decimalSep)

		// then
		if err != nil {
			return
		}
		if m.Currency != c {
			t.Fatalf("ParseAmount(%q) is in %s, not %s", in, m.Currency, c)
		}
		if !writtenAs(in, decimalSep, m) {
			t.Fatalf("ParseAmount(%q, %q) = %s, which is not the number written", in, decimalSep, m)
		}
		plain := m.Decimal()
		for _, again := range []struct {
			text string
			sep  byte
		}{{plain, '.'}, {plain, 0}, {strings.Replace(plain, ".", ",", 1), ','}} {
			back, err := ParseAmount(again.text, c, again.sep)
			if err != nil {
				t.Fatalf("ParseAmount(%q) = %s, which does not read back as %q with %q: %v", in, m, again.text, again.sep, err)
			}
			if diff := cmp.Diff(m, back); diff != "" {
				t.Fatalf("ParseAmount(%q) = %s, read back from %q differs (-first +again):\n%s", in, m, again.text, diff)
			}
		}
	})
}

// writtenAs says whether m is the number in, read with decimalSep as the decimal
// separator and the other one dropped, or for 0 with either of the two: an account of
// what ParseAmount reads that does not share its code.
func writtenAs(in string, decimalSep byte, m Money) bool {
	seps := []byte{decimalSep}
	if decimalSep == 0 {
		seps = []byte{'.', ','}
	}
	got := new(big.Rat).SetFrac(big.NewInt(m.Minor), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(m.Currency.Exponent())), nil))
	for _, sep := range seps {
		other := ","
		if sep == ',' {
			other = "."
		}
		text := strings.NewReplacer(" ", "", "\u00a0", "", "'", "", "−", "-", other, "").Replace(strings.TrimSpace(in))
		text = strings.Replace(text, string(sep), ".", 1)
		if want, ok := new(big.Rat).SetString(text); ok && want.Cmp(got) == 0 {
			return true
		}
	}
	return false
}
