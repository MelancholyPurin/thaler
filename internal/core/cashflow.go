package core

import (
	"context"
	"math/big"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// CashFlowReport is what came in and what went out in each month from req.From to
// req.To, in the base currency, and the share of the income that was kept. Income is
// the Income categories and spending the Expenses ones, as in BudgetReport, so a
// transfer between your accounts is neither. Every month of the range is there.
func (s *Local) CashFlowReport(ctx context.Context, req CashFlowRequest) (CashFlowReport, error) {
	months, firstDay, end, err := s.monthRange(req.From, req.To)
	if err != nil {
		return CashFlowReport{}, err
	}
	totals, err := s.repo.queries().categoryTotals(ctx, firstDay, end)
	if err != nil {
		return CashFlowReport{}, err
	}
	base, err := s.BaseCurrency(ctx)
	if err != nil {
		return CashFlowReport{}, err
	}
	book, err := s.rateBook(ctx)
	if err != nil {
		return CashFlowReport{}, err
	}

	zero := ledger.Money{Currency: base}
	index := make(map[string]int, len(months))
	out := CashFlowReport{Base: base, Months: make([]CashFlowMonth, len(months))}
	for i, m := range months {
		index[m] = i
		out.Months[i] = CashFlowMonth{Month: m, Income: zero, Spending: zero, Net: zero}
	}
	out.Total = CashFlowMonth{Income: zero, Spending: zero, Net: zero}
	missing := make(map[ledger.Currency]bool)
	for _, t := range totals {
		amount, ok := s.convertOn(book, t.Amount, base, req.RateDate, t.date, end)
		if !ok {
			missing[t.Amount.Currency] = true
			continue
		}
		month := &out.Months[index[t.date[:len(monthLayout)]]]
		switch t.Type {
		case ledger.Income:
			month.Income.Minor -= amount.Minor // income is stored as credits
			out.Total.Income.Minor -= amount.Minor
		case ledger.Expenses:
			month.Spending.Minor += amount.Minor
			out.Total.Spending.Minor += amount.Minor
		}
	}
	for i := range out.Months {
		out.Months[i].settle()
	}
	out.Total.settle()
	out.Missing = sortedCurrencies(missing)
	return out, nil
}

// settle works out what was kept and the share of the income it is.
func (c *CashFlowMonth) settle() {
	c.Net.Minor = c.Income.Minor - c.Spending.Minor
	c.Savings = savingsRate(c.Net.Minor, c.Income.Minor)
}

// savingsRate is net as a share of income in basis points, to the nearest one with halves
// away from zero. Without income there is no share: Known is false.
func savingsRate(net, income int64) SavingsRate {
	if income <= 0 {
		return SavingsRate{}
	}
	num := new(big.Int).Mul(big.NewInt(net), big.NewInt(10000))
	neg := num.Sign() < 0
	num.Abs(num)
	den := big.NewInt(income)
	// (2n + d) / 2d is n/d rounded to the nearest, a half going up
	num.Lsh(num, 1).Add(num, den)
	q := num.Quo(num, den.Lsh(den, 1))
	if neg {
		q.Neg(q)
	}
	return SavingsRate{BasisPoints: q.Int64(), Known: true}
}
