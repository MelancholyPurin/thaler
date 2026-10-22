package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/melancholypurin/thaler/internal/ledger"
)

var ctx = context.Background()

func eur(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "EUR"} }
func usd(minor int64) ledger.Money { return ledger.Money{Minor: minor, Currency: "USD"} }

func day(s string) time.Time {
	d, err := ledger.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func newStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func mustAccount(t *testing.T, st *Store, name string, cur ledger.Currency) Account {
	t.Helper()
	a, err := st.AddAccount(ctx, name, cur)
	if err != nil {
		t.Fatalf("AddAccount(%s): %v", name, err)
	}
	return a
}

func spend(t *testing.T, st *Store, date, category string, amount int64) {
	t.Helper()
	_, err := st.AddTransaction(ctx, NewTransaction{
		Date:     day(date),
		Payee:    "shop",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-amount)}, {Account: category, Amount: eur(amount)}},
	})
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
}

func TestAccounts(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	mustAccount(t, st, "Expenses:Food", "")

	if _, err := st.AddAccount(ctx, "Assets:Bank", "EUR"); !errors.Is(err, ErrAccountExists) {
		t.Errorf("duplicate account: got %v, want ErrAccountExists", err)
	}
	if _, err := st.AddAccount(ctx, "Assets:Cash", ""); err == nil {
		t.Error("asset accounts must require a currency")
	}
	if _, err := st.AddAccount(ctx, "bank", "EUR"); err == nil {
		t.Error("invalid account name must be rejected")
	}
	if _, err := st.GetAccount(ctx, "Assets:Nope"); !errors.Is(err, ErrNoAccount) {
		t.Errorf("missing account: got %v, want ErrNoAccount", err)
	}
	accs, err := st.ListAccounts(ctx)
	if err != nil || len(accs) != 2 || accs[0].Name != "Assets:Bank" || accs[0].Currency != "EUR" || accs[1].Currency != "" {
		t.Errorf("ListAccounts = %+v, %v", accs, err)
	}
}

func TestAddTransaction(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")

	res, err := st.AddTransaction(ctx, NewTransaction{
		Date:     day("2026-10-01"),
		Payee:    "REWE",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-2347)}, {Account: "Expenses:Groceries", Amount: eur(2347)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 1 || res.Created[0] != "Expenses:Groceries" {
		t.Errorf("category should be created implicitly, Created = %v", res.Created)
	}

	failures := map[string]NewTransaction{
		"unbalanced": {Date: day("2026-10-01"), Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-100)}, {Account: "Expenses:Groceries", Amount: eur(90)}}},
		"wrong currency for account": {Date: day("2026-10-01"), Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: usd(-100)}, {Account: "Expenses:Groceries", Amount: usd(100)}}},
		"unknown asset account": {Date: day("2026-10-01"), Postings: []ledger.Posting{
			{Account: "Assets:Ghost", Amount: eur(-100)}, {Account: "Expenses:Groceries", Amount: eur(100)}}},
		"no date": {Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-100)}, {Account: "Expenses:Groceries", Amount: eur(100)}}},
	}
	for name, nt := range failures {
		if _, err := st.AddTransaction(ctx, nt); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	// Failed transactions must leave nothing behind, including implicit accounts.
	_, err = st.AddTransaction(ctx, NewTransaction{
		Date: day("2026-10-02"),
		Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-100)},
			{Account: "Expenses:ShouldNotExist", Amount: eur(90)},
		},
	})
	if err == nil {
		t.Fatal("expected unbalanced error")
	}
	if _, err := st.GetAccount(ctx, "Expenses:ShouldNotExist"); !errors.Is(err, ErrNoAccount) {
		t.Errorf("rolled-back transaction left an account behind: %v", err)
	}
	if problems, err := st.Check(ctx); err != nil || len(problems) != 0 {
		t.Errorf("Check = %v, %v", problems, err)
	}
}

