package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func usd(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "USD"} }

// scheduleModel is newModel's ledger with a dollar account, so that a transfer can change
// currency, and these schedules already defined.
func scheduleModel(t *testing.T, defined ...core.NewSchedule) (*Model, *core.Local) {
	t.Helper()
	st := openLedger(t)
	openAccounts(t, st, "Assets:Bank EUR", "Assets:Savings EUR", "Assets:Dollars USD")
	book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
	for _, ns := range defined {
		scheduled(t, st, ns)
	}
	return modelOn(t, st, 100, 24), st
}

// rentSchedule is a schedule that is there before the keys are pressed.
func rentSchedule() core.NewSchedule {
	return core.NewSchedule{Name: "Rent", Start: startOn("2026-11-28"), Payee: "Landlord", Postings: pair("Expenses:Rent", 90000)}
}

func Test_Update_scheduleForm(t *testing.T) {
	type observed struct {
		Mode      string
		Status    string
		Err       string
		Schedules []core.Schedule
	}
	bankLeg := func(minor int64) core.PostingView {
		return core.PostingView{Account: "Assets:Bank", Amount: eur(minor)}
	}
	catLeg := func(name string, minor int64) core.PostingView {
		return core.PostingView{Account: name, Amount: eur(minor)}
	}
	created := func(edit func(*core.Schedule)) []core.Schedule {
		s := core.Schedule{
			ID: 1, Name: "Rent", Every: 1, Unit: core.EveryMonth, Start: "2026-10-22", Payee: "Rent",
			Postings: []core.PostingView{bankLeg(-90000), catLeg("Expenses:Groceries", 90000)},
		}
		if edit != nil {
			edit(&s)
		}
		return []core.Schedule{s}
	}
	// Fields in the order tab goes through them: name, amount, from, to, every, period, start, end, tolerance.
	const toRent = "F n text:Rent tab text:900 tab tab text:Groc"
	tcs := []struct {
		name    string
		defined []core.NewSchedule
		keys    string
		want    observed
	}{
		{
			name: "nothing is there to begin with, and the popup opens",
			keys: "F n",
			want: observed{Mode: "schedule"},
		},
		{
			name: "a schedule that repeats",
			keys: toRent + " ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: created(nil)},
		},
		{
			name: "z takes a new schedule back",
			keys: toRent + " ctrl+s z",
			want: observed{Mode: "list", Status: "Undid the new schedule Rent"},
		},
		{
			name: "enter goes on to the next field and saves on the last",
			keys: toRent + " enter enter enter enter enter enter",
			want: observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: created(nil)},
		},
		{
			name: "money coming in, to a category that is made",
			keys: "F n text:Pay tab text:+3000 tab tab text:Income:Salary ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Pay saved · z undoes", Schedules: created(func(s *core.Schedule) {
				s.Name, s.Payee = "Pay", "Pay"
				s.Postings = []core.PostingView{bankLeg(300000), catLeg("Income:Salary", -300000)}
			})},
		},
		{
			name: "every other week, from a day, until a day",
			keys: toRent + " tab ctrl+u text:2 tab up tab ctrl+u text:2026-11-02 tab text:2027-02-01 ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: created(func(s *core.Schedule) {
				s.Every, s.Unit, s.Start, s.End = 2, core.EveryWeek, "2026-11-02", "2027-02-01"
			})},
		},
		{
			name: "yearly",
			keys: toRent + " tab tab up up ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: created(func(s *core.Schedule) { s.Unit = core.EveryYear })},
		},
		{
			name: "an estimate",
			keys: toRent + " tab tab tab tab tab text:25 ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: created(func(s *core.Schedule) { s.Tolerance = 25 })},
		},
		{
			name: "from another account",
			keys: "F n text:Rent tab text:900 tab down down tab text:Groc ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: created(func(s *core.Schedule) {
				s.Postings = []core.PostingView{{Account: "Assets:Savings", Amount: eur(-90000)}, catLeg("Expenses:Groceries", 90000)}
			})},
		},
		{
			name: "a transfer to another account",
			keys: "F n text:Saving tab text:200 tab tab text:Sav ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Saving saved · z undoes", Schedules: created(func(s *core.Schedule) {
				s.Name, s.Payee, s.Transfer = "Saving", "", true
				s.Postings = []core.PostingView{bankLeg(-20000), {Account: "Assets:Savings", Amount: eur(20000)}}
			})},
		},
		{
			name: "a transfer between currencies says what arrives",
			keys: "F n text:Dollars tab text:200 tab tab text:Doll tab",
			want: observed{Mode: "schedule"},
		},
		{
			name: "and is saved with it",
			keys: "F n text:Dollars tab text:200 tab tab text:Doll tab shift+tab shift+tab shift+tab text:215 ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Dollars saved · z undoes", Schedules: created(func(s *core.Schedule) {
				s.Name, s.Payee, s.Transfer = "Dollars", "", true
				s.Postings = []core.PostingView{bankLeg(-20000), {Account: "Assets:Dollars", Amount: usd(21500)}}
			})},
		},
		{
			name: "esc leaves nothing behind",
			keys: toRent + " esc",
			want: observed{Mode: "list"},
		},
		{
			name: "no category chosen",
			keys: "F n text:Rent tab text:900 ctrl+s",
			want: observed{Mode: "schedule", Err: "no category matches; type a full name like Expenses:Gifts to create one"},
		},
		{
			name: "no amount",
			keys: "F n text:Rent tab tab tab text:Groc ctrl+s",
			want: observed{Mode: "schedule", Err: "amount: invalid amount \"\""},
		},
		{
			name: "an amount that is zero",
			keys: "F n text:Rent tab text:0 tab tab text:Groc ctrl+s",
			want: observed{Mode: "schedule", Err: "amount must be greater than zero"},
		},
		{
			name: "every that is not a number",
			keys: toRent + " tab ctrl+u text:often ctrl+s",
			want: observed{Mode: "schedule", Err: "every: a whole number, 1 or more"},
		},
		{
			name: "every that is zero",
			keys: toRent + " tab ctrl+u text:0 ctrl+s",
			want: observed{Mode: "schedule", Err: "a schedule repeats every 1 or more weeks, months or years, not every 0"},
		},
		{
			name: "a start that is not a date",
			keys: toRent + " tab tab tab ctrl+u text:someday ctrl+s",
			want: observed{Mode: "schedule", Err: `date: invalid date "someday": use YYYY-MM-DD or DD.MM.YYYY`},
		},
		{
			name: "an end that is not a date",
			keys: toRent + " tab tab tab tab text:someday ctrl+s",
			want: observed{Mode: "schedule", Err: `end: invalid date "someday": use YYYY-MM-DD or DD.MM.YYYY`},
		},
		{
			name: "an end before the start",
			keys: toRent + " tab tab tab tab text:2026-10-01 ctrl+s",
			want: observed{Mode: "schedule", Err: "the end of a schedule (2026-10-01) cannot be before its start (2026-10-22)"},
		},
		{
			name: "a tolerance that is not a number",
			keys: toRent + " tab tab tab tab tab text:much ctrl+s",
			want: observed{Mode: "schedule", Err: "tolerance: a whole number from 0 to 100"},
		},
		{
			name: "a tolerance past all of it",
			keys: toRent + " tab tab tab tab tab text:150 ctrl+s",
			want: observed{Mode: "schedule", Err: "a tolerance is a percentage from 0 to 100, not 150"},
		},
		{
			name:    "a name that is taken",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    toRent + " ctrl+s",
			want: observed{Mode: "schedule", Err: `there is already a schedule called "Rent"`, Schedules: []core.Schedule{{
				ID: 1, Name: "Rent", Every: 1, Unit: core.EveryMonth, Start: "2026-11-28", Payee: "Landlord",
				Postings: []core.PostingView{bankLeg(-90000), catLeg("Expenses:Rent", 90000)},
			}}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := scheduleModel(t, tc.defined...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status}
			if m.schedForm != nil {
				got.Err = m.schedForm.err
			}
			var err error
			if got.Schedules, err = st.Schedules(t.Context()); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_scheduleEdit(t *testing.T) {
	type observed struct {
		Mode      string
		Status    string
		Fields    []string // what the popup holds, when it is open: name, amount, to, every, start
		Schedules []core.Schedule
	}
	rent := func(edit func(*core.Schedule)) core.Schedule {
		s := core.Schedule{
			ID: 1, Name: "Rent", Every: 1, Unit: core.EveryMonth, Start: "2026-11-28", Payee: "Landlord",
			Postings: []core.PostingView{{Account: "Assets:Bank", Amount: eur(-90000)}, {Account: "Expenses:Rent", Amount: eur(90000)}},
		}
		if edit != nil {
			edit(&s)
		}
		return s
	}
	transfer := core.NewSchedule{
		Name: "Saving", Start: startOn("2026-11-01"), Transfer: true,
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: "Assets:Savings", Amount: eur(20000)}},
	}
	split := core.NewSchedule{Name: "Split", Start: startOn("2026-12-01"), Postings: []ledger.Posting{
		{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Fun", Amount: eur(600)}, {Account: "Expenses:Power", Amount: eur(400)}}}
	tcs := []struct {
		name    string
		defined []core.NewSchedule
		keys    string
		want    observed
	}{
		{
			name:    "the popup holds the schedule",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F e",
			want:    observed{Mode: "schedule", Fields: []string{"Rent", "900.00", "Expenses:Rent", "1", "2026-11-28"}, Schedules: []core.Schedule{rent(nil)}},
		},
		{
			name:    "a new amount; the payee someone gave it stays",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F e tab ctrl+u text:950 ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: []core.Schedule{rent(func(s *core.Schedule) {
				s.Postings = []core.PostingView{{Account: "Assets:Bank", Amount: eur(-95000)}, {Account: "Expenses:Rent", Amount: eur(95000)}}
			})}},
		},
		{
			name:    "a new name; a payee that was the name follows it",
			defined: []core.NewSchedule{func() core.NewSchedule { ns := rentSchedule(); ns.Payee = "Rent"; return ns }()},
			keys:    "F e ctrl+u text:Flat ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Flat saved · z undoes", Schedules: []core.Schedule{rent(func(s *core.Schedule) {
				s.Name, s.Payee = "Flat", "Flat"
			})}},
		},
		{
			name:    "money in instead of out",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F e tab ctrl+u text:+900 ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: []core.Schedule{rent(func(s *core.Schedule) {
				s.Postings = []core.PostingView{{Account: "Assets:Bank", Amount: eur(90000)}, {Account: "Expenses:Rent", Amount: eur(-90000)}}
			})}},
		},
		{
			name:    "a category kept when it is only looked at",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F e tab tab tab tab ctrl+s",
			want:    observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: []core.Schedule{rent(nil)}},
		},
		{
			name:    "an end is set and then taken away",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F e tab tab tab tab tab tab tab text:2027-06-28 ctrl+s e tab tab tab tab tab tab tab ctrl+u ctrl+s",
			want:    observed{Mode: "list", Status: "Schedule Rent saved · z undoes", Schedules: []core.Schedule{rent(nil)}},
		},
		{
			name:    "z puts the old one back",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F e tab ctrl+u text:950 ctrl+s z",
			want:    observed{Mode: "list", Status: "Undid the change to Rent", Schedules: []core.Schedule{rent(nil)}},
		},
		{
			name:    "a transfer holds its accounts",
			defined: []core.NewSchedule{transfer},
			keys:    "F e",
			want: observed{Mode: "schedule", Fields: []string{"Saving", "200.00", "Assets:Savings", "1", "2026-11-01"}, Schedules: []core.Schedule{{
				ID: 1, Name: "Saving", Every: 1, Unit: core.EveryMonth, Start: "2026-11-01", Transfer: true,
				Postings: []core.PostingView{{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: "Assets:Savings", Amount: eur(20000)}},
			}}},
		},
		{
			name:    "a transfer with a new amount stays a transfer",
			defined: []core.NewSchedule{transfer},
			keys:    "F e tab ctrl+u text:250 ctrl+s",
			want: observed{Mode: "list", Status: "Schedule Saving saved · z undoes", Schedules: []core.Schedule{{
				ID: 1, Name: "Saving", Every: 1, Unit: core.EveryMonth, Start: "2026-11-01", Transfer: true,
				Postings: []core.PostingView{{Account: "Assets:Bank", Amount: eur(-25000)}, {Account: "Assets:Savings", Amount: eur(25000)}},
			}}},
		},
		{
			name:    "one with several postings is not edited here",
			defined: []core.NewSchedule{split},
			keys:    "F e",
			want: observed{Mode: "list", Status: "a schedule with several postings cannot be edited here", Schedules: []core.Schedule{{
				ID: 1, Name: "Split", Every: 1, Unit: core.EveryMonth, Start: "2026-12-01",
				Postings: []core.PostingView{{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Fun", Amount: eur(600)}, {Account: "Expenses:Power", Amount: eur(400)}},
			}}},
		},
		{name: "nothing selected, nothing opens", keys: "F e", want: observed{Mode: "list"}},
		{
			name:    "the selected one is the one edited",
			defined: []core.NewSchedule{rentSchedule(), transfer},
			keys:    "F down e",
			want: observed{Mode: "schedule", Fields: []string{"Rent", "900.00", "Expenses:Rent", "1", "2026-11-28"}, Schedules: []core.Schedule{ // by name; the screen has the soonest first, so Saving is the first there and Rent the second
				rent(nil),
				{ID: 2, Name: "Saving", Every: 1, Unit: core.EveryMonth, Start: "2026-11-01", Transfer: true,
					Postings: []core.PostingView{{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: "Assets:Savings", Amount: eur(20000)}}},
			}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := scheduleModel(t, tc.defined...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status}
			if f := m.schedForm; f != nil {
				got.Fields = []string{f.name.Value(), f.amount.Value(), f.to.Value(), f.every.Value(), f.start.Value()}
			}
			var err error
			if got.Schedules, err = st.Schedules(t.Context()); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_schedulePause(t *testing.T) {
	type observed struct {
		Status  string
		Paused  []bool // per schedule, in the order stored
		Screens string // what the screen says about it
	}
	tcs := []struct {
		name    string
		defined []core.NewSchedule
		keys    string
		want    observed
	}{
		{
			name:    "p pauses the selected schedule",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F p",
			want:    observed{Status: "Rent is paused · z undoes", Paused: []bool{true}},
		},
		{
			name:    "and p again resumes it",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F p p",
			want:    observed{Status: "Rent runs again · z undoes", Paused: []bool{false}},
		},
		{
			name:    "z takes a pause back",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F p z",
			want:    observed{Status: "Undid the pause of Rent", Paused: []bool{false}},
		},
		{
			name:    "z takes a resume back",
			defined: []core.NewSchedule{rentSchedule()},
			keys:    "F p p z",
			want:    observed{Status: "Rent is paused again", Paused: []bool{true}},
		},
		{
			name:    "only the selected one",
			defined: []core.NewSchedule{rentSchedule(), {Name: "Gym", Start: startOn("2026-11-01"), Postings: pair("Expenses:Fun", 3000)}},
			keys:    "F p",
			want:    observed{Status: "Gym is paused · z undoes", Paused: []bool{true, false}},
		},
		{name: "none to pause", keys: "F p", want: observed{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := scheduleModel(t, tc.defined...)

			// when
			play(m, tc.keys)

			// then
			got := observed{Status: m.status}
			all, err := st.Schedules(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range all {
				got.Paused = append(got.Paused, s.Paused())
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_schedulePopup(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "the fields of an ordinary schedule, and the first one's hint",
			keys:  "F n",
			shows: []string{"New schedule", "Name", "Amount", "From", "To", "Every", "Period", "Start", "End", "Tolerance", "EUR", "what to call it", "enter next"},
			hides: []string{"Received"},
		},
		{
			name:  "while the To field is typed in, what it can go to",
			keys:  "F n tab tab tab",
			shows: []string{"Expenses:Groceries", "Assets:Savings", "your account · transfer"},
			hides: []string{"Assets:Bank  "},
		},
		{
			name:  "a category that is not there is offered as new",
			keys:  "F n tab tab tab text:Expenses:Gifts",
			shows: []string{"Expenses:Gifts", "new category"},
		},
		{
			name:  "a transfer has no tolerance",
			keys:  "F n tab tab tab text:Sav tab",
			shows: []string{"Assets:Savings", "⇄", "Every"},
			hides: []string{"Tolerance", "Received"},
		},
		{
			name:  "a transfer between currencies asks what arrives",
			keys:  "F n tab tab tab text:Doll tab",
			shows: []string{"Received", "USD", "Assets:Dollars"},
			hides: []string{"Tolerance"},
		},
		{
			name:  "the amount's hint says which way the money goes",
			keys:  "F n tab",
			shows: []string{"no sign: money out", "+ money in"},
		},
		{
			name:  "the account it comes from cannot be where it goes",
			keys:  "F n tab tab down down tab",
			shows: []string{"Assets:Bank", "Assets:Dollars"},
			hides: []string{"Assets:Savings  "},
		},
		{
			name:  "editing says so",
			keys:  "F e",
			shows: []string{"Edit schedule", "Rent", "Expenses:Rent"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := scheduleModel(t, rentSchedule())

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
