package core

import (
	"context"
	"sort"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// spendingMonthsDefault is how many months the report covers when it is not told
// where to start, and spendingMonthsMax the most it will cover.
const (
	spendingMonthsDefault = 12
	spendingMonthsMax     = 120
)

// SpendingReport is what each Expenses category spent in each month from req.From to
// req.To, in the base currency. Every month of the range is there, one without
// spending too, so that the columns of the categories line up.
func (s *Local) SpendingReport(ctx context.Context, req SpendingRequest) (SpendingReport, error) {
	to := req.To
	if to == "" {
		to = s.today()[:len(monthLayout)]
	}
	last, err := time.Parse(monthLayout, to)
	if err != nil {
		return SpendingReport{}, coded.New("invalid_month", map[string]any{"Text": to})
	}
	first := last.AddDate(0, -(spendingMonthsDefault - 1), 0)
	if req.From != "" {
		if first, err = time.Parse(monthLayout, req.From); err != nil {
			return SpendingReport{}, coded.New("invalid_month", map[string]any{"Text": req.From})
		}
	}
	if first.After(last) {
		return SpendingReport{}, coded.New("spending_backwards", map[string]any{"From": first.Format(monthLayout), "To": to})
	}
	var months []string
	for m := first; !m.After(last); m = m.AddDate(0, 1, 0) {
		if len(months) == spendingMonthsMax {
			return SpendingReport{}, coded.New("spending_too_long", map[string]any{"Max": spendingMonthsMax})
		}
		months = append(months, m.Format(monthLayout))
	}
	end := last.AddDate(0, 1, -1).Format(ledger.DateLayout)

	totals, err := s.repo.queries().categoryTotals(ctx, first.Format(ledger.DateLayout), end)
	if err != nil {
		return SpendingReport{}, err
	}
	base, err := s.BaseCurrency(ctx)
	if err != nil {
		return SpendingReport{}, err
	}
	book, err := s.rateBook(ctx)
	if err != nil {
		return SpendingReport{}, err
	}

	zero := ledger.Money{Currency: base}
	index := make(map[string]int, len(months))
	for i, m := range months {
		index[m] = i
	}
	out := SpendingReport{Base: base, Months: months, Totals: make([]ledger.Money, len(months)), Total: zero}
	for i := range out.Totals {
		out.Totals[i] = zero
	}
	byName := make(map[string]*CategorySpending)
	missing := make(map[ledger.Currency]bool)
	for _, t := range totals {
		if t.Type != ledger.Expenses {
			continue
		}
		amount, ok := s.convertOn(book, t.Amount, base, req.RateDate, t.date, end)
		if !ok {
			missing[t.Amount.Currency] = true
			continue
		}
		line := byName[t.Account]
		if line == nil {
			line = &CategorySpending{Name: t.Account, Months: make([]ledger.Money, len(months)), Total: zero}
			for i := range line.Months {
				line.Months[i] = zero
			}
			byName[t.Account] = line
		}
		i := index[t.date[:len(monthLayout)]]
		line.Months[i].Minor += amount.Minor
		line.Total.Minor += amount.Minor
		out.Totals[i].Minor += amount.Minor
		out.Total.Minor += amount.Minor
	}
	for _, line := range byName {
		out.Categories = append(out.Categories, *line)
	}
	sort.Slice(out.Categories, func(i, j int) bool {
		a, b := out.Categories[i], out.Categories[j]
		if a.Total.Minor != b.Total.Minor {
			return a.Total.Minor > b.Total.Minor
		}
		return a.Name < b.Name
	})
	out.Missing = sortedCurrencies(missing)
	return out, nil
}
