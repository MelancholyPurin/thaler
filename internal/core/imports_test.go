package core

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// request runs an import as given, for its side effects.
func request(req ImportRequest) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.Import(t.Context(), req); err != nil {
			t.Fatalf("import into %s: %v", req.Account, err)
		}
	}
}

// deleting removes transactions.
func deleting(ids ...int64) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if err := st.DeleteTransactions(t.Context(), ids); err != nil {
			t.Fatalf("DeleteTransactions(%v): %v", ids, err)
		}
	}
}

func Test_ListImports(t *testing.T) {
	accts := accounts("Assets:Bank EUR", "Assets:Savings EUR")
	rewe := row(2, "2026-10-03", "REWE", "", eur(-2347))
	edeka := row(3, "2026-10-04", "Edeka", "", eur(-1000))
	landlord := row(2, "2026-11-28", "LANDLORD GMBH", "Miete", eur(-90000))
	tcs := []struct {
		name  string
		setup []given
		want  []ImportRecord
	}{
		{name: "nothing was imported", setup: []given{accts}},
		{
			name: "an import says where it came from, when and how much",
			setup: []given{accts, today("2026-11-20"),
				request(ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{rewe, edeka}, Source: "export.csv", Profile: "sparkasse"})},
			want: []ImportRecord{{ID: 1, Account: "Assets:Bank", Source: "export.csv", Profile: "sparkasse", At: day("2026-11-20"), Added: 2}},
		},
		{
			name:  "the source and the profile are optional",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Savings", nil, rewe)},
			want:  []ImportRecord{{ID: 1, Account: "Assets:Savings", At: day("2026-11-20"), Added: 1}},
		},
		{
			name: "the newest is first",
			setup: []given{accts, today("2026-11-20"),
				request(ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{rewe}, Source: "one.csv"}),
				today("2026-11-21"),
				request(ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{edeka}, Source: "two.csv"})},
			want: []ImportRecord{
				{ID: 2, Account: "Assets:Savings", Source: "two.csv", At: day("2026-11-21"), Added: 1},
				{ID: 1, Account: "Assets:Bank", Source: "one.csv", At: day("2026-11-20"), Added: 1},
			},
		},
		{
			name: "a dry run and an import that finds everything stored leave no record",
			setup: []given{accts, today("2026-11-20"),
				request(ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{rewe}, DryRun: true}),
				importing("Assets:Bank", nil, rewe),
				importing("Assets:Bank", nil, rewe)},
			want: []ImportRecord{{ID: 1, Account: "Assets:Bank", At: day("2026-11-20"), Added: 1}},
		},
		{
			name: "an import that is refused leaves no record",
			setup: []given{accts, today("2026-11-20"), func(t *testing.T, st *Local) {
				usdRow := row(2, "2026-10-03", "REWE", "", usd(-100))
				if _, err := st.Import(t.Context(), ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{usdRow}}); err == nil {
					t.Fatal("a row in another currency should be refused")
				}
			}},
		},
		{
			name:  "what was deleted since is no longer counted",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, rewe, edeka), deleting(1)},
			want:  []ImportRecord{{ID: 1, Account: "Assets:Bank", At: day("2026-11-20"), Added: 1}},
		},
		{
			name: "an entry of yours that a row took over is counted apart",
			setup: []given{accts, today("2026-11-20"),
				handEntered("Assets:Bank", "2026-10-03", "REWE", "Expenses:Groceries", -2347),
				importing("Assets:Bank", nil, rewe, edeka)},
			want: []ImportRecord{{ID: 1, Account: "Assets:Bank", At: day("2026-11-20"), Added: 1, Adopted: 1}},
		},
		{
			name: "a pairing is counted",
			setup: []given{accts, today("2026-11-20"),
				importing("Assets:Bank", nil, row(2, "2026-10-04", "out", "", eur(-50000))),
				importing("Assets:Savings", nil, row(2, "2026-10-04", "in", "", eur(50000)))},
			want: []ImportRecord{
				{ID: 2, Account: "Assets:Savings", At: day("2026-11-20"), Added: 1, Paired: 1},
				{ID: 1, Account: "Assets:Bank", At: day("2026-11-20"), Added: 1},
			},
		},
		{
			name:  "a date of a schedule that a row answered is counted",
			setup: []given{accts, today("2026-11-20"), schedule(rent()), importing("Assets:Bank", nil, landlord)},
			want:  []ImportRecord{{ID: 1, Account: "Assets:Bank", At: day("2026-11-20"), Added: 1, Settled: 1}},
		},
		{
			name: "it follows an account that was renamed",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, rewe), func(t *testing.T, st *Local) {
				if _, err := st.RenameAccount(t.Context(), "Assets:Bank", "Assets:Sparkasse"); err != nil {
					t.Fatal(err)
				}
			}},
			want: []ImportRecord{{ID: 1, Account: "Assets:Sparkasse", At: day("2026-11-20"), Added: 1}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.ListImports(t.Context())

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("imports mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Import_recordsItsID(t *testing.T) {
	rewe := row(2, "2026-10-03", "REWE", "", eur(-2347))
	tcs := []struct {
		name   string
		req    ImportRequest
		setup  []given
		wantID int64
	}{
		{name: "a real import names its record", req: ImportRequest{Rows: []importer.Row{rewe}}, wantID: 1},
		{name: "a second import has the next", setup: []given{importing("Assets:Bank", nil, row(3, "2026-10-04", "Edeka", "", eur(-1000)))}, req: ImportRequest{Rows: []importer.Row{rewe}}, wantID: 2},
		{name: "a dry run has none", req: ImportRequest{Rows: []importer.Row{rewe}, DryRun: true}},
		{name: "nothing new has none", setup: []given{importing("Assets:Bank", nil, rewe)}, req: ImportRequest{Rows: []importer.Row{rewe}}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append([]given{accounts("Assets:Bank EUR")}, tc.setup...)...)
			tc.req.Account = "Assets:Bank"

			// when
			got, err := st.Import(t.Context(), tc.req)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.wantID, got.ImportID); diff != "" {
				t.Errorf("record mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_UndoImport(t *testing.T) {
	accts := accounts("Assets:Bank EUR", "Assets:Savings EUR")
	rewe := row(2, "2026-10-03", "REWE", "", eur(-2347))
	edeka := row(3, "2026-10-04", "Edeka", "", eur(-1000))
	out := row(2, "2026-10-04", "out", "", eur(-50000))
	in := row(2, "2026-10-04", "in", "", eur(50000))
	landlord := row(2, "2026-11-28", "LANDLORD GMBH", "Miete", eur(-90000))
	record := func(id int64, account string, added int) ImportRecord {
		return ImportRecord{ID: id, Account: account, At: day("2026-11-20"), Added: added}
	}
	tcs := []struct {
		name       string
		setup      []given
		id         int64
		want       ImportUndone
		wantTxs    []TxView
		wantLeft   []int64  // the imports still listed
		wantDue    []string // the dates of schedules that are due, up to 2026-12-31
		wantErr    string
		wantAgain  ImportRequest // imported again afterwards, its rows are stored once more
		wantAgainN int           // how many of them
	}{
		{
			name:  "what it added goes, and what was there before stays",
			setup: []given{accts, today("2026-11-20"), handEntered("Assets:Bank", "2026-09-01", "Bakery", "Expenses:Groceries", -300), importing("Assets:Bank", nil, rewe, edeka)},
			id:    1,
			want:  ImportUndone{Record: record(1, "Assets:Bank", 2)},
			wantTxs: []TxView{
				tv(1, "2026-09-01", "Bakery", "", pe("Assets:Bank", -300), pe("Expenses:Groceries", 300)),
			},
			wantAgain: ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{rewe, edeka}}, wantAgainN: 2,
		},
		{
			name:  "another import is not touched",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, rewe), importing("Assets:Savings", nil, edeka)},
			id:    1,
			want:  ImportUndone{Record: record(1, "Assets:Bank", 1)},
			wantTxs: []TxView{
				tv(2, "2026-10-04", "Edeka", "", pe("Assets:Savings", -1000), pe(ledger.UncategorizedExpense, 1000)),
			},
			wantLeft: []int64{2},
		},
		{
			name: "an entry of yours that a row took over is hand-entered again, and stays",
			setup: []given{accts, today("2026-11-20"),
				handEntered("Assets:Bank", "2026-10-03", "REWE", "Expenses:Groceries", -2347),
				importing("Assets:Bank", nil, rewe, edeka)},
			id:   1,
			want: ImportUndone{Record: ImportRecord{ID: 1, Account: "Assets:Bank", At: day("2026-11-20"), Added: 1, Adopted: 1}, Released: 1},
			wantTxs: []TxView{
				tv(1, "2026-10-03", "REWE", "", pe("Assets:Bank", -2347), pe("Expenses:Groceries", 2347)),
			},
			wantAgain: ImportRequest{Account: "Assets:Bank", Rows: []importer.Row{rewe, edeka}}, wantAgainN: 2,
		},
		{
			name:  "the half it paired goes back to where it was",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, out), importing("Assets:Savings", nil, in)},
			id:    2,
			want:  ImportUndone{Record: ImportRecord{ID: 2, Account: "Assets:Savings", At: day("2026-11-20"), Added: 1, Paired: 1}, Unpaired: 1},
			wantTxs: []TxView{
				tv(1, "2026-10-04", "out", "", pe("Assets:Bank", -50000), pe(ledger.UncategorizedExpense, 50000)),
			},
			wantLeft:  []int64{1},
			wantAgain: ImportRequest{Account: "Assets:Savings", Rows: []importer.Row{in}}, wantAgainN: 1,
		},
		{
			name: "a half a rule had marked as a transfer stays one",
			setup: []given{accts, today("2026-11-20"),
				importing("Assets:Bank", []importer.Rule{{Contains: "out", Category: ledger.TransferAccount}}, out),
				importing("Assets:Savings", nil, in)},
			id:   2,
			want: ImportUndone{Record: ImportRecord{ID: 2, Account: "Assets:Savings", At: day("2026-11-20"), Added: 1, Paired: 1}},
			wantTxs: []TxView{
				tv(1, "2026-10-04", "out", "", pe("Assets:Bank", -50000), pe(ledger.TransferAccount, 50000)),
			},
			wantLeft: []int64{1},
		},
		{
			name: "a half you categorized since keeps the category you gave it",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, out), importing("Assets:Savings", nil, in),
				func(t *testing.T, st *Local) {
					if _, err := st.SetCategories(t.Context(), []CategoryChange{{TransactionID: 1, Category: "Expenses:Gifts"}}); err != nil {
						t.Fatal(err)
					}
				}},
			id:   2,
			want: ImportUndone{Record: ImportRecord{ID: 2, Account: "Assets:Savings", At: day("2026-11-20"), Added: 1, Paired: 1}},
			wantTxs: []TxView{
				tv(1, "2026-10-04", "out", "", pe("Assets:Bank", -50000), pe("Expenses:Gifts", 50000)),
			},
			wantLeft: []int64{1},
		},
		{
			name:  "an earlier import taken back leaves the half a later one paired uncategorized",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, out), importing("Assets:Savings", nil, in)},
			id:    1,
			want:  ImportUndone{Record: record(1, "Assets:Bank", 1), Unpaired: 1},
			wantTxs: []TxView{
				tv(2, "2026-10-04", "in", "", pe("Assets:Savings", 50000), pe(ledger.UncategorizedIncome, -50000)),
			},
			wantLeft: []int64{2},
		},
		{
			name:  "a date of a schedule that a row answered is due again",
			setup: []given{accts, today("2026-11-20"), schedule(rent()), importing("Assets:Bank", nil, landlord)},
			id:    1,
			want:  ImportUndone{Record: ImportRecord{ID: 1, Account: "Assets:Bank", At: day("2026-11-20"), Added: 1, Settled: 1}},
			wantDue: []string{
				"2026-11-28 Rent",
				"2026-12-28 Rent",
			},
		},
		{
			name:  "what is already deleted is not looked for",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, rewe, edeka), deleting(1)},
			id:    1,
			want:  ImportUndone{Record: record(1, "Assets:Bank", 1)},
		},
		{
			name:  "an import with nothing left is taken off the list",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, rewe), deleting(1)},
			id:    1,
			want:  ImportUndone{Record: record(1, "Assets:Bank", 0)},
		},
		{
			name:  "an import that does not exist changes nothing",
			setup: []given{accts, today("2026-11-20"), importing("Assets:Bank", nil, rewe)},
			id:    7,
			wantTxs: []TxView{
				tv(1, "2026-10-03", "REWE", "", pe("Assets:Bank", -2347), pe(ledger.UncategorizedExpense, 2347)),
			},
			wantLeft: []int64{1},
			wantErr:  "import 7 does not exist",
		},
		{
			name: "an import that only took over entries is refused on a closed account too",
			setup: []given{accts, today("2026-11-20"),
				handEntered("Assets:Bank", "2026-10-03", "in", "Income:Gifts", 100),
				handEntered("Assets:Bank", "2026-10-04", "out", "Expenses:Gifts", -100),
				importing("Assets:Bank", nil, row(2, "2026-10-03", "in", "", eur(100)), row(3, "2026-10-04", "out", "", eur(-100))),
				closing("Assets:Bank")},
			id: 1,
			wantTxs: []TxView{
				tv(1, "2026-10-03", "in", "", pe("Assets:Bank", 100), pe("Income:Gifts", -100)),
				tv(2, "2026-10-04", "out", "", pe("Assets:Bank", -100), pe("Expenses:Gifts", 100)),
			},
			wantLeft: []int64{1},
			wantErr:  "Assets:Bank is closed: reopen it first",
		},
		{
			name: "an account that is closed is not changed, and nothing is",
			setup: []given{accts, today("2026-11-20"),
				importing("Assets:Bank", nil, row(2, "2026-10-03", "in", "", eur(100)), row(3, "2026-10-04", "out", "", eur(-100))),
				closing("Assets:Bank")},
			id: 1,
			wantTxs: []TxView{
				tv(1, "2026-10-03", "in", "", pe("Assets:Bank", 100), pe(ledger.UncategorizedIncome, -100)),
				tv(2, "2026-10-04", "out", "", pe("Assets:Bank", -100), pe(ledger.UncategorizedExpense, 100)),
			},
			wantLeft: []int64{1},
			wantErr:  "Assets:Bank is closed: reopen it first",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)
			if tc.wantTxs == nil && tc.wantErr == "" {
				tc.wantTxs = []TxView{}
			}

			// when
			got, err := st.UndoImport(t.Context(), tc.id)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("undone mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantTxs, stateOf(t, st).Transactions, equalEmpty, ignoreHashes); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
			left, err := st.ListImports(t.Context())
			checkErr(t, err, "", nil)
			var leftIDs []int64
			for _, r := range left {
				leftIDs = append(leftIDs, r.ID)
			}
			if diff := cmp.Diff(tc.wantLeft, leftIDs, equalEmpty); diff != "" {
				t.Errorf("imports still listed (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
			if tc.wantDue != nil {
				due, err := st.Due(t.Context(), "2026-12-31")
				checkErr(t, err, "", nil)
				var names []string
				for _, o := range due {
					names = append(names, o.Date+" "+o.Schedule.Name)
				}
				if diff := cmp.Diff(tc.wantDue, names); diff != "" {
					t.Errorf("dates due (-want +got):\n%s", diff)
				}
			}
			if tc.wantAgainN > 0 {
				again, err := st.Import(t.Context(), tc.wantAgain)
				checkErr(t, err, "", nil)
				if diff := cmp.Diff(tc.wantAgainN, again.Inserted+again.Adopted); diff != "" {
					t.Errorf("importing the rows again should store them (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// Taking an import back is all or nothing: when the last step fails, the half it
// paired is where it was, and the entry of yours it took over still stands for the
// bank row.
func Test_UndoImport_allOrNothing(t *testing.T) {
	tcs := []struct {
		name    string
		wantErr string
	}{
		{name: "a delete that fails after the rest was done undoes the rest", wantErr: "deleting: no"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR", "Assets:Savings EUR"), today("2026-11-20"),
				handEntered("Assets:Savings", "2026-10-05", "Gift", "Expenses:Gifts", -500),
				importing("Assets:Bank", nil, row(2, "2026-10-04", "out", "", eur(-50000))),
				importing("Assets:Savings", nil, row(2, "2026-10-04", "in", "", eur(50000)), row(3, "2026-10-05", "Gift", "", eur(-500))),
				rawSQL(`CREATE TRIGGER no_deletes BEFORE DELETE ON transactions BEGIN SELECT RAISE(ABORT, 'deleting: no'); END`))
			before := stateOf(t, st).Transactions
			wantRecords, err := st.ListImports(t.Context())
			check(t, err)

			// when
			_, err = st.UndoImport(t.Context(), 2)

			// then
			if got := errText(err); !strings.Contains(got, tc.wantErr) {
				t.Errorf("error %q should say %q", got, tc.wantErr)
			}
			if diff := cmp.Diff(before, stateOf(t, st).Transactions, equalEmpty); diff != "" {
				t.Errorf("nothing may change, hashes included (-want +got):\n%s", diff)
			}
			records, err := st.ListImports(t.Context())
			check(t, err)
			if diff := cmp.Diff(wantRecords, records); diff != "" {
				t.Errorf("the record stays as it was (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

// A transaction that was part of an import still is after a delete and an undo of
// the delete, so that taking the import back still takes it.
func Test_RestoreTransaction_keepsItsImport(t *testing.T) {
	tcs := []struct {
		name        string
		undoImport  bool // the import is taken back before the restore
		wantAdded   int
		wantRecords int
	}{
		{name: "the import still counts it", wantAdded: 2, wantRecords: 1},
		{name: "an import that is gone leaves it on its own", undoImport: true},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"), today("2026-11-20"),
				importing("Assets:Bank", nil, row(2, "2026-10-03", "REWE", "", eur(-2347)), row(3, "2026-10-04", "Edeka", "", eur(-1000))))
			txs, err := st.ListTransactions(t.Context(), TxFilter{ID: 1})
			check(t, err)
			old, err := txs[0].NewTransaction()
			check(t, err)
			check(t, st.DeleteTransaction(t.Context(), 1))
			if tc.undoImport {
				_, err := st.UndoImport(t.Context(), 1)
				check(t, err)
			}

			// when
			err = st.RestoreTransaction(t.Context(), 1, old)

			// then
			check(t, err)
			records, err := st.ListImports(t.Context())
			check(t, err)
			added := 0
			if len(records) > 0 {
				added = records[0].Added
			}
			if diff := cmp.Diff([]int{tc.wantRecords, tc.wantAdded}, []int{len(records), added}); diff != "" {
				t.Errorf("records and what they count (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}