func TestImportDeduplication(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	mk := func(hash string, amount int64) NewTransaction {
		return NewTransaction{
			Date:       day("2026-10-05"),
			Payee:      "Bakery",
			ImportHash: hash,
			Postings:   []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-amount)}, {Account: ledger.UncategorizedExpense, Amount: eur(amount)}},
		}
	}

	first, err := st.AddTransactions(ctx, []NewTransaction{mk("a", 320), mk("b", 320)})
	if err != nil || first.Inserted != 2 || first.Duplicates != 0 {
		t.Fatalf("first import = %+v, %v", first, err)
	}
	second, err := st.AddTransactions(ctx, []NewTransaction{mk("a", 320), mk("b", 320), mk("c", 500)})
	if err != nil || second.Inserted != 1 || second.Duplicates != 2 {
		t.Fatalf("second import = %+v, %v", second, err)
	}
	if ok, err := st.HasImportHash(ctx, "c"); err != nil || !ok {
		t.Errorf("HasImportHash(c) = %v, %v", ok, err)
	}
	if ok, err := st.HasImportHash(ctx, "zzz"); err != nil || ok {
		t.Errorf("HasImportHash(zzz) = %v, %v", ok, err)
	}

	// One bad row aborts the whole batch.
	bad := mk("d", 100)
	bad.Postings[1].Amount = eur(1)
	if _, err := st.AddTransactions(ctx, []NewTransaction{mk("e", 100), bad}); err == nil {
		t.Fatal("expected the batch to fail")
	}
	if ok, _ := st.HasImportHash(ctx, "e"); ok {
		t.Error("a failed batch must be rolled back as a whole")
	}
}

func TestListTransactions(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	spend(t, st, "2026-10-01", "Expenses:Food:Groceries", 1000)
	spend(t, st, "2026-10-02", "Expenses:Food:Eating_Out", 2000)
	spend(t, st, "2026-10-03", "Expenses:Transport", 300)
	spend(t, st, "2026-10-04", "Expenses:Foodie", 400) // must NOT match "Expenses:Food"

	all, err := st.ListTransactions(ctx, TxFilter{})
	if err != nil || len(all) != 4 {
		t.Fatalf("all = %d, %v", len(all), err)
	}
	if all[0].Date != "2026-10-01" || all[3].Date != "2026-10-04" {
		t.Errorf("transactions must be ordered oldest to newest, got %s .. %s", all[0].Date, all[3].Date)
	}
	if len(all[0].Postings) != 2 {
		t.Errorf("each transaction should carry both postings, got %d", len(all[0].Postings))
	}

	food, _ := st.ListTransactions(ctx, TxFilter{Account: "Expenses:Food"})
	if len(food) != 2 {
		t.Errorf("account filter should include sub-accounts but not Foodie, got %d", len(food))
	}
	underscore, _ := st.ListTransactions(ctx, TxFilter{Account: "Expenses:Food:Eating_Out"})
	if len(underscore) != 1 {
		t.Errorf("underscore must be matched literally, got %d", len(underscore))
	}
	ranged, _ := st.ListTransactions(ctx, TxFilter{From: "2026-10-02", To: "2026-10-03"})
	if len(ranged) != 2 {
		t.Errorf("date range = %d, want 2", len(ranged))
	}
	last2, _ := st.ListTransactions(ctx, TxFilter{Limit: 2})
	if len(last2) != 2 || last2[0].Date != "2026-10-03" || last2[1].Date != "2026-10-04" {
		t.Errorf("limit should keep the newest two, oldest first: %+v", last2)
	}
}

func TestBalances(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	mustAccount(t, st, "Assets:Cash-USD", "USD")

	if _, err := st.AddTransaction(ctx, NewTransaction{
		Date: day("2026-10-01"),
		Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(250000)}, {Account: "Income:Salary", Amount: eur(-250000)}},
	}); err != nil {
		t.Fatal(err)
	}
	spend(t, st, "2026-10-10", "Expenses:Food", 4000)

	// Cross-currency transfer: 100.00 EUR -> 108.50 USD via the conversion account.
	if _, err := st.AddTransaction(ctx, NewTransaction{
		Date: day("2026-10-12"),
		Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: eur(-10000)},
			{Account: ledger.ConversionAccount, Amount: eur(10000)},
			{Account: ledger.ConversionAccount, Amount: usd(-10850)},
			{Account: "Assets:Cash-USD", Amount: usd(10850)},
		},
	}); err != nil {
		t.Fatal(err)
	}

	get := func(asOf string) map[string]ledger.Money {
		bals, err := st.Balances(ctx, asOf)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]ledger.Money{}
		for _, b := range bals {
			m[b.Account+"/"+string(b.Amount.Currency)] = b.Amount
		}
		return m
	}
	now := get("")
	if now["Assets:Bank/EUR"].Minor != 250000-4000-10000 {
		t.Errorf("bank = %v", now["Assets:Bank/EUR"])
	}
	if now["Assets:Cash-USD/USD"].Minor != 10850 {
		t.Errorf("usd cash = %v", now["Assets:Cash-USD/USD"])
	}
	if _, ok := now["Equity:Conversions/EUR"]; !ok {
		t.Error("conversion account should hold the EUR leg")
	}
	early := get("2026-10-09")
	if early["Assets:Bank/EUR"].Minor != 250000 {
		t.Errorf("as-of balance = %v, want 2500.00", early["Assets:Bank/EUR"])
	}
	if _, ok := early["Assets:Cash-USD/USD"]; ok {
		t.Error("USD cash did not exist yet on 2026-10-09")
	}
}

