package ledger

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
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
	return time.Time{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD or DD.MM.YYYY", s)
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
		return errors.New("a transaction needs at least two postings")
	}
	sums := make(map[Currency]int64)
	for _, p := range postings {
		if p.Account == "" {
			return errors.New("posting without an account")
		}
		if p.Amount.Currency == "" {
			return fmt.Errorf("posting to %s has no currency", p.Account)
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
