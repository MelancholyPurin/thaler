package core

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_Import_settlingScheduledOccurrences(t *testing.T) {
	accts := accounts("Assets:Bank EUR", "Assets:Savings EUR")
	// The rent: 900.00 from the bank on the 28th of every month, from November.
	rentSchedule := schedule(rent())
	named := func(name string, edit func(*NewSchedule)) given {
		ns := rent()
		ns.Name = name
		if edit != nil {
			edit(&ns)
		}
		return schedule(ns)
	}
	landlord := func(date string) importer.Row { return row(2, date, "LANDLORD GMBH", "Miete", eur(-90000)) }
	booked := func(date string) TxView {
		return tv(1, date, "LANDLORD GMBH", "Miete", pe("Assets:Bank", -90000), pe("Expenses:Rent", 90000))
	}
	unbooked := func(id int64, date string) TxView {
		return tv(id, date, "LANDLORD GMBH", "Miete", pe("Assets:Bank", -90000), pe(ledger.UncategorizedExpense, 90000))
	}
	settled := func(id int64, date string) []SettledOccurrence {
		return []SettledOccurrence{{ScheduleID: 1, Schedule: "Rent", Date: date, TransactionID: id}}
	}
	const (
		allThree = "2026-11-28 Rent"
		december = "2026-12-28 Rent"
	)
	tcs := []struct {
		name        string
		withoutRent bool // the ledger has the schedules of setup only
		setup       []given
		req         ImportRequest
		want        ImportResult
		wantTxs     []TxView
		wantDue     []string // up to 2026-12-31
	}{
		{
			name:    "a row on the day answers the occurrence and takes the schedule's category",
			req:     ImportRequest{Rows: []importer.Row{landlord("2026-11-28")}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Settled: settled(1, "2026-11-28")},
			wantTxs: []TxView{booked("2026-11-28")},
			wantDue: []string{december},
		},
		{
			name: "previewed: nothing is answered yet",
			req:  ImportRequest{Rows: []importer.Row{landlord("2026-11-28")}, DryRun: true},
			want: ImportResult{
				Inserted: 1, Settled: settled(0, "2026-11-28"),
				Rows: []ImportedRow{{Row: landlord("2026-11-28"), Category: "Expenses:Rent", Settles: &SettledOccurrence{ScheduleID: 1, Schedule: "Rent", Date: "2026-11-28"}}},
			},
			wantDue: []string{allThree, december},
		},
		{
			name:    "three days late is still the rent",
			req:     ImportRequest{Rows: []importer.Row{landlord("2026-12-01")}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Settled: settled(1, "2026-11-28")},
			wantTxs: []TxView{booked("2026-12-01")},
			wantDue: []string{december},
		},
		{
			name:    "three days early, before it is due",
			setup:   []given{today("2026-11-25")},
			req:     ImportRequest{Rows: []importer.Row{landlord("2026-11-25")}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Settled: settled(1, "2026-11-28")},
			wantTxs: []TxView{booked("2026-11-25")},
			wantDue: []string{december},
		},
		{
			name:    "four days late is another payment",
			req:     ImportRequest{Rows: []importer.Row{landlord("2026-12-02")}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{unbooked(1, "2026-12-02")},
			wantDue: []string{allThree, december},
		},
		{
			name:    "another amount is no match",
			req:     ImportRequest{Rows: []importer.Row{row(2, "2026-11-28", "LANDLORD GMBH", "Miete", eur(-90001))}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{tv(1, "2026-11-28", "LANDLORD GMBH", "Miete", pe("Assets:Bank", -90001), pe(ledger.UncategorizedExpense, 90001))},
			wantDue: []string{allThree, december},
		},
		{
			name:    "money coming in is no match",
			req:     ImportRequest{Rows: []importer.Row{row(2, "2026-11-28", "LANDLORD GMBH", "Miete", eur(90000))}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedIncome}},
			wantTxs: []TxView{tv(1, "2026-11-28", "LANDLORD GMBH", "Miete", pe("Assets:Bank", 90000), pe(ledger.UncategorizedIncome, -90000))},
			wantDue: []string{allThree, december},
		},
		{
			name:    "another account is no match",
			req:     ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{landlord("2026-11-28")}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{tv(1, "2026-11-28", "LANDLORD GMBH", "Miete", pe("Assets:Savings", -90000), pe(ledger.UncategorizedExpense, 90000))},
			wantDue: []string{allThree, december},
		},
		{
			name:    "a skipped occurrence is answered: the row is just a payment",
			setup:   []given{today("2026-12-01"), skipped(1, "2026-11-28")},
			req:     ImportRequest{Rows: []importer.Row{landlord("2026-11-28")}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{unbooked(1, "2026-11-28")},
			wantDue: []string{december},
		},
		{
			name:    "one already confirmed: the row takes over what was booked",
			setup:   []given{today("2026-12-01"), confirmed(1, "2026-11-28")},
			req:     ImportRequest{Rows: []importer.Row{landlord("2026-11-28")}},
			want:    ImportResult{Adopted: 1, AdoptedIDs: []int64{1}},
			wantTxs: []TxView{tv(1, "2026-11-28", "Landlord", "Rent", pe("Assets:Bank", -90000), pe("Expenses:Rent", 90000))},
			wantDue: []string{december},
		},
		{
			name:    "a rule's category stays, and the occurrence is answered all the same",
			req:     ImportRequest{Rows: []importer.Row{landlord("2026-11-28")}, Rules: []importer.Rule{{Contains: "landlord", Category: "Expenses:Housing"}}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Settled: settled(1, "2026-11-28"), Created: []string{"Expenses:Housing"}},
			wantTxs: []TxView{tv(1, "2026-11-28", "LANDLORD GMBH", "Miete", pe("Assets:Bank", -90000), pe("Expenses:Housing", 90000))},
			wantDue: []string{december},
		},
		{
			name: "two rows, one occurrence: the second is just a payment",
			req: ImportRequest{Rows: []importer.Row{
				row(2, "2026-11-28", "LANDLORD GMBH", "first", eur(-90000)),
				row(3, "2026-11-28", "LANDLORD GMBH", "second", eur(-90000)),
			}},
			want: ImportResult{Inserted: 2, IDs: []int64{1, 2}, Settled: settled(1, "2026-11-28"), Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{
				tv(1, "2026-11-28", "LANDLORD GMBH", "first", pe("Assets:Bank", -90000), pe("Expenses:Rent", 90000)),
				tv(2, "2026-11-28", "LANDLORD GMBH", "second", pe("Assets:Bank", -90000), pe(ledger.UncategorizedExpense, 90000)),
			},
			wantDue: []string{december},
		},
		{
			name: "a schedule with no category of its own leaves the row uncategorized", withoutRent: true,
			setup: []given{named("Mystery", func(ns *NewSchedule) {
				ns.Start = day("2026-11-10")
				ns.Postings = []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-777)}, {Account: ledger.UncategorizedExpense, Amount: eur(777)}}
			})},
			req:     ImportRequest{Rows: []importer.Row{row(2, "2026-11-10", "someone", "", eur(-777))}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Settled: []SettledOccurrence{{ScheduleID: 1, Schedule: "Mystery", Date: "2026-11-10", TransactionID: 1}}},
			wantTxs: []TxView{tv(1, "2026-11-10", "someone", "", pe("Assets:Bank", -777), pe(ledger.UncategorizedExpense, 777))},
			wantDue: []string{"2026-12-10 Mystery"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			steps := []given{accts}
			if !tc.withoutRent {
				steps = append(steps, rentSchedule)
			}
			st := newLedger(t, append(steps, tc.setup...)...)
			req := tc.req
			if req.Account == "" {
				req.Account = "Assets:Bank"
			}

			// when
			got, err := st.Import(t.Context(), req)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
			txs, err := st.ListTransactions(t.Context(), TxFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.wantTxs, txs, equalEmpty, ignoreHashes); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2026-12-31")); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_Import_settlingChoosesTheNearest(t *testing.T) {
	weekly := func(name string, start string) given {
		ns := rent()
		ns.Name, ns.Unit, ns.Start = name, EveryWeek, day(start)
		ns.Postings = []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-500)}, {Account: "Expenses:Food", Amount: eur(500)}}
		return schedule(ns)
	}
	tcs := []struct {
		name  string
		setup []given
		date  string
		want  []SettledOccurrence
	}{
		{name: "three days after one, four before the next", setup: []given{weekly("Shop", "2026-11-02")}, date: "2026-11-12", want: []SettledOccurrence{{1, "Shop", "2026-11-09", 1}}},
		{name: "four days after one, three before the next", setup: []given{weekly("Shop", "2026-11-02")}, date: "2026-11-13", want: []SettledOccurrence{{1, "Shop", "2026-11-16", 1}}},
		{name: "two schedules the same day: the one listed first", setup: []given{weekly("Shop", "2026-11-02"), weekly("Market", "2026-11-02")}, date: "2026-11-09", want: []SettledOccurrence{{2, "Market", "2026-11-09", 1}}},
		{name: "two schedules: the nearer", setup: []given{weekly("Shop", "2026-11-02"), weekly("Market", "2026-11-03")}, date: "2026-11-10", want: []SettledOccurrence{{2, "Market", "2026-11-10", 1}}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR")}, tc.setup...)...)

			// when
			got, err := st.Import(t.Context(), ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{row(2, tc.date, "Bakery", "", eur(-500))}})

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got.Settled); diff != "" {
				t.Errorf("settled mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Import_settlingAndWhatHappensToIt(t *testing.T) {
	row28 := row(2, "2026-11-28", "LANDLORD GMBH", "Miete", eur(-90000))
	tcs := []struct {
		name    string
		act     func(t *testing.T, st *Local)
		wantDue []string // up to 2026-11-30
	}{
		{
			name: "importing the same file again settles nothing more",
			act: func(t *testing.T, st *Local) {
				_, err := st.Import(t.Context(), ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{row28}})
				check(t, err)
			},
			wantDue: nil,
		},
		{
			name:    "taking the import back puts the occurrence back",
			act:     func(t *testing.T, st *Local) { check(t, st.DeleteTransaction(t.Context(), 1)) },
			wantDue: []string{"2026-11-28 Rent"},
		},
		{
			name: "deleting the schedule leaves the transaction",
			act:  func(t *testing.T, st *Local) { check(t, st.DeleteSchedule(t.Context(), 1)) },
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"), schedule(rent()), importing("Assets:Bank", nil, row28))

			// when
			tc.act(t, st)

			// then
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2026-11-30"), equalEmpty); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_Import_settlingSkipsWhatIsNotProposed(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		want  []SettledOccurrence
	}{
		{name: "running", want: []SettledOccurrence{{1, "Rent", "2026-11-28", 1}}},
		{name: "paused", setup: []given{today("2026-11-20"), func(t *testing.T, st *Local) { check(t, st.PauseSchedule(t.Context(), 1)) }}},
		{
			name: "paused before and resumed after: the row is just a payment",
			setup: []given{
				today("2026-11-20"), func(t *testing.T, st *Local) { check(t, st.PauseSchedule(t.Context(), 1)) },
				today("2026-12-20"), func(t *testing.T, st *Local) { check(t, st.ResumeSchedule(t.Context(), 1)) },
			},
		},
		{
			name: "ended before the date it would settle",
			setup: []given{func(t *testing.T, st *Local) {
				ns := rent()
				ns.Start, ns.End = day("2026-10-28"), day("2026-11-20")
				check(t, st.ReplaceSchedule(t.Context(), 1, ns))
			}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR"), schedule(rent())}, tc.setup...)...)

			// when
			got, err := st.Import(t.Context(), ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{row(2, "2026-11-28", "LANDLORD GMBH", "Miete", eur(-90000))}})

			// then
			check(t, err)
			if diff := cmp.Diff(tc.want, got.Settled, equalEmpty); diff != "" {
				t.Errorf("settled mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Import_settlingScheduledTransfers(t *testing.T) {
	accts := accounts("Assets:Bank EUR", "Assets:Savings EUR")
	out := row(2, "2026-11-01", "Dauerauftrag", "Sparen", eur(-20000))
	in := row(2, "2026-11-02", "Sparen", "", eur(20000))
	settled := func(id int64) []SettledOccurrence {
		return []SettledOccurrence{{ScheduleID: 1, Schedule: "Saving", Date: "2026-11-01", TransactionID: id}}
	}
	tcs := []struct {
		name            string
		scheduleInSetup bool // the saving is defined by setup, after what it lists
		setup           []given
		req             ImportRequest
		want            ImportResult
		wantTxs         []TxView
		wantDue         []string // up to 2026-12-31
	}{
		{
			name:    "the row on the account it leaves answers the occurrence and is booked to Equity:Transfers",
			req:     ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{out}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Settled: settled(1), Created: []string{ledger.TransferAccount}},
			wantTxs: []TxView{tv(1, "2026-11-01", "Dauerauftrag", "Sparen", pe("Assets:Bank", -20000), pe(ledger.TransferAccount, 20000))},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name:    "so does the row on the account it arrives in",
			req:     ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{in}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Settled: settled(1), Created: []string{ledger.TransferAccount}},
			wantTxs: []TxView{tv(1, "2026-11-02", "Sparen", "", pe("Assets:Savings", 20000), pe(ledger.TransferAccount, -20000))},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name:  "the other statement pairs with it and settles nothing twice",
			setup: []given{importing("Assets:Bank", nil, out)},
			req:   ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{in}},
			want: ImportResult{
				Inserted: 1, IDs: []int64{2}, Transfers: 1,
				Linked: []TransferLink{{ID: 2, OtherID: 1, OtherWas: ledger.TransferAccount}},
			},
			wantTxs: []TxView{
				linked(tv(1, "2026-11-01", "Dauerauftrag", "Sparen", pe("Assets:Bank", -20000), pe(ledger.TransferAccount, 20000)), 2),
				linked(tv(2, "2026-11-02", "Sparen", "", pe("Assets:Savings", 20000), pe(ledger.TransferAccount, -20000)), 1),
			},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name: "a half stored before the schedule existed is paired by the row that settles", scheduleInSetup: true,
			setup: []given{importing("Assets:Savings", nil, in), schedule(saving())},
			req:   ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{out}},
			want: ImportResult{
				Inserted: 1, IDs: []int64{2}, Transfers: 1, Settled: settled(2), Created: []string{ledger.TransferAccount},
				Linked: []TransferLink{{ID: 2, OtherID: 1, OtherWas: ledger.UncategorizedIncome}},
			},
			wantTxs: []TxView{ // by date
				linked(tv(2, "2026-11-01", "Dauerauftrag", "Sparen", pe("Assets:Bank", -20000), pe(ledger.TransferAccount, 20000)), 1),
				linked(tv(1, "2026-11-02", "Sparen", "", pe("Assets:Savings", 20000), pe(ledger.TransferAccount, -20000)), 2),
			},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name:  "the row that arrives first, then the one that leaves: still paired",
			setup: []given{importing("Assets:Savings", nil, in)},
			req:   ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{out}},
			want: ImportResult{
				Inserted: 1, IDs: []int64{2}, Transfers: 1,
				Linked: []TransferLink{{ID: 2, OtherID: 1, OtherWas: ledger.TransferAccount}},
			},
			wantTxs: []TxView{
				linked(tv(2, "2026-11-01", "Dauerauftrag", "Sparen", pe("Assets:Bank", -20000), pe(ledger.TransferAccount, 20000)), 1),
				linked(tv(1, "2026-11-02", "Sparen", "", pe("Assets:Savings", 20000), pe(ledger.TransferAccount, -20000)), 2),
			},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name:    "a rule that calls the row a transfer still settles it",
			req:     ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{out}, Rules: []importer.Rule{{Contains: "sparen", Category: ledger.TransferAccount}}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Settled: settled(1), Created: []string{ledger.TransferAccount}},
			wantTxs: []TxView{tv(1, "2026-11-01", "Dauerauftrag", "Sparen", pe("Assets:Bank", -20000), pe(ledger.TransferAccount, 20000))},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name: "across currencies the row is booked to Equity:Conversions", scheduleInSetup: true,
			setup: []given{accounts("Assets:Dollars USD"), func(t *testing.T, st *Local) {
				ns := saving()
				ns.Postings[1] = ledger.Posting{Account: "Assets:Dollars", Amount: usd(21500)}
				_, err := st.CreateSchedule(t.Context(), ns)
				check(t, err)
			}},
			req:     ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{out}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Settled: settled(1), Created: []string{ledger.ConversionAccount}},
			wantTxs: []TxView{tv(1, "2026-11-01", "Dauerauftrag", "Sparen", pe("Assets:Bank", -20000), pe(ledger.ConversionAccount, 20000))},
			wantDue: []string{"2026-12-01 Saving"},
		},
		{
			name:    "another amount is no match",
			req:     ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{row(2, "2026-11-01", "Dauerauftrag", "Sparen", eur(-20001))}},
			want:    ImportResult{Inserted: 1, IDs: []int64{1}, Uncategorized: 1, Created: []string{ledger.UncategorizedExpense}},
			wantTxs: []TxView{tv(1, "2026-11-01", "Dauerauftrag", "Sparen", pe("Assets:Bank", -20001), pe(ledger.UncategorizedExpense, 20001))},
			wantDue: []string{"2026-11-01 Saving", "2026-12-01 Saving"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			steps := []given{accts}
			if !tc.scheduleInSetup {
				steps = append(steps, schedule(saving()))
			}
			st := newLedger(t, append(steps, tc.setup...)...)

			// when
			got, err := st.Import(t.Context(), tc.req)

			// then
			check(t, err)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
			txs, err := st.ListTransactions(t.Context(), TxFilter{})
			check(t, err)
			if diff := cmp.Diff(tc.wantTxs, txs, equalEmpty, ignoreHashes); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantDue, dueDates(t, st, "2026-12-31"), equalEmpty); diff != "" {
				t.Errorf("due mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_Import_settlingIgnoresRuleTransfersOfOrdinarySchedules(t *testing.T) {
	tcs := []struct {
		name string
		want []SettledOccurrence
	}{{name: "a row a rule calls a transfer is not the rent"}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"), schedule(rent()))

			// when
			got, err := st.Import(t.Context(), ImportRequest{
				Account: "Assets:Bank", Rows: []importer.Row{row(2, "2026-11-28", "LANDLORD GMBH", "Miete", eur(-90000))},
				Rules: []importer.Rule{{Contains: "landlord", Category: ledger.TransferAccount}},
			})

			// then
			check(t, err)
			if diff := cmp.Diff(tc.want, got.Settled, equalEmpty); diff != "" {
				t.Errorf("settled mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
