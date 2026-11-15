package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// accountState is everything an account touches, as lines of text, for comparing what a call
// left behind with what it should.
type accountState struct {
	Accounts     []string // name, and "closed" for a closed one
	Transactions []string
	Schedules    []string
	Minimums     []string
}

func accountStateOf(t *testing.T, st *Local) accountState {
	t.Helper()
	ctx := t.Context()
	var s accountState
	accs, err := st.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range accs {
		if !a.Type.NeedsCurrency() {
			continue
		}
		line := a.Name
		if a.Closed {
			line += " closed"
		}
		s.Accounts = append(s.Accounts, line)
	}
	postings := func(ps []PostingView) string {
		var parts []string
		for _, p := range ps {
			parts = append(parts, p.Account+" "+p.Amount.String())
		}
		return strings.Join(parts, ", ")
	}
	txs, err := st.ListTransactions(ctx, TxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range txs {
		s.Transactions = append(s.Transactions, fmt.Sprintf("%d %s %s: %s", tx.ID, tx.Date, tx.Payee, postings(tx.Postings)))
	}
	schedules, err := st.Schedules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range schedules {
		s.Schedules = append(s.Schedules, sc.Name+": "+postings(sc.Postings))
	}
	mins, err := st.MinimumBalances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mins {
		s.Minimums = append(s.Minimums, m.Account+" "+m.Amount.String())
	}
	return s
}

// bankWorld is a bank account with a schedule and a minimum, an old account that took
// money in and paid it all out again, a savings account with something in it, and a card.
func bankWorld() []given {
	return []given{
		accounts("Assets:Bank EUR", "Assets:Old EUR", "Assets:Savings EUR", "Liabilities:Card EUR"),
		spending("2026-10-06", "Expenses:Food", 800),
		handEntered("Assets:Old", "2026-09-01", "employer", "Income:Salary", 5000),
		handEntered("Assets:Old", "2026-09-10", "shop", "Expenses:Food", -5000),
		handEntered("Assets:Savings", "2026-09-02", "gift", "Income:Gift", 2000),
		lunch(),
		minimum("Assets:Bank", eur(10000)),
	}
}

var bankState = accountState{
	Accounts: []string{"Assets:Bank", "Assets:Old", "Assets:Savings", "Liabilities:Card"},
	Transactions: []string{
		"2 2026-09-01 employer: Assets:Old 50.00 EUR, Income:Salary -50.00 EUR",
		"4 2026-09-02 gift: Assets:Savings 20.00 EUR, Income:Gift -20.00 EUR",
		"3 2026-09-10 shop: Assets:Old -50.00 EUR, Expenses:Food 50.00 EUR",
		"1 2026-10-06 shop: Assets:Bank -8.00 EUR, Expenses:Food 8.00 EUR",
	},
	Schedules: []string{"Lunch: Assets:Bank -10.00 EUR, Expenses:Food 10.00 EUR"},
	Minimums:  []string{"Assets:Bank 100.00 EUR"},
}

func withAccounts(f func(*accountState)) accountState {
	s := bankState
	s.Accounts = append([]string(nil), s.Accounts...)
	s.Transactions = append([]string(nil), s.Transactions...)
	s.Schedules = append([]string(nil), s.Schedules...)
	s.Minimums = append([]string(nil), s.Minimums...)
	f(&s)
	return s
}

func Test_bankWorld(t *testing.T) {
	tcs := []struct {
		name string
		want accountState
	}{{name: "the ledger the cases start from", want: bankState}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, bankWorld()...)

			// when
			got := accountStateOf(t, st)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_RenameAccount(t *testing.T) {
	tcs := []struct {
		name     string
		setup    []given
		from, to string
		want     Account
		state    accountState
	}{
		{
			name: "the transactions, schedules and minimum follow the name",
			from: "Assets:Bank", to: "Assets:Sparkasse",
			want: Account{ID: 1, Name: "Assets:Sparkasse", Type: ledger.Assets, Currency: "EUR"},
			state: withAccounts(func(s *accountState) {
				s.Accounts = []string{"Assets:Old", "Assets:Savings", "Assets:Sparkasse", "Liabilities:Card"}
				s.Transactions[3] = "1 2026-10-06 shop: Assets:Sparkasse -8.00 EUR, Expenses:Food 8.00 EUR"
				s.Schedules = []string{"Lunch: Assets:Sparkasse -10.00 EUR, Expenses:Food 10.00 EUR"}
				s.Minimums = []string{"Assets:Sparkasse 100.00 EUR"}
			}),
		},
		{
			name: "a card",
			from: "Liabilities:Card", to: "Liabilities:Visa",
			want: Account{ID: 4, Name: "Liabilities:Visa", Type: ledger.Liabilities, Currency: "EUR"},
			state: withAccounts(func(s *accountState) {
				s.Accounts = []string{"Assets:Bank", "Assets:Old", "Assets:Savings", "Liabilities:Visa"}
			}),
		},
		{
			name: "only the case changes",
			from: "Assets:Bank", to: "Assets:BANK",
			want: Account{ID: 1, Name: "Assets:BANK", Type: ledger.Assets, Currency: "EUR"},
			state: withAccounts(func(s *accountState) {
				s.Accounts = []string{"Assets:BANK", "Assets:Old", "Assets:Savings", "Liabilities:Card"}
				s.Transactions[3] = "1 2026-10-06 shop: Assets:BANK -8.00 EUR, Expenses:Food 8.00 EUR"
				s.Schedules = []string{"Lunch: Assets:BANK -10.00 EUR, Expenses:Food 10.00 EUR"}
				s.Minimums = []string{"Assets:BANK 100.00 EUR"}
			}),
		},
		{
			name:  "a closed account can be renamed too, and stays closed",
			setup: []given{closing("Assets:Old")},
			from:  "Assets:Old", to: "Assets:Archive",
			want: Account{ID: 2, Name: "Assets:Archive", Type: ledger.Assets, Currency: "EUR", Closed: true},
			state: withAccounts(func(s *accountState) {
				s.Accounts = []string{"Assets:Archive closed", "Assets:Bank", "Assets:Savings", "Liabilities:Card"}
				s.Transactions[0] = "2 2026-09-01 employer: Assets:Archive 50.00 EUR, Income:Salary -50.00 EUR"
				s.Transactions[2] = "3 2026-09-10 shop: Assets:Archive -50.00 EUR, Expenses:Food 50.00 EUR"
			}),
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(bankWorld(), tc.setup...)...)

			// when
			got, err := st.RenameAccount(t.Context(), tc.from, tc.to)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("account mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.state, accountStateOf(t, st)); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
			again, err := st.GetAccount(t.Context(), tc.to)
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, again); diff != "" {
				t.Errorf("GetAccount mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_RenameAccount_refused(t *testing.T) {
	tcs := []struct {
		name     string
		setup    []given
		from, to string
		wantErr  string
	}{
		{name: "no such account", from: "Assets:Nope", to: "Assets:New", wantErr: "account does not exist: Assets:Nope"},
		{name: "a category is renamed as one", from: "Expenses:Food", to: "Expenses:Dining", wantErr: "Expenses:Food is not one of your accounts: only Assets and Liabilities accounts are renamed or closed"},
		{name: "Equity neither", from: "Equity:Transfers", to: "Equity:Moves", setup: []given{accounts("Equity:Transfers")}, wantErr: "Equity:Transfers is not one of your accounts: only Assets and Liabilities accounts are renamed or closed"},
		{name: "the type stays", from: "Assets:Bank", to: "Liabilities:Bank", wantErr: "cannot rename Assets:Bank to Liabilities:Bank: an account stays Assets, so the new name starts with Assets:"},
		{name: "not into a category", from: "Assets:Bank", to: "Expenses:Bank", wantErr: "cannot rename Assets:Bank to Expenses:Bank: an account stays Assets, so the new name starts with Assets:"},
		{name: "the same name", from: "Assets:Bank", to: "Assets:Bank", wantErr: "Assets:Bank already has that name"},
		{name: "a name that is taken", from: "Assets:Bank", to: "Assets:Savings", wantErr: "account already exists: Assets:Savings"},
		{name: "taken by a closed account", from: "Assets:Bank", to: "Assets:Old", setup: []given{closing("Assets:Old")}, wantErr: "account already exists: Assets:Old"},
		{name: "not a name", from: "Assets:Bank", to: "Assets:My Bank", wantErr: `account "Assets:My Bank": invalid character " " (use letters, digits, '-' and '_')`},
		{name: "no type", from: "Assets:Bank", to: "Bank", wantErr: `account "Bank" must start with one of Assets, Liabilities, Income, Expenses, Equity`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(bankWorld(), tc.setup...)...)
			before := accountStateOf(t, st)

			// when
			_, err := st.RenameAccount(t.Context(), tc.from, tc.to)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(before, accountStateOf(t, st)); diff != "" {
				t.Errorf("a refused call changed something (-before +after):\n%s", diff)
			}
		})
	}
}

// closing closes an account for its side effects.
func closing(name string) given {
	return func(t *testing.T, st *Local) {
		t.Helper()
		if _, err := st.CloseAccount(t.Context(), name); err != nil {
			t.Fatalf("CloseAccount(%s): %v", name, err)
		}
	}
}

func Test_CloseAccount(t *testing.T) {
	tcs := []struct {
		name  string
		setup []given
		name1 string
		want  Account
		state accountState
	}{
		{
			name: "an account that holds nothing is set aside, with its history", name1: "Assets:Old",
			want: Account{ID: 2, Name: "Assets:Old", Type: ledger.Assets, Currency: "EUR", Closed: true},
			state: withAccounts(func(s *accountState) {
				s.Accounts = []string{"Assets:Bank", "Assets:Old closed", "Assets:Savings", "Liabilities:Card"}
			}),
		},
		{
			name: "an account that never had anything", name1: "Liabilities:Card",
			want: Account{ID: 4, Name: "Liabilities:Card", Type: ledger.Liabilities, Currency: "EUR", Closed: true},
			state: withAccounts(func(s *accountState) {
				s.Accounts = []string{"Assets:Bank", "Assets:Old", "Assets:Savings", "Liabilities:Card closed"}
			}),
		},
		{
			name: "a schedule that is gone no longer stops it", name1: "Assets:Bank",
			setup: []given{rawSQL(`DELETE FROM schedules`), rawSQL(`DELETE FROM transactions WHERE id = 1`)},
			want:  Account{ID: 1, Name: "Assets:Bank", Type: ledger.Assets, Currency: "EUR", Closed: true},
			state: withAccounts(func(s *accountState) {
				s.Accounts = []string{"Assets:Bank closed", "Assets:Old", "Assets:Savings", "Liabilities:Card"}
				s.Transactions = s.Transactions[:3]
				s.Schedules = nil
			}),
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(bankWorld(), tc.setup...)...)

			// when
			got, err := st.CloseAccount(t.Context(), tc.name1)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("account mismatch (-want +got):\n%s", diff)
			}
			again, err := st.GetAccount(t.Context(), tc.name1)
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, again); diff != "" {
				t.Errorf("GetAccount mismatch (-want +got):\n%s", diff)
			}
			state := accountStateOf(t, st)
			state.Minimums = tc.state.Minimums // a closed account's minimum is not what is tested here
			if diff := cmp.Diff(tc.state, state); diff != "" {
				t.Errorf("state mismatch (-want +got):\n%s", diff)
			}
			checkConsistent(t, st)
		})
	}
}

