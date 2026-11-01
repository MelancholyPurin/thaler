package ledger

import (
	"math/big"
	"sort"
	"strings"

	"github.com/melancholypurin/thaler/internal/coded"
)

// ParseRate parses an exchange rate such as "0.92" or "1,0850". It returns the
// exact rational value and a normalised decimal string suitable for storage.
func ParseRate(s string) (*big.Rat, string, error) {
	clean := strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	bad := clean == "" || clean == "." || strings.Count(clean, ".") > 1
	for _, r := range clean {
		if r != '.' && (r < '0' || r > '9') {
			bad = true
		}
	}
	if bad {
		return nil, "", coded.New("invalid_rate", map[string]any{"Text": s})
	}
	r, ok := new(big.Rat).SetString(clean)
	if !ok || r.Sign() <= 0 {
		return nil, "", coded.New("rate_not_positive", map[string]any{"Text": s})
	}
	return r, clean, nil
}

// RateEntry states that on Date one unit of Base was worth Rate units of Quote.
type RateEntry struct {
	Date  string // YYYY-MM-DD
	Base  Currency
	Quote Currency
	Rate  *big.Rat
}

// RateBook answers "what was 1 X worth in Y on date D" from a set of entries.
type RateBook struct {
	byPair     map[[2]Currency][]RateEntry // each slice sorted by date ascending
	currencies []Currency                  // every currency mentioned, sorted
}

// NewRateBook indexes entries. Entries with a missing or non-positive rate are ignored.
func NewRateBook(entries []RateEntry) *RateBook {
	b := &RateBook{byPair: make(map[[2]Currency][]RateEntry)}
	seen := make(map[Currency]bool)
	for _, e := range entries {
		if e.Rate == nil || e.Rate.Sign() <= 0 {
			continue
		}
		k := [2]Currency{e.Base, e.Quote}
		b.byPair[k] = append(b.byPair[k], e)
		seen[e.Base], seen[e.Quote] = true, true
	}
	for k, list := range b.byPair {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Date < list[j].Date })
		b.byPair[k] = list
	}
	for c := range seen {
		b.currencies = append(b.currencies, c)
	}
	sort.Slice(b.currencies, func(i, j int) bool { return b.currencies[i] < b.currencies[j] })
	return b
}

// Lookup returns how many `to` units one `from` unit is worth on date, using
// the most recent entry on or before that date. It tries, in order, a direct
// entry, the inverse of an opposite-direction entry, and a two-step route
// through one intermediate currency. The returned value must not be modified.
func (b *RateBook) Lookup(from, to Currency, date string) (*big.Rat, bool) {
	if from == to {
		return big.NewRat(1, 1), true
	}
	if r, ok := b.oneStep(from, to, date); ok {
		return r, true
	}
	for _, mid := range b.currencies {
		if mid == from || mid == to {
			continue
		}
		r1, ok1 := b.oneStep(from, mid, date)
		if !ok1 {
			continue
		}
		r2, ok2 := b.oneStep(mid, to, date)
		if !ok2 {
			continue
		}
		return new(big.Rat).Mul(r1, r2), true
	}
	return nil, false
}

// Convert converts m into currency `to` at the rate valid on date.
func (b *RateBook) Convert(m Money, to Currency, date string) (Money, bool) {
	r, ok := b.Lookup(m.Currency, to, date)
	if !ok {
		return Money{}, false
	}
	return Convert(m, to, r), true
}

func (b *RateBook) oneStep(from, to Currency, date string) (*big.Rat, bool) {
	var best *big.Rat
	bestDate := ""
	if e, ok := latestOnOrBefore(b.byPair[[2]Currency{from, to}], date); ok {
		best, bestDate = e.Rate, e.Date
	}
	if e, ok := latestOnOrBefore(b.byPair[[2]Currency{to, from}], date); ok && (best == nil || e.Date > bestDate) {
		best = new(big.Rat).Inv(e.Rate)
	}
	return best, best != nil
}

func latestOnOrBefore(list []RateEntry, date string) (RateEntry, bool) {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Date <= date {
			return list[i], true
		}
	}
	return RateEntry{}, false
}