func TestRatesAndBaseCurrency(t *testing.T) {
	st := newStore(t)
	if c, err := st.BaseCurrency(ctx); err != nil || c != "EUR" {
		t.Fatalf("default base currency = %v, %v", c, err)
	}
	if err := st.SetBaseCurrency(ctx, "USD"); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.BaseCurrency(ctx); c != "USD" {
		t.Errorf("base currency = %v after change", c)
	}

	for _, r := range []struct{ date, base, quote, rate string }{
		{"2026-01-01", "USD", "EUR", "0.90"},
		{"2026-10-01", "USD", "EUR", "0.92"},
		{"2026-10-01", "MXN", "EUR", "0.05"},
	} {
		if err := st.SetRate(ctx, r.date, ledger.Currency(r.base), ledger.Currency(r.quote), r.rate); err != nil {
			t.Fatal(err)
		}
	}
	// Setting the same day again overwrites instead of failing.
	if err := st.SetRate(ctx, "2026-10-01", "USD", "EUR", "0.93"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRate(ctx, "2026-10-01", "EUR", "EUR", "1"); err == nil {
		t.Error("a rate from a currency to itself must be rejected")
	}

	book, err := st.RateBook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := book.Convert(ledger.Money{Minor: 10000, Currency: "USD"}, "EUR", "2026-10-21")
	if !ok || got.Minor != 9300 {
		t.Errorf("USD->EUR = %v, %v (want 93.00 after the overwrite)", got, ok)
	}
	via, ok := book.Convert(ledger.Money{Minor: 1000, Currency: "MXN"}, "USD", "2026-10-21")
	if !ok || via.Currency != "USD" {
		t.Errorf("MXN->USD via EUR = %v, %v", via, ok)
	}
}

func TestMigrationsAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "thaler.db")

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	mustAccount(t, st, "Assets:Bank", "EUR")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// Re-opening applies no migration twice and keeps the data.
	st, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.GetAccount(ctx, "Assets:Bank"); err != nil {
		t.Fatalf("data lost after reopening: %v", err)
	}

	backup := filepath.Join(dir, "backups", "it's a backup.db") // quote in the name on purpose
	if err := st.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err := st.Backup(ctx, backup); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("backup must not overwrite an existing file, got %v", err)
	}
	restored, err := Open(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err := restored.GetAccount(ctx, "Assets:Bank"); err != nil {
		t.Errorf("backup does not contain the data: %v", err)
	}
}

