package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Deduper derives stable keys for imported rows so that importing an
// overlapping export a second time never creates duplicates.
//
// The key hashes the account, booking date, amount, currency, counterparty,
// IBAN, purpose and booking text. Two genuinely identical bookings on the same
// day (two coffees at the same bakery) get different keys because the n-th
// identical row within one file is numbered. That numbering is stable as long
// as each export covers whole days, which is how bank exports work.
type Deduper struct {
	account string
	seen    map[string]int
}

// NewDeduper creates a Deduper for one import run. accountKey should be a
// stable identifier of the target account, such as its database id.
func NewDeduper(accountKey string) *Deduper {
	return &Deduper{account: accountKey, seen: make(map[string]int)}
}

// Key returns the dedupe key for r. Call it once per row, in file order.
func (d *Deduper) Key(r Row) string {
	base := strings.Join([]string{
		d.account,
		r.Date.Format("2006-01-02"),
		strconv.FormatInt(r.Amount.Minor, 10),
		string(r.Amount.Currency),
		norm(r.Payee),
		norm(r.IBAN),
		norm(r.Purpose),
		norm(r.BookingText),
	}, "\x1f")
	n := d.seen[base]
	d.seen[base] = n + 1
	sum := sha256.Sum256([]byte(base + "\x1f" + strconv.Itoa(n)))
	return hex.EncodeToString(sum[:])
}

func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
