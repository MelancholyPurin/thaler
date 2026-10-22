// Package store persists the ledger in a single SQLite file.
//
// It uses the pure-Go driver modernc.org/sqlite, so no cgo is needed. The
// connection pool is limited to one connection: thaler is a single-user local
// tool, and one connection avoids "database is locked" surprises. Never run a
// second query while iterating over rows, or the call will block forever.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"github.com/melancholypurin/thaler/internal/ledger"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var (
	// ErrNoAccount means a named account does not exist.
	ErrNoAccount = errors.New("account does not exist")
	// ErrAccountExists means an account with that name is already present.
	ErrAccountExists = errors.New("account already exists")
	// ErrDuplicate means a transaction with the same import hash is already stored.
	ErrDuplicate = errors.New("transaction already imported")
)

// Store is a handle to the database.
type Store struct {
	db *sql.DB
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Open opens (creating if needed) the database at path and applies pending
// migrations. Use ":memory:" for a throwaway database in tests.
func Open(ctx context.Context, path string) (*Store, error) {
	var dsn string
	if path == ":memory:" {
		dsn = "file::memory:?_pragma=foreign_keys(1)"
	} else {
		if strings.ContainsAny(path, "?#") {
			return nil, fmt.Errorf("database path %q must not contain '?' or '#'", path)
		}
		// The directory is private: it holds financial data.
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		dsn = "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// migrate applies migrations/NNNN_name.sql files in order. The schema version
// is kept in SQLite's user_version pragma, and each migration runs in its own
// transaction.
func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	type migration struct {
		version int
		file    string
	}
	var migrations []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		n, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil {
			return fmt.Errorf("bad migration file name %q (want NNNN_name.sql)", e.Name())
		}
		migrations = append(migrations, migration{version: n, file: e.Name()})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })

	if n := len(migrations); n > 0 && current > migrations[n-1].version {
		return fmt.Errorf("database schema version %d is newer than this build understands (%d); update thaler", current, migrations[n-1].version)
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + m.file)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.file, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.file, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s: %w", m.file, err)
		}
	}
	return nil
}

// ---- settings --------------------------------------------------------------

// BaseCurrency returns the currency that reports are converted into.
func (s *Store) BaseCurrency(ctx context.Context) (ledger.Currency, error) {
	var v string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'base_currency'`).Scan(&v); err != nil {
		return "", err
	}
	return ledger.ParseCurrency(v)
}

// SetBaseCurrency changes the reporting currency. Stored amounts are untouched.
func (s *Store) SetBaseCurrency(ctx context.Context, c ledger.Currency) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES ('base_currency', ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`, string(c))
	return err
}

// ---- accounts --------------------------------------------------------------

// Account is a node in the chart of accounts.
type Account struct {
	ID       int64
	Name     string
	Type     ledger.AccountType
	Currency ledger.Currency // empty = any currency
}

// AddAccount creates an account. Assets and Liabilities require a currency.
func (s *Store) AddAccount(ctx context.Context, name string, currency ledger.Currency) (Account, error) {
	if err := ledger.ValidateAccountName(name); err != nil {
		return Account{}, err
	}
	typ, _ := ledger.TypeOf(name)
	if typ.NeedsCurrency() && currency == "" {
		return Account{}, fmt.Errorf("%s accounts need a currency, e.g. --currency EUR", typ)
	}
	acc, err := insertAccount(ctx, s.db, name, typ, currency)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return Account{}, fmt.Errorf("%w: %s", ErrAccountExists, name)
	}
	return acc, err
}

