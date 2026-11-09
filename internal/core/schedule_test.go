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
