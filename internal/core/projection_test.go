package core

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_Projection(t *testing.T) {
	bank := accounts("Assets:Bank EUR")
	funded := openingBalance("Assets:Bank", eur(200000), "2026-10-01")
	named := func(name, start string, minor int64) given {
		ns := rent()
		ns.Name, ns.Start = name, day(start)
		ns.Postings = []ledger.Posting{{Account: "Assets:Bank", Amount: eur(minor)}, {Account: "Expenses:Rent", Amount: eur(-minor)}}
		return schedule(ns)
	}
	pausedFrom := func(id int64, date string) given {
		return func(t *testing.T, st *Local) {
			t.Helper()
			today(date)(t, st)
			check(t, st.PauseSchedule(t.Context(), id))
		}
	}
	endsOn := func(date string) given {
		return func(t *testing.T, st *Local) {
			t.Helper()
			ns := rent()
			ns.End = day(date)
			check(t, st.ReplaceSchedule(t.Context(), 1, ns))
		}
	}
	account := func(name string, start ledger.Money, days ...ProjectedDay) ProjectedAccount {
		typ := ledger.Assets
		if name[0] == 'L' {
			typ = ledger.Liabilities
		}
		return ProjectedAccount{Name: name, Type: typ, Start: start, Days: days}
	}
	move := func(id int64, name string, amount ledger.Money) ProjectedMove {
		return ProjectedMove{ScheduleID: id, Schedule: name, Amount: amount}
	}
	moveDay := func(date string, change, balance ledger.Money, moves ...ProjectedMove) ProjectedDay {
		return ProjectedDay{Date: date, Change: change, Balance: balance, Moves: moves}
	}

	tcs := []struct {
		name    string
		setup   []given
		upTo    string
		want    Projection
		wantErr string
	}{
		{
			name:  "nothing scheduled: each account holds what it holds",
			setup: []given{bank, accounts("Assets:Savings EUR", "Expenses:Rent"), funded, today("2026-11-10")},
			upTo:  "2027-01-31",
			want: Projection{From: "2026-11-10", To: "2027-01-31", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000)), account("Assets:Savings", eur(0)),
			}},
		},
		{
			name:  "a monthly rent takes the balance down day by day",
			setup: []given{bank, funded, schedule(rent()), today("2026-11-10")},
			upTo:  "2027-01-31",
			want: Projection{From: "2026-11-10", To: "2027-01-31", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000),
					moveDay("2026-11-28", eur(-90000), eur(110000), move(1, "Rent", eur(-90000))),
					moveDay("2026-12-28", eur(-90000), eur(20000), move(1, "Rent", eur(-90000))),
					moveDay("2027-01-28", eur(-90000), eur(-70000), move(1, "Rent", eur(-90000))),
				),
			}},
		},
		{
			name:  "the last day is included, the day after is not",
			setup: []given{bank, funded, schedule(rent()), today("2026-11-10")},
			upTo:  "2026-11-28",
			want: Projection{From: "2026-11-10", To: "2026-11-28", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-28", eur(-90000), eur(110000), move(1, "Rent", eur(-90000)))),
			}},
		},
		{
			name:  "today itself, when a date falls on it",
			setup: []given{bank, funded, schedule(rent()), today("2026-11-28")},
			upTo:  "2026-11-28",
			want: Projection{From: "2026-11-28", To: "2026-11-28", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-28", eur(-90000), eur(110000), move(1, "Rent", eur(-90000)))),
			}},
		},
		{
			name:  "two schedules on one day add up, in the order of their names",
			setup: []given{bank, funded, named("Rent", "2026-11-28", -90000), named("Gym", "2026-11-28", -3000), today("2026-11-10")},
			upTo:  "2026-11-30",
			want: Projection{From: "2026-11-10", To: "2026-11-30", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-28", eur(-93000), eur(107000), move(2, "Gym", eur(-3000)), move(1, "Rent", eur(-90000)))),
			}},
		},
		{
			name:  "money in lifts it again",
			setup: []given{bank, funded, named("Salary", "2026-11-25", 300000), named("Rent", "2026-11-28", -90000), today("2026-11-10")},
			upTo:  "2026-11-30",
			want: Projection{From: "2026-11-10", To: "2026-11-30", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000),
					moveDay("2026-11-25", eur(300000), eur(500000), move(1, "Salary", eur(300000))),
					moveDay("2026-11-28", eur(-90000), eur(410000), move(2, "Rent", eur(-90000))),
				),
			}},
		},
		{
			name:  "a date nobody answered is counted today",
			setup: []given{bank, funded, named("Rent", "2026-11-01", -90000), today("2026-11-10")},
			upTo:  "2026-11-20",
			want: Projection{From: "2026-11-10", To: "2026-11-20", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-10", eur(-90000), eur(110000), ProjectedMove{ScheduleID: 1, Schedule: "Rent", Amount: eur(-90000), Overdue: true})),
			}},
		},
		{
			name:  "an overdue date and today's own date are two moves on one day",
			setup: []given{bank, funded, named("Rent", "2026-10-10", -90000), today("2026-11-10")},
			upTo:  "2026-11-10",
			want: Projection{From: "2026-11-10", To: "2026-11-10", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-10", eur(-180000), eur(20000),
					ProjectedMove{ScheduleID: 1, Schedule: "Rent", Amount: eur(-90000), Overdue: true}, move(1, "Rent", eur(-90000)))),
			}},
		},
		{
			name:  "a confirmed date is in the balance already, not in the moves",
			setup: []given{bank, funded, schedule(rent()), today("2026-11-28"), confirmed(1, "2026-11-28")},
			upTo:  "2026-12-31",
			want: Projection{From: "2026-11-28", To: "2026-12-31", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(110000), moveDay("2026-12-28", eur(-90000), eur(20000), move(1, "Rent", eur(-90000)))),
			}},
		},
		{
			name:  "a skipped date moves nothing",
			setup: []given{bank, funded, schedule(rent()), today("2026-11-28"), skipped(1, "2026-11-28")},
			upTo:  "2026-12-31",
			want: Projection{From: "2026-11-28", To: "2026-12-31", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-12-28", eur(-90000), eur(110000), move(1, "Rent", eur(-90000)))),
			}},
		},
		{
			name:  "a paused schedule moves nothing from the day it was paused",
			setup: []given{bank, funded, schedule(rent()), pausedFrom(1, "2026-11-30")},
			upTo:  "2027-02-28",
			want: Projection{From: "2026-11-30", To: "2027-02-28", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-30", eur(-90000), eur(110000), ProjectedMove{ScheduleID: 1, Schedule: "Rent", Amount: eur(-90000), Overdue: true})),
			}},
		},
		{
			name:  "a schedule that ends stops there",
			setup: []given{bank, funded, schedule(rent()), endsOn("2026-12-28"), today("2026-11-10")},
			upTo:  "2027-03-31",
			want: Projection{From: "2026-11-10", To: "2027-03-31", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000),
					moveDay("2026-11-28", eur(-90000), eur(110000), move(1, "Rent", eur(-90000))),
					moveDay("2026-12-28", eur(-90000), eur(20000), move(1, "Rent", eur(-90000))),
				),
			}},
		},
		{
			name:  "a transfer takes it from one account and puts it on the other",
			setup: []given{bank, accounts("Assets:Savings EUR"), funded, schedule(saving()), today("2026-10-30")},
			upTo:  "2026-11-30",
			want: Projection{From: "2026-10-30", To: "2026-11-30", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-01", eur(-20000), eur(180000), move(1, "Saving", eur(-20000)))),
				account("Assets:Savings", eur(0), moveDay("2026-11-01", eur(20000), eur(20000), move(1, "Saving", eur(20000)))),
			}},
		},
		{
			name: "a transfer between currencies moves each account in its own",
			setup: []given{bank, accounts("Assets:Dollars USD"), funded, today("2026-10-30"), schedule(NewSchedule{
				Name: "Dollars", Every: 1, Unit: EveryMonth, Start: day("2026-11-01"), Transfer: true,
				Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-10000)}, {Account: "Assets:Dollars", Amount: usd(11000)}},
			})},
			upTo: "2026-11-30",
			want: Projection{From: "2026-10-30", To: "2026-11-30", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-01", eur(-10000), eur(190000), move(1, "Dollars", eur(-10000)))),
				account("Assets:Dollars", usd(0), moveDay("2026-11-01", usd(11000), usd(11000), move(1, "Dollars", usd(11000)))),
			}},
		},
		{
			name: "a liability is an account too, and keeps its sign",
			setup: []given{bank, accounts("Liabilities:Card EUR"), funded, today("2026-11-10"), schedule(NewSchedule{
				Name: "Card", Every: 1, Unit: EveryMonth, Start: day("2026-11-15"), Transfer: true,
				Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-5000)}, {Account: "Liabilities:Card", Amount: eur(5000)}},
			})},
			upTo: "2026-11-30",
			want: Projection{From: "2026-11-10", To: "2026-11-30", Accounts: []ProjectedAccount{
				account("Assets:Bank", eur(200000), moveDay("2026-11-15", eur(-5000), eur(195000), move(1, "Card", eur(-5000)))),
				account("Liabilities:Card", eur(0), moveDay("2026-11-15", eur(5000), eur(5000), move(1, "Card", eur(5000)))),
			}},
		},
		{
			name:    "a day before today",
			setup:   []given{bank, today("2026-11-10")},
			upTo:    "2026-11-09",
			wantErr: "the projection starts today (2026-11-10); 2026-11-09 is before that",
		},
		{name: "a date that is not one", setup: []given{bank}, upTo: "soon", wantErr: `invalid date "soon": use YYYY-MM-DD or DD.MM.YYYY`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.Projection(t.Context(), tc.upTo)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("projection mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ProjectedAccount_BalanceOn(t *testing.T) {
	a := ProjectedAccount{
		Start: eur(1000),
		Days: []ProjectedDay{
			{Date: "2026-11-10", Balance: eur(900)},
			{Date: "2026-11-20", Balance: eur(400)},
		},
	}
	tcs := []struct {
		name string
		day  string
		want ledger.Money
	}{
		{name: "before the first move it is what it holds", day: "2026-11-09", want: eur(1000)},
		{name: "on the day of a move it is after the move", day: "2026-11-10", want: eur(900)},
		{name: "between two moves it keeps the earlier one", day: "2026-11-19", want: eur(900)},
		{name: "after the last", day: "2027-01-01", want: eur(400)},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := a.BalanceOn(tc.day)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("balance mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Open_withClock(t *testing.T) {
	tcs := []struct {
		name string
		opts []Option
		want string
	}{
		{name: "the clock given is today", opts: []Option{WithClock(func() time.Time { return day("2027-03-05") })}, want: "2027-03-05"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st, err := Open(t.Context(), ":memory:", tc.opts...)
			check(t, err)
			t.Cleanup(func() { st.Close() })

			// when
			got, err := st.Projection(t.Context(), "2027-03-06")

			// then
			check(t, err)
			if diff := cmp.Diff(tc.want, got.From); diff != "" {
				t.Errorf("today mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func minimum(account string, amount ledger.Money) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		check(t, st.SetMinimumBalance(t.Context(), account, amount))
	}
}

func Test_SetMinimumBalance(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		account string
		amount  ledger.Money
		clear   bool
		want    []MinimumBalance
		wantErr string
	}{
		{name: "set", account: "Assets:Bank", amount: eur(50000), want: []MinimumBalance{{"Assets:Bank", eur(50000)}}},
		{name: "zero is a limit too", account: "Assets:Bank", amount: eur(0), want: []MinimumBalance{{"Assets:Bank", eur(0)}}},
		{name: "an agreed overdraft", account: "Assets:Bank", amount: eur(-30000), want: []MinimumBalance{{"Assets:Bank", eur(-30000)}}},
		{
			name: "setting it again replaces it", setup: []given{minimum("Assets:Bank", eur(50000))},
			account: "Assets:Bank", amount: eur(10000), want: []MinimumBalance{{"Assets:Bank", eur(10000)}},
		},
		{
			name: "listed by account", setup: []given{minimum("Assets:Savings", eur(100000))},
			account: "Assets:Bank", amount: eur(50000), want: []MinimumBalance{{"Assets:Bank", eur(50000)}, {"Assets:Savings", eur(100000)}},
		},
		{
			name: "clearing", setup: []given{minimum("Assets:Bank", eur(50000)), minimum("Assets:Savings", eur(100000))},
			account: "Assets:Bank", clear: true, want: []MinimumBalance{{"Assets:Savings", eur(100000)}},
		},
		{name: "clearing one that is not set", account: "Assets:Bank", clear: true},
		{name: "another currency", account: "Assets:Bank", amount: usd(50000), wantErr: "Assets:Bank holds EUR, so its minimum is in EUR, not USD"},
		{name: "not an asset", account: "Liabilities:Card", amount: eur(0), wantErr: "Liabilities:Card is not an Assets account: only an Assets account can have a minimum balance"},
		{name: "a category", account: "Expenses:Rent", amount: eur(0), wantErr: "Expenses:Rent is not an Assets account: only an Assets account can have a minimum balance"},
		{name: "no such account", account: "Assets:Nowhere", amount: eur(0), wantErr: "account does not exist: Assets:Nowhere"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR", "Assets:Savings EUR", "Liabilities:Card EUR", "Expenses:Rent")}, tc.setup...)...)

			// when
			var err error
			if tc.clear {
				err = st.ClearMinimumBalance(t.Context(), tc.account)
			} else {
				err = st.SetMinimumBalance(t.Context(), tc.account, tc.amount)
			}
			got, listErr := st.MinimumBalances(t.Context())

			// then
			checkErr(t, err, tc.wantErr, nil)
			check(t, listErr)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("minimums mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_LowBalances(t *testing.T) {
	funded := openingBalance("Assets:Bank", eur(200000), "2026-10-01")
	accts := accounts("Assets:Bank EUR", "Assets:Savings EUR", "Liabilities:Card EUR")
	named := func(name, start string, minor int64) given {
		ns := rent()
		ns.Name, ns.Start = name, day(start)
		ns.Postings = []ledger.Posting{{Account: "Assets:Bank", Amount: eur(minor)}, {Account: "Expenses:Rent", Amount: eur(-minor)}}
		return schedule(ns)
	}
	tcs := []struct {
		name    string
		setup   []given
		upTo    string
		want    []LowBalance
		wantErr string
	}{
		{name: "no minimum, no warning", setup: []given{funded, schedule(rent()), today("2026-11-10")}, upTo: "2027-06-30"},
		{
			name:  "the day the rent takes it under",
			setup: []given{funded, schedule(rent()), minimum("Assets:Bank", eur(50000)), today("2026-11-10")},
			upTo:  "2027-06-30",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(50000), Date: "2026-12-28", Balance: eur(20000), Schedule: "Rent"}},
		},
		{
			name:  "exactly the minimum is not under it",
			setup: []given{funded, schedule(rent()), minimum("Assets:Bank", eur(110000)), today("2026-11-10")},
			upTo:  "2026-12-27",
		},
		{
			name:  "one cent under is",
			setup: []given{funded, schedule(rent()), minimum("Assets:Bank", eur(110001)), today("2026-11-10")},
			upTo:  "2026-12-27",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(110001), Date: "2026-11-28", Balance: eur(110000), Schedule: "Rent"}},
		},
		{
			name:  "not within the days asked for",
			setup: []given{funded, schedule(rent()), minimum("Assets:Bank", eur(50000)), today("2026-11-10")},
			upTo:  "2026-12-27",
		},
		{
			name:  "the day asked for is included",
			setup: []given{funded, schedule(rent()), minimum("Assets:Bank", eur(50000)), today("2026-11-10")},
			upTo:  "2026-12-28",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(50000), Date: "2026-12-28", Balance: eur(20000), Schedule: "Rent"}},
		},
		{
			name:  "already under, so today",
			setup: []given{funded, minimum("Assets:Bank", eur(300000)), today("2026-11-10")},
			upTo:  "2026-11-30",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(300000), Date: "2026-11-10", Balance: eur(200000), Now: true}},
		},
		{
			name:  "under already, and the rent does not change the answer",
			setup: []given{funded, schedule(rent()), minimum("Assets:Bank", eur(300000)), today("2026-11-10")},
			upTo:  "2027-06-30",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(300000), Date: "2026-11-10", Balance: eur(200000), Now: true}},
		},
		{
			name:  "an overdue date takes it under today, and is not 'now'",
			setup: []given{funded, named("Rent", "2026-11-01", -90000), minimum("Assets:Bank", eur(150000)), today("2026-11-10")},
			upTo:  "2026-11-30",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(150000), Date: "2026-11-10", Balance: eur(110000), Schedule: "Rent"}},
		},
		{
			name:  "an agreed overdraft is the limit",
			setup: []given{funded, schedule(rent()), minimum("Assets:Bank", eur(-50000)), today("2026-11-10")},
			upTo:  "2027-06-30",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(-50000), Date: "2027-01-28", Balance: eur(-70000), Schedule: "Rent"}},
		},
		{
			name:  "a day's money in and out is taken together",
			setup: []given{funded, named("Salary", "2026-11-28", 300000), named("Rent", "2026-11-28", -90000), minimum("Assets:Bank", eur(150000)), today("2026-11-10")},
			upTo:  "2026-11-30",
		},
		{
			name:  "the schedule that takes the most is named",
			setup: []given{funded, named("Gym", "2026-11-28", -3000), named("Rent", "2026-11-28", -90000), minimum("Assets:Bank", eur(150000)), today("2026-11-10")},
			upTo:  "2026-11-30",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(150000), Date: "2026-11-28", Balance: eur(107000), Schedule: "Rent"}},
		},
		{
			name: "soonest first, whatever the accounts' names",
			setup: []given{
				funded, openingBalance("Assets:Savings", eur(100000), "2026-10-01"), schedule(saving()), named("Rent", "2026-11-28", -90000),
				minimum("Assets:Bank", eur(100000)), minimum("Assets:Savings", eur(400000)), today("2026-10-30"),
			},
			upTo: "2026-11-30",
			want: []LowBalance{
				{Account: "Assets:Savings", Minimum: eur(400000), Date: "2026-10-30", Balance: eur(100000), Now: true},
				{Account: "Assets:Bank", Minimum: eur(100000), Date: "2026-11-28", Balance: eur(90000), Schedule: "Rent"},
			},
		},
		{
			name:  "a transfer takes it from the account it leaves",
			setup: []given{funded, openingBalance("Assets:Savings", eur(100000), "2026-10-01"), schedule(saving()), minimum("Assets:Bank", eur(190000)), minimum("Assets:Savings", eur(100000)), today("2026-10-30")},
			upTo:  "2026-11-30",
			want:  []LowBalance{{Account: "Assets:Bank", Minimum: eur(190000), Date: "2026-11-01", Balance: eur(180000), Schedule: "Saving"}},
		},
		{name: "a day before today", setup: []given{today("2026-11-10")}, upTo: "2026-11-09", wantErr: "the projection starts today (2026-11-10); 2026-11-09 is before that"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accts}, tc.setup...)...)

			// when
			got, err := st.LowBalances(t.Context(), tc.upTo)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("low balances mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
