package core

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func eur(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "EUR"} }
func usd(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "USD"} }

func day(s string) time.Time {
	d, err := ledger.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func newLocal(t *testing.T) *Local {
	t.Helper()
	st, err := Open(t.Context(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// given is one step that puts a fresh ledger into the state a test case starts
// from. A case lists its steps in order.
type given func(t *testing.T, st *Local)

// run applies steps to a fresh in-memory ledger.
func newLedger(t *testing.T, steps ...given) *Local {
	t.Helper()
	st := newLocal(t)
	for _, step := range steps {
		step(t, st)
	}
	return st
}

// accounts creates accounts from specs like "Assets:Bank EUR" or "Expenses:Food".
func accounts(specs ...string) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		for _, spec := range specs {
			fields := strings.Fields(spec)
			var cur ledger.Currency
			if len(fields) > 1 {
				cur = ledger.Currency(fields[1])
			}
			if _, err := st.AddAccount(t.Context(), fields[0], cur); err != nil {
				t.Fatalf("AddAccount(%s): %v", spec, err)
			}
		}
	}
}

// today fixes the clock the ledger reads "today" from.
func today(date string) given {
	return func(t *testing.T, st *Local) { st.now = func() time.Time { return day(date) } }
}

// stored books a transaction exactly as given.
func stored(nt NewTransaction) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.AddTransaction(t.Context(), nt); err != nil {
			t.Fatalf("AddTransaction: %v", err)
		}
	}
}

// spending books an expense of amount on Assets:Bank, with the payee "shop".
func spending(date, category string, amount int64) given {
	return stored(NewTransaction{
		Date: day(date), Payee: "shop",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-amount)}, {Account: category, Amount: eur(amount)}},
	})
}

// earning books income of amount on Assets:Bank.
func earning(date, category string, amount int64) given {
	return stored(NewTransaction{
		Date: day(date), Payee: "employer",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(amount)}, {Account: category, Amount: eur(-amount)}},
	})
}

// handEntered books a transaction the way the interface does: an amount on an
// account against a category, with no import hash. A negative amount leaves the
// account.
func handEntered(account, date, payee, category string, minor int64) given {
	return stored(NewTransaction{
		Date: day(date), Payee: payee,
		Postings: []ledger.Posting{{Account: account, Amount: eur(minor)}, {Account: category, Amount: eur(-minor)}},
	})
}

// importing runs an import for its side effects.
func importing(account string, rules []importer.Rule, rows ...importer.Row) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.Import(t.Context(), ImportRequest{Account: account, Rows: rows, Rules: rules}); err != nil {
			t.Fatalf("import into %s: %v", account, err)
		}
	}
}

// rawSQL runs a statement against the database underneath, to set up (or
// simulate) something the Go layer would never produce.
func rawSQL(statement string, args ...any) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.repo.db.ExecContext(t.Context(), statement, args...); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func row(line int, date, payee, purpose string, amount ledger.Money) importer.Row {
	return importer.Row{Line: line, Date: day(date), Payee: payee, Purpose: purpose, Amount: amount}
}

