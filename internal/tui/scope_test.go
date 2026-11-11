package tui

import (
	"golang.org/x/text/language"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// scopeLedger is newModel's ledger with a third account, Cash, which no schedule
// moves, and three schedules: Rent on the bank, Interest into the savings and Saving,
// a transfer between the two. Rent and Interest each have a date nobody answered.
func scopeLedger(t *testing.T, st *core.Local) {
	t.Helper()
	openAccounts(t, st, "Assets:Cash EUR")
	book(t, st, "2026-10-02", "ACME", "Assets:Bank", "Income:Salary", 300000)
	scheduled(t, st, core.NewSchedule{Name: "Rent", Start: startOn("2026-09-28"), Postings: pair("Expenses:Rent", 90000)})
	scheduled(t, st, core.NewSchedule{Name: "Interest", Start: startOn("2026-10-01"), Postings: []ledger.Posting{
		{Account: "Assets:Savings", Amount: eur(500)}, {Account: "Income:Interest", Amount: eur(-500)}}})
	scheduled(t, st, core.NewSchedule{Name: "Saving", Start: startOn("2026-11-15"), Transfer: true, Postings: []ledger.Posting{
		{Account: "Assets:Bank", Amount: eur(-20000)}, {Account: "Assets:Savings", Amount: eur(20000)}}})
}

func Test_forecastScope(t *testing.T) {
	tcs := []struct {
		name  string
		setup func(t *testing.T, st *core.Local)
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "all of them to begin with",
			keys:  "F",
			shows: []string{"Schedules", "Rent", "Interest", "Saving", "2 dates are waiting", "a account"},
			hides: []string{"Schedules ·"},
		},
		{
			name:  "a limits it to the first account: its schedules, and the transfers from it",
			keys:  "F a",
			shows: []string{"Schedules · Bank", "Rent", "Saving", "1 date is waiting"},
			hides: []string{"Interest", "Schedules · Savings"},
		},
		{
			name:  "the next account, with the transfers into it",
			keys:  "F a a a",
			shows: []string{"Schedules · Savings", "Interest", "Saving"},
			hides: []string{"Rent", "Schedules · Bank"},
		},
		{
			name:  "an account no schedule moves",
			keys:  "F a a",
			shows: []string{"Schedules · Cash", "No schedule moves Cash."},
			hides: []string{"Rent", "Interest", "Saving", "waiting"},
		},
		{
			name:  "after the last, all of them again",
			keys:  "F a a a a",
			shows: []string{"Schedules", "Rent", "Interest", "Saving", "2 dates are waiting"},
			hides: []string{"Schedules ·"},
		},
		{
			name:  "the balances of that account only, with its name in the title",
			keys:  "F a tab",
			shows: []string{"Balances · Bank", "Today", "2,923.00 EUR"},
			hides: []string{"Savings", "Cash"},
		},
		{
			name:  "the balances of an account no schedule moves",
			keys:  "F a a tab",
			shows: []string{"Balances · Cash", "No schedule moves this account."},
			hides: []string{"Bank", "Savings"},
		},
		{
			name:  "the scope is kept when the view changes",
			keys:  "F a tab tab",
			shows: []string{"Schedules · Bank", "Rent"},
			hides: []string{"Interest"},
		},
		{
			name:  "only the warnings of the account",
			setup: func(t *testing.T, st *core.Local) { minimumOf(t, st, "Assets:Bank", 400000) },
			keys:  "F a a",
			shows: []string{"Schedules · Cash"},
			hides: []string{"below its minimum"},
		},
		{
			name:  "the warning of the account is there",
			setup: func(t *testing.T, st *core.Local) { minimumOf(t, st, "Assets:Bank", 400000) },
			keys:  "F a",
			shows: []string{"Bank holds 2,923.00 EUR, below its minimum of 4,000.00 EUR"},
		},
		{
			name:  "enter goes through the dates of that account only",
			keys:  "F a a a enter",
			shows: []string{"Interest"},
			hides: []string{"Rent"},
		},
		{
			name:  "nothing due for an account: nothing opens",
			keys:  "F a a enter",
			shows: []string{"Schedules · Cash"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			scopeLedger(t, st)
			if tc.setup != nil {
				tc.setup(t, st)
			}
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

func Test_Update_forecastScope(t *testing.T) {
	type observed struct {
		Account   string
		Mode      string
		Schedules int
		Cursor    int
		Top       int
	}
	tcs := []struct {
		name string
		keys string
		want observed
	}{
		{name: "all", keys: "F", want: observed{Mode: "list", Schedules: 3}},
		{name: "a", keys: "F a", want: observed{Account: "Assets:Bank", Mode: "list", Schedules: 2}},
		{name: "a a", keys: "F a a", want: observed{Account: "Assets:Cash", Mode: "list"}},
		{name: "a a a", keys: "F a a a", want: observed{Account: "Assets:Savings", Mode: "list", Schedules: 2}},
		{name: "a a a a: round again", keys: "F a a a a", want: observed{Mode: "list", Schedules: 3}},
		{name: "the cursor starts again at the top", keys: "F down down a", want: observed{Account: "Assets:Bank", Mode: "list", Schedules: 2}},
		{name: "the balances scroll starts again", keys: "F tab down down a", want: observed{Account: "Assets:Bank", Mode: "list", Schedules: 2}},
		{name: "a does nothing in a popup that types", keys: "F m a", want: observed{Mode: "minimum", Schedules: 3}},
		{name: "a on another screen is not this key", keys: "B a", want: observed{Mode: "list", Schedules: 3}},
		{name: "a new schedule starts on the account", keys: "F a a a n", want: observed{Account: "Assets:Savings", Mode: "schedule", Schedules: 2}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			scopeLedger(t, st)
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}

			// when
			play(m, tc.keys)
			m.View()

			// then
			got := observed{Account: m.fcAccount, Mode: modeNames[m.mode], Schedules: len(m.schedules), Cursor: m.sched.cursor, Top: m.balTop}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("observed mismatch (-want +got):\n%s", diff)
			}
			if m.mode == modeSchedule {
				if name := m.accounts[m.schedForm.from].account.Name; name != "Assets:Savings" {
					t.Errorf("the new schedule starts on %s, want Assets:Savings", name)
				}
			}
		})
	}
}

func Test_refreshSchedules_scope(t *testing.T) {
	tcs := []struct {
		name        string
		scope       string
		wantScope   string
		wantNames   []string
		afterCreate func(t *testing.T, st *core.Local) int64 // a schedule made after the scope was set, whose id is selected
		wantCursor  string
	}{
		{name: "no scope", wantNames: []string{"Interest", "Rent", "Saving"}},
		{name: "the bank", scope: "Assets:Bank", wantScope: "Assets:Bank", wantNames: []string{"Rent", "Saving"}},
		{name: "an account that is gone is dropped", scope: "Assets:Gone", wantNames: []string{"Interest", "Rent", "Saving"}},
		{
			name: "a schedule made outside the scope shows them all", scope: "Assets:Bank",
			afterCreate: func(t *testing.T, st *core.Local) int64 {
				id, err := st.CreateSchedule(t.Context(), core.NewSchedule{
					Name: "Cash gift", Every: 1, Unit: core.EveryMonth, Start: startOn("2026-12-01"),
					Postings: []ledger.Posting{{Account: "Assets:Cash", Amount: eur(1000)}, {Account: "Income:Gifts", Amount: eur(-1000)}},
				})
				if err != nil {
					t.Fatal(err)
				}
				return id
			},
			wantNames: []string{"Cash gift", "Interest", "Rent", "Saving"}, wantCursor: "Cash gift",
		},
		{
			name: "one made inside it keeps it", scope: "Assets:Bank", wantScope: "Assets:Bank",
			afterCreate: func(t *testing.T, st *core.Local) int64 {
				id, err := st.CreateSchedule(t.Context(), core.NewSchedule{
					Name: "Gym", Every: 1, Unit: core.EveryMonth, Start: startOn("2026-12-01"), Postings: pair("Expenses:Fun", 3000),
				})
				if err != nil {
					t.Fatal(err)
				}
				return id
			},
			wantNames: []string{"Gym", "Rent", "Saving"}, wantCursor: "Gym",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			scopeLedger(t, st)
			m.fcAccount = tc.scope
			var id int64
			if tc.afterCreate != nil {
				id = tc.afterCreate(t, st)
			}

			// when
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}
			if id != 0 {
				m.selectSchedule(id)
			}

			// then
			names := make([]string, 0, len(m.schedules))
			for _, s := range m.schedules {
				names = append(names, s.Name)
			}
			if diff := cmp.Diff(tc.wantNames, sortedStrings(names)); diff != "" {
				t.Errorf("schedules mismatch (-want +got):\n%s", diff)
			}
			if m.fcAccount != tc.wantScope {
				t.Errorf("scope = %q, want %q", m.fcAccount, tc.wantScope)
			}
			if tc.wantCursor != "" {
				if got := m.schedules[m.sched.cursor].Name; got != tc.wantCursor {
					t.Errorf("selected %s, want %s", got, tc.wantCursor)
				}
			}
		})
	}
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func Test_forecastScope_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		shows []string
		hides []string
	}{
		{name: "Spanish", tag: language.Spanish, shows: []string{"Programaciones · Cash", "Ninguna programación mueve Cash."}, hides: []string{"No schedule moves"}},
		{name: "German", tag: language.German, shows: []string{"Zeitpläne · Cash", "Kein Zeitplan bewegt Cash."}, hides: []string{"No schedule moves"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR", "Assets:Cash EUR")
			scheduled(t, st, core.NewSchedule{Name: "Miete", Start: startOn("2026-11-28"), Postings: pair("Expenses:Rent", 90000)})
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, "F a a")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