func Test_CloseAccount_refused(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		account string
		wantErr string
	}{
		{name: "no such account", account: "Assets:Nope", wantErr: "account does not exist: Assets:Nope"},
		{name: "a category", account: "Expenses:Food", wantErr: "Expenses:Food is not one of your accounts: only Assets and Liabilities accounts are renamed or closed"},
		{name: "money in it", account: "Assets:Savings", wantErr: "Assets:Savings still holds 20.00 EUR: move it out before closing the account"},
		{name: "money owed on a card", account: "Liabilities:Card", setup: []given{handEntered("Liabilities:Card", "2026-10-01", "shop", "Expenses:Food", -3000)}, wantErr: "Liabilities:Card still holds -30.00 EUR: move it out before closing the account"},
		{name: "a schedule books to it", account: "Assets:Bank", setup: []given{rawSQL(`DELETE FROM transactions WHERE id = 1`)}, wantErr: "Assets:Bank still has 1 schedule: delete it before closing the account"},
		{
			name: "two schedules", account: "Assets:Old",
			setup: []given{
				schedule(NewSchedule{Name: "One", Every: 1, Unit: EveryMonth, Start: day("2026-10-15"), Postings: []ledger.Posting{{Account: "Assets:Old", Amount: eur(-100)}, {Account: "Expenses:Food", Amount: eur(100)}}}),
				schedule(NewSchedule{Name: "Two", Every: 1, Unit: EveryMonth, Start: day("2026-10-16"), Postings: []ledger.Posting{{Account: "Assets:Old", Amount: eur(-200)}, {Account: "Expenses:Food", Amount: eur(200)}}}),
			},
			wantErr: "Assets:Old still has 2 schedules: delete them before closing the account",
		},
		{
			name: "a schedule that books to it twice is one schedule", account: "Assets:Old",
			setup: []given{schedule(NewSchedule{Name: "Split", Every: 1, Unit: EveryMonth, Start: day("2026-10-15"), Postings: []ledger.Posting{
				{Account: "Assets:Old", Amount: eur(-100)}, {Account: "Assets:Old", Amount: eur(-50)}, {Account: "Expenses:Food", Amount: eur(150)},
			}})},
			wantErr: "Assets:Old still has 1 schedule: delete it before closing the account",
		},
		{name: "already closed", account: "Assets:Old", setup: []given{closing("Assets:Old")}, wantErr: "Assets:Old is already closed"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(bankWorld(), tc.setup...)...)
			before := accountStateOf(t, st)

			// when
			_, err := st.CloseAccount(t.Context(), tc.account)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(before, accountStateOf(t, st)); diff != "" {
				t.Errorf("a refused call changed something (-before +after):\n%s", diff)
			}
		})
	}
}