func insertAccount(ctx context.Context, ex execer, name string, typ ledger.AccountType, cur ledger.Currency) (Account, error) {
	var c any // stays nil (SQL NULL) for multi-currency accounts
	if cur != "" {
		c = string(cur)
	}
	res, err := ex.ExecContext(ctx, `INSERT INTO accounts (name, type, currency) VALUES (?, ?, ?)`, name, string(typ), c)
	if err != nil {
		return Account{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Account{}, err
	}
	return Account{ID: id, Name: name, Type: typ, Currency: cur}, nil
}

func getAccount(ctx context.Context, q queryer, name string) (Account, error) {
	var (
		a   Account
		typ string
		cur sql.NullString
	)
	err := q.QueryRowContext(ctx, `SELECT id, name, type, currency FROM accounts WHERE name = ?`, name).
		Scan(&a.ID, &a.Name, &typ, &cur)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, fmt.Errorf("%w: %s", ErrNoAccount, name)
	}
	if err != nil {
		return Account{}, err
	}
	a.Type = ledger.AccountType(typ)
	a.Currency = ledger.Currency(cur.String)
	return a, nil
}

// GetAccount looks an account up by its full name.
func (s *Store) GetAccount(ctx context.Context, name string) (Account, error) {
	return getAccount(ctx, s.db, name)
}

// ListAccounts returns all accounts ordered by name.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, type, currency FROM accounts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var (
			a   Account
			typ string
			cur sql.NullString
		)
		if err := rows.Scan(&a.ID, &a.Name, &typ, &cur); err != nil {
			return nil, err
		}
		a.Type = ledger.AccountType(typ)
		a.Currency = ledger.Currency(cur.String)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- transactions ----------------------------------------------------------

// NewTransaction is the input for adding a transaction. Postings reference
// accounts by name. Income, Expenses and Equity accounts that do not exist yet
// are created on the fly; Assets and Liabilities must already exist.
type NewTransaction struct {
	Date        time.Time
	Payee       string
	Description string
	ImportHash  string // optional; a second transaction with the same hash is rejected
	Postings    []ledger.Posting
}

// AddResult describes a stored transaction.
type AddResult struct {
	ID      int64
	Created []string // names of accounts that were created implicitly
}

// BatchResult summarises AddTransactions.
type BatchResult struct {
	Inserted   int
	Duplicates int
	Created    []string
}

// AddTransaction validates and stores one transaction atomically.
func (s *Store) AddTransaction(ctx context.Context, nt NewTransaction) (AddResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AddResult{}, err
	}
	defer tx.Rollback()
	var created []string
	id, err := addTransaction(ctx, tx, nt, &created)
	if err != nil {
		return AddResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AddResult{}, err
	}
	return AddResult{ID: id, Created: created}, nil
}

// AddTransactions stores many transactions in one database transaction:
// either everything is committed or nothing is. Transactions whose import
// hash is already present are counted as duplicates and skipped.
func (s *Store) AddTransactions(ctx context.Context, txs []NewTransaction) (BatchResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BatchResult{}, err
	}
	defer tx.Rollback()
	var res BatchResult
	for i, nt := range txs {
		_, err := addTransaction(ctx, tx, nt, &res.Created)
		if errors.Is(err, ErrDuplicate) {
			res.Duplicates++
			continue
		}
		if err != nil {
			return BatchResult{}, fmt.Errorf("transaction %d (%s %s): %w", i+1, nt.Date.Format(ledger.DateLayout), nt.Payee, err)
		}
		res.Inserted++
	}
	if err := tx.Commit(); err != nil {
		return BatchResult{}, err
	}
	return res, nil
}

// HasImportHash reports whether an imported transaction with this hash exists.
func (s *Store) HasImportHash(ctx context.Context, hash string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM transactions WHERE import_hash = ?`, hash).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func addTransaction(ctx context.Context, tx *sql.Tx, nt NewTransaction, created *[]string) (int64, error) {
	return insertTransaction(ctx, tx, nt, 0, created)
}

// checkNew validates nt and resolves its postings to account ids, creating
// category accounts that do not exist yet.
func checkNew(ctx context.Context, tx *sql.Tx, nt NewTransaction, created *[]string) ([]int64, error) {
	if nt.Date.IsZero() {
		return nil, errors.New("transaction has no date")
	}
	if err := ledger.CheckBalanced(nt.Postings); err != nil {
		return nil, err
	}
	accountIDs := make([]int64, len(nt.Postings))
	for i, p := range nt.Postings {
		acc, err := resolveAccount(ctx, tx, p.Account, p.Amount.Currency, created)
		if err != nil {
			return nil, err
		}
		accountIDs[i] = acc.ID
	}
	return accountIDs, nil
}

func insertPostings(ctx context.Context, tx *sql.Tx, id int64, nt NewTransaction, accountIDs []int64) error {
	for i, p := range nt.Postings {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO postings (transaction_id, account_id, amount, currency) VALUES (?, ?, ?, ?)`,
			id, accountIDs[i], p.Amount.Minor, string(p.Amount.Currency)); err != nil {
			return err
		}
	}
	return nil
}

