package tui

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// projected is an account worth start, which the schedules move on the days given:
// a day is its date and the balance after it.
func projected(start int64, days ...struct {
	date    string
	change  int64
	balance int64
}) core.ProjectedAccount {
	a := core.ProjectedAccount{Name: "Assets:Bank", Type: ledger.Assets, Start: eur(start)}
	for _, d := range days {
		a.Days = append(a.Days, core.ProjectedDay{Date: d.date, Change: eur(d.change), Balance: eur(d.balance)})
	}
	return a
}

func Test_weekRows(t *testing.T) {
	type day = struct {
		date    string
		change  int64
		balance int64
	}
	type observed struct {
		Count int
		First []weekRow // as many weeks as the case lists
	}
	row := func(date string, change, balance int64, moved bool) weekRow {
		return weekRow{date: date, change: eur(change), balance: eur(balance), moved: moved}
	}
	tcs := []struct {
		name    string
		account core.ProjectedAccount
		want    observed
	}{
		{
			name:    "nothing moves it: a week after week of the same balance",
			account: projected(1000),
			want: observed{Count: 13, First: []weekRow{
				row("2026-10-29", 0, 1000, false), row("2026-11-05", 0, 1000, false),
			}},
		},
		{
			name:    "a move falls in the week it ends in",
			account: projected(1000, day{"2026-11-02", -300, 700}),
			want: observed{Count: 13, First: []weekRow{
				row("2026-10-29", 0, 1000, false), row("2026-11-05", -300, 700, true), row("2026-11-12", 0, 700, false),
			}},
		},
		{
			name:    "the last day of a week is in it, the next day is in the next",
			account: projected(1000, day{"2026-10-29", -100, 900}, day{"2026-10-30", -200, 700}),
			want: observed{Count: 13, First: []weekRow{
				row("2026-10-29", -100, 900, true), row("2026-11-05", -200, 700, true),
			}},
		},
		{
			name:    "today is in the first week: a date nobody answered",
			account: projected(1000, day{"2026-10-22", -300, 700}),
			want: observed{Count: 13, First: []weekRow{
				row("2026-10-29", -300, 700, true), row("2026-11-05", 0, 700, false),
			}},
		},
		{
			name:    "two moves in a week are added",
			account: projected(1000, day{"2026-11-02", -300, 700}, day{"2026-11-04", 500, 1200}),
			want: observed{Count: 13, First: []weekRow{
				row("2026-10-29", 0, 1000, false), row("2026-11-05", 200, 1200, true),
			}},
		},
		{
			name:    "a move that nets to nothing is still a move",
			account: projected(1000, day{"2026-11-02", 0, 1000}),
			want: observed{Count: 13, First: []weekRow{
				row("2026-10-29", 0, 1000, false), row("2026-11-05", 0, 1000, true),
			}},
		},
		{
			name:    "the thirteenth week ends 91 days on, and a later move is not in it",
			account: projected(1000, day{"2027-01-21", -100, 900}, day{"2027-01-22", -100, 800}),
			want: observed{Count: 13, First: []weekRow{
				row("2026-10-29", 0, 1000, false),
			}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			from := startOn("2026-10-22")

			// when
			rows := weekRows(tc.account, from)

			// then
			got := observed{Count: len(rows), First: rows[:len(tc.want.First)]}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(weekRow{})); diff != "" {
				t.Errorf("weekRows mismatch (-want +got):\n%s", diff)
			}
			if last := rows[len(rows)-1]; last.date != "2027-01-21" {
				t.Errorf("last week ends %s, want 2027-01-21", last.date)
			}
		})
	}
}

