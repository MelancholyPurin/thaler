package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// scheduled defines a schedule, on the ledger of newModel.
func scheduled(t *testing.T, st *core.Local, ns core.NewSchedule) {
	t.Helper()
	if ns.Every == 0 {
		ns.Every = 1
	}
	if ns.Unit == "" {
		ns.Unit = core.EveryMonth
	}
	if _, err := st.CreateSchedule(t.Context(), ns); err != nil {
		t.Fatal(err)
	}
}

func startOn(date string) time.Time {
	d, err := ledger.ParseDate(date)
	if err != nil {
		panic(err)
	}
	return d
}

// pair is the two postings of an ordinary schedule: money leaving the bank for a category.
func pair(category string, minor int64) []ledger.Posting {
	return []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-minor)}, {Account: category, Amount: eur(minor)}}
}

func Test_forecastScreen(t *testing.T) {
	tcs := []struct {
		name   string
		setup  func(t *testing.T, st *core.Local)
		keys   string
		shows  []string
		hides  []string
		status string
	}{
		{
			name:  "no schedules",
			shows: []string{"Schedules", "No schedules yet", "thaler cli schedule add"},
			hides: []string{"Not built yet", "waiting"},
		},
		{
			name: "what is coming",
			setup: func(t *testing.T, st *core.Local) {
				scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-11-28"), Postings: pair("Expenses:Rent", 90000)})
			},
			shows: []string{"Schedules", "Rent", "-900.00 EUR", "2026-11-28", "every month", "1 of 1", "tab balances"},
			hides: []string{"waiting", "!"},
		},
		{
			name: "dates nobody answered are overdue and said so",
			setup: func(t *testing.T, st *core.Local) {
				scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-09-28"), Postings: pair("Expenses:Rent", 90000)})
			},
			shows: []string{"Rent", "! 2026-09-28", "1 date is waiting for an answer", "thaler cli schedule due"},
		},
		{
			name: "two dates",
			setup: func(t *testing.T, st *core.Local) {
				scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-08-28"), Postings: pair("Expenses:Rent", 90000)})
			},
			shows: []string{"! 2026-08-28", "2 dates are waiting for an answer"},
		},
		{
			name: "the soonest first",
			setup: func(t *testing.T, st *core.Local) {
				scheduled(t, st, core.NewSchedule{Name: "Zoo", Start: startOn("2026-12-01"), Postings: pair("Expenses:Fun", 1000)})
				scheduled(t, st, core.NewSchedule{Name: "Aaa", Start: startOn("2027-01-01"), Postings: pair("Expenses:Fun", 1000)})
			},
			shows: []string{"Zoo", "Aaa"},
		},
		{
			name: "a transfer, an estimate and an end",
			setup: func(t *testing.T, st *core.Local) {
				scheduled(t, st, core.NewSchedule{Name: "Sparen", Start: startOn("2026-11-01"), Transfer: true,
					Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: "Assets:Savings", Amount: eur(20000)}}})
				scheduled(t, st, core.NewSchedule{Name: "Strom", Start: startOn("2026-11-15"), Tolerance: 25, Postings: pair("Expenses:Power", 8000)})
				scheduled(t, st, core.NewSchedule{Name: "Haftpflicht", Unit: core.EveryYear, Start: startOn("2027-03-01"), End: startOn("2028-12-31"), Postings: pair("Expenses:Insurance", 30000)})
			},
			shows: []string{"⇄ Sparen", "-200.00 EUR", "transfer to Savings", "Strom", "~-80.00 EUR", "estimate ±25%", "Haftpflicht", "every year", "ends 2028-12-31"},
		},
		{
			name: "the day a balance would fall under its minimum, and what takes the most",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-10-02", "ACME", "Assets:Bank", "Income:Salary", 300000)
				scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-11-28"), Postings: pair("Expenses:Rent", 90000)})
				minimumOf(t, st, "Assets:Bank", 250000)
			},
			shows: []string{"Bank would fall to 2,023.00 EUR on 2026-11-28 (Rent takes the most), below its minimum of 2,500.00"},
		},
		{
			name: "under it already",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-10-02", "ACME", "Assets:Bank", "Income:Salary", 300000)
				scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-11-28"), Postings: pair("Expenses:Rent", 90000)})
				minimumOf(t, st, "Assets:Bank", 400000)
			},
			shows: []string{"Bank holds 2,923.00 EUR, below its minimum of 4,000.00 EUR", "Rent"},
			hides: []string{"would fall"},
		},
		{
			name: "a date nobody answered counts today",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-10-02", "ACME", "Assets:Bank", "Income:Salary", 300000)
				scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-09-28"), Postings: pair("Expenses:Rent", 90000)})
				minimumOf(t, st, "Assets:Bank", 250000)
			},
			shows: []string{"Bank would fall to 2,023.00 EUR on 2026-10-22 (Rent takes the most)", "1 date is waiting"},
		},
		{
			name: "above its minimum: nothing is said",
			setup: func(t *testing.T, st *core.Local) {
				book(t, st, "2026-10-02", "ACME", "Assets:Bank", "Income:Salary", 300000)
				scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-11-28"), Postings: pair("Expenses:Rent", 90000)})
				minimumOf(t, st, "Assets:Bank", 100000)
			},
			shows: []string{"Rent"},
			hides: []string{"below its minimum", "would fall"},
		},
		{
			name: "a minimum with no schedules is still said",
			setup: func(t *testing.T, st *core.Local) {
				minimumOf(t, st, "Assets:Bank", 100000)
			},
			shows: []string{"Bank holds -77.00 EUR, below its minimum of 1,000.00 EUR", "No schedules yet"},
		},
		{
			name: "a minimum on another account is its own",
			setup: func(t *testing.T, st *core.Local) {
				scheduled(t, st, core.NewSchedule{Name: "Sparen", Start: startOn("2026-11-01"), Transfer: true,
					Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: "Assets:Savings", Amount: eur(20000)}}})
				minimumOf(t, st, "Assets:Savings", 50000)
			},
			shows: []string{"Savings holds 5.00 EUR, below its minimum of 500.00 EUR"},
			hides: []string{"Bank holds"},
		},
		{
			name: "every other week",
			setup: func(t *testing.T, st *core.Local) {
				scheduled(t, st, core.NewSchedule{Name: "Bike", Every: 2, Unit: core.EveryWeek, Start: startOn("2026-11-02"), Postings: pair("Expenses:Fun", 1000)})
			},
			shows: []string{"every 2 weeks"},
		},
		{
			name: "one with several postings has no single amount",
			setup: func(t *testing.T, st *core.Local) {
				scheduled(t, st, core.NewSchedule{Name: "Split", Start: startOn("2026-11-02"), Postings: []ledger.Posting{
					{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Fun", Amount: eur(600)}, {Account: "Expenses:Power", Amount: eur(400)}}})
			},
			shows: []string{"Split", "2026-11-02"},
			hides: []string{"-10.00"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			if tc.setup != nil {
				tc.setup(t, st)
			}
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, "F "+tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_forecastScreen_states(t *testing.T) {
	status := func(name, next string, overdue int, edit func(*core.ScheduleStatus)) core.ScheduleStatus {
		s := core.ScheduleStatus{
			Schedule: core.Schedule{
				ID: 1, Name: name, Every: 1, Unit: core.EveryMonth, Start: "2026-01-01",
				Postings: []core.PostingView{{Account: "Assets:Bank", Amount: eur(-5000)}, {Account: "Expenses:Fun", Amount: eur(5000)}},
			},
			Next: next, Overdue: overdue,
		}
		if edit != nil {
			edit(&s)
		}
		return s
	}
	paused := func(s *core.ScheduleStatus) { s.Pauses = []core.SchedulePause{{From: "2026-10-01"}} }
	tcs := []struct {
		name     string
		statuses []core.ScheduleStatus
		shows    []string
		hides    []string
	}{
		{
			name:     "paused, with nothing waiting",
			statuses: []core.ScheduleStatus{status("Gym", "", 0, paused)},
			shows:    []string{"Gym", "paused", "every month"},
			hides:    []string{"waiting", "ended"},
		},
		{
			name:     "paused, with a date still waiting",
			statuses: []core.ScheduleStatus{status("Gym", "2026-09-01", 1, paused)},
			shows:    []string{"! 2026-09-01", "1 date is waiting", "paused"},
		},
		{
			name:     "run out",
			statuses: []core.ScheduleStatus{status("Course", "", 0, func(s *core.ScheduleStatus) { s.End = "2026-06-30" })},
			shows:    []string{"Course", "ended", "ends 2026-06-30"},
			hides:    []string{"paused"},
		},
		{
			name: "the dates waiting are added up over the schedules",
			statuses: []core.ScheduleStatus{
				status("A", "2026-08-01", 3, nil),
				status("B", "2026-09-01", 2, nil),
				status("C", "2026-12-01", 0, nil),
			},
			shows: []string{"5 dates are waiting"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			press(m, "F")
			m.schedules = tc.statuses

			// when
			view := m.View()

			// then
			checkView(t, view, tc.shows, tc.hides)
		})
	}
}

func Test_Update_forecastKeys(t *testing.T) {
	type observed struct {
		Selected string
		Position string
	}
	tcs := []struct {
		name  string
		keys  string
		want  observed
		shows []string // as well as the position
		hides []string
	}{
		{name: "the first is selected", keys: "F", want: observed{Selected: "S01", Position: "1 of 12"}, hides: []string{"S12"}},
		{name: "down", keys: "F down down", want: observed{Selected: "S03", Position: "3 of 12"}},
		{name: "j and k", keys: "F j j j k", want: observed{Selected: "S03", Position: "3 of 12"}},
		{name: "up stops at the first", keys: "F up", want: observed{Selected: "S01", Position: "1 of 12"}},
		{name: "G goes to the last and shows it", keys: "F G", want: observed{Selected: "S12", Position: "12 of 12"}, shows: []string{"S12"}, hides: []string{"S01"}},
		{name: "down stops at the last", keys: "F G down", want: observed{Selected: "S12", Position: "12 of 12"}},
		{name: "g goes back", keys: "F G g", want: observed{Selected: "S01", Position: "1 of 12"}, shows: []string{"S01"}},
		{name: "the other screens keep their keys", keys: "F down B F", want: observed{Selected: "S02", Position: "2 of 12"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			for i := 1; i <= 12; i++ {
				scheduled(t, st, core.NewSchedule{
					Name: fmt.Sprintf("S%02d", i), Start: startOn(fmt.Sprintf("2026-11-%02d", i)), Postings: pair("Expenses:Fun", 1000),
				})
			}
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)

			// then
			got := observed{Selected: m.schedules[m.sched.cursor].Name, Position: fmt.Sprintf("%d of %d", m.sched.cursor+1, len(m.schedules))}
			view := m.View()
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
			checkView(t, view, append([]string{tc.want.Position}, tc.shows...), tc.hides)
		})
	}
}

// testSchedules defines the schedules the fit and language tests show on the Forecast: one
// with a date waiting, a transfer into a dollar account, an estimate and one with an
// end, on a ledger that has Assets:Bank in euros and dollars in account.
func testSchedules(t *testing.T, st *core.Local, dollars string) {
	t.Helper()
	scheduled(t, st, core.NewSchedule{Name: "Miete", Start: startOn("2026-09-28"), Postings: pair("Expenses:Rent", 90000)})
	scheduled(t, st, core.NewSchedule{Name: "Sparen", Every: 2, Unit: core.EveryWeek, Start: startOn("2026-11-01"), Transfer: true,
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: dollars, Amount: ledger.Money{Minor: 21500, Currency: "USD"}}}})
	scheduled(t, st, core.NewSchedule{Name: "Strom", Start: startOn("2026-11-15"), Tolerance: 25, Postings: pair("Expenses:Power", 8000)})
	scheduled(t, st, core.NewSchedule{Name: "Haftpflicht", Unit: core.EveryYear, Start: startOn("2027-03-01"), End: startOn("2028-12-31"), Postings: pair("Expenses:Insurance", 30000)})
}

// minimumOf gives account a minimum balance.
func minimumOf(t *testing.T, st *core.Local, account string, minor int64) {
	t.Helper()
	if err := st.SetMinimumBalance(t.Context(), account, eur(minor)); err != nil {
		t.Fatal(err)
	}
}