func TestCheckFindsCorruption(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	spend(t, st, "2026-10-01", "Expenses:Food", 1000)

	// Bypass the Go layer to simulate a hand-edited database.
	if _, err := st.db.ExecContext(ctx, `UPDATE postings SET amount = amount + 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	problems, err := st.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "does not balance") {
		t.Errorf("problems = %v", problems)
	}
}

func TestSetCategories(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	mustAccount(t, st, "Assets:Savings", "EUR")
	spend(t, st, "2026-10-01", ledger.UncategorizedExpense, 500)
	spend(t, st, "2026-10-02", ledger.UncategorizedExpense, 700)

	txs, err := st.ListTransactions(ctx, TxFilter{Account: ledger.UncategorizedExpense})
	if err != nil || len(txs) != 2 {
		t.Fatalf("setup: %v, %d transactions", err, len(txs))
	}

	created, err := st.SetCategories(ctx, []CategoryChange{
		{TransactionID: txs[0].ID, Category: "Expenses:Groceries"},
		{TransactionID: txs[1].ID, Category: "Expenses:Groceries"},
	})
	if err != nil || len(created) != 1 || created[0] != "Expenses:Groceries" {
		t.Fatalf("SetCategories = %v, %v", created, err)
	}
	left, _ := st.ListTransactions(ctx, TxFilter{Account: ledger.UncategorizedExpense})
	if len(left) != 0 {
		t.Errorf("%d transactions still uncategorized", len(left))
	}
	if problems, err := st.Check(ctx); err != nil || len(problems) != 0 {
		t.Errorf("Check after recategorizing = %v, %v", problems, err)
	}

	// All or nothing: one bad change rolls the good one back.
	spend(t, st, "2026-10-03", ledger.UncategorizedExpense, 100)
	more, _ := st.ListTransactions(ctx, TxFilter{Account: ledger.UncategorizedExpense})
	_, err = st.SetCategories(ctx, []CategoryChange{
		{TransactionID: more[0].ID, Category: "Expenses:Fun"},
		{TransactionID: more[0].ID, Category: "Assets:Savings"},
	})
	if err == nil {
		t.Fatal("an Assets account must not be accepted as a category")
	}
	after, _ := st.ListTransactions(ctx, TxFilter{Account: ledger.UncategorizedExpense})
	if len(after) != 1 {
		t.Errorf("failed batch was not rolled back: %d uncategorized", len(after))
	}
	if _, err := st.GetAccount(ctx, "Expenses:Fun"); !errors.Is(err, ErrNoAccount) {
		t.Errorf("account created by a rolled-back batch survived: %v", err)
	}
}

func TestSetCategoriesRejectsTransfers(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	mustAccount(t, st, "Assets:Savings", "EUR")
	res, err := st.AddTransaction(ctx, NewTransaction{
		Date:     day("2026-10-01"),
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Assets:Savings", Amount: eur(1000)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetCategories(ctx, []CategoryChange{{TransactionID: res.ID, Category: "Expenses:Fun"}}); err == nil {
		t.Error("a transfer has no category leg and must be rejected")
	}
	if _, err := st.SetCategories(ctx, []CategoryChange{{TransactionID: 999, Category: "Expenses:Fun"}}); err == nil {
		t.Error("an unknown transaction must be rejected")
	}
}

func TestNetWorth(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	mustAccount(t, st, "Assets:Cash-USD", "USD")
	if _, err := st.AddTransaction(ctx, NewTransaction{
		Date:     day("2026-10-01"),
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(100000)}, {Account: "Income:Salary", Amount: eur(-100000)}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTransaction(ctx, NewTransaction{
		Date:     day("2026-10-02"),
		Postings: []ledger.Posting{{Account: "Assets:Cash-USD", Amount: usd(10000)}, {Account: "Income:Gift", Amount: usd(-10000)}},
	}); err != nil {
		t.Fatal(err)
	}

	nw, err := st.NetWorth(ctx, "2026-10-21")
	if err != nil {
		t.Fatal(err)
	}
	if nw.Total != eur(100000) || len(nw.Missing) != 1 || nw.Missing[0] != "USD" {
		t.Errorf("without a rate: %+v", nw)
	}

	if err := st.SetRate(ctx, "2026-10-01", "USD", "EUR", "0.92"); err != nil {
		t.Fatal(err)
	}
	nw, err = st.NetWorth(ctx, "2026-10-21")
	if err != nil {
		t.Fatal(err)
	}
	if nw.Total != eur(109200) || len(nw.Missing) != 0 {
		t.Errorf("with a rate: %+v", nw)
	}
}

func TestCategoryTotals(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	spend(t, st, "2026-09-30", "Expenses:Food", 300)
	spend(t, st, "2026-10-01", "Expenses:Food", 500)
	spend(t, st, "2026-10-15", "Expenses:Food", 250)
	spend(t, st, "2026-10-20", "Expenses:Rent", 100000)
	if _, err := st.AddTransaction(ctx, NewTransaction{
		Date:     day("2026-10-02"),
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(200000)}, {Account: "Income:Salary", Amount: eur(-200000)}},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.CategoryTotals(ctx, "2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"Expenses:Food": 750, "Expenses:Rent": 100000, "Income:Salary": -200000}
	if len(got) != len(want) {
		t.Fatalf("CategoryTotals = %+v", got)
	}
	for _, b := range got {
		if want[b.Account] != b.Amount.Minor {
			t.Errorf("%s = %d, want %d", b.Account, b.Amount.Minor, want[b.Account])
		}
	}
	all, _ := st.CategoryTotals(ctx, "", "")
	for _, b := range all {
		if b.Account == "Expenses:Food" && b.Amount.Minor != 1050 {
			t.Errorf("all-time food = %d, want 1050", b.Amount.Minor)
		}
	}
}

func TestDeleteReplaceRestore(t *testing.T) {
	st := newStore(t)
	mustAccount(t, st, "Assets:Bank", "EUR")
	res, err := st.AddTransaction(ctx, NewTransaction{
		Date: day("2026-10-01"), Payee: "REWE", Description: "weekly", ImportHash: "h1",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1000)}, {Account: "Expenses:Groceries", Amount: eur(1000)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	orig, err := st.GetTransaction(ctx, res.ID)
	if err != nil || orig.ImportHash != "h1" || len(orig.Postings) != 2 {
		t.Fatalf("GetTransaction = %+v, %v", orig, err)
	}

	// Edit in place: id and import hash survive, a new category is created.
	edited := NewTransaction{
		Date: day("2026-10-02"), Payee: "Edeka", Description: "weekly",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-1250)}, {Account: "Expenses:Food", Amount: eur(1250)}},
	}
	created, err := st.ReplaceTransaction(ctx, res.ID, edited)
	if err != nil || len(created) != 1 || created[0] != "Expenses:Food" {
		t.Fatalf("ReplaceTransaction = %v, %v", created, err)
	}
	got, _ := st.GetTransaction(ctx, res.ID)
	if got.Payee != "Edeka" || got.Date != "2026-10-02" || got.ImportHash != "h1" || got.Postings[0].Amount != eur(-1250) {
		t.Errorf("after edit: %+v", got)
	}
	if ok, _ := st.HasImportHash(ctx, "h1"); !ok {
		t.Error("an edited imported row must still be recognised on re-import")
	}

	// Undo the edit by replacing with the original, then delete and restore.
	back, _ := orig.NewTransaction()
	if _, err := st.ReplaceTransaction(ctx, res.ID, back); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteTransaction(ctx, res.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTransaction(ctx, res.ID); err == nil {
		t.Error("deleted transaction still there")
	}
	if err := st.DeleteTransaction(ctx, res.ID); err == nil {
		t.Error("deleting twice must fail")
	}
	if err := st.RestoreTransaction(ctx, res.ID, back); err != nil {
		t.Fatal(err)
	}
	again, _ := st.GetTransaction(ctx, res.ID)
	if again.Payee != "REWE" || again.ImportHash != "h1" || again.Postings[0].Amount != eur(-1000) {
		t.Errorf("after restore: %+v", again)
	}
	if problems, err := st.Check(ctx); err != nil || len(problems) != 0 {
		t.Errorf("Check = %v, %v", problems, err)
	}

	// A failed edit changes nothing.
	bad := edited
	bad.Postings = []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-5)}, {Account: "Expenses:Food", Amount: eur(4)}}
	if _, err := st.ReplaceTransaction(ctx, res.ID, bad); err == nil {
		t.Error("an unbalanced edit must be rejected")
	}
	if kept, _ := st.GetTransaction(ctx, res.ID); kept.Payee != "REWE" {
		t.Errorf("a rejected edit modified the transaction: %+v", kept)
	}
	if _, err := st.ReplaceTransaction(ctx, 999, edited); err == nil {
		t.Error("editing an unknown transaction must fail")
	}
}

func TestMigrateAdoptsLegacyUserVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-schema_migrations state: schema present, version in the pragma.
	if _, err := s.db.ExecContext(ctx, `DROP TABLE schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopening a legacy database: %v", err)
	}
	defer s.Close()
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema_migrations rows = %d, err %v; want 1", n, err)
	}
}
