package core

import (
	"context"
	"sort"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// Projection works out the balances the schedules lead to.
func (s *Local) Projection(ctx context.Context, upTo string) (Projection, error) {
	end, err := ledger.ParseDate(upTo)
	if err != nil {
		return Projection{}, err
	}
	today := s.today()
	to := end.Format(ledger.DateLayout)
	if to < today {
		return Projection{}, coded.New("projection_before_today", map[string]any{"Date": to, "Today": today})
	}

	q := s.repo.queries()
	accounts, err := q.accounts(ctx)
	if err != nil {
		return Projection{}, err
	}
	balances, err := q.balances(ctx, today)
	if err != nil {
		return Projection{}, err
	}
	schedules, err := q.schedules(ctx)
	if err != nil {
		return Projection{}, err
	}
	done, err := q.handledOccurrences(ctx)
	if err != nil {
		return Projection{}, err
	}

	// What each account holds today, and the days it moves on.
	byName := make(map[string]*ProjectedAccount)
	out := Projection{From: today, To: to}
	for _, a := range accounts {
		if !a.Type.NeedsCurrency() {
			continue
		}
		out.Accounts = append(out.Accounts, ProjectedAccount{Name: a.Name, Type: a.Type, Start: ledger.Money{Currency: a.Currency}})
	}
	for i := range out.Accounts {
		byName[out.Accounts[i].Name] = &out.Accounts[i]
	}
	for _, b := range balances {
		if a := byName[b.Account]; a != nil {
			a.Start = b.Amount
		}
	}

	days := make(map[string]map[string]*ProjectedDay) // account, then date
	for _, sch := range schedules {
		dates, err := scheduleDates(sch, end)
		if err != nil {
			return Projection{}, err
		}
		for _, d := range dates {
			if done[handled{sch.ID, d}] {
				continue
			}
			on, overdue := d, d < today
			if overdue {
				on = today
			}
			for _, p := range sch.Postings {
				a := byName[p.Account]
				if a == nil { // a category, or the other side of a conversion
					continue
				}
				if days[a.Name] == nil {
					days[a.Name] = make(map[string]*ProjectedDay)
				}
				day := days[a.Name][on]
				if day == nil {
					day = &ProjectedDay{Date: on, Change: ledger.Money{Currency: a.Start.Currency}}
					days[a.Name][on] = day
				}
				if day.Change, err = day.Change.Add(p.Amount); err != nil {
					return Projection{}, err
				}
				day.Moves = append(day.Moves, ProjectedMove{ScheduleID: sch.ID, Schedule: sch.Name, Amount: p.Amount, Overdue: overdue})
			}
		}
	}

	for i := range out.Accounts {
		a := &out.Accounts[i]
		for _, day := range days[a.Name] {
			a.Days = append(a.Days, *day)
		}
		sort.Slice(a.Days, func(x, y int) bool { return a.Days[x].Date < a.Days[y].Date })
		running := a.Start
		for j := range a.Days {
			// Schedules come by name, so a tie on the day already keeps that order.
			if running, err = running.Add(a.Days[j].Change); err != nil {
				return Projection{}, err
			}
			a.Days[j].Balance = running
		}
	}
	return out, nil
}

// SetMinimumBalance stores the lowest account should fall.
func (s *Local) SetMinimumBalance(ctx context.Context, account string, amount ledger.Money) error {
	acc, err := s.assetAccount(ctx, account)
	if err != nil {
		return err
	}
	if amount.Currency != acc.Currency {
		return coded.New("minimum_wrong_currency", map[string]any{"Name": acc.Name, "Currency": acc.Currency, "Got": amount.Currency})
	}
	return s.repo.queries().putMinimumBalance(ctx, acc.ID, amount.Minor)
}

// ClearMinimumBalance takes the limit away.
func (s *Local) ClearMinimumBalance(ctx context.Context, account string) error {
	acc, err := s.assetAccount(ctx, account)
	if err != nil {
		return err
	}
	return s.repo.queries().deleteMinimumBalance(ctx, acc.ID)
}

// MinimumBalances lists the limits.
func (s *Local) MinimumBalances(ctx context.Context) ([]MinimumBalance, error) {
	return s.repo.queries().minimumBalances(ctx)
}

func (s *Local) assetAccount(ctx context.Context, name string) (Account, error) {
	acc, err := s.repo.queries().account(ctx, name)
	if err != nil {
		return Account{}, err
	}
	if acc.Type != ledger.Assets {
		return Account{}, coded.New("minimum_not_asset", map[string]any{"Name": acc.Name})
	}
	return acc, nil
}

// LowBalances finds the first day each account with a minimum would fall below it.
func (s *Local) LowBalances(ctx context.Context, upTo string) ([]LowBalance, error) {
	p, err := s.Projection(ctx, upTo)
	if err != nil {
		return nil, err
	}
	minimums, err := s.repo.queries().minimumBalances(ctx)
	if err != nil {
		return nil, err
	}
	var out []LowBalance
	for _, m := range minimums {
		for _, a := range p.Accounts {
			if a.Name != m.Account {
				continue
			}
			if low, ok := firstBelow(a, m.Amount, p.From); ok {
				out = append(out, low)
			}
		}
	}
	// By account already; the sort is stable, so a tie on the day keeps that.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// firstBelow is the first day a holds less than limit. Balances are compared at the
// end of a day: what a day's schedules take and add in between is not ordered.
func firstBelow(a ProjectedAccount, limit ledger.Money, today string) (LowBalance, bool) {
	if a.Start.Minor < limit.Minor {
		return LowBalance{Account: a.Name, Minimum: limit, Date: today, Balance: a.Start, Now: true}, true
	}
	for _, d := range a.Days {
		if d.Balance.Minor >= limit.Minor {
			continue
		}
		low := LowBalance{Account: a.Name, Minimum: limit, Date: d.Date, Balance: d.Balance}
		var most int64
		for _, m := range d.Moves {
			if m.Amount.Minor < most {
				most, low.Schedule = m.Amount.Minor, m.Schedule
			}
		}
		return low, true
	}
	return LowBalance{}, false
}
