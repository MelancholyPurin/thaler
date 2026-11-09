package core

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// rent is a schedule that is valid unless a case changes it.
func rent() NewSchedule {
	return NewSchedule{
		Name: "Rent", Every: 1, Unit: EveryMonth, Start: day("2026-11-28"),
		Payee: "Landlord", Description: "Rent",
		Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-90000)},
			{Account: "Expenses:Rent", Amount: eur(90000)},
		},
	}
}

func rentView(id int64) Schedule {
	return Schedule{
		ID: id, Name: "Rent", Every: 1, Unit: EveryMonth, Start: "2026-11-28",
		Payee: "Landlord", Description: "Rent",
		Postings: []PostingView{{"Assets:Bank", eur(-90000)}, {"Expenses:Rent", eur(90000)}},
	}
}

func Test_CreateSchedule(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		edit    func(*NewSchedule)
		want    []Schedule
		wantErr string
	}{
		{
			name: "a monthly schedule",
			want: []Schedule{rentView(1)},
		},
		{
			name: "every other week",
			edit: func(ns *NewSchedule) { ns.Every, ns.Unit = 2, EveryWeek },
			want: []Schedule{func() Schedule { s := rentView(1); s.Every, s.Unit = 2, EveryWeek; return s }()},
		},
		{
			name: "yearly, with a split",
			edit: func(ns *NewSchedule) {
				ns.Name, ns.Unit, ns.Payee, ns.Description = "Insurance", EveryYear, "", ""
				ns.Postings = []ledger.Posting{
					{Account: "Assets:Bank", Amount: eur(-30000)},
					{Account: "Expenses:Insurance", Amount: eur(20000)},
					{Account: "Expenses:Tax", Amount: eur(10000)},
				}
			},
			want: []Schedule{{
				ID: 1, Name: "Insurance", Every: 1, Unit: EveryYear, Start: "2026-11-28",
				Postings: []PostingView{{"Assets:Bank", eur(-30000)}, {"Expenses:Insurance", eur(20000)}, {"Expenses:Tax", eur(10000)}},
			}},
		},
		{
			name:  "in another currency",
			setup: []given{accounts("Assets:Dollars USD")},
			edit: func(ns *NewSchedule) {
				ns.Postings = []ledger.Posting{{Account: "Assets:Dollars", Amount: usd(-5000)}, {Account: "Expenses:Rent", Amount: usd(5000)}}
			},
			want: []Schedule{func() Schedule {
				s := rentView(1)
				s.Postings = []PostingView{{"Assets:Dollars", usd(-5000)}, {"Expenses:Rent", usd(5000)}}
				return s
			}()},
		},
		{
			name: "the name is trimmed",
			edit: func(ns *NewSchedule) { ns.Name = "  Rent \n" },
			want: []Schedule{rentView(1)},
		},
		{
			name:    "a name is needed",
			edit:    func(ns *NewSchedule) { ns.Name = "  " },
			wantErr: "a schedule needs a name",
		},
		{
			name:    "a name is taken, whatever the case",
			setup:   []given{schedule(rent())},
			edit:    func(ns *NewSchedule) { ns.Name = "RENT" },
			want:    []Schedule{rentView(1)},
			wantErr: `there is already a schedule called "RENT"`,
		},
		{
			name:    "every must be 1 or more",
			edit:    func(ns *NewSchedule) { ns.Every = 0 },
			wantErr: "a schedule repeats every 1 or more weeks, months or years, not every 0",
		},
		{
			name:    "every cannot be negative",
			edit:    func(ns *NewSchedule) { ns.Every = -2 },
			wantErr: "a schedule repeats every 1 or more weeks, months or years, not every -2",
		},
		{
			name:    "a day is not a unit",
			edit:    func(ns *NewSchedule) { ns.Unit = "day" },
			wantErr: `a schedule repeats every week, month or year, not every "day"`,
		},
		{
			name:    "a start date is needed",
			edit:    func(ns *NewSchedule) { ns.Start = time.Time{} },
			wantErr: "transaction has no date",
		},
		{
			name:    "postings that do not balance",
			edit:    func(ns *NewSchedule) { ns.Postings[1].Amount = eur(80000) },
			wantErr: "postings do not balance: off by EUR -100.00",
		},
		{
			name:    "an asset that does not exist is not made up",
			edit:    func(ns *NewSchedule) { ns.Postings[0].Account = "Assets:Nope" },
			wantErr: "account Assets:Nope does not exist; create it first as a Assets account in EUR",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR")}, tc.setup...)...)
			ns := rent()
			if tc.edit != nil {
				tc.edit(&ns)
			}

			// when
			_, err := st.CreateSchedule(t.Context(), ns)

			// then
			checkErr(t, err, tc.wantErr, nil)
			got, lerr := st.Schedules(t.Context())
			if lerr != nil {
				t.Fatal(lerr)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("schedules mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_CreateSchedule_makesCategories(t *testing.T) {
	tcs := []struct {
		name string
		want []string
	}{
		{name: "a category the schedule names is created", want: []string{"Assets:Bank", "Expenses:Rent"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"))

			// when
			_, err := st.CreateSchedule(t.Context(), rent())

			// then
			if err != nil {
				t.Fatal(err)
			}
			accs, err := st.ListAccounts(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, a := range accs {
				got = append(got, a.Name)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("accounts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ReplaceSchedule(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		id      int64
		edit    func(*NewSchedule)
		want    []Schedule
		wantErr string
	}{
		{
			name: "the amount and the rhythm change, the id stays",
			id:   1,
			edit: func(ns *NewSchedule) {
				ns.Every, ns.Unit = 3, EveryMonth
				ns.Postings = []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-95000)}, {Account: "Expenses:Rent", Amount: eur(95000)}}
			},
			want: []Schedule{func() Schedule {
				s := rentView(1)
				s.Every = 3
				s.Postings = []PostingView{{"Assets:Bank", eur(-95000)}, {"Expenses:Rent", eur(95000)}}
				return s
			}()},
		},
		{
			name: "it can keep its own name",
			id:   1,
			edit: func(ns *NewSchedule) { ns.Payee = "New landlord" },
			want: []Schedule{func() Schedule { s := rentView(1); s.Payee = "New landlord"; return s }()},
		},
		{
			name: "fewer postings leave none of the old ones behind",
			id:   1,
			edit: func(ns *NewSchedule) {
				ns.Postings = []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-100)}, {Account: "Expenses:Food", Amount: eur(100)}}
			},
			want: []Schedule{func() Schedule {
				s := rentView(1)
				s.Postings = []PostingView{{"Assets:Bank", eur(-100)}, {"Expenses:Food", eur(100)}}
				return s
			}()},
		},
		{
			name:    "another schedule's name is taken",
			setup:   []given{schedule(func() NewSchedule { ns := rent(); ns.Name = "Gym"; return ns }())},
			id:      1,
			edit:    func(ns *NewSchedule) { ns.Name = "gym" },
			want:    []Schedule{func() Schedule { s := rentView(2); s.Name = "Gym"; return s }(), rentView(1)},
			wantErr: `there is already a schedule called "gym"`,
		},
		{
			name:    "one that does not exist",
			id:      9,
			want:    []Schedule{rentView(1)},
			wantErr: "schedule 9 does not exist",
		},
		{
			name:    "a failed edit changes nothing",
			id:      1,
			edit:    func(ns *NewSchedule) { ns.Every = 0; ns.Postings = ns.Postings[:1] },
			want:    []Schedule{rentView(1)},
			wantErr: "a schedule repeats every 1 or more weeks, months or years, not every 0",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR"), schedule(rent())}, tc.setup...)...)
			ns := rent()
			if tc.edit != nil {
				tc.edit(&ns)
			}

			// when
			err := st.ReplaceSchedule(t.Context(), tc.id, ns)

			// then
			checkErr(t, err, tc.wantErr, nil)
			got, lerr := st.Schedules(t.Context())
			if lerr != nil {
				t.Fatal(lerr)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("schedules mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_DeleteSchedule(t *testing.T) {
	tcs := []struct {
		name    string
		id      int64
		want    []Schedule
		wantErr string
	}{
		{name: "it goes, and the other stays", id: 1, want: []Schedule{func() Schedule { s := rentView(2); s.Name = "Gym"; return s }()}},
		{name: "one that does not exist", id: 9, want: []Schedule{func() Schedule { s := rentView(2); s.Name = "Gym"; return s }(), rentView(1)}, wantErr: "schedule 9 does not exist"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"), schedule(rent()), schedule(func() NewSchedule { ns := rent(); ns.Name = "Gym"; return ns }()))

			// when
			err := st.DeleteSchedule(t.Context(), tc.id)

			// then
			checkErr(t, err, tc.wantErr, nil)
			got, lerr := st.Schedules(t.Context())
			if lerr != nil {
				t.Fatal(lerr)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("schedules mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_Schedules_order(t *testing.T) {
	tcs := []struct {
		name  string
		names []string
		want  []string
	}{
		{name: "by name, whatever the case", names: []string{"rent", "Gym", "Bike"}, want: []string{"Bike", "Gym", "rent"}},
		{name: "none", want: nil},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			steps := []given{accounts("Assets:Bank EUR")}
			for _, n := range tc.names {
				ns := rent()
				ns.Name = n
				steps = append(steps, schedule(ns))
			}
			st := newLedger(t, steps...)

			// when
			got, err := st.Schedules(t.Context())

			// then
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, s := range got {
				names = append(names, s.Name)
			}
			if diff := cmp.Diff(tc.want, names); diff != "" {
				t.Errorf("order mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// schedule defines ns in a ledger.
func schedule(ns NewSchedule) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.CreateSchedule(t.Context(), ns); err != nil {
			t.Fatal(err)
		}
	}
}

func Test_occurrencesUpTo(t *testing.T) {
	tcs := []struct {
		name  string
		start string
		every int
		unit  ScheduleUnit
		upTo  string
		want  []string
	}{
		{name: "monthly on the 28th", start: "2026-11-28", every: 1, unit: EveryMonth, upTo: "2027-02-28", want: []string{"2026-11-28", "2026-12-28", "2027-01-28", "2027-02-28"}},
		{name: "the day before the last one is not enough", start: "2026-11-28", every: 1, unit: EveryMonth, upTo: "2027-02-27", want: []string{"2026-11-28", "2026-12-28", "2027-01-28"}},
		{name: "the start day itself", start: "2026-11-28", every: 1, unit: EveryMonth, upTo: "2026-11-28", want: []string{"2026-11-28"}},
		{name: "before the start", start: "2026-11-28", every: 1, unit: EveryMonth, upTo: "2026-11-27", want: nil},
		{name: "the 31st shortens in short months and comes back", start: "2026-01-31", every: 1, unit: EveryMonth, upTo: "2026-05-31", want: []string{"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31"}},
		{name: "the 31st in a leap year", start: "2028-01-31", every: 1, unit: EveryMonth, upTo: "2028-03-31", want: []string{"2028-01-31", "2028-02-29", "2028-03-31"}},
		{name: "the 30th", start: "2026-01-30", every: 1, unit: EveryMonth, upTo: "2026-03-30", want: []string{"2026-01-30", "2026-02-28", "2026-03-30"}},
		{name: "every 3 months across a year end", start: "2026-11-15", every: 3, unit: EveryMonth, upTo: "2027-11-15", want: []string{"2026-11-15", "2027-02-15", "2027-05-15", "2027-08-15", "2027-11-15"}},
		{name: "every 2 weeks", start: "2026-11-02", every: 2, unit: EveryWeek, upTo: "2026-12-14", want: []string{"2026-11-02", "2026-11-16", "2026-11-30", "2026-12-14"}},
		{name: "weekly over a month end", start: "2026-10-28", every: 1, unit: EveryWeek, upTo: "2026-11-11", want: []string{"2026-10-28", "2026-11-04", "2026-11-11"}},
		{name: "yearly", start: "2026-03-10", every: 1, unit: EveryYear, upTo: "2028-12-31", want: []string{"2026-03-10", "2027-03-10", "2028-03-10"}},
		{name: "yearly from 29 February", start: "2028-02-29", every: 1, unit: EveryYear, upTo: "2032-03-01", want: []string{"2028-02-29", "2029-02-28", "2030-02-28", "2031-02-28", "2032-02-29"}},
		{name: "every 2 years", start: "2026-06-01", every: 2, unit: EveryYear, upTo: "2030-06-01", want: []string{"2026-06-01", "2028-06-01", "2030-06-01"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			start, upTo := day(tc.start), day(tc.upTo)

			// when
			dates := occurrencesUpTo(start, tc.every, tc.unit, upTo)

			// then
			var got []string
			for _, d := range dates {
				got = append(got, d.Format(ledger.DateLayout))
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("dates mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Due(t *testing.T) {
	named := func(name, start string, every int, unit ScheduleUnit) given {
		ns := rent()
		ns.Name, ns.Start, ns.Every, ns.Unit = name, day(start), every, unit
		return schedule(ns)
	}
	view := func(id int64, name, start string, every int, unit ScheduleUnit) Schedule {
		s := rentView(id)
		s.Name, s.Start, s.Every, s.Unit = name, start, every, unit
		return s
	}
	tcs := []struct {
		name    string
		setup   []given
		upTo    string
		want    []Occurrence
		wantErr string
	}{
		{name: "no schedules", upTo: "2027-01-01"},
		{
			name:  "a schedule that has not started is not due",
			setup: []given{named("Rent", "2026-12-28", 1, EveryMonth)},
			upTo:  "2026-12-27",
		},
		{
			name:  "oldest first, and a tie goes by name",
			setup: []given{named("Rent", "2026-11-28", 1, EveryMonth), named("Gym", "2026-11-28", 1, EveryMonth), named("Bike", "2026-12-01", 1, EveryYear)},
			upTo:  "2026-12-28",
			want: []Occurrence{
				{view(2, "Gym", "2026-11-28", 1, EveryMonth), "2026-11-28"},
				{view(1, "Rent", "2026-11-28", 1, EveryMonth), "2026-11-28"},
				{view(3, "Bike", "2026-12-01", 1, EveryYear), "2026-12-01"},
				{view(2, "Gym", "2026-11-28", 1, EveryMonth), "2026-12-28"},
				{view(1, "Rent", "2026-11-28", 1, EveryMonth), "2026-12-28"},
			},
		},
		{
			name:  "a date typed the German way",
			setup: []given{named("Rent", "2026-11-28", 1, EveryMonth)},
			upTo:  "28.11.2026",
			want:  []Occurrence{{view(1, "Rent", "2026-11-28", 1, EveryMonth), "2026-11-28"}},
		},
		{name: "a date that is not one", upTo: "soon", wantErr: `invalid date "soon": use YYYY-MM-DD or DD.MM.YYYY`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR")}, tc.setup...)...)

			// when
			got, err := st.Due(t.Context(), tc.upTo)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// dueDates is what Due lists, as "date name" lines.
func dueDates(t *testing.T, st *Local, upTo string) []string {
	t.Helper()
	due, err := st.Due(t.Context(), upTo)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, o := range due {
		out = append(out, o.Date+" "+o.Schedule.Name)
	}
	return out
}

func Test_ConfirmOccurrence(t *testing.T) {
	tcs := []struct {
		name     string
		setup    []given
		id       int64
		date     string
		postings []ledger.Posting
		want     []TxView // every transaction afterwards
		wantDue  []string // up to 2027-01-31
		wantErr  string
	}{
		{
			name: "the template is booked on the day it falls",
			id:   1, date: "2026-11-28",
			want:    []TxView{{ID: 1, Date: "2026-11-28", Payee: "Landlord", Description: "Rent", Postings: []PostingView{{"Assets:Bank", eur(-90000)}, {"Expenses:Rent", eur(90000)}}}},
			wantDue: []string{"2026-12-28 Rent", "2027-01-28 Rent"},
		},
		{
			name: "an older occurrence is booked on its own date",
			id:   1, date: "2026-12-28", setup: []given{today("2027-01-05")},
			want:    []TxView{{ID: 1, Date: "2026-12-28", Payee: "Landlord", Description: "Rent", Postings: []PostingView{{"Assets:Bank", eur(-90000)}, {"Expenses:Rent", eur(90000)}}}},
			wantDue: []string{"2026-11-28 Rent", "2027-01-28 Rent"},
		},
		{
			name: "another amount",
			id:   1, date: "2026-11-28",
			postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-95000)}, {Account: "Expenses:Rent", Amount: eur(95000)}},
			want:     []TxView{{ID: 1, Date: "2026-11-28", Payee: "Landlord", Description: "Rent", Postings: []PostingView{{"Assets:Bank", eur(-95000)}, {"Expenses:Rent", eur(95000)}}}},
			wantDue:  []string{"2026-12-28 Rent", "2027-01-28 Rent"},
		},
		{
			name: "the date typed the German way",
			id:   1, date: "28.11.2026",
			want:    []TxView{{ID: 1, Date: "2026-11-28", Payee: "Landlord", Description: "Rent", Postings: []PostingView{{"Assets:Bank", eur(-90000)}, {"Expenses:Rent", eur(90000)}}}},
			wantDue: []string{"2026-12-28 Rent", "2027-01-28 Rent"},
		},
		{
			name: "not today yet",
			id:   1, date: "2026-12-28",
			wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent"},
			wantErr: `2026-12-28 of "Rent" is not due yet`,
		},
		{
			name: "a day the schedule does not fall on",
			id:   1, date: "2026-11-27",
			wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent"},
			wantErr: `"Rent" does not fall on 2026-11-27`,
		},
		{
			name: "before the schedule starts",
			id:   1, date: "2026-10-28",
			wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent"},
			wantErr: `"Rent" does not fall on 2026-10-28`,
		},
		{
			name: "one that was confirmed already",
			id:   1, date: "2026-11-28", setup: []given{confirmed(1, "2026-11-28")},
			want:    []TxView{{ID: 1, Date: "2026-11-28", Payee: "Landlord", Description: "Rent", Postings: []PostingView{{"Assets:Bank", eur(-90000)}, {"Expenses:Rent", eur(90000)}}}},
			wantDue: []string{"2026-12-28 Rent", "2027-01-28 Rent"},
			wantErr: `2026-11-28 of "Rent" was already confirmed or skipped`,
		},
		{
			name: "one that was skipped",
			id:   1, date: "2026-11-28", setup: []given{skipped(1, "2026-11-28")},
			wantDue: []string{"2026-12-28 Rent", "2027-01-28 Rent"},
			wantErr: `2026-11-28 of "Rent" was already confirmed or skipped`,
		},
		{
			name: "amounts that do not balance book nothing and leave it due",
			id:   1, date: "2026-11-28",
			postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-95000)}, {Account: "Expenses:Rent", Amount: eur(90000)}},
			wantDue:  []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent"},
			wantErr:  "postings do not balance: off by EUR -50.00",
		},
		{
			name: "a schedule that does not exist",
			id:   9, date: "2026-11-28",
			wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent"},
			wantErr: "schedule 9 does not exist",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			steps := []given{accounts("Assets:Bank EUR"), schedule(rent()), today("2026-12-01")}
			st := newLedger(t, append(steps, tc.setup...)...)

			// when
			res, err := st.ConfirmOccurrence(t.Context(), tc.id, tc.date, tc.postings)

			// then
			checkErr(t, err, tc.wantErr, nil)
			txs, lerr := st.ListTransactions(t.Context(), TxFilter{})
			if lerr != nil {
				t.Fatal(lerr)
			}
			if diff := cmp.Diff(tc.want, txs, equalEmpty); diff != "" {
				t.Errorf("transactions mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2027-01-31"), equalEmpty); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
			if err == nil && res.ID == 0 {
				t.Error("no transaction id returned")
			}
			checkConsistent(t, st)
		})
	}
}

func Test_SkipOccurrence(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		id      int64
		date    string
		wantDue []string // up to 2027-01-31
		wantErr string
	}{
		{name: "it is not listed again", id: 1, date: "2026-11-28", wantDue: []string{"2026-12-28 Rent", "2027-01-28 Rent"}},
		{name: "the others stay due", id: 1, date: "2026-12-28", setup: []given{today("2026-12-30")}, wantDue: []string{"2026-11-28 Rent", "2027-01-28 Rent"}},
		{name: "not today yet", id: 1, date: "2026-12-28", wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent"}, wantErr: `2026-12-28 of "Rent" is not due yet`},
		{name: "a day the schedule does not fall on", id: 1, date: "2026-11-29", wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent"}, wantErr: `"Rent" does not fall on 2026-11-29`},
		{name: "twice", id: 1, date: "2026-11-28", setup: []given{skipped(1, "2026-11-28")}, wantDue: []string{"2026-12-28 Rent", "2027-01-28 Rent"}, wantErr: `2026-11-28 of "Rent" was already confirmed or skipped`},
		{name: "a schedule that does not exist", id: 9, date: "2026-11-28", wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent"}, wantErr: "schedule 9 does not exist"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			steps := []given{accounts("Assets:Bank EUR"), schedule(rent()), today("2026-12-01")}
			st := newLedger(t, append(steps, tc.setup...)...)

			// when
			err := st.SkipOccurrence(t.Context(), tc.id, tc.date)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2027-01-31")); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
			txs, lerr := st.ListTransactions(t.Context(), TxFilter{})
			if lerr != nil {
				t.Fatal(lerr)
			}
			if diff := cmp.Diff([]TxView(nil), txs, equalEmpty); diff != "" {
				t.Errorf("a skip must book nothing (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ConfirmOccurrence_whatBecomesOfIt(t *testing.T) {
	tcs := []struct {
		name       string
		act        func(t *testing.T, st *Local)
		wantDue    []string // up to 2026-12-31
		wantBooked int
	}{
		{
			name:       "deleting the transaction makes it due again",
			act:        func(t *testing.T, st *Local) { check(t, st.DeleteTransaction(t.Context(), 1)) },
			wantDue:    []string{"2026-11-28 Rent", "2026-12-28 Rent"},
			wantBooked: 0,
		},
		{
			name: "editing the transaction leaves it answered",
			act: func(t *testing.T, st *Local) {
				nt := NewTransaction{Date: day("2026-11-30"), Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1)}, {Account: "Expenses:Rent", Amount: eur(1)}}}
				_, err := st.ReplaceTransaction(t.Context(), 1, nt)
				check(t, err)
			},
			wantDue:    []string{"2026-12-28 Rent"},
			wantBooked: 1,
		},
		{
			name:       "deleting the schedule keeps what it booked",
			act:        func(t *testing.T, st *Local) { check(t, st.DeleteSchedule(t.Context(), 1)) },
			wantDue:    nil,
			wantBooked: 1,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"), schedule(rent()), today("2026-12-31"), confirmed(1, "2026-11-28"))

			// when
			tc.act(t, st)

			// then
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2026-12-31"), equalEmpty); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
			txs, err := st.ListTransactions(t.Context(), TxFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.wantBooked, len(txs)); diff != "" {
				t.Errorf("booked mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// confirmed and skipped answer an occurrence in a ledger.
func confirmed(id int64, date string) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		_, err := st.ConfirmOccurrence(t.Context(), id, date, nil)
		check(t, err)
	}
}

func skipped(id int64, date string) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		check(t, st.SkipOccurrence(t.Context(), id, date))
	}
}