// insertTransaction stores nt. A non-zero id is used as the row id (to bring a
// deleted transaction back under its old number); zero lets SQLite choose.
func insertTransaction(ctx context.Context, tx *sql.Tx, nt NewTransaction, id int64, created *[]string) (int64, error) {
	if nt.Date.IsZero() {
		return 0, errors.New("transaction has no date")
	}
	if err := ledger.CheckBalanced(nt.Postings); err != nil {
		return 0, err
	}
	if nt.ImportHash != "" {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM transactions WHERE import_hash = ?`, nt.ImportHash).Scan(&one)
		if err == nil {
			return 0, ErrDuplicate
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	accountIDs, err := checkNew(ctx, tx, nt, created)
	if err != nil {
		return 0, err
	}

	var hash any // NULL for manual entries
	if nt.ImportHash != "" {
		hash = nt.ImportHash
	}
	var rowID any // NULL lets SQLite assign the next id
	if id != 0 {
		rowID = id
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO transactions (id, date, payee, description, import_hash) VALUES (?, ?, ?, ?, ?)`,
		rowID, nt.Date.Format(ledger.DateLayout), nt.Payee, nt.Description, hash)
	if err != nil {
		return 0, err
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return newID, insertPostings(ctx, tx, newID, nt, accountIDs)
}

// DeleteTransaction removes a transaction and its postings.
func (s *Store) DeleteTransaction(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM transactions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("transaction #%d does not exist", id)
	}
	return nil
}

