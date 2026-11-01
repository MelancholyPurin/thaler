package ledger

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
)

// DateLayout is the storage and display format for dates.
const DateLayout = "2006-01-02"

// ParseDate accepts YYYY-MM-DD and DD.MM.YYYY and returns midnight UTC.
func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{DateLayout, "02.01.2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, coded.New("invalid_date", map[string]any{"Text": s, "Formats": "YYYY-MM-DD or DD.MM.YYYY"})
}

// Posting is one leg of a transaction: an amount moving into (positive) or out
// of (negative) an account.
type Posting struct {
	Account string
	Amount  Money
}

// CheckBalanced enforces the double-entry rule: a transaction has at least two
// postings and the postings of every currency sum to exactly zero. An all-zero
// transaction is allowed because banks book them (card verification charges);
// interactive entry points reject a zero amount themselves.
func CheckBalanced(postings []Posting) error {
	if len(postings) < 2 {
		return coded.New("too_few_postings", nil)
	}
	sums := make(map[Currency]int64)
	for _, p := range postings {
		if p.Account == "" {
			return coded.New("posting_without_account", nil)
		}
		if p.Amount.Currency == "" {
			return coded.New("posting_no_currency", map[string]any{"Account": p.Account})
		}
		sums[p.Amount.Currency] += p.Amount.Minor
	}
	var off []string
	for cur, sum := range sums {
		if sum != 0 {
			off = append(off, fmt.Sprintf("%s is off by %s", cur, Money{Minor: sum, Currency: cur}.Decimal()))
		}
	}
	if len(off) > 0 {
		sort.Strings(off)
		return fmt.Errorf("postings do not balance: %s", strings.Join(off, ", "))
	}
	return nil
}