func Test_ReopenAccount(t *testing.T) {
	tcs := []struct {
		name    string
		setup   []given
		account string
		want    Account
		wantErr string
	}{
		{name: "a closed account is back", setup: []given{closing("Assets:Old")}, account: "Assets:Old", want: Account{ID: 2, Name: "Assets:Old", Type: ledger.Assets, Currency: "EUR"}},
		{name: "one that is open", account: "Assets:Old", wantErr: "Assets:Old is not closed"},
		{name: "a category", account: "Expenses:Food", wantErr: "Expenses:Food is not one of your accounts: only Assets and Liabilities accounts are renamed or closed"},
		{name: "no such account", account: "Assets:Nope", wantErr: "account does not exist: Assets:Nope"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(bankWorld(), tc.setup...)...)

			// when
			got, err := st.ReopenAccount(t.Context(), tc.account)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("account mismatch (-want +got):\n%s", diff)
			}
			if tc.wantErr == "" {
				if diff := cmp.Diff(bankState, accountStateOf(t, st)); diff != "" {
					t.Errorf("state mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// A closed account keeps its history and takes nothing new that would change what it holds.
func Test_closedAccount(t *testing.T) {
	const closed = "Assets:Old is closed: reopen it first"
	newTx := func(account string, minor int64) NewTransaction {
		return NewTransaction{Date: day("2026-10-20"), Payee: "x", Postings: []ledger.Posting{{Account: account, Amount: eur(minor)}, {Account: "Expenses:Food", Amount: eur(-minor)}}}
	}
	tcs := []struct {
		name    string
		do      func(t *testing.T, st *Local) error
		wantErr string
	}{
		{name: "a transaction on it", do: func(t *testing.T, st *Local) error {
			_, err := st.AddTransaction(t.Context(), newTx("Assets:Old", -100))
			return err
		}, wantErr: closed},
		{name: "a transaction on another account is fine", do: func(t *testing.T, st *Local) error {
			_, err := st.AddTransaction(t.Context(), newTx("Assets:Bank", -100))
			return err
		}},
		{name: "a transfer out of it", do: func(t *testing.T, st *Local) error {
			_, err := st.Transfer(t.Context(), TransferRequest{From: "Assets:Old", To: "Assets:Bank", Amount: eur(100), Date: day("2026-10-20")})
			return err
		}, wantErr: closed},
		{name: "a transfer into it", do: func(t *testing.T, st *Local) error {
			_, err := st.Transfer(t.Context(), TransferRequest{From: "Assets:Bank", To: "Assets:Old", Amount: eur(100), Date: day("2026-10-20")})
			return err
		}, wantErr: closed},
		{name: "an import into it", do: func(t *testing.T, st *Local) error {
			_, err := st.Import(t.Context(), ImportRequest{Account: "Assets:Old", Rows: []importer.Row{{Line: 2, Date: day("2026-10-20"), Payee: "x", Amount: eur(-100)}}})
			return err
		}, wantErr: closed},
		{name: "an opening balance", do: func(t *testing.T, st *Local) error {
			_, err := st.SetOpeningBalance(t.Context(), OpeningBalanceRequest{Account: "Assets:Old", Balance: eur(0), AsOf: day("2026-09-01")})
			return err
		}, wantErr: closed},
		{name: "an opening balance that would change nothing is refused too", do: func(t *testing.T, st *Local) error {
			_, err := st.SetOpeningBalance(t.Context(), OpeningBalanceRequest{Account: "Assets:Old", Balance: eur(0), AsOf: day("2026-10-01")})
			return err
		}, wantErr: closed},
		{name: "a schedule that books to it", do: func(t *testing.T, st *Local) error {
			_, err := st.CreateSchedule(t.Context(), NewSchedule{Name: "Rent", Every: 1, Unit: EveryMonth, Start: day("2026-10-15"), Postings: []ledger.Posting{{Account: "Assets:Old", Amount: eur(-100)}, {Account: "Expenses:Food", Amount: eur(100)}}})
			return err
		}, wantErr: closed},
		{name: "editing a transaction of it", do: func(t *testing.T, st *Local) error {
			_, err := st.ReplaceTransaction(t.Context(), 2, newTx("Assets:Old", 5000))
			return err
		}, wantErr: closed},
		{name: "moving a transaction of it to another account", do: func(t *testing.T, st *Local) error {
			_, err := st.ReplaceTransaction(t.Context(), 2, newTx("Assets:Bank", 5000))
			return err
		}, wantErr: closed},
		{name: "deleting a transaction of it", do: func(t *testing.T, st *Local) error {
			return st.DeleteTransaction(t.Context(), 2)
		}, wantErr: closed},
		{name: "editing a transaction of an account that is open", do: func(t *testing.T, st *Local) error {
			_, err := st.ReplaceTransaction(t.Context(), 1, newTx("Assets:Bank", -900))
			return err
		}},
		{name: "deleting a transaction of an account that is open", do: func(t *testing.T, st *Local) error {
			return st.DeleteTransaction(t.Context(), 1)
		}},
		{name: "its category can still change", do: func(t *testing.T, st *Local) error {
			_, err := st.SetCategories(t.Context(), []CategoryChange{{TransactionID: 3, Category: "Expenses:Fun"}})
			return err
		}},
		{name: "it can be read", do: func(t *testing.T, st *Local) error {
			if _, err := st.GetAccount(t.Context(), "Assets:Old"); err != nil {
				return err
			}
			_, err := st.GetTransaction(t.Context(), 2)
			return err
		}},
		{name: "reopened, it takes money again", do: func(t *testing.T, st *Local) error {
			if _, err := st.ReopenAccount(t.Context(), "Assets:Old"); err != nil {
				return err
			}
			_, err := st.AddTransaction(t.Context(), newTx("Assets:Old", -100))
			return err
		}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(bankWorld(), closing("Assets:Old"))...)
			held := func() []ledger.Money {
				var out []ledger.Money
				acc, err := st.GetAccount(t.Context(), "Assets:Old")
				checkErr(t, err, "", nil)
				out, err = st.repo.queries().accountHoldings(t.Context(), acc.ID)
				checkErr(t, err, "", nil)
				return out
			}
			before := accountStateOf(t, st)

			// when
			err := tc.do(t, st)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if tc.wantErr != "" {
				if diff := cmp.Diff(before, accountStateOf(t, st)); diff != "" {
					t.Errorf("a refused call changed something (-before +after):\n%s", diff)
				}
			}
			if acc, _ := st.GetAccount(t.Context(), "Assets:Old"); acc.Closed {
				if diff := cmp.Diff([]ledger.Money(nil), held(), equalEmpty); diff != "" {
					t.Errorf("a closed account holds something (-want +got):\n%s", diff)
				}
			}
			checkConsistent(t, st)
		})
	}
}

// The past of a closed account is still counted: what it held on a day before it was closed
// is in the balance of that day, and the account is not in the balance of today.
func Test_closedAccount_history(t *testing.T) {
	tcs := []struct {
		name string
		asOf string
		want []string
	}{
		{name: "while it held money", asOf: "2026-09-05", want: []string{"Assets:Old 50.00 EUR", "Assets:Savings 20.00 EUR"}},
		{name: "today", asOf: "2026-10-20", want: []string{"Assets:Bank -8.00 EUR", "Assets:Savings 20.00 EUR"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(bankWorld(), closing("Assets:Old"))...)

			// when
			rep, err := st.BalanceReport(t.Context(), BalanceRequest{AsOf: tc.asOf})

			// then
			checkErr(t, err, "", nil)
			var got []string
			for _, l := range rep.Lines {
				got = append(got, l.Account+" "+l.Amount.String())
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("balances mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A forecast leaves a closed account out: it holds nothing and nothing is booked to it.
func Test_closedAccount_projection(t *testing.T) {
	tcs := []struct {
		name string
		want []string
	}{{name: "the accounts that are open", want: []string{"Assets:Bank", "Assets:Savings", "Liabilities:Card"}}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, append(bankWorld(), today("2026-10-20"), closing("Assets:Old"))...)

			// when
			p, err := st.Projection(t.Context(), "2026-11-20")

			// then
			checkErr(t, err, "", nil)
			var got []string
			for _, a := range p.Accounts {
				got = append(got, a.Name)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("accounts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