func Test_weekRows_lastWeek(t *testing.T) {
	tcs := []struct {
		name    string
		account core.ProjectedAccount
		want    weekRow
	}{
		{
			name: "a move on the last day counts",
			account: projected(1000, struct {
				date    string
				change  int64
				balance int64
			}{"2027-01-21", -100, 900}),
			want: weekRow{date: "2027-01-21", change: eur(-100), balance: eur(900), moved: true},
		},
		{
			name: "a move the day after does not",
			account: projected(1000, struct {
				date    string
				change  int64
				balance int64
			}{"2027-01-22", -100, 900}),
			want: weekRow{date: "2027-01-21", change: eur(0), balance: eur(1000)},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			from := startOn("2026-10-22")

			// when
			rows := weekRows(tc.account, from)

			// then
			if diff := cmp.Diff(tc.want, rows[len(rows)-1], cmp.AllowUnexported(weekRow{})); diff != "" {
				t.Errorf("last week mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_balancesScreen(t *testing.T) {
	rent := func(t *testing.T, st *core.Local, start string) {
		book(t, st, "2026-10-02", "ACME", "Assets:Bank", "Income:Salary", 300000) // the bank holds 2,923.00 EUR
		scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn(start), Postings: pair("Expenses:Rent", 90000)})
	}
	tcs := []struct {
		name  string
		setup func(t *testing.T, st *core.Local)
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "tab shows the balances, with today and the week a move falls in",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2026-11-28") },
			keys:  "F tab",
			shows: []string{"Balances", "Bank", "Today", "2,923.00 EUR", "Change", "Balance", "2026-10-29", "2026-12-03", "-900.00 EUR", "2,023.00 EUR", "at the end of each week", "tab schedules"},
			hides: []string{"Rent", "Next"},
		},
		{
			name:  "tab again goes back",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2026-11-28") },
			keys:  "F tab tab",
			shows: []string{"Schedules", "Rent", "tab balances"},
			hides: []string{"Balances", "Today"},
		},
		{
			name:  "a date nobody answered is in the first week, with the next one",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2026-09-28") },
			keys:  "F tab",
			shows: []string{"2026-10-29", "-1,800.00 EUR", "1,123.00 EUR"},
		},
		{
			name:  "a week under the minimum is marked, the ones above are not",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2026-11-28"); minimumOf(t, st, "Assets:Bank", 250000) },
			keys:  "F tab",
			shows: []string{"! 2,023.00 EUR", "Bank would fall to 2,023.00 EUR on 2026-11-28"},
			hides: []string{"! 2,923.00"},
		},
		{
			name:  "equal to the minimum is not under it",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2026-11-28"); minimumOf(t, st, "Assets:Bank", 112300) }, // the lowest it falls, with the second rent
			keys:  "F tab",
			shows: []string{"1,123.00 EUR"},
			hides: []string{"!", "below its minimum"},
		},
		{
			name:  "the last week is the thirteenth: a date in it is counted",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2027-01-15") },
			keys:  "F tab G",
			shows: []string{"2027-01-21", "-900.00 EUR", "2,023.00 EUR"},
		},
		{
			name:  "a date after the thirteenth week is not",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2027-01-22") },
			keys:  "F tab",
			shows: []string{"Bank", "No schedule moves this account."},
			hides: []string{"-900.00 EUR"},
		},
		{
			name:  "an account no schedule moves says so",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2026-11-28") },
			keys:  "F tab G",
			shows: []string{"Savings", "No schedule moves this account."},
		},
		{
			name:  "the minimum popup opens over the balances",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2026-11-28") },
			keys:  "F tab m",
			shows: []string{"Minimum balance"},
		},
		{
			name:  "a minimum set from there marks the weeks",
			setup: func(t *testing.T, st *core.Local) { rent(t, st, "2026-11-28") },
			keys:  "F tab m text:2500 enter",
			shows: []string{"! 2,023.00 EUR", "Minimum of Bank: 2,500.00 EUR"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			tc.setup(t, st)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_balancesScreen_noAccounts(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
	}{
		{name: "nothing to project", keys: "F tab", shows: []string{"Balances", "No asset or liability accounts yet."}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := noAccountsModel(t)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

func Test_Update_balancesKeys(t *testing.T) {
	type observed struct {
		Balances bool
		Top      int
		Mode     string
		Cursor   int // the schedule selected
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "the schedules first", keys: "F", want: observed{}},
		{name: "tab", keys: "F tab", want: observed{Balances: true}},
		{name: "tab twice", keys: "F tab tab", want: observed{}},
		{name: "down scrolls a line", keys: "F tab down", want: observed{Balances: true, Top: 1}},
		{name: "j and k", keys: "F tab j j j k", want: observed{Balances: true, Top: 2}},
		{name: "up stops at the top", keys: "F tab up", want: observed{Balances: true}},
		{name: "G goes to the end and stays inside", keys: "F tab G", want: observed{Balances: true, Top: -1}},
		{name: "pgdown scrolls a page", keys: "F tab pgdown", want: observed{Balances: true, Top: -1}},
		{name: "pgup comes back a page", keys: "F tab pgdown pgup", want: observed{Balances: true}},
		{name: "g comes back", keys: "F tab down down g", want: observed{Balances: true}},
		{name: "enter, n, e and p do nothing", keys: "F tab enter n e p", want: observed{Balances: true}},
		{name: "the schedule selected is kept", keys: "F down tab tab", want: observed{Cursor: 1}},
		{name: "m opens the minimum", keys: "F tab m", want: observed{Balances: true, Mode: "minimum"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			book(t, st, "2026-10-02", "ACME", "Assets:Bank", "Income:Salary", 300000)
			scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-11-28"), Postings: pair("Expenses:Rent", 90000)})
			scheduled(t, st, core.NewSchedule{Name: "Gym", Start: startOn("2026-11-29"), Postings: pair("Expenses:Fun", 3000)})
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)
			m.View() // holds the scroll inside the lines

			// then
			got := observed{Balances: m.balances, Top: m.balTop, Mode: modeNames[m.mode], Cursor: m.sched.cursor}
			if tc.want.Top == -1 { // the end: some lines are scrolled away, and no more than there are
				if got.Top <= 0 {
					t.Errorf("G did not scroll: top = %d", got.Top)
				}
				got.Top = -1
			}
			if tc.want.Mode == "" {
				tc.want.Mode = modeNames[modeList]
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
