package store

import (
	"context"
	"sort"
	"time"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// NetWorth is the sum of every asset and liability balance converted into the
// base currency.
type NetWorth struct {
	Base  ledger.Currency
	Total ledger.Money
	// Missing lists currencies that have no usable exchange rate to Base. Their
	// accounts are left out of Total instead of being guessed.
	Missing []ledger.Currency
}

// NetWorth computes the net worth. Exchange rates are looked up on date
// (YYYY-MM-DD, empty = today); every posting counts regardless of its date.
func (s *Store) NetWorth(ctx context.Context, date string) (NetWorth, error) {
	if date == "" {
		date = time.Now().Format(ledger.DateLayout)
	}
	base, err := s.BaseCurrency(ctx)
	if err != nil {
		return NetWorth{}, err
	}
	book, err := s.RateBook(ctx)
	if err != nil {
		return NetWorth{}, err
	}
	balances, err := s.Balances(ctx, "")
	if err != nil {
		return NetWorth{}, err
	}
	nw := NetWorth{Base: base, Total: ledger.Money{Currency: base}}
	missing := make(map[ledger.Currency]bool)
	for _, b := range balances {
		if !b.Type.NeedsCurrency() {
			continue
		}
		converted, ok := book.Convert(b.Amount, base, date)
		if !ok {
			missing[b.Amount.Currency] = true
			continue
		}
		if nw.Total, err = nw.Total.Add(converted); err != nil {
			return NetWorth{}, err
		}
	}
	for c := range missing {
		nw.Missing = append(nw.Missing, c)
	}
	sort.Slice(nw.Missing, func(i, j int) bool { return nw.Missing[i] < nw.Missing[j] })
	return nw, nil
}

// CategoryTotals sums the postings on Income and Expenses accounts per account
// and currency, for transactions dated from..to (YYYY-MM-DD, inclusive; an
// empty bound is open). Expenses come out positive and income negative, the
// ledger's own sign convention.
func (s *Store) CategoryTotals(ctx context.Context, from, to string) ([]Balance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.name, a.type, p.currency, SUM(p.amount)
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		JOIN transactions t ON t.id = p.transaction_id
		WHERE a.type IN ('Income', 'Expenses')
		  AND (? = '' OR t.date >= ?)
		  AND (? = '' OR t.date <= ?)
		GROUP BY a.id, p.currency
		HAVING SUM(p.amount) <> 0
		ORDER BY a.name, p.currency`, from, from, to, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Balance
	for rows.Next() {
		var (
			name, typ, cur string
			sum            int64
		)
		if err := rows.Scan(&name, &typ, &cur, &sum); err != nil {
			return nil, err
		}
		out = append(out, Balance{
			Account: name,
			Type:    ledger.AccountType(typ),
			Amount:  ledger.Money{Minor: sum, Currency: ledger.Currency(cur)},
		})
	}
	return out, rows.Err()
}