// ReplaceTransaction rewrites the date, payee, description and postings of an
// existing transaction in place. Its id and import hash are kept, so a bank
// row that was corrected here is still recognised as imported. Categories that
// do not exist yet are created, and their names are returned.
func (s *Store) ReplaceTransaction(ctx context.Context, id int64, nt NewTransaction) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM transactions WHERE id = ?`, id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("transaction #%d does not exist", id)
	} else if err != nil {
		return nil, err
	}
	var created []string
	accountIDs, err := checkNew(ctx, tx, nt, &created)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE transactions SET date = ?, payee = ?, description = ? WHERE id = ?`,
		nt.Date.Format(ledger.DateLayout), nt.Payee, nt.Description, id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM postings WHERE transaction_id = ?`, id); err != nil {
		return nil, err
	}
	if err := insertPostings(ctx, tx, id, nt, accountIDs); err != nil {
		return nil, err
	}
	return created, tx.Commit()
}

// RestoreTransaction stores nt under the given id, which must be free. It
// brings back a transaction deleted by mistake, with its old number.
func (s *Store) RestoreTransaction(ctx context.Context, id int64, nt NewTransaction) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var created []string
	if _, err := insertTransaction(ctx, tx, nt, id, &created); err != nil {
		return err
	}
	return tx.Commit()
}

// GetTransaction returns one transaction with its postings.
func (s *Store) GetTransaction(ctx context.Context, id int64) (TxView, error) {
	txs, err := s.ListTransactions(ctx, TxFilter{ID: id})
	if err != nil {
		return TxView{}, err
	}
	if len(txs) == 0 {
		return TxView{}, fmt.Errorf("transaction #%d does not exist", id)
	}
	return txs[0], nil
}

// NewTransaction converts a stored transaction back into input for the store,
// for undoing a delete or an edit.
func (t TxView) NewTransaction() (NewTransaction, error) {
	d, err := ledger.ParseDate(t.Date)
	if err != nil {
		return NewTransaction{}, err
	}
	nt := NewTransaction{Date: d, Payee: t.Payee, Description: t.Description, ImportHash: t.ImportHash}
	for _, p := range t.Postings {
		nt.Postings = append(nt.Postings, ledger.Posting{Account: p.Account, Amount: p.Amount})
	}
	return nt, nil
}

// resolveAccount returns the account a posting in currency cur is booked to.
// Income, Expenses and Equity accounts that do not exist yet are created and
// appended to created; Assets and Liabilities must already exist. It fails if
// the account is bound to a different currency.
func resolveAccount(ctx context.Context, tx *sql.Tx, name string, cur ledger.Currency, created *[]string) (Account, error) {
	acc, err := getAccount(ctx, tx, name)
	if errors.Is(err, ErrNoAccount) {
		typ, terr := ledger.TypeOf(name)
		if terr != nil {
			return Account{}, terr
		}
		if verr := ledger.ValidateAccountName(name); verr != nil {
			return Account{}, verr
		}
		if typ.NeedsCurrency() {
			return Account{}, fmt.Errorf("account %s does not exist; create it first with: thaler cli account add %s --currency %s",
				name, name, cur)
		}
		acc, err = insertAccount(ctx, tx, name, typ, "")
		if err == nil {
			*created = append(*created, name)
		}
	}
	if err != nil {
		return Account{}, err
	}
	if acc.Currency != "" && acc.Currency != cur {
		return Account{}, fmt.Errorf("account %s holds %s, but the posting is in %s", acc.Name, acc.Currency, cur)
	}
	return acc, nil
}

// CategoryChange moves the category leg of one transaction to another account.
type CategoryChange struct {
	TransactionID int64
	Category      string
}

// SetCategories re-books the category leg of each listed transaction in one
// database transaction: either every change is applied or none is. A
// transaction's category leg is its only posting to an Income, Expenses or
// Equity account; transactions without exactly one such posting (transfers
// between your own accounts, say) are rejected. Categories that do not exist
// yet are created, and their names are returned.
func (s *Store) SetCategories(ctx context.Context, changes []CategoryChange) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var created []string
	for _, c := range changes {
		if err := setCategory(ctx, tx, c, &created); err != nil {
			return nil, fmt.Errorf("transaction #%d: %w", c.TransactionID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func setCategory(ctx context.Context, tx *sql.Tx, c CategoryChange, created *[]string) error {
	if typ, err := ledger.TypeOf(c.Category); err != nil {
		return err
	} else if typ.NeedsCurrency() {
		return fmt.Errorf("%s is not a category; use an Income, Expenses or Equity account", c.Category)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, p.currency
		FROM postings p JOIN accounts a ON a.id = p.account_id
		WHERE p.transaction_id = ? AND a.type IN ('Income', 'Expenses', 'Equity')`, c.TransactionID)
	if err != nil {
		return err
	}
	var (
		postingIDs []int64
		currency   string
	)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id, &currency); err != nil {
			rows.Close()
			return err
		}
		postingIDs = append(postingIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(postingIDs) != 1 {
		return fmt.Errorf("has %d category postings; only transactions with exactly one can be recategorized", len(postingIDs))
	}
	acc, err := resolveAccount(ctx, tx, c.Category, ledger.Currency(currency), created)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE postings SET account_id = ? WHERE id = ?`, acc.ID, postingIDs[0])
	return err
}

// TxFilter narrows ListTransactions. Empty fields mean "no restriction".
type TxFilter struct {
	ID      int64  // a single transaction; 0 = no restriction
	Account string // matches the account itself and everything below it
	From    string // YYYY-MM-DD, inclusive
	To      string // YYYY-MM-DD, inclusive
	Limit   int    // newest N transactions; <= 0 means all
}

// PostingView is one leg of a stored transaction.
type PostingView struct {
	Account string
	Amount  ledger.Money
}

// TxView is a stored transaction with its postings.
type TxView struct {
	ID          int64
	Date        string
	Payee       string
	Description string
	ImportHash  string // empty for manual entries
	Postings    []PostingView
}

// Legs splits a simple two-posting transaction into the leg on your own account
// (source) and the leg on its category. ok is false for transfers, splits and
// anything else that is not exactly one category posting plus one other.
func (t TxView) Legs() (source, category PostingView, ok bool) {
	if len(t.Postings) != 2 {
		return PostingView{}, PostingView{}, false
	}
	for i, p := range t.Postings {
		typ, err := ledger.TypeOf(p.Account)
		if err != nil || typ.NeedsCurrency() {
			continue
		}
		other := t.Postings[1-i]
		if otherTyp, err := ledger.TypeOf(other.Account); err != nil || !otherTyp.NeedsCurrency() {
			return PostingView{}, PostingView{}, false
		}
		return other, p, true
	}
	return PostingView{}, PostingView{}, false
}

// ListTransactions returns the matching transactions ordered oldest to newest,
// so that the most recent one is printed last. With a Limit, the newest
// Limit transactions are selected first and then ordered.
func (s *Store) ListTransactions(ctx context.Context, f TxFilter) ([]TxView, error) {
	var where []string
	var args []any
	if f.ID != 0 {
		where = append(where, "t2.id = ?")
		args = append(args, f.ID)
	}
	if f.From != "" {
		where = append(where, "t2.date >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, "t2.date <= ?")
		args = append(args, f.To)
	}
	if f.Account != "" {
		where = append(where, `EXISTS (
			SELECT 1 FROM postings p2 JOIN accounts a2 ON a2.id = p2.account_id
			WHERE p2.transaction_id = t2.id AND (a2.name = ? OR a2.name LIKE ? ESCAPE '\'))`)
		args = append(args, f.Account, escapeLike(f.Account)+":%")
	}
	cond := "1 = 1"
	if len(where) > 0 {
		cond = strings.Join(where, " AND ")
	}
	limit := -1 // SQLite: negative LIMIT means no limit
	if f.Limit > 0 {
		limit = f.Limit
	}
	args = append(args, limit)

	q := `SELECT t.id, t.date, t.payee, t.description, COALESCE(t.import_hash, ''), a.name, p.amount, p.currency
	      FROM transactions t
	      JOIN postings p ON p.transaction_id = t.id
	      JOIN accounts a ON a.id = p.account_id
	      WHERE t.id IN (SELECT t2.id FROM transactions t2 WHERE ` + cond + `
	                     ORDER BY t2.date DESC, t2.id DESC LIMIT ?)
	      ORDER BY t.date DESC, t.id DESC, p.id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TxView
	for rows.Next() {
		var (
			id                                 int64
			date, payee, desc, hash, acct, cur string
			amount                             int64
		)
		if err := rows.Scan(&id, &date, &payee, &desc, &hash, &acct, &amount, &cur); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, TxView{ID: id, Date: date, Payee: payee, Description: desc, ImportHash: hash})
		}
		last := &out[len(out)-1]
		last.Postings = append(last.Postings, PostingView{
			Account: acct,
			Amount:  ledger.Money{Minor: amount, Currency: ledger.Currency(cur)},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ---- balances --------------------------------------------------------------

// Balance is the sum of all postings of one account in one currency.
type Balance struct {
	Account string
	Type    ledger.AccountType
	Amount  ledger.Money
}

// Balances returns non-zero balances per account and currency. With a non-empty
// asOf (YYYY-MM-DD) only transactions up to and including that day count.
// Postings keep the sign convention of the ledger: assets are positive,
// liabilities, income and equity are negative when they hold a credit balance.
func (s *Store) Balances(ctx context.Context, asOf string) ([]Balance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.name, a.type, p.currency, SUM(p.amount)
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		JOIN transactions t ON t.id = p.transaction_id
		WHERE (? = '' OR t.date <= ?)
		GROUP BY a.id, p.currency
		HAVING SUM(p.amount) <> 0
		ORDER BY a.name, p.currency`, asOf, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Balance
	for rows.Next() {
		var (
			name, typ, cur string
			sum            int64
		)
		if err := rows.Scan(&name, &typ, &cur, &sum); err != nil {
			return nil, err
		}
		out = append(out, Balance{
			Account: name,
			Type:    ledger.AccountType(typ),
			Amount:  ledger.Money{Minor: sum, Currency: ledger.Currency(cur)},
		})
	}
	return out, rows.Err()
}

// ---- exchange rates --------------------------------------------------------

// SetRate records that on date one unit of base is worth rate units of quote.
// rate must be a normalised decimal string from ledger.ParseRate.
func (s *Store) SetRate(ctx context.Context, date string, base, quote ledger.Currency, rate string) error {
	if base == quote {
		return errors.New("base and quote currency must differ")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rates (date, base, quote, rate) VALUES (?, ?, ?, ?)
		 ON CONFLICT (date, base, quote) DO UPDATE SET rate = excluded.rate`,
		date, string(base), string(quote), rate)
	return err
}

// Rates returns every stored exchange rate, oldest first.
func (s *Store) Rates(ctx context.Context) ([]ledger.RateEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT date, base, quote, rate FROM rates ORDER BY date, base, quote`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.RateEntry
	for rows.Next() {
		var date, base, quote, rate string
		if err := rows.Scan(&date, &base, &quote, &rate); err != nil {
			return nil, err
		}
		r, ok := new(big.Rat).SetString(rate)
		if !ok {
			return nil, fmt.Errorf("corrupt exchange rate %q for %s/%s on %s", rate, base, quote, date)
		}
		out = append(out, ledger.RateEntry{Date: date, Base: ledger.Currency(base), Quote: ledger.Currency(quote), Rate: r})
	}
	return out, rows.Err()
}

// RateBook loads all exchange rates into a lookup structure.
func (s *Store) RateBook(ctx context.Context) (*ledger.RateBook, error) {
	entries, err := s.Rates(ctx)
	if err != nil {
		return nil, err
	}
	return ledger.NewRateBook(entries), nil
}

// ---- maintenance -----------------------------------------------------------

// Check verifies the database invariants and returns a description of every
// problem found (an empty slice means the ledger is consistent).
func (s *Store) Check(ctx context.Context) ([]string, error) {
	var problems []string

	var integrity string
	if err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return nil, err
	}
	if integrity != "ok" {
		problems = append(problems, "sqlite integrity check: "+integrity)
	}

	unbalanced, err := s.checkUnbalanced(ctx)
	if err != nil {
		return nil, err
	}
	problems = append(problems, unbalanced...)

	mismatched, err := s.checkCurrencies(ctx)
	if err != nil {
		return nil, err
	}
	return append(problems, mismatched...), nil
}

func (s *Store) checkUnbalanced(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT transaction_id, currency, SUM(amount)
		FROM postings
		GROUP BY transaction_id, currency
		HAVING SUM(amount) <> 0
		ORDER BY transaction_id, currency`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			id  int64
			cur string
			sum int64
		)
		if err := rows.Scan(&id, &cur, &sum); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("transaction #%d does not balance: %s is off by %s",
			id, cur, ledger.Money{Minor: sum, Currency: ledger.Currency(cur)}.Decimal()))
	}
	return out, rows.Err()
}

func (s *Store) checkCurrencies(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.transaction_id, a.name, a.currency, p.currency
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		WHERE a.currency IS NOT NULL AND a.currency <> p.currency
		ORDER BY p.transaction_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			id                      int64
			account, accCur, posCur string
		)
		if err := rows.Scan(&id, &account, &accCur, &posCur); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("transaction #%d posts %s to %s, which holds %s", id, posCur, account, accCur))
	}
	return out, rows.Err()
}

// Backup writes a consistent snapshot of the database to dest using
// VACUUM INTO. dest must not exist yet. The snapshot is NOT encrypted.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	literal := "'" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO "+literal); err != nil {
		return fmt.Errorf("backup to %s: %w", dest, err)
	}
	return nil
}