// pe is a posting in euros.
func pe(account string, minor int64) PostingView {
	return PostingView{Account: account, Amount: eur(minor)}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// checkErr compares a failure by its message and, when the caller relies on it,
// by the sentinel it wraps.
func checkErr(t *testing.T, err error, wantErr string, wantIs error) {
	t.Helper()
	if diff := cmp.Diff(wantErr, errText(err)); diff != "" {
		t.Errorf("error mismatch (-want +got):\n%s", diff)
	}
	if wantIs != nil {
		if diff := cmp.Diff(true, errors.Is(err, wantIs)); diff != "" {
			t.Errorf("error should wrap %v (-want +got):\n%s", wantIs, diff)
		}
	}
}

// checkConsistent fails if Check finds anything wrong with the ledger.
func checkConsistent(t *testing.T, st *Local) {
	t.Helper()
	problems, err := st.Check(t.Context())
	checkErr(t, err, "", nil)
	if diff := cmp.Diff([]string(nil), problems, equalEmpty); diff != "" {
		t.Errorf("Check found problems (-want +got):\n%s", diff)
	}
}

// ledgerState is everything that is stored, for checking that a refused call
// left nothing behind.
type ledgerState struct {
	Accounts     []string
	Transactions []TxView
}

func stateOf(t *testing.T, st *Local) ledgerState {
	t.Helper()
	var s ledgerState
	accs, err := st.ListAccounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accs {
		s.Accounts = append(s.Accounts, a.Name)
	}
	if s.Transactions, err = st.ListTransactions(t.Context(), TxFilter{}); err != nil {
		t.Fatal(err)
	}
	return s
}

// ids are the ids of txs, in order.
func ids(txs []TxView) []int64 {
	var out []int64
	for _, tx := range txs {
		out = append(out, tx.ID)
	}
	return out
}

// equalEmpty treats nil and empty slices alike.
var equalEmpty = cmpopts.EquateEmpty()

func Test_BaseCurrency(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		set   ledger.Currency // changed to this first, if not empty
		want  ledger.Currency
	}{
		{name: "euro until told otherwise", want: "EUR"},
		{name: "after it is changed", set: "USD", want: "USD"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)
			if tc.set != "" {
				if err := st.SetBaseCurrency(t.Context(), tc.set); err != nil {
					t.Fatal(err)
				}
			}

			// when
			got, err := st.BaseCurrency(t.Context())

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_SetRate(t *testing.T) {
	type rate struct {
		date, base, quote, rate string
	}
	tcs := []struct {
		name    string
		sets    []rate
		want    []string // the rates stored afterwards, "date base/quote value"
		wantErr string
	}{
		{
			name: "a rate",
			sets: []rate{{"2026-10-01", "USD", "EUR", "0.92"}},
			want: []string{"2026-10-01 USD/EUR 23/25"},
		},
		{
			name: "the same day again overwrites instead of failing",
			sets: []rate{{"2026-10-01", "USD", "EUR", "0.92"}, {"2026-10-01", "USD", "EUR", "0.93"}},
			want: []string{"2026-10-01 USD/EUR 93/100"},
		},
		{
			name: "several days and pairs are kept",
			sets: []rate{{"2026-10-01", "USD", "EUR", "0.92"}, {"2026-01-01", "USD", "EUR", "0.90"}, {"2026-10-01", "MXN", "EUR", "0.05"}},
			want: []string{"2026-01-01 USD/EUR 9/10", "2026-10-01 MXN/EUR 1/20", "2026-10-01 USD/EUR 23/25"},
		},
		{
			name:    "a currency against itself",
			sets:    []rate{{"2026-10-01", "EUR", "EUR", "1"}},
			wantErr: "base and quote currency must differ",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t)

			// when
			var err error
			for _, r := range tc.sets {
				if err = st.SetRate(t.Context(), r.date, ledger.Currency(r.base), ledger.Currency(r.quote), r.rate); err != nil {
					break
				}
			}

			// then
			checkErr(t, err, tc.wantErr, nil)
			entries, rerr := st.Rates(t.Context())
			if rerr != nil {
				t.Fatal(rerr)
			}
			var got []string
			for _, e := range entries {
				got = append(got, e.Date+" "+string(e.Base)+"/"+string(e.Quote)+" "+e.Rate.RatString())
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("stored rates mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_AddAccount(t *testing.T) {
	tcs := []struct {
		name     string
		setup    []given
		account  string
		currency ledger.Currency
		want     Account
		wantErr  string
		wantIs   error
	}{
		{name: "an asset", account: "Assets:Bank", currency: "EUR", want: Account{ID: 1, Name: "Assets:Bank", Type: ledger.Assets, Currency: "EUR"}},
		{name: "a liability", account: "Liabilities:Card", currency: "EUR", want: Account{ID: 1, Name: "Liabilities:Card", Type: ledger.Liabilities, Currency: "EUR"}},
		{name: "a category needs no currency", account: "Expenses:Food", want: Account{ID: 1, Name: "Expenses:Food", Type: ledger.Expenses}},
		{
			name: "one that exists", setup: []given{accounts("Assets:Bank EUR")}, account: "Assets:Bank", currency: "EUR",
			wantErr: "account already exists: Assets:Bank", wantIs: ErrAccountExists,
		},
		{name: "an asset without a currency", account: "Assets:Cash", wantErr: "Assets accounts need a currency, e.g. EUR"},
		{name: "a liability without a currency", account: "Liabilities:Card", wantErr: "Liabilities accounts need a currency, e.g. EUR"},
		{name: "a name without a type", account: "bank", currency: "EUR", wantErr: `account "bank" must start with one of Assets, Liabilities, Income, Expenses, Equity`},
		{name: "a name with a space", account: "Assets:My Bank", currency: "EUR", wantErr: `account "Assets:My Bank": invalid character ' ' (use letters, digits, '-' and '_')`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.AddAccount(t.Context(), tc.account, tc.currency)

			// then
			checkErr(t, err, tc.wantErr, tc.wantIs)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("account mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_GetAccount(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		get     string
		want    Account
		wantErr string
		wantIs  error
	}{
		{name: "an asset", setup: []given{accounts("Assets:Bank EUR")}, get: "Assets:Bank", want: Account{ID: 1, Name: "Assets:Bank", Type: ledger.Assets, Currency: "EUR"}},
		{name: "the second one", setup: []given{accounts("Assets:Bank EUR", "Expenses:Food")}, get: "Expenses:Food", want: Account{ID: 2, Name: "Expenses:Food", Type: ledger.Expenses}},
		{name: "one that does not exist", get: "Assets:Nope", wantErr: "account does not exist: Assets:Nope", wantIs: ErrNoAccount},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.GetAccount(t.Context(), tc.get)

			// then
			checkErr(t, err, tc.wantErr, tc.wantIs)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("account mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ListAccounts(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		want  []Account
	}{
		{name: "none yet"},
		{
			name:  "ordered by name, with their currencies",
			setup: []given{accounts("Expenses:Food", "Assets:Bank EUR", "Assets:Cash USD")},
			want: []Account{
				{ID: 2, Name: "Assets:Bank", Type: ledger.Assets, Currency: "EUR"},
				{ID: 3, Name: "Assets:Cash", Type: ledger.Assets, Currency: "USD"},
				{ID: 1, Name: "Expenses:Food", Type: ledger.Expenses},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.ListAccounts(t.Context())

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("accounts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Account_LedgerBalance(t *testing.T) {
	tcs := []struct {
		name  string
		acct  Account
		shown ledger.Money
		want  ledger.Money
	}{
		{name: "an asset keeps its sign", acct: Account{Type: ledger.Assets}, shown: eur(1000), want: eur(1000)},
		{name: "an overdrawn asset", acct: Account{Type: ledger.Assets}, shown: eur(-1000), want: eur(-1000)},
		{name: "a debt is shown as the amount owed", acct: Account{Type: ledger.Liabilities}, shown: eur(50000), want: eur(-50000)},
		{name: "a credit on a liability", acct: Account{Type: ledger.Liabilities}, shown: eur(-50000), want: eur(50000)},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.acct and tc.shown

			// when
			got := tc.acct.LedgerBalance(tc.shown)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_TxView_Legs(t *testing.T) {
	type legs struct {
		Source, Category PostingView
		OK               bool
	}
	tcs := []struct {
		name string
		tx   TxView
		want legs
	}{
		{
			name: "an expense", tx: TxView{Postings: []PostingView{pe("Assets:Bank", -500), pe("Expenses:Food", 500)}},
			want: legs{pe("Assets:Bank", -500), pe("Expenses:Food", 500), true},
		},
		{
			name: "income, with the postings the other way round", tx: TxView{Postings: []PostingView{pe("Income:Salary", -500), pe("Assets:Bank", 500)}},
			want: legs{pe("Assets:Bank", 500), pe("Income:Salary", -500), true},
		},
		{name: "a transfer has no category", tx: TxView{Postings: []PostingView{pe("Assets:Bank", -500), pe("Assets:Savings", 500)}}},
		{name: "a split has several", tx: TxView{Postings: []PostingView{pe("Assets:Bank", -500), pe("Expenses:A", 300), pe("Expenses:B", 200)}}},
		{name: "two categories and no account", tx: TxView{Postings: []PostingView{pe("Expenses:A", -500), pe("Expenses:B", 500)}}},
		{name: "no postings"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.tx

			// when
			source, category, ok := tc.tx.Legs()

			// then
			if diff := cmp.Diff(tc.want, legs{source, category, ok}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_TxView_NewTransaction(t *testing.T) {
	tcs := []struct {
		name    string
		tx      TxView
		want    NewTransaction
		wantErr string
	}{
		{
			name: "everything is carried over",
			tx:   TxView{ID: 7, Date: "2026-10-01", Payee: "REWE", Description: "weekly", ImportHash: "h1", Postings: []PostingView{pe("Assets:Bank", -500), pe("Expenses:Food", 500)}},
			want: NewTransaction{
				Date: day("2026-10-01"), Payee: "REWE", Description: "weekly", ImportHash: "h1",
				Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-500)}, {Account: "Expenses:Food", Amount: eur(500)}},
			},
		},
		{name: "a date that does not parse", tx: TxView{Date: "someday"}, wantErr: `invalid date "someday": use YYYY-MM-DD or DD.MM.YYYY`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.tx

			// when
			got, err := tc.tx.NewTransaction()

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_AddTransaction(t *testing.T) {
	groceries := func(minor int64) []ledger.Posting {
		return []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-minor)}, {Account: "Expenses:Groceries", Amount: eur(minor)}}
	}
	withHash := func(hash string) NewTransaction {
		return NewTransaction{Date: day("2026-10-05"), Payee: "Bakery", ImportHash: hash,
			Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-320)}, {Account: ledger.UncategorizedExpense, Amount: eur(320)}}}
	}
	bank := []given{accounts("Assets:Bank EUR")}
	tcs := []struct {
		name      string
		setup     []given
		nt        NewTransaction
		want      AddResult
		wantState ledgerState
		wantErr   string
		wantIs    error
	}{
		{
			name: "a category that does not exist is created", setup: bank,
			nt:   NewTransaction{Date: day("2026-10-01"), Payee: "REWE", Postings: groceries(2347)},
			want: AddResult{ID: 1, Created: []string{"Expenses:Groceries"}},
			wantState: ledgerState{
				Accounts:     []string{"Assets:Bank", "Expenses:Groceries"},
				Transactions: []TxView{{ID: 1, Date: "2026-10-01", Payee: "REWE", Postings: []PostingView{pe("Assets:Bank", -2347), pe("Expenses:Groceries", 2347)}}},
			},
		},
		{
			name: "a category that exists is not created again", setup: []given{accounts("Assets:Bank EUR", "Expenses:Groceries")},
			nt:   NewTransaction{Date: day("2026-10-01"), Postings: groceries(100)},
			want: AddResult{ID: 1},
			wantState: ledgerState{
				Accounts:     []string{"Assets:Bank", "Expenses:Groceries"},
				Transactions: []TxView{{ID: 1, Date: "2026-10-01", Postings: []PostingView{pe("Assets:Bank", -100), pe("Expenses:Groceries", 100)}}},
			},
		},
		{
			name: "an unbalanced transaction, which also leaves no new account behind", setup: bank,
			nt: NewTransaction{Date: day("2026-10-02"), Postings: []ledger.Posting{
				{Account: "Assets:Bank", Amount: eur(-100)}, {Account: "Expenses:ShouldNotExist", Amount: eur(90)}}},
			wantErr:   "postings do not balance: EUR is off by -0.10",
			wantState: ledgerState{Accounts: []string{"Assets:Bank"}},
		},
		{
			name: "an account that holds another currency", setup: bank,
			nt: NewTransaction{Date: day("2026-10-01"), Postings: []ledger.Posting{
				{Account: "Assets:Bank", Amount: usd(-100)}, {Account: "Expenses:Groceries", Amount: usd(100)}}},
			wantErr:   "account Assets:Bank holds EUR, but the posting is in USD",
			wantState: ledgerState{Accounts: []string{"Assets:Bank"}},
		},
		{
			name: "an asset account that does not exist", setup: bank,
			nt: NewTransaction{Date: day("2026-10-01"), Postings: []ledger.Posting{
				{Account: "Assets:Ghost", Amount: eur(-100)}, {Account: "Expenses:Groceries", Amount: eur(100)}}},
			wantErr:   "account Assets:Ghost does not exist; create it first as a Assets account in EUR",
			wantState: ledgerState{Accounts: []string{"Assets:Bank"}},
		},
		{
			name: "no date", setup: bank, nt: NewTransaction{Postings: groceries(100)},
			wantErr: "transaction has no date", wantState: ledgerState{Accounts: []string{"Assets:Bank"}},
		},
		{
			name: "an import hash that is new", setup: bank, nt: withHash("a"), want: AddResult{ID: 1, Created: []string{ledger.UncategorizedExpense}},
			wantState: ledgerState{
				Accounts:     []string{"Assets:Bank", ledger.UncategorizedExpense},
				Transactions: []TxView{{ID: 1, Date: "2026-10-05", Payee: "Bakery", ImportHash: "a", Postings: []PostingView{pe("Assets:Bank", -320), pe(ledger.UncategorizedExpense, 320)}}},
			},
		},
		{
			name: "an import hash that is already stored", setup: []given{accounts("Assets:Bank EUR"), stored(withHash("a"))}, nt: withHash("a"),
			wantErr: "transaction already imported", wantIs: ErrDuplicate,
			wantState: ledgerState{
				Accounts:     []string{"Assets:Bank", ledger.UncategorizedExpense},
				Transactions: []TxView{{ID: 1, Date: "2026-10-05", Payee: "Bakery", ImportHash: "a", Postings: []PostingView{pe("Assets:Bank", -320), pe(ledger.UncategorizedExpense, 320)}}},
			},
		},
		{
			name: "entries without a hash never collide", setup: []given{accounts("Assets:Bank EUR"), stored(withHash(""))}, nt: withHash(""),
			want: AddResult{ID: 2},
			wantState: ledgerState{
				Accounts: []string{"Assets:Bank", ledger.UncategorizedExpense},
				Transactions: []TxView{
					{ID: 1, Date: "2026-10-05", Payee: "Bakery", Postings: []PostingView{pe("Assets:Bank", -320), pe(ledger.UncategorizedExpense, 320)}},
					{ID: 2, Date: "2026-10-05", Payee: "Bakery", Postings: []PostingView{pe("Assets:Bank", -320), pe(ledger.UncategorizedExpense, 320)}},
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.AddTransaction(t.Context(), tc.nt)

			// then
			checkErr(t, err, tc.wantErr, tc.wantIs)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("result mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantState, stateOf(t, st), equalEmpty); diff != "" {
				t.Errorf("stored state mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_importHashExists(t *testing.T) {
	tcs := []struct {
		name string
		hash string
		want bool
	}{
		{name: "one that is stored", hash: "a", want: true},
		{name: "one that is not", hash: "zzz", want: false},
		{name: "none at all", hash: "", want: false}, // hand-entered transactions have no hash
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"),
				stored(NewTransaction{Date: day("2026-10-05"), ImportHash: "a", Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1)}, {Account: "Expenses:X", Amount: eur(1)}}}),
				handEntered("Assets:Bank", "2026-10-06", "by hand", "Expenses:X", -2))

			// when
			got, err := st.repo.queries().importHashExists(t.Context(), tc.hash)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_GetTransaction(t *testing.T) {
	setup := []given{accounts("Assets:Bank EUR"), stored(NewTransaction{
		Date: day("2026-10-01"), Payee: "REWE", Description: "weekly", ImportHash: "h1",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Groceries", Amount: eur(1000)}},
	})}
	tcs := []struct {
		name    string
		id      int64
		want    TxView
		wantErr string
	}{
		{
			name: "one that exists", id: 1,
			want: TxView{ID: 1, Date: "2026-10-01", Payee: "REWE", Description: "weekly", ImportHash: "h1", Postings: []PostingView{pe("Assets:Bank", -1000), pe("Expenses:Groceries", 1000)}},
		},
		{name: "one that does not", id: 99, wantErr: "transaction #99 does not exist"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, setup...)

			// when
			got, err := st.GetTransaction(t.Context(), tc.id)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ListTransactions(t *testing.T) {
	setup := []given{
		accounts("Assets:Bank EUR"),
		spending("2026-10-01", "Expenses:Food:Groceries", 1000),
		spending("2026-10-02", "Expenses:Food:Eating_Out", 2000),
		spending("2026-10-03", "Expenses:Transport", 300),
		spending("2026-10-04", "Expenses:Foodie", 400), // must not match "Expenses:Food"
	}
	tcs := []struct {
		name   string
		filter TxFilter
		want   []int64 // ids, oldest first
	}{
		{name: "everything, oldest first", filter: TxFilter{}, want: []int64{1, 2, 3, 4}},
		{name: "an account includes its sub-accounts but not a lookalike", filter: TxFilter{Account: "Expenses:Food"}, want: []int64{1, 2}},
		{name: "an underscore is matched literally", filter: TxFilter{Account: "Expenses:Food:Eating_Out"}, want: []int64{2}},
		{name: "an account that nothing touches", filter: TxFilter{Account: "Expenses:Fun"}, want: nil},
		{name: "a date range, inclusive", filter: TxFilter{From: "2026-10-02", To: "2026-10-03"}, want: []int64{2, 3}},
		{name: "from a date on", filter: TxFilter{From: "2026-10-03"}, want: []int64{3, 4}},
		{name: "up to a date", filter: TxFilter{To: "2026-10-01"}, want: []int64{1}},
		{name: "a limit keeps the newest, oldest first", filter: TxFilter{Limit: 2}, want: []int64{3, 4}},
		{name: "one by id", filter: TxFilter{ID: 2}, want: []int64{2}},
		{name: "filters combine", filter: TxFilter{Account: "Expenses:Food", From: "2026-10-02"}, want: []int64{2}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, setup...)

			// when
			got, err := st.ListTransactions(t.Context(), tc.filter)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, ids(got), equalEmpty); diff != "" {
				t.Errorf("ids mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("each transaction carries its postings", func(t *testing.T) {
		// given
		st := newLedger(t, setup...)

		// when
		got, err := st.ListTransactions(t.Context(), TxFilter{ID: 3})

		// then
		checkErr(t, err, "", nil)
		want := []TxView{{ID: 3, Date: "2026-10-03", Payee: "shop", Postings: []PostingView{pe("Assets:Bank", -300), pe("Expenses:Transport", 300)}}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
}

func Test_ReplaceTransaction(t *testing.T) {
	original := NewTransaction{
		Date: day("2026-10-01"), Payee: "REWE", Description: "weekly", ImportHash: "h1",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Groceries", Amount: eur(1000)}},
	}
	setup := []given{accounts("Assets:Bank EUR"), stored(original)}
	edited := NewTransaction{
		Date: day("2026-10-02"), Payee: "Edeka", Description: "weekly",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1250)}, {Account: "Expenses:Food", Amount: eur(1250)}},
	}
	unchanged := TxView{ID: 1, Date: "2026-10-01", Payee: "REWE", Description: "weekly", ImportHash: "h1", Postings: []PostingView{pe("Assets:Bank", -1000), pe("Expenses:Groceries", 1000)}}
	tcs := []struct {
		name        string
		id          int64
		nt          NewTransaction
		wantCreated []string
		wantTx      TxView
		wantErr     string
	}{
		{
			name: "rewritten in place: the id and import hash stay, a new category is created", id: 1, nt: edited,
			wantCreated: []string{"Expenses:Food"},
			wantTx:      TxView{ID: 1, Date: "2026-10-02", Payee: "Edeka", Description: "weekly", ImportHash: "h1", Postings: []PostingView{pe("Assets:Bank", -1250), pe("Expenses:Food", 1250)}},
		},
		{
			name: "an unbalanced edit changes nothing", id: 1,
			nt: NewTransaction{Date: day("2026-10-02"), Payee: "Edeka", Postings: []ledger.Posting{
				{Account: "Assets:Bank", Amount: eur(-5)}, {Account: "Expenses:Food", Amount: eur(4)}}},
			wantTx: unchanged, wantErr: "postings do not balance: EUR is off by -0.01",
		},
		{name: "a transaction that does not exist", id: 999, nt: edited, wantTx: unchanged, wantErr: "transaction #999 does not exist"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, setup...)

			// when
			created, err := st.ReplaceTransaction(t.Context(), tc.id, tc.nt)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.wantCreated, created, equalEmpty); diff != "" {
				t.Errorf("created categories mismatch (-want +got):\n%s", diff)
			}
			got, gerr := st.GetTransaction(t.Context(), 1)
			if gerr != nil {
				t.Fatal(gerr)
			}
			if diff := cmp.Diff(tc.wantTx, got); diff != "" {
				t.Errorf("stored transaction mismatch (-want +got):\n%s", diff)
			}
			stillKnown, herr := st.repo.queries().importHashExists(t.Context(), "h1")
			checkErr(t, herr, "", nil)
			if diff := cmp.Diff(true, stillKnown); diff != "" {
				t.Errorf("an edited imported row must still be recognised on re-import (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_DeleteTransaction(t *testing.T) {
	setup := []given{accounts("Assets:Bank EUR"), spending("2026-10-01", "Expenses:Food", 100), spending("2026-10-02", "Expenses:Food", 200)}
	tcs := []struct {
		name    string
		id      int64
		want    []int64 // the ids left
		wantErr string
	}{
		{name: "one of two", id: 1, want: []int64{2}},
		{name: "one that does not exist", id: 99, want: []int64{1, 2}, wantErr: "transaction #99 does not exist"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, setup...)

			// when
			err := st.DeleteTransaction(t.Context(), tc.id)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, ids(stateOf(t, st).Transactions)); diff != "" {
				t.Errorf("remaining transactions mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("deleting twice fails the second time", func(t *testing.T) {
		// given
		st := newLedger(t, setup...)
		if err := st.DeleteTransaction(t.Context(), 1); err != nil {
			t.Fatal(err)
		}

		// when
		err := st.DeleteTransaction(t.Context(), 1)

		// then
		checkErr(t, err, "transaction #1 does not exist", nil)
	})
}

func Test_RestoreTransaction(t *testing.T) {
	original := NewTransaction{
		Date: day("2026-10-01"), Payee: "REWE", ImportHash: "h1",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Groceries", Amount: eur(1000)}},
	}
	setup := []given{accounts("Assets:Bank EUR"), stored(original), func(t *testing.T, st *Local) {
		if err := st.DeleteTransaction(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
	}}
	tcs := []struct {
		name    string
		id      int64
		nt      NewTransaction
		want    []TxView
		wantErr string
	}{
		{
			name: "a deleted transaction comes back under its old number, hash included", id: 1, nt: original,
			want: []TxView{{ID: 1, Date: "2026-10-01", Payee: "REWE", ImportHash: "h1", Postings: []PostingView{pe("Assets:Bank", -1000), pe("Expenses:Groceries", 1000)}}},
		},
		{
			name: "an unbalanced one is refused", id: 1,
			nt: NewTransaction{Date: day("2026-10-01"), Postings: []ledger.Posting{
				{Account: "Assets:Bank", Amount: eur(-5)}, {Account: "Expenses:Groceries", Amount: eur(4)}}},
			wantErr: "postings do not balance: EUR is off by -0.01",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, setup...)

			// when
			err := st.RestoreTransaction(t.Context(), tc.id, tc.nt)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, stateOf(t, st).Transactions, equalEmpty); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_SetCategories(t *testing.T) {
	uncategorized := []given{
		accounts("Assets:Bank EUR", "Assets:Savings EUR"),
		spending("2026-10-01", ledger.UncategorizedExpense, 500),
		spending("2026-10-02", ledger.UncategorizedExpense, 700),
	}
	categoryOf := func(st *Local) []string {
		var out []string
		txs, _ := st.ListTransactions(t.Context(), TxFilter{})
		for _, tx := range txs {
			_, cat, _ := tx.Legs()
			out = append(out, cat.Account)
		}
		return out
	}
	tcs := []struct {
		name        string
		setup       []given
		changes     []CategoryChange
		wantCreated []string
		wantCats    []string // the category of each transaction afterwards, by id
		wantAccts   []string
		wantErr     string
	}{
		{
			name: "two at once, creating the category once", setup: uncategorized,
			changes:     []CategoryChange{{TransactionID: 1, Category: "Expenses:Groceries"}, {TransactionID: 2, Category: "Expenses:Groceries"}},
			wantCreated: []string{"Expenses:Groceries"},
			wantCats:    []string{"Expenses:Groceries", "Expenses:Groceries"},
			wantAccts:   []string{"Assets:Bank", "Assets:Savings", "Expenses:Groceries", ledger.UncategorizedExpense},
		},
		{
			name: "a category that exists is not created", setup: append(append([]given{}, uncategorized...), accounts("Expenses:Food")),
			changes:   []CategoryChange{{TransactionID: 1, Category: "Expenses:Food"}},
			wantCats:  []string{"Expenses:Food", ledger.UncategorizedExpense},
			wantAccts: []string{"Assets:Bank", "Assets:Savings", "Expenses:Food", ledger.UncategorizedExpense},
		},
		{
			name: "all or nothing: a bad change rolls the good one and its new category back", setup: uncategorized,
			changes:   []CategoryChange{{TransactionID: 1, Category: "Expenses:Fun"}, {TransactionID: 1, Category: "Assets:Savings"}},
			wantCats:  []string{ledger.UncategorizedExpense, ledger.UncategorizedExpense},
			wantAccts: []string{"Assets:Bank", "Assets:Savings", ledger.UncategorizedExpense},
			wantErr:   "transaction #1: Assets:Savings is not a category; use an Income, Expenses or Equity account",
		},
		{
			name: "a transaction that does not exist", setup: uncategorized,
			changes:   []CategoryChange{{TransactionID: 999, Category: "Expenses:Fun"}},
			wantCats:  []string{ledger.UncategorizedExpense, ledger.UncategorizedExpense},
			wantAccts: []string{"Assets:Bank", "Assets:Savings", ledger.UncategorizedExpense},
			wantErr:   "transaction #999: has 0 category postings; only transactions with exactly one can be recategorized",
		},
		{
			name: "a transfer between two accounts has no category leg",
			setup: []given{accounts("Assets:Bank EUR", "Assets:Savings EUR"), stored(NewTransaction{Date: day("2026-10-01"),
				Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Assets:Savings", Amount: eur(1000)}}})},
			changes:   []CategoryChange{{TransactionID: 1, Category: "Expenses:Fun"}},
			wantCats:  []string{""},
			wantAccts: []string{"Assets:Bank", "Assets:Savings"},
			wantErr:   "transaction #1: has 0 category postings; only transactions with exactly one can be recategorized",
		},
		{
			name: "no changes", setup: uncategorized, wantCats: []string{ledger.UncategorizedExpense, ledger.UncategorizedExpense},
			wantAccts: []string{"Assets:Bank", "Assets:Savings", ledger.UncategorizedExpense},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			created, err := st.SetCategories(t.Context(), tc.changes)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.wantCreated, created, equalEmpty); diff != "" {
				t.Errorf("created categories mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantCats, categoryOf(st)); diff != "" {
				t.Errorf("categories mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantAccts, stateOf(t, st).Accounts); diff != "" {
				t.Errorf("accounts mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}
