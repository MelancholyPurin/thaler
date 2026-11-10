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
			name:  "adding one that ends",
			setup: [][]string{addBank},
			args:  append(append([]string{}, addRent...), "--end", "2027-06-28"),
			want:  "Schedule #1 Rent: every month from 2026-08-28 until 2027-06-28, -900.00 EUR  Assets:Bank -> Expenses:Rent\n",
		},
		{
			name:    "an end before the start",
			setup:   [][]string{addBank},
			args:    append(append([]string{}, addRent...), "--end", "2026-08-27"),
			wantErr: "the end of a schedule (2026-08-27) cannot be before its start (2026-08-28)",
		},
		{
			name:  "pausing",
			setup: [][]string{addBank, addRent},
			args:  []string{"cli", "schedule", "pause", "1"},
			want:  "Paused schedule #1 Rent: nothing is proposed until it is resumed\n",
		},
		{
			name:  "the list says what is paused and what ends",
			setup: [][]string{addBank, addRent, addSalary, {"cli", "schedule", "pause", "1"}, {"cli", "schedule", "edit", "1", "--end", "2027-06-28"}, {"cli", "schedule", "edit", "2", "--end", "2027-01-25"}},
			args:  []string{"cli", "schedule"},
			want: "Schedule  ID      Repeats       Start       Amount                     Note\n" +
				"Rent       1  every month  2026-08-28  -900.00 EUR  paused, ends 2027-06-28\n" +
				"Salary     2  every month  2026-09-25  3000.00 EUR          ends 2027-01-25\n",
		},
		{
			name:  "resuming takes the note away",
			setup: [][]string{addBank, addRent, {"cli", "schedule", "pause", "1"}},
			args:  []string{"cli", "schedule", "resume", "1"},
			want:  "Resumed schedule #1 Rent\n",
		},
		{
			name:  "resumed: no note column",
			setup: [][]string{addBank, addRent, {"cli", "schedule", "pause", "1"}, {"cli", "schedule", "resume", "1"}},
			args:  []string{"cli", "schedule"},
			want:  "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-08-28  -900.00 EUR\n",
		},
		{
			name:    "pausing one that does not exist",
			setup:   [][]string{addBank},
			args:    []string{"cli", "schedule", "pause", "3"},
			wantErr: "schedule 3 does not exist",
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
			name: "an end", args: []string{"cli", "schedule", "edit", "1", "--end", "2027-06-28"}, want: "Changed schedule #1 Rent\n",
			wantNow: "Schedule  ID      Repeats       Start       Amount             Note\nRent       1  every month  2026-08-28  -900.00 EUR  ends 2027-06-28\n",
		},
		{
			name: "changing something else keeps the end", setup: [][]string{{"cli", "schedule", "edit", "1", "--end", "2027-06-28"}},
			args: []string{"cli", "schedule", "edit", "1", "--amount=-950"}, want: "Changed schedule #1 Rent\n",
			wantNow: "Schedule  ID      Repeats       Start       Amount             Note\nRent       1  every month  2026-08-28  -950.00 EUR  ends 2027-06-28\n",
		},
		{
			name: "none takes the end away", setup: [][]string{{"cli", "schedule", "edit", "1", "--end", "2027-06-28"}},
			args: []string{"cli", "schedule", "edit", "1", "--end", "none"}, want: "Changed schedule #1 Rent\n",
			wantNow: "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-08-28  -900.00 EUR\n",
		},
		{
			name:    "an end before the start",
			args:    []string{"cli", "schedule", "edit", "1", "--end", "2026-08-01"},
			wantErr: "the end of a schedule (2026-08-01) cannot be before its start (2026-08-28)",
			wantNow: "Schedule  ID      Repeats       Start       Amount\nRent       1  every month  2026-08-28  -900.00 EUR\n",
		},
		{
			name:    "nothing asked",
			args:    []string{"cli", "schedule", "edit", "1"},
			wantErr: "nothing to change: give at least one of --name, --every, --unit, --start, --end, --amount, --to-amount, --tolerance, --payee, --memo",
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
			e := newCLIEnv(t, nil, append([][]string{addBank, addRent}, tc.setup...)...)

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

const rentCSV = csvHeader + `"29.08.26";"DAUERAUFTRAG";"Miete";"Landlord";"-900,00";"EUR";"Umsatz gebucht"` + "\n"

func Test_schedule_importSettles(t *testing.T) {
	files := map[string]string{"bank.csv": rentCSV}
	imp := func(extra ...string) []string {
		return append([]string{"cli", "import", "{dir}/bank.csv", "-a", "Assets:Bank"}, extra...)
	}
	tcs := []cliCase{
		{
			name: "previewed", files: files, setup: [][]string{addBank, addRent},
			args: imp("--dry-run"),
			want: "new  2026-08-29  Expenses:Rent                 Landlord  -900.00 EUR\n\n" +
				"Dry run: 1 new, 0 already imported, 0 pending row(s) ignored. Nothing was written.\n" +
				"1 row(s) are the bank's booking of a scheduled transaction and would answer it (see thaler cli schedule due).\n",
		},
		{
			name: "imported", files: files, setup: [][]string{addBank, addRent},
			args: imp(),
			want: "Imported 1 new transaction(s) into Assets:Bank; 0 already present (skipped); 0 pending row(s) ignored.\n" +
				"Settled 2026-08-28 of the schedule Rent (#1).\n",
		},
		{
			name: "the occurrence is no longer due", files: files, setup: [][]string{addBank, addRent, imp()},
			args: []string{"cli", "schedule", "due", "--until", "2026-08-31"},
			want: "Nothing is due up to 2026-08-31.\n",
		},
		{
			name: "no schedule, no mention", files: files, setup: [][]string{addBank},
			args: imp(),
			want: "Imported 1 new transaction(s) into Assets:Bank; 0 already present (skipped); 0 pending row(s) ignored.\n" +
				"Created new account Expenses:Uncategorized\n" +
				"1 row(s) in this file matched no rule and are uncategorized; review them with: thaler cli list -a Expenses:Uncategorized\n",
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

var (
	addSaving   = []string{"cli", "schedule", "add", "Saving", "--unit", "month", "-a", "Assets:Bank", "--amount", "200", "--to", "Assets:Savings", "--start", "2026-08-01"}
	addDollarly = []string{"cli", "schedule", "add", "Dollars", "--unit", "month", "-a", "Assets:Bank", "--amount=-200", "--to", "Assets:Dollars", "--to-amount", "215", "--start", "2026-08-01"}
)

func Test_schedule_transfers(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "a transfer between two accounts",
			setup: [][]string{addBank, addSavings},
			args:  addSaving,
			want:  "Schedule #1 Saving: every month from 2026-08-01, transfer 200.00 EUR  Assets:Bank -> Assets:Savings\n",
		},
		{
			name:  "the sign of the amount does not matter",
			setup: [][]string{addBank, addSavings},
			args:  []string{"cli", "schedule", "add", "Saving", "--unit", "month", "-a", "Assets:Bank", "--amount=-200", "--to", "Assets:Savings", "--start", "2026-08-01"},
			want:  "Schedule #1 Saving: every month from 2026-08-01, transfer 200.00 EUR  Assets:Bank -> Assets:Savings\n",
		},
		{
			name:  "between currencies, with what arrives",
			setup: [][]string{addBank, addDollars},
			args:  addDollarly,
			want:  "Schedule #1 Dollars: every month from 2026-08-01, transfer 200.00 EUR  Assets:Bank -> Assets:Dollars (arrives 215.00 USD)\n",
		},
		{
			name:    "between currencies without what arrives",
			setup:   [][]string{addBank, addDollars},
			args:    []string{"cli", "schedule", "add", "Dollars", "--unit", "month", "-a", "Assets:Bank", "--amount", "200", "--to", "Assets:Dollars"},
			wantErr: "Assets:Dollars holds USD, not EUR: say how much arrives with --to-amount",
		},
		{
			name:    "what arrives within one currency",
			setup:   [][]string{addBank, addSavings},
			args:    append(append([]string{}, addSaving...), "--to-amount", "200"),
			wantErr: "both accounts hold the same currency: there is no separate --to-amount",
		},
		{
			name:    "what arrives for something that is not a transfer",
			setup:   [][]string{addBank},
			args:    append(append([]string{}, addRent...), "--to-amount", "5"),
			wantErr: "--to-amount is for a transfer between your own accounts",
		},
		{
			name:    "an account that does not exist",
			setup:   [][]string{addBank},
			args:    []string{"cli", "schedule", "add", "Saving", "--unit", "month", "-a", "Assets:Bank", "--amount", "200", "--to", "Assets:Nope"},
			wantErr: "account does not exist: Assets:Nope",
		},
		{
			name:    "the same account twice",
			setup:   [][]string{addBank},
			args:    []string{"cli", "schedule", "add", "Saving", "--unit", "month", "-a", "Assets:Bank", "--amount", "200", "--to", "Assets:Bank"},
			wantErr: "source and destination are the same account",
		},
		{
			name:  "the list says where it goes",
			setup: [][]string{addBank, addSavings, addSaving},
			args:  []string{"cli", "schedule"},
			want: "Schedule  ID      Repeats       Start       Amount                        Note\n" +
				"Saving     1  every month  2026-08-01  -200.00 EUR  transfer to Assets:Savings\n",
		},
		{
			name:  "what is due",
			setup: [][]string{addBank, addSavings, addSaving},
			args:  []string{"cli", "schedule", "due", "--until", "2026-09-30"},
			want: "Date        ID       Amount  Schedule\n" +
				"2026-08-01   1  -200.00 EUR    Saving\n" +
				"2026-09-01   1  -200.00 EUR    Saving\n" +
				"\nAnswer each with: thaler cli schedule confirm ID DATE [--amount=N], or thaler cli schedule skip ID DATE\n",
		},
		{
			name:  "confirming books a transfer",
			setup: [][]string{addBank, addSavings, addSaving},
			args:  []string{"cli", "schedule", "confirm", "1", "2026-08-01"},
			want:  "Transferred #1 and #2  2026-08-01  200.00 EUR  Assets:Bank -> Assets:Savings\nCreated new account Equity:Transfers\n",
		},
		{
			name:  "confirming for another amount",
			setup: [][]string{addBank, addSavings, addSaving},
			args:  []string{"cli", "schedule", "confirm", "1", "2026-08-01", "--amount", "250"},
			want:  "Transferred #1 and #2  2026-08-01  250.00 EUR  Assets:Bank -> Assets:Savings\nCreated new account Equity:Transfers\n",
		},
		{
			name:  "confirming between currencies",
			setup: [][]string{addBank, addDollars, addDollarly},
			args:  []string{"cli", "schedule", "confirm", "1", "2026-08-01", "--to-amount", "220"},
			want:  "Transferred #1 and #2  2026-08-01  200.00 EUR  Assets:Bank -> Assets:Dollars (arrives 220.00 USD)\nCreated new account Equity:Conversions\n",
		},
		{
			name:    "another amount between currencies needs what arrives",
			setup:   [][]string{addBank, addDollars, addDollarly},
			args:    []string{"cli", "schedule", "confirm", "1", "2026-08-01", "--amount", "250"},
			wantErr: "Assets:Dollars holds USD, not EUR: say how much arrives with --to-amount",
		},
		{
			name:  "the ledger holds the two halves, linked",
			setup: [][]string{addBank, addSavings, addSaving, {"cli", "schedule", "confirm", "1", "2026-08-01"}},
			args:  []string{"cli", "list"},
			want: "2026-08-01  #1     Transfer to Savings\n    Assets:Bank       -200.00 EUR\n    Equity:Transfers  +200.00 EUR\n" +
				"2026-08-01  #2     Transfer from Bank\n    Assets:Savings    +200.00 EUR\n    Equity:Transfers  -200.00 EUR\n",
		},
		{
			name:  "editing the amount keeps it a transfer",
			setup: [][]string{addBank, addSavings, addSaving, {"cli", "schedule", "edit", "1", "--amount", "250"}},
			args:  []string{"cli", "schedule", "confirm", "1", "2026-08-01"},
			want:  "Transferred #1 and #2  2026-08-01  250.00 EUR  Assets:Bank -> Assets:Savings\nCreated new account Equity:Transfers\n",
		},
		{
			name:    "editing what arrives within one currency",
			setup:   [][]string{addBank, addSavings, addSaving},
			args:    []string{"cli", "schedule", "edit", "1", "--to-amount", "250"},
			wantErr: "both accounts hold the same currency: there is no separate --to-amount",
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

var addPower = []string{"cli", "schedule", "add", "Electricity", "--unit", "month", "-a", "Assets:Bank", "--amount=-80", "-t", "Expenses:Power", "--start", "2026-08-15", "--tolerance", "25"}

func Test_schedule_estimates(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "an estimate with a tolerance",
			setup: [][]string{addBank},
			args:  addPower,
			want:  "Schedule #1 Electricity: every month from 2026-08-15, -80.00 EUR (an estimate, ±25%)  Assets:Bank -> Expenses:Power\n",
		},
		{
			name:    "more than all of it",
			setup:   [][]string{addBank},
			args:    append(append([]string{}, addRent...), "--tolerance", "150"),
			wantErr: "a tolerance is a percentage from 0 to 100, not 150",
		},
		{
			name:    "less than none",
			setup:   [][]string{addBank},
			args:    append(append([]string{}, addRent...), "--tolerance=-5"),
			wantErr: "a tolerance is a percentage from 0 to 100, not -5",
		},
		{
			name:    "a transfer is exact",
			setup:   [][]string{addBank, addSavings},
			args:    append(append([]string{}, addSaving...), "--tolerance", "10"),
			wantErr: "a scheduled transfer moves an exact amount: it has no tolerance",
		},
		{
			name:  "the list marks the estimate",
			setup: [][]string{addBank, addPower, addRent},
			args:  []string{"cli", "schedule"},
			want: "Schedule     ID      Repeats       Start       Amount            Note\n" +
				"Electricity   1  every month  2026-08-15  ~-80.00 EUR  estimate, ±25%\n" +
				"Rent          2  every month  2026-08-28  -900.00 EUR\n",
		},
		{
			name:  "so does what is due",
			setup: [][]string{addBank, addPower},
			args:  []string{"cli", "schedule", "due", "--until", "2026-08-31"},
			want: "Date        ID       Amount     Schedule\n" +
				"2026-08-15   1  ~-80.00 EUR  Electricity\n" +
				"\nAnswer each with: thaler cli schedule confirm ID DATE [--amount=N], or thaler cli schedule skip ID DATE\n",
		},
		{
			name:  "confirming books what the bill really was",
			setup: [][]string{addBank, addPower},
			args:  []string{"cli", "schedule", "confirm", "1", "2026-08-15", "--amount=-93.40"},
			want:  "Booked #1  2026-08-15  -93.40 EUR  Assets:Bank -> Expenses:Power\n",
		},
		{
			name:  "editing the tolerance",
			setup: [][]string{addBank, addRent},
			args:  []string{"cli", "schedule", "edit", "1", "--tolerance", "10"},
			want:  "Changed schedule #1 Rent\n",
		},
		{
			name:  "the tolerance stays when something else changes",
			setup: [][]string{addBank, addPower, {"cli", "schedule", "edit", "1", "--amount=-85"}},
			args:  []string{"cli", "schedule"},
			want: "Schedule     ID      Repeats       Start       Amount            Note\n" +
				"Electricity   1  every month  2026-08-15  ~-85.00 EUR  estimate, ±25%\n",
		},
		{
			name:  "0 makes it exact again",
			setup: [][]string{addBank, addPower, {"cli", "schedule", "edit", "1", "--tolerance", "0"}},
			args:  []string{"cli", "schedule"},
			want:  "Schedule     ID      Repeats       Start      Amount\nElectricity   1  every month  2026-08-15  -80.00 EUR\n",
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

func Test_schedule_importSettlesAnEstimate(t *testing.T) {
	// The rent is 900.00 and the bank took 950.00 on the 29th.
	files := map[string]string{"bank.csv": csvHeader + `"29.08.26";"DAUERAUFTRAG";"Miete";"Landlord";"-950,00";"EUR";"Umsatz gebucht"` + "\n"}
	imp := []string{"cli", "import", "{dir}/bank.csv", "-a", "Assets:Bank"}
	tcs := []cliCase{
		{
			name: "within the tolerance", files: files,
			setup: [][]string{addBank, addRent, {"cli", "schedule", "edit", "1", "--tolerance", "10"}},
			args:  imp,
			want: "Imported 1 new transaction(s) into Assets:Bank; 0 already present (skipped); 0 pending row(s) ignored.\n" +
				"Settled 2026-08-28 of the schedule Rent (#1).\n",
		},
		{
			name: "past it", files: files,
			setup: [][]string{addBank, addRent, {"cli", "schedule", "edit", "1", "--tolerance", "5"}},
			args:  imp,
			want: "Imported 1 new transaction(s) into Assets:Bank; 0 already present (skipped); 0 pending row(s) ignored.\n" +
				"Created new account Expenses:Uncategorized\n" +
				"1 row(s) in this file matched no rule and are uncategorized; review them with: thaler cli list -a Expenses:Uncategorized\n",
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
