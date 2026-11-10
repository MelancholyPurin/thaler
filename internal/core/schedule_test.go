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

func Test_CreateSchedule_end(t *testing.T) {
	tcs := []struct {
		name    string
		end     string
		want    string // the end stored
		wantDue []string
		wantErr string
	}{
		{name: "none", want: "", wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent", "2027-02-28 Rent"}},
		{name: "after the second date", end: "2026-12-30", want: "2026-12-30", wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent"}},
		{name: "on a date it falls on: that one still counts", end: "2026-12-28", want: "2026-12-28", wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent"}},
		{name: "the day before a date it falls on", end: "2026-12-27", want: "2026-12-27", wantDue: []string{"2026-11-28 Rent"}},
		{name: "on the start day", end: "2026-11-28", want: "2026-11-28", wantDue: []string{"2026-11-28 Rent"}},
		{name: "before the start", end: "2026-11-27", wantErr: "the end of a schedule (2026-11-27) cannot be before its start (2026-11-28)"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"))
			ns := rent()
			if tc.end != "" {
				ns.End = day(tc.end)
			}

			// when
			_, err := st.CreateSchedule(t.Context(), ns)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if tc.wantErr != "" {
				return
			}
			got, lerr := st.Schedules(t.Context())
			check(t, lerr)
			if diff := cmp.Diff(tc.want, got[0].End); diff != "" {
				t.Errorf("end mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2027-02-28")); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ReplaceSchedule_end(t *testing.T) {
	tcs := []struct {
		name string
		edit func(*NewSchedule)
		want string
	}{
		{name: "an end can be set", edit: func(ns *NewSchedule) { ns.End = day("2027-06-01") }, want: "2027-06-01"},
		{name: "and cleared", edit: func(ns *NewSchedule) { ns.End = time.Time{} }, want: ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			first := rent()
			first.End = day("2027-01-01")
			st := newLedger(t, accounts("Assets:Bank EUR"), schedule(first))
			ns := rent()
			tc.edit(&ns)

			// when
			check(t, st.ReplaceSchedule(t.Context(), 1, ns))

			// then
			got, err := st.Schedules(t.Context())
			check(t, err)
			if diff := cmp.Diff(tc.want, got[0].End); diff != "" {
				t.Errorf("end mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_PauseSchedule(t *testing.T) {
	// Rent falls on the 28th. Pausing and resuming are dated by the clock.
	pausedOn := func(date string) given {
		return func(t *testing.T, st *Local) {
			t.Helper()
			today(date)(t, st)
			check(t, st.PauseSchedule(t.Context(), 1))
		}
	}
	resumedOn := func(date string) given {
		return func(t *testing.T, st *Local) {
			t.Helper()
			today(date)(t, st)
			check(t, st.ResumeSchedule(t.Context(), 1))
		}
	}
	tcs := []struct {
		name       string
		setup      []given
		act        func(t *testing.T, st *Local) error
		wantDue    []string // up to 2027-02-28
		wantPauses []SchedulePause
		wantErr    string
	}{
		{
			name:       "a paused schedule proposes nothing from the day it was paused",
			setup:      []given{pausedOn("2026-12-10")},
			wantDue:    []string{"2026-11-28 Rent"},
			wantPauses: []SchedulePause{{From: "2026-12-10"}},
		},
		{
			name:       "the dates of the pause are dropped, not held back",
			setup:      []given{pausedOn("2026-12-10"), resumedOn("2027-01-15")},
			wantDue:    []string{"2026-11-28 Rent", "2027-01-28 Rent", "2027-02-28 Rent"},
			wantPauses: []SchedulePause{{From: "2026-12-10", To: "2027-01-15"}},
		},
		{
			name:       "paused on the day itself, the day is dropped",
			setup:      []given{pausedOn("2026-12-28"), resumedOn("2027-01-29")},
			wantDue:    []string{"2026-11-28 Rent", "2027-02-28 Rent"},
			wantPauses: []SchedulePause{{From: "2026-12-28", To: "2027-01-29"}},
		},
		{
			name:       "resumed on the day itself, the day is proposed",
			setup:      []given{pausedOn("2026-12-10"), resumedOn("2027-01-28")},
			wantDue:    []string{"2026-11-28 Rent", "2027-01-28 Rent", "2027-02-28 Rent"},
			wantPauses: []SchedulePause{{From: "2026-12-10", To: "2027-01-28"}},
		},
		{
			name:       "two pauses",
			setup:      []given{pausedOn("2026-11-29"), resumedOn("2026-12-29"), pausedOn("2027-02-01"), resumedOn("2027-03-01")},
			wantDue:    []string{"2026-11-28 Rent", "2027-01-28 Rent"},
			wantPauses: []SchedulePause{{From: "2026-11-29", To: "2026-12-29"}, {From: "2027-02-01", To: "2027-03-01"}},
		},
		{
			name:       "pausing a paused schedule changes nothing",
			setup:      []given{pausedOn("2026-12-10"), pausedOn("2026-12-20")},
			wantDue:    []string{"2026-11-28 Rent"},
			wantPauses: []SchedulePause{{From: "2026-12-10"}},
		},
		{
			name:       "resuming one that runs changes nothing",
			setup:      []given{resumedOn("2026-12-10")},
			wantDue:    []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent", "2027-02-28 Rent"},
			wantPauses: nil,
		},
		{
			name:       "paused and resumed on the same day, then paused again",
			setup:      []given{pausedOn("2026-12-10"), resumedOn("2026-12-10"), pausedOn("2026-12-10")},
			wantDue:    []string{"2026-11-28 Rent"},
			wantPauses: []SchedulePause{{From: "2026-12-10"}},
		},
		{
			name:       "an answer before the pause stays an answer",
			setup:      []given{today("2026-12-01"), skipped(1, "2026-11-28"), pausedOn("2026-12-10")},
			wantDue:    nil,
			wantPauses: []SchedulePause{{From: "2026-12-10"}},
		},
		{
			name:  "a date in a pause cannot be confirmed",
			setup: []given{pausedOn("2026-11-01"), resumedOn("2026-12-30")},
			act: func(t *testing.T, st *Local) error {
				_, err := st.ConfirmOccurrence(t.Context(), 1, "2026-12-28", nil)
				return err
			},
			wantDue:    []string{"2027-01-28 Rent", "2027-02-28 Rent"},
			wantPauses: []SchedulePause{{From: "2026-11-01", To: "2026-12-30"}},
			wantErr:    `"Rent" does not fall on 2026-12-28`,
		},
		{
			name:    "a schedule that does not exist",
			act:     func(t *testing.T, st *Local) error { return st.PauseSchedule(t.Context(), 9) },
			wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent", "2027-02-28 Rent"},
			wantErr: "schedule 9 does not exist",
		},
		{
			name:    "resuming one that does not exist",
			act:     func(t *testing.T, st *Local) error { return st.ResumeSchedule(t.Context(), 9) },
			wantDue: []string{"2026-11-28 Rent", "2026-12-28 Rent", "2027-01-28 Rent", "2027-02-28 Rent"},
			wantErr: "schedule 9 does not exist",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR"), schedule(rent()), today("2026-11-20")}, tc.setup...)...)

			// when
			var err error
			if tc.act != nil {
				err = tc.act(t, st)
			}

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2027-02-28"), equalEmpty); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
			got, lerr := st.Schedules(t.Context())
			check(t, lerr)
			if diff := cmp.Diff(tc.wantPauses, got[0].Pauses, equalEmpty); diff != "" {
				t.Errorf("pauses mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_Schedule_Paused(t *testing.T) {
	tcs := []struct {
		name   string
		pauses []SchedulePause
		want   bool
	}{
		{name: "never paused"},
		{name: "paused now", pauses: []SchedulePause{{From: "2026-12-10"}}, want: true},
		{name: "resumed", pauses: []SchedulePause{{From: "2026-12-10", To: "2026-12-20"}}},
		{name: "paused again", pauses: []SchedulePause{{From: "2026-12-10", To: "2026-12-20"}, {From: "2027-01-01"}}, want: true},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			s := Schedule{Pauses: tc.pauses}

			// when
			got := s.Paused()

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("paused mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// saving is a scheduled transfer of 200.00 from the bank to the savings.
func saving() NewSchedule {
	return NewSchedule{
		Name: "Saving", Every: 1, Unit: EveryMonth, Start: day("2026-11-01"), Transfer: true, Description: "monthly saving",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: "Assets:Savings", Amount: eur(20000)}},
	}
}

func Test_CreateSchedule_transfer(t *testing.T) {
	tcs := []struct {
		name    string
		edit    func(*NewSchedule)
		want    []PostingView
		wantErr string
	}{
		{name: "between two accounts", want: []PostingView{{"Assets:Bank", eur(-20000)}, {"Assets:Savings", eur(20000)}}},
		{
			name: "across currencies, with what arrives",
			edit: func(ns *NewSchedule) { ns.Postings[1] = ledger.Posting{Account: "Assets:Dollars", Amount: usd(21500)} },
			want: []PostingView{{"Assets:Bank", eur(-20000)}, {"Assets:Dollars", usd(21500)}},
		},
		{
			name: "both postings arriving",
			edit: func(ns *NewSchedule) {
				ns.Postings[0] = ledger.Posting{Account: "Assets:Bank", Amount: eur(20000)}
				ns.Postings[1] = ledger.Posting{Account: "Assets:Dollars", Amount: usd(21500)}
			},
			wantErr: "a scheduled transfer has two postings: the amount that leaves one account and the amount that arrives in another",
		},
		{name: "one posting", edit: func(ns *NewSchedule) { ns.Postings = ns.Postings[:1] }, wantErr: "a scheduled transfer has two postings: the amount that leaves one account and the amount that arrives in another"},
		{name: "three postings", edit: func(ns *NewSchedule) { ns.Postings = append(ns.Postings, ns.Postings[1]) }, wantErr: "a scheduled transfer has two postings: the amount that leaves one account and the amount that arrives in another"},
		{
			name:    "it must leave first",
			edit:    func(ns *NewSchedule) { ns.Postings[0], ns.Postings[1] = ns.Postings[1], ns.Postings[0] },
			wantErr: "a scheduled transfer has two postings: the amount that leaves one account and the amount that arrives in another",
		},
		{
			name:    "within one currency what arrives is what leaves",
			edit:    func(ns *NewSchedule) { ns.Postings[1].Amount = eur(19000) },
			wantErr: "a scheduled transfer has two postings: the amount that leaves one account and the amount that arrives in another",
		},
		{name: "the same account twice", edit: func(ns *NewSchedule) { ns.Postings[1].Account = "Assets:Bank" }, wantErr: "source and destination are the same account"},
		{name: "a category is not an account of yours", edit: func(ns *NewSchedule) { ns.Postings[1].Account = "Expenses:Food" }, wantErr: "transfers work between accounts with a fixed currency (Assets or Liabilities)"},
		{name: "an account that does not exist", edit: func(ns *NewSchedule) { ns.Postings[1].Account = "Assets:Nope" }, wantErr: "account does not exist: Assets:Nope"},
		{
			name:    "an amount in the wrong currency",
			edit:    func(ns *NewSchedule) { ns.Postings[0].Amount = usd(-20000) },
			wantErr: "amount is in USD, but Assets:Bank holds EUR",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR", "Assets:Savings EUR", "Assets:Dollars USD", "Expenses:Food"))
			ns := saving()
			if tc.edit != nil {
				tc.edit(&ns)
			}

			// when
			_, err := st.CreateSchedule(t.Context(), ns)

			// then
			checkErr(t, err, tc.wantErr, nil)
			got, lerr := st.Schedules(t.Context())
			check(t, lerr)
			if tc.wantErr != "" {
				if diff := cmp.Diff([]Schedule(nil), got, equalEmpty); diff != "" {
					t.Errorf("nothing should be stored (-want +got):\n%s", diff)
				}
				return
			}
			want := []Schedule{{ID: 1, Name: "Saving", Every: 1, Unit: EveryMonth, Start: "2026-11-01", Transfer: true, Description: "monthly saving", Postings: tc.want}}
			if diff := cmp.Diff(want, got, equalEmpty); diff != "" {
				t.Errorf("schedules mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ConfirmOccurrence_transfer(t *testing.T) {
	tcs := []struct {
		name     string
		setup    []given
		postings []ledger.Posting
		want     []TxView
		wantDue  []string // up to 2026-12-31
		wantErr  string
	}{
		{
			name: "two linked halves on the day it falls",
			want: []TxView{
				linked(tv(1, "2026-11-01", "Transfer to Savings", "monthly saving", pe("Assets:Bank", -20000), pe(ledger.TransferAccount, 20000)), 2),
				linked(tv(2, "2026-11-01", "Transfer from Bank", "monthly saving", pe("Assets:Savings", 20000), pe(ledger.TransferAccount, -20000)), 1),
			},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name:     "another amount",
			postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-25000)}, {Account: "Assets:Savings", Amount: eur(25000)}},
			want: []TxView{
				linked(tv(1, "2026-11-01", "Transfer to Savings", "monthly saving", pe("Assets:Bank", -25000), pe(ledger.TransferAccount, 25000)), 2),
				linked(tv(2, "2026-11-01", "Transfer from Bank", "monthly saving", pe("Assets:Savings", 25000), pe(ledger.TransferAccount, -25000)), 1),
			},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name:     "the postings of a transfer or nothing",
			postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-25000)}, {Account: "Expenses:Food", Amount: eur(25000)}},
			wantDue:  []string{"2026-11-01 Saving", "2026-12-01 Saving"},
			wantErr:  "transfers work between accounts with a fixed currency (Assets or Liabilities)",
		},
		{
			name:     "an amount that does not add up",
			postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-25000)}, {Account: "Assets:Savings", Amount: eur(24000)}},
			wantDue:  []string{"2026-11-01 Saving", "2026-12-01 Saving"},
			wantErr:  "a scheduled transfer has two postings: the amount that leaves one account and the amount that arrives in another",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR", "Assets:Savings EUR", "Expenses:Food"), schedule(saving()), today("2026-12-01")}, tc.setup...)...)

			// when
			res, err := st.ConfirmOccurrence(t.Context(), 1, "2026-11-01", tc.postings)

			// then
			checkErr(t, err, tc.wantErr, nil)
			txs, lerr := st.ListTransactions(t.Context(), TxFilter{})
			check(t, lerr)
			if diff := cmp.Diff(tc.want, txs, equalEmpty); diff != "" {
				t.Errorf("transactions mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2026-12-31"), equalEmpty); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
			if err == nil && (res.ID != 1 || res.InID != 2) {
				t.Errorf("ids = %d and %d, want 1 and 2", res.ID, res.InID)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_ConfirmOccurrence_transferAcrossCurrencies(t *testing.T) {
	tcs := []struct {
		name string
		want []TxView
	}{
		{
			name: "through Equity:Conversions, with the amount that arrives",
			want: []TxView{
				linked(TxView{ID: 1, Date: "2026-11-01", Payee: "Transfer to Dollars", Description: "monthly saving", Postings: []PostingView{{"Assets:Bank", eur(-20000)}, {ledger.ConversionAccount, eur(20000)}}}, 2),
				linked(TxView{ID: 2, Date: "2026-11-01", Payee: "Transfer from Bank", Description: "monthly saving", Postings: []PostingView{{"Assets:Dollars", usd(21500)}, {ledger.ConversionAccount, usd(-21500)}}}, 1),
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			ns := saving()
			ns.Postings[1] = ledger.Posting{Account: "Assets:Dollars", Amount: usd(21500)}
			st := newLedger(t, accounts("Assets:Bank EUR", "Assets:Dollars USD"), schedule(ns), today("2026-11-02"))

			// when
			_, err := st.ConfirmOccurrence(t.Context(), 1, "2026-11-01", nil)

			// then
			check(t, err)
			txs, lerr := st.ListTransactions(t.Context(), TxFilter{})
			check(t, lerr)
			if diff := cmp.Diff(tc.want, txs, equalEmpty); diff != "" {
				t.Errorf("transactions mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_SkipOccurrence_transfer(t *testing.T) {
	tcs := []struct {
		name string
	}{{name: "books nothing, as for any schedule"}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR", "Assets:Savings EUR"), schedule(saving()), today("2026-11-02"))

			// when
			err := st.SkipOccurrence(t.Context(), 1, "2026-11-01")

			// then
			check(t, err)
			txs, lerr := st.ListTransactions(t.Context(), TxFilter{})
			check(t, lerr)
			if diff := cmp.Diff([]TxView(nil), txs, equalEmpty); diff != "" {
				t.Errorf("transactions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_withinTolerance(t *testing.T) {
	type result struct {
		Gap int64
		OK  bool
	}
	tcs := []struct {
		name     string
		estimate ledger.Money
		amount   ledger.Money
		percent  int
		want     result
	}{
		{name: "exactly the estimate", estimate: eur(-8000), amount: eur(-8000), percent: 25, want: result{0, true}},
		{name: "on the edge above", estimate: eur(-8000), amount: eur(-10000), percent: 25, want: result{2000, true}},
		{name: "on the edge below", estimate: eur(-8000), amount: eur(-6000), percent: 25, want: result{2000, true}},
		{name: "a cent past the edge above", estimate: eur(-8000), amount: eur(-10001), percent: 25, want: result{2001, false}},
		{name: "a cent past the edge below", estimate: eur(-8000), amount: eur(-5999), percent: 25, want: result{2001, false}},
		{name: "no tolerance, a cent off", estimate: eur(-8000), amount: eur(-8001), percent: 0, want: result{1, false}},
		{name: "no tolerance, exact", estimate: eur(-8000), amount: eur(-8000), percent: 0, want: result{0, true}},
		{name: "income works the same way", estimate: eur(300000), amount: eur(316000), percent: 5, want: result{16000, false}},
		{name: "income within it", estimate: eur(300000), amount: eur(315000), percent: 5, want: result{15000, true}},
		{name: "the other direction is no match", estimate: eur(-8000), amount: eur(8000), percent: 100, want: result{0, false}},
		{name: "another currency is no match", estimate: eur(-8000), amount: usd(-8000), percent: 100, want: result{0, false}},
		{name: "a zero amount is no match", estimate: eur(-8000), amount: eur(0), percent: 100, want: result{0, false}},
		{name: "a zero estimate is no match", estimate: eur(0), amount: eur(-5), percent: 100, want: result{0, false}},
		{name: "a percentage that does not divide the estimate", estimate: eur(-999), amount: eur(-1198), percent: 20, want: result{199, true}},
		{name: "and one cent more", estimate: eur(-999), amount: eur(-1199), percent: 20, want: result{200, false}},
		{name: "a very large estimate does not overflow", estimate: eur(-9000000000000000000), amount: eur(-9000000000000000000), percent: 100, want: result{0, true}},
		{name: "100 percent reaches double", estimate: eur(-1000), amount: eur(-2000), percent: 100, want: result{1000, true}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			gap, ok := withinTolerance(tc.estimate, tc.amount, tc.percent)

			// then
			if diff := cmp.Diff(tc.want, result{gap, ok}); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_CreateSchedule_tolerance(t *testing.T) {
	tcs := []struct {
		name    string
		edit    func(*NewSchedule)
		want    int
		wantErr string
	}{
		{name: "none is exact", want: 0},
		{name: "a percentage", edit: func(ns *NewSchedule) { ns.Tolerance = 25 }, want: 25},
		{name: "all of it", edit: func(ns *NewSchedule) { ns.Tolerance = 100 }, want: 100},
		{name: "negative", edit: func(ns *NewSchedule) { ns.Tolerance = -1 }, wantErr: "a tolerance is a percentage from 0 to 100, not -1"},
		{name: "past all of it", edit: func(ns *NewSchedule) { ns.Tolerance = 101 }, wantErr: "a tolerance is a percentage from 0 to 100, not 101"},
		{
			name:    "a transfer is exact",
			edit:    func(ns *NewSchedule) { *ns = saving(); ns.Tolerance = 10 },
			wantErr: "a scheduled transfer moves an exact amount: it has no tolerance",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR", "Assets:Savings EUR"))
			ns := rent()
			if tc.edit != nil {
				tc.edit(&ns)
			}

			// when
			_, err := st.CreateSchedule(t.Context(), ns)

			// then
			checkErr(t, err, tc.wantErr, nil)
			got, lerr := st.Schedules(t.Context())
			check(t, lerr)
			if tc.wantErr != "" {
				if diff := cmp.Diff([]Schedule(nil), got, equalEmpty); diff != "" {
					t.Errorf("nothing should be stored (-want +got):\n%s", diff)
				}
				return
			}
			if diff := cmp.Diff(tc.want, got[0].Tolerance); diff != "" {
				t.Errorf("tolerance mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ReplaceSchedule_tolerance(t *testing.T) {
	tcs := []struct {
		name string
		edit func(*NewSchedule)
		want int
	}{
		{name: "it can be set", edit: func(ns *NewSchedule) { ns.Tolerance = 30 }, want: 30},
		{name: "and taken away", edit: func(ns *NewSchedule) { ns.Tolerance = 0 }, want: 0},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			first := rent()
			first.Tolerance = 10
			st := newLedger(t, accounts("Assets:Bank EUR"), schedule(first))
			ns := rent()
			tc.edit(&ns)

			// when
			check(t, st.ReplaceSchedule(t.Context(), 1, ns))

			// then
			got, err := st.Schedules(t.Context())
			check(t, err)
			if diff := cmp.Diff(tc.want, got[0].Tolerance); diff != "" {
				t.Errorf("tolerance mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ScheduleStatuses(t *testing.T) {
	type status struct {
		Name    string
		Next    string
		Overdue int
		Paused  bool
	}
	pause := func(date string) given {
		return func(t *testing.T, st *Local) {
			t.Helper()
			today(date)(t, st)
			check(t, st.PauseSchedule(t.Context(), 1))
		}
	}
	resume := func(date string) given {
		return func(t *testing.T, st *Local) {
			t.Helper()
			today(date)(t, st)
			check(t, st.ResumeSchedule(t.Context(), 1))
		}
	}
	ending := func(end string) given {
		return func(t *testing.T, st *Local) {
			t.Helper()
			ns := rent()
			ns.End = day(end)
			check(t, st.ReplaceSchedule(t.Context(), 1, ns))
		}
	}
	other := func(name, start string, unit ScheduleUnit) given {
		ns := rent()
		ns.Name, ns.Start, ns.Unit = name, day(start), unit
		return schedule(ns)
	}
	tcs := []struct {
		name    string
		noRent  bool // the ledger has the schedules of setup only
		setup   []given
		asOf    string
		want    []status
		wantErr string
	}{
		{name: "no schedules", noRent: true, asOf: "2026-12-01"},
		{name: "one that has not started: its first date", asOf: "2026-11-10", want: []status{{"Rent", "2026-11-28", 0, false}}},
		{name: "the day it falls is due, not yet overdue past the day", asOf: "2026-11-28", want: []status{{"Rent", "2026-11-28", 1, false}}},
		{name: "unanswered dates are overdue, the oldest is next", asOf: "2026-12-30", want: []status{{"Rent", "2026-11-28", 2, false}}},
		{
			name: "an answered date is not next", setup: []given{today("2026-12-01"), confirmed(1, "2026-11-28")},
			asOf: "2026-12-01", want: []status{{"Rent", "2026-12-28", 0, false}},
		},
		{
			name: "confirmed and skipped: the next to come", setup: []given{today("2026-12-30"), confirmed(1, "2026-11-28"), skipped(1, "2026-12-28")},
			asOf: "2026-12-30", want: []status{{"Rent", "2027-01-28", 0, false}},
		},
		{
			name: "answered out of order", setup: []given{today("2026-12-30"), skipped(1, "2026-12-28")},
			asOf: "2026-12-30", want: []status{{"Rent", "2026-11-28", 1, false}},
		},
		{
			name: "paused with something waiting", setup: []given{pause("2026-12-01")},
			asOf: "2026-12-01", want: []status{{"Rent", "2026-11-28", 1, true}},
		},
		{
			name: "paused with nothing waiting: nothing is next", setup: []given{today("2026-12-01"), confirmed(1, "2026-11-28"), pause("2026-12-01")},
			asOf: "2026-12-01", want: []status{{"Rent", "", 0, true}},
		},
		{
			name: "resumed: the dates of the pause are gone", setup: []given{pause("2026-12-10"), resume("2027-01-15"), func(t *testing.T, st *Local) { skipped(1, "2026-11-28")(t, st) }},
			asOf: "2027-01-20", want: []status{{"Rent", "2027-01-28", 0, false}},
		},
		{
			name:  "asked about a day before a pause that has been and gone: its dates are skipped over",
			setup: []given{today("2026-12-01"), confirmed(1, "2026-11-28"), pause("2026-12-10"), resume("2027-01-15")},
			asOf:  "2026-12-01", want: []status{{"Rent", "2027-01-28", 0, false}},
		},
		{
			name: "past its end with nothing waiting", setup: []given{today("2026-12-30"), confirmed(1, "2026-11-28"), confirmed(1, "2026-12-28"), ending("2026-12-30")},
			asOf: "2027-03-01", want: []status{{"Rent", "", 0, false}},
		},
		{
			name: "past its end with something waiting", setup: []given{ending("2026-12-30")},
			asOf: "2027-03-01", want: []status{{"Rent", "2026-11-28", 2, false}},
		},
		{
			name: "its last date is still to come", setup: []given{today("2026-12-01"), confirmed(1, "2026-11-28"), ending("2026-12-28")},
			asOf: "2026-12-01", want: []status{{"Rent", "2026-12-28", 0, false}},
		},
		{
			name:  "the soonest first, a tie by name, and what has nothing coming last",
			setup: []given{other("Zoo", "2026-11-28", EveryMonth), other("Bike", "2026-12-05", EveryYear), other("Gym", "2026-12-03", EveryWeek), today("2026-11-28"), skipped(1, "2026-11-28"), pause("2026-11-28")},
			asOf:  "2026-11-25",
			want: []status{
				{"Zoo", "2026-11-28", 0, false},
				{"Gym", "2026-12-03", 0, false},
				{"Bike", "2026-12-05", 0, false},
				{"Rent", "", 0, true},
			},
		},
		{name: "a date that is not one", asOf: "soon", wantErr: `invalid date "soon": use YYYY-MM-DD or DD.MM.YYYY`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			steps := []given{accounts("Assets:Bank EUR")}
			if !tc.noRent {
				steps = append(steps, schedule(rent()))
			}
			st := newLedger(t, append(steps, tc.setup...)...)

			// when
			got, err := st.ScheduleStatuses(t.Context(), tc.asOf)

			// then
			checkErr(t, err, tc.wantErr, nil)
			var statuses []status
			for _, s := range got {
				statuses = append(statuses, status{s.Name, s.Next, s.Overdue, s.Paused()})
			}
			if diff := cmp.Diff(tc.want, statuses, equalEmpty); diff != "" {
				t.Errorf("statuses mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
