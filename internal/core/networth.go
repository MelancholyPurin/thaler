package core

import (
	"context"
	"time"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// NetWorthReport is the net worth at the end of each month from req.From to req.To, in the
// base currency. Each account's balance at the end of the month is converted at the
// rate of that day (the latest on or before it, never later than req.RateDate), so the
// figure of a past month stays when a rate is entered afterwards; a day before the
// first rate of a currency uses the rate of the end of the range, as in the flow
// reports. It is what BalanceReport gives for the last day of the month.
func (s *Local) NetWorthReport(ctx context.Context, req NetWorthRequest) (NetWorthReport, error) {
	months, firstDay, end, err := s.monthRange(req.From, req.To)
	if err != nil {
		return NetWorthReport{}, err
	}
	moves, err := s.repo.queries().assetMovements(ctx, firstDay, end)
	if err != nil {
		return NetWorthReport{}, err
	}
	base, err := s.BaseCurrency(ctx)
	if err != nil {
		return NetWorthReport{}, err
	}
	book, err := s.rateBook(ctx)
	if err != nil {
		return NetWorthReport{}, err
	}

	// For each account and currency: the balance before the range, and what each month
	// moved it by; the balance at the end of a month is the two added up to there.
	type key struct {
		account string
		cur     ledger.Currency
	}
	index := make(map[string]int, len(months))
	for i, m := range months {
		index[m] = i
	}
	opening := make(map[key]int64)
	moved := make(map[key][]int64)
	for _, mv := range moves {
		k := key{mv.account, mv.amount.Currency}
		if moved[k] == nil {
			moved[k] = make([]int64, len(months))
		}
		if mv.month == "" {
			opening[k] += mv.amount.Minor
			continue
		}
		moved[k][index[mv.month]] += mv.amount.Minor
	}

	zero := ledger.Money{Currency: base}
	out := NetWorthReport{Base: base, Months: make([]NetWorthMonth, len(months))}
	for i, m := range months {
		out.Months[i] = NetWorthMonth{Month: m, Total: zero}
	}
	missing := make(map[ledger.Currency]bool)
	for k, movement := range moved {
		balance := opening[k]
		for i, v := range movement {
			balance += v
			if balance == 0 {
				continue
			}
			amount, ok := s.convertOn(book, ledger.Money{Minor: balance, Currency: k.cur}, base, req.RateDate, lastDayOf(months[i]), end)
			if !ok {
				missing[k.cur] = true
				continue
			}
			out.Months[i].Total.Minor += amount.Minor
		}
	}
	out.Missing = sortedCurrencies(missing)
	return out, nil
}

// lastDayOf is the last day of a month (YYYY-MM) as YYYY-MM-DD; month is one monthRange made.
func lastDayOf(month string) string {
	first, _ := time.Parse(monthLayout, month)
	return first.AddDate(0, 1, -1).Format(ledger.DateLayout)
}
