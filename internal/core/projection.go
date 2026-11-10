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
