package core

import (
	"context"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// Split is a transaction on one account of yours divided between categories: a shop bill
// that is groceries and household goods, a salary that is pay and a bonus. It is one
// transaction, with one posting on the account and one for each line, so every report and
// the budget count each category's share. With one line it is an ordinary transaction.
type Split struct {
	Date        time.Time
	Payee       string
	Description string
	Account     string       // Assets or Liabilities
	Amount      ledger.Money // on the account: below zero for spending, above for income
	Lines       []SplitLine
}

// SplitLine is one category of a split and its share.
type SplitLine struct {
	Category string // Income or Expenses
	// Amount is the share, without a sign, in the currency of the split. Zero takes what
	// the other lines leave; one line at most may do that.
	Amount ledger.Money
}

// Split reads the transaction as a split: one posting on your own account and one or more
// on Income or Expenses accounts, in its currency, each taking from it (money out is
// spent, money in is earned). The line that takes the rest is written with its amount. ok
// is false for anything else: a transfer, an opening balance, a line that gives something
// back.
func (t TxView) Split() (Split, bool) {
	s := Split{Payee: t.Payee, Description: t.Description}
	d, err := ledger.ParseDate(t.Date)
	if err != nil {
		return Split{}, false
	}
	s.Date = d
	var cats []PostingView
	for _, p := range t.Postings {
		typ, err := ledger.TypeOf(p.Account)
		if err != nil {
			return Split{}, false
		}
		if typ.NeedsCurrency() {
			if s.Account != "" {
				return Split{}, false
			}
			s.Account, s.Amount = p.Account, p.Amount
			continue
		}
		cats = append(cats, p)
	}
	if s.Account == "" || s.Amount.Minor == 0 || len(cats) == 0 {
		return Split{}, false
	}
	for _, c := range cats {
		if !isSplitCategory(c.Account) || c.Amount.Currency != s.Amount.Currency || c.Amount.Minor == 0 || (c.Amount.Minor > 0) == (s.Amount.Minor > 0) {
			return Split{}, false
		}
		size := c.Amount
		if size.Minor < 0 {
			size = size.Neg()
		}
		s.Lines = append(s.Lines, SplitLine{Category: c.Account, Amount: size})
	}
	return s, true
}

// isSplitCategory says whether a line of a split may be booked to the account.
func isSplitCategory(name string) bool {
	typ, err := ledger.TypeOf(name)
	return err == nil && (typ == ledger.Income || typ == ledger.Expenses)
}

// transaction checks the split and writes it as postings that add up to nothing: the
// amount on the account, and each line's share on its category with the opposite sign.
// The line without an amount gets what the others leave.
func (s Split) transaction() (NewTransaction, error) {
	if s.Date.IsZero() {
		return NewTransaction{}, coded.New("no_date", nil)
	}
	if typ, err := ledger.TypeOf(s.Account); err != nil {
		return NewTransaction{}, err
	} else if !typ.NeedsCurrency() {
		return NewTransaction{}, coded.New("split_not_own_account", map[string]any{"Name": s.Account})
	}
	if s.Amount.Minor == 0 {
		return NewTransaction{}, coded.New("split_zero_amount", nil)
	}
	if len(s.Lines) == 0 {
		return NewTransaction{}, coded.New("split_no_lines", nil)
	}
	cur := s.Amount.Currency
	total := s.Amount.Minor
	if total < 0 {
		total = -total
	}
	sizes := make([]int64, len(s.Lines))
	rest, left := -1, total
	for i, l := range s.Lines {
		n := i + 1
		switch {
		case strings.TrimSpace(l.Category) == "":
			return NewTransaction{}, coded.New("split_no_category", map[string]any{"N": n})
		case !isSplitCategory(l.Category):
			return NewTransaction{}, coded.New("split_line_category", map[string]any{"N": n, "Name": l.Category})
		case l.Amount.Minor < 0:
			return NewTransaction{}, coded.New("split_line_positive", map[string]any{"N": n})
		case l.Amount.Minor == 0 && rest >= 0:
			return NewTransaction{}, coded.New("split_one_rest", nil)
		case l.Amount.Minor == 0:
			rest = i
			continue
		case l.Amount.Currency != cur:
			return NewTransaction{}, coded.New("split_line_currency", map[string]any{"N": n, "Currency": string(l.Amount.Currency), "Holds": string(cur)})
		}
		sizes[i] = l.Amount.Minor
		left -= l.Amount.Minor
	}
	switch {
	case rest >= 0 && left <= 0:
		return NewTransaction{}, coded.New("split_nothing_left", map[string]any{"N": rest + 1})
	case rest >= 0:
		sizes[rest] = left
	case left > 0:
		return NewTransaction{}, coded.New("split_lines_short", map[string]any{"Amount": ledger.Money{Minor: left, Currency: cur}.Decimal(), "Currency": string(cur)})
	case left < 0:
		return NewTransaction{}, coded.New("split_lines_over", map[string]any{"Amount": ledger.Money{Minor: -left, Currency: cur}.Decimal(), "Currency": string(cur)})
	}

	nt := NewTransaction{
		Date: s.Date, Payee: s.Payee, Description: s.Description,
		Postings: []ledger.Posting{{Account: s.Account, Amount: s.Amount}},
	}
	for i, l := range s.Lines {
		// Spending is booked to its categories as a plus, income as a minus.
		share := ledger.Money{Minor: sizes[i], Currency: cur}
		if s.Amount.Minor > 0 {
			share = share.Neg()
		}
		nt.Postings = append(nt.Postings, ledger.Posting{Account: l.Category, Amount: share})
	}
	return nt, nil
}

// AddSplit checks the split and stores it as one transaction.
func (s *Local) AddSplit(ctx context.Context, sp Split) (AddResult, error) {
	nt, err := sp.transaction()
	if err != nil {
		return AddResult{}, err
	}
	return s.AddTransaction(ctx, nt)
}

// ReplaceSplit checks the split and writes it over the transaction id, all of it or
// nothing: the account, the amount, the day, the payee, the description and the lines.
// The transaction keeps its number and its import hash. A half of a transfer is refused,
// since it would stop being one. Categories that do not exist yet are created, and their
// names are returned.
func (s *Local) ReplaceSplit(ctx context.Context, id int64, sp Split) ([]string, error) {
	nt, err := sp.transaction()
	if err != nil {
		return nil, err
	}
	var created []string
	err = s.repo.inTx(ctx, func(q queries) error {
		partner, err := q.transferPartner(ctx, id)
		if err != nil {
			return err
		}
		if partner != 0 {
			return coded.New("already_transfer", map[string]any{"ID": id})
		}
		return replaceTransaction(ctx, q, id, nt, &created)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
