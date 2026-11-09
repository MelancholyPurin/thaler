package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

var (
	addRent    = []string{"cli", "schedule", "add", "Rent", "--unit", "month", "-a", "Assets:Bank", "--amount=-900", "-t", "Expenses:Rent", "-p", "Landlord", "--start", "2026-08-28"}
	addSalary  = []string{"cli", "schedule", "add", "Salary", "--unit", "month", "-a", "Assets:Bank", "--amount=3000", "-t", "Income:Salary", "--start", "2026-09-25"}
	addInsure  = []string{"cli", "schedule", "add", "Insurance", "--unit", "years", "-a", "Assets:Bank", "--amount=-300", "-t", "Expenses:Insurance", "--start", "2026-03-01"}
	confirmAug = []string{"cli", "schedule", "confirm", "1", "2026-08-28"}
)

func Test_schedule(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "none yet",
			setup: nil,
			args:  []string{"cli", "schedule"},
			want:  "No schedules. Add one with: thaler cli schedule add Rent --unit month -a Assets:Bank --amount=-900 -t Expenses:Rent --start 2026-11-28\n",
		},
		{
			name:  "adding one",
			setup: [][]string{addBank},
			args:  addRent,
			want:  "Schedule #1 Rent: every month from 2026-08-28, -900.00 EUR  Assets:Bank -> Expenses:Rent\n",
		},
		{
			name:  "every other week, income by its sign",
			setup: [][]string{addBank},
			args:  []string{"cli", "schedule", "add", "Pay", "--every", "2", "--unit", "week", "-a", "Assets:Bank", "--amount", "500", "-d", "01.09.2026"},
			want:  "Schedule #1 Pay: every 2 weeks from 2026-09-01, +500.00 EUR  Assets:Bank -> Income:Uncategorized\n",
		},
		{
			name:  "the schedules, by name",
			setup: [][]string{addBank, addRent, addSalary, addInsure},
			args:  []string{"cli", "schedule", "list"},
			want: "Schedule   ID      Repeats       Start       Amount\n" +
				"Insurance   3   every year  2026-03-01  -300.00 EUR\n" +
				"Rent        1  every month  2026-08-28  -900.00 EUR\n" +
				"Salary      2  every month  2026-09-25  3000.00 EUR\n",
		},
		{
			name:  "what is due, oldest first",
			setup: [][]string{addBank, addRent, addSalary},
			args:  []string{"cli", "schedule", "due"},
			want: "Date        ID       Amount  Schedule\n" +
				"2026-08-28   1  -900.00 EUR      Rent\n" +
				"2026-09-25   2  3000.00 EUR    Salary\n" +
				"2026-09-28   1  -900.00 EUR      Rent\n" +
				"\nAnswer each with: thaler cli schedule confirm ID DATE [--amount=N], or thaler cli schedule skip ID DATE\n",
		},
		{
			name:  "due up to another date",
			setup: [][]string{addBank, addRent},
			args:  []string{"cli", "schedule", "due", "--until", "2026-08-31"},
			want: "Date        ID       Amount  Schedule\n" +
				"2026-08-28   1  -900.00 EUR      Rent\n" +
				"\nAnswer each with: thaler cli schedule confirm ID DATE [--amount=N], or thaler cli schedule skip ID DATE\n",
		},
		{
			name:  "nothing is due before the start",
			setup: [][]string{addBank, addRent},
			args:  []string{"cli", "schedule", "due", "--until", "2026-08-27"},
			want:  "Nothing is due up to 2026-08-27.\n",
		},
		{
			name:  "confirming books it on its own day",
			setup: [][]string{addBank, addRent},
			args:  confirmAug,
			want:  "Booked #1  2026-08-28  -900.00 EUR  Assets:Bank -> Expenses:Rent\n",
		},
		{
			name:  "confirming for another amount",
			setup: [][]string{addBank, addRent},
			args:  []string{"cli", "schedule", "confirm", "1", "2026-08-28", "--amount=-912.40"},
			want:  "Booked #1  2026-08-28  -912.40 EUR  Assets:Bank -> Expenses:Rent\n",
		},
		{
			name:  "a confirmed date is not due again",
			setup: [][]string{addBank, addRent, confirmAug},
			args:  []string{"cli", "schedule", "due"},
			want: "Date        ID       Amount  Schedule\n" +
				"2026-09-28   1  -900.00 EUR      Rent\n" +
				"\nAnswer each with: thaler cli schedule confirm ID DATE [--amount=N], or thaler cli schedule skip ID DATE\n",
		},
		{
			name:    "confirming twice",
			setup:   [][]string{addBank, addRent, confirmAug},
			args:    confirmAug,
			wantErr: `2026-08-28 of "Rent" was already confirmed or skipped`,
		},
		{
			name:    "a date the schedule does not fall on",
			setup:   [][]string{addBank, addRent},
			args:    []string{"cli", "schedule", "confirm", "1", "2026-08-29"},
			wantErr: `"Rent" does not fall on 2026-08-29`,
		},
		{
			name:  "skipping books nothing",
			setup: [][]string{addBank, addRent},
			args:  []string{"cli", "schedule", "skip", "1", "2026-08-28"},
			want:  "Skipped 2026-08-28 of Rent\n",
		},
		{
			name:  "a skipped date is not due again",
			setup: [][]string{addBank, addRent, {"cli", "schedule", "skip", "1", "2026-08-28"}},
			args:  []string{"cli", "schedule", "due"},
			want: "Date        ID       Amount  Schedule\n" +
				"2026-09-28   1  -900.00 EUR      Rent\n" +
				"\nAnswer each with: thaler cli schedule confirm ID DATE [--amount=N], or thaler cli schedule skip ID DATE\n",
		},
		{
			name:    "a schedule that does not exist",
			setup:   [][]string{addBank},
			args:    []string{"cli", "schedule", "skip", "7", "2026-08-28"},
			wantErr: "schedule 7 does not exist",
		},
		{
			name:    "an id that is not a number",
			args:    []string{"cli", "schedule", "skip", "rent", "2026-08-28"},
			wantErr: `"rent" is not a schedule number; see thaler cli schedule`,
		},
		{
			name:    "a unit that is not one",
			setup:   [][]string{addBank},
			args:    []string{"cli", "schedule", "add", "Rent", "--unit", "day", "-a", "Assets:Bank", "--amount=-1"},
			wantErr: `--unit must be week, month or year, not "day"`,
		},
		{
			name:    "a name taken",
			setup:   [][]string{addBank, addRent},
			args:    []string{"cli", "schedule", "add", "rent", "--unit", "month", "-a", "Assets:Bank", "--amount=-1"},
			wantErr: `there is already a schedule called "rent"`,
		},
		{
			name:  "deleting one",
			setup: [][]string{addBank, addRent},
			args:  []string{"cli", "schedule", "delete", "1"},
			want:  "Deleted schedule #1 Rent\n",
		},
		{
			name:    "deleting one that does not exist",
			args:    []string{"cli", "schedule", "delete", "1"},
			wantErr: "schedule 1 does not exist",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_schedule_edit(t *testing.T) {
	listed := []string{"cli", "schedule", "list"}
	tcs := []struct {
		name    string
		setup   [][]string
		args    []string
		want    string // what edit says
		wantNow string // the list afterwards
		wantErr string
	}{
		{
			name: "a new amount follows on the category side",
			args: []string{"cli", "schedule", "edit", "1", "--amount=-950"}, want: "Changed schedule #1 Rent\n",
			wantNow: "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-08-28  -950.00 EUR\n",
		},
		{
			name: "the rhythm and the name, the rest stays",
			args: []string{"cli", "schedule", "edit", "1", "--name", "Flat", "--every", "3", "--unit", "month"}, want: "Changed schedule #1 Flat\n",
			wantNow: "Schedule  ID         Repeats       Start       Amount\nFlat       1  every 3 months  2026-08-28  -900.00 EUR\n",
		},
		{
			name: "a new start",
			args: []string{"cli", "schedule", "edit", "1", "-d", "2026-09-01"}, want: "Changed schedule #1 Rent\n",
			wantNow: "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-09-01  -900.00 EUR\n",
		},
		{
			name:    "nothing asked",
			args:    []string{"cli", "schedule", "edit", "1"},
			wantErr: "nothing to change: give at least one of --name, --every, --unit, --start, --amount, --payee, --memo",
			wantNow: "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-08-28  -900.00 EUR\n",
		},
		{
			name:    "a schedule that does not exist",
			args:    []string{"cli", "schedule", "edit", "4", "--amount=-1"},
			wantErr: "no schedule number 4; see thaler cli schedule",
			wantNow: "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-08-28  -900.00 EUR\n",
		},
		{
			name:    "a zero amount",
			args:    []string{"cli", "schedule", "edit", "1", "--amount=0"},
			wantErr: "amount must not be zero",
			wantNow: "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-08-28  -900.00 EUR\n",
		},
		{
			name:    "a bad rhythm leaves it as it was",
			args:    []string{"cli", "schedule", "edit", "1", "--every", "0"},
			wantErr: "a schedule repeats every 1 or more weeks, months or years, not every 0",
			wantNow: "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-08-28  -900.00 EUR\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, nil, addBank, addRent)

			// when
			got, err := e.run(tc.args...)
			now, lerr := e.run(listed...)

			// then
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			if lerr != nil {
				t.Fatal(lerr)
			}
			if diff := cmp.Diff(tc.wantNow, now); diff != "" {
				t.Errorf("schedules afterwards (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_schedule_confirmedTransactions(t *testing.T) {
	tcs := []struct {
		name  string
		setup [][]string
		want  string // thaler cli list afterwards
	}{
		{
			name:  "a confirmed rent is in the ledger with its payee",
			setup: [][]string{addBank, addRent, confirmAug},
			want:  "2026-08-28  #1     Landlord\n    Assets:Bank    -900.00 EUR\n    Expenses:Rent  +900.00 EUR\n",
		},
		{
			name:  "another amount is what the ledger holds",
			setup: [][]string{addBank, addRent, {"cli", "schedule", "confirm", "1", "2026-08-28", "--amount=-912.40"}},
			want:  "2026-08-28  #1     Landlord\n    Assets:Bank    -912.40 EUR\n    Expenses:Rent  +912.40 EUR\n",
		},
		{
			name:  "a skipped one books nothing",
			setup: [][]string{addBank, addRent, {"cli", "schedule", "skip", "1", "2026-08-28"}},
			want:  "No transactions found.\n",
		},
		{
			name:  "defining one books nothing",
			setup: [][]string{addBank, addRent},
			want:  "No transactions found.\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, nil, tc.setup...)

			// when
			got, err := e.run("cli", "list")

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ledger mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
