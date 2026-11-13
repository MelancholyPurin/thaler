package core

import (
	"context"
	"sort"
	"strings"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// PayeeReport is where the most money went in the months from req.From to req.To: the
// payees of the transactions that put money on Expenses accounts, the one that took
// most first. Payees are told apart without regard to case or to the spaces between
// words, and are shown as they were last written. Transactions with no payee are one
// line with an empty Name.
func (s *Local) PayeeReport(ctx context.Context, req PayeeRequest) (PayeeReport, error) {
	months, firstDay, end, err := s.monthRange(req.From, req.To)
	if err != nil {
		return PayeeReport{}, err
	}
	spends, err := s.repo.queries().expensesByTransaction(ctx, firstDay, end)
	if err != nil {
		return PayeeReport{}, err
	}
	base, err := s.BaseCurrency(ctx)
	if err != nil {
		return PayeeReport{}, err
	}
	book, err := s.rateBook(ctx)
	if err != nil {
		return PayeeReport{}, err
	}

	type tally struct {
		name   string
		amount ledger.Money
		txs    map[int64]bool
	}
	zero := ledger.Money{Currency: base}
	byKey := make(map[string]*tally)
	out := PayeeReport{Base: base, From: months[0], To: months[len(months)-1], Total: zero}
	missing := make(map[ledger.Currency]bool)
	for _, sp := range spends { // oldest first, so the last spelling seen is the newest
		amount, ok := s.convertOn(book, sp.amount, base, req.RateDate, sp.date, end)
		if !ok {
			missing[sp.amount.Currency] = true
			continue
		}
		name := strings.Join(strings.Fields(sp.payee), " ")
		key := strings.ToLower(name)
		t := byKey[key]
		if t == nil {
			t = &tally{amount: zero, txs: make(map[int64]bool)}
			byKey[key] = t
		}
		t.name = name
		t.amount.Minor += amount.Minor
		t.txs[sp.txID] = true
		out.Total.Minor += amount.Minor
	}
	for _, t := range byKey {
		out.Payees = append(out.Payees, PayeeSpending{Name: t.name, Amount: t.amount, Transactions: len(t.txs)})
	}
	sort.Slice(out.Payees, func(i, j int) bool {
		a, b := out.Payees[i], out.Payees[j]
		if a.Amount.Minor != b.Amount.Minor {
			return a.Amount.Minor > b.Amount.Minor
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name) // two names alike but for case are one payee
	})
	if req.Limit > 0 && len(out.Payees) > req.Limit {
		out.OtherPayees = len(out.Payees) - req.Limit
		out.Others = PayeeSpending{Amount: zero}
		for _, p := range out.Payees[req.Limit:] {
			out.Others.Amount.Minor += p.Amount.Minor
			out.Others.Transactions += p.Transactions
		}
		out.Payees = out.Payees[:req.Limit]
	}
	out.Missing = sortedCurrencies(missing)
	return out, nil
}

// Share is the part of the report's total that p took, in basis points; it is not known
// when the total is nothing or less (a refund overall), as for a SavingsRate.
func (r PayeeReport) Share(p PayeeSpending) SavingsRate {
	return savingsRate(p.Amount.Minor, r.Total.Minor)
}
