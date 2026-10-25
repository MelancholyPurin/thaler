package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"github.com/melancholypurin/thaler/internal/ledger"
)

// The repository is Local's SQL and nothing else: it runs the statements the
// service asks for and decides nothing. Rules (is this balanced? may this
// category be created? does the currency fit?) live in service.go.
//
// It uses the pure-Go driver modernc.org/sqlite, so no cgo is needed. The
// connection pool is limited to one connection: thaler is a small tool, and one
// connection avoids "database is locked" surprises. Never run a second query
// while iterating over rows, or the call will block forever.

// nowSQL is the timestamp format of every created_at and updated_at column.
const nowSQL = `strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`

type repository struct {
	db *sql.DB
}

// dbtx is what *sql.DB and *sql.Tx have in common.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// queries are the statements, run either on the connection pool or inside one
// open transaction.
type queries struct {
	db dbtx
}

// openRepository opens (creating if needed) the database at path and applies
// pending migrations. Use ":memory:" for a throwaway database in tests.
func openRepository(ctx context.Context, path string) (*repository, error) {
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
	r := &repository{db: db}
	if err := r.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

func (r *repository) close() error { return r.db.Close() }

// queries returns statements that run directly on the pool, for reads.
func (r *repository) queries() queries { return queries{r.db} }

// inTx runs fn in one database transaction: it commits if fn returns nil and
// rolls everything back otherwise.
func (r *repository) inTx(ctx context.Context, fn func(q queries) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(queries{tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- settings --------------------------------------------------------------

func (q queries) setting(ctx context.Context, key string) (string, error) {
	var v string
	err := q.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v, err
}

func (q queries) putSetting(ctx context.Context, key, value string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---- accounts --------------------------------------------------------------

func (q queries) insertAccount(ctx context.Context, name string, typ ledger.AccountType, cur ledger.Currency) (Account, error) {
	var c any // stays nil (SQL NULL) for multi-currency accounts
	if cur != "" {
		c = string(cur)
	}
	res, err := q.db.ExecContext(ctx, `INSERT INTO accounts (uuid, name, type, currency) VALUES (?, ?, ?, ?)`,
		uuid.NewV4().String(), name, string(typ), c)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Account{}, fmt.Errorf("%w: %s", ErrAccountExists, name)
		}
		return Account{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Account{}, err
	}
	return Account{ID: id, Name: name, Type: typ, Currency: cur}, nil
}

func (q queries) account(ctx context.Context, name string) (Account, error) {
	var (
		a   Account
		typ string
		cur sql.NullString
	)
	err := q.db.QueryRowContext(ctx, `SELECT id, name, type, currency FROM accounts WHERE name = ?`, name).
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

func (q queries) accounts(ctx context.Context) ([]Account, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT id, name, type, currency FROM accounts ORDER BY name`)
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

func (q queries) transactionExists(ctx context.Context, id int64) (bool, error) {
	return q.exists(ctx, `SELECT 1 FROM transactions WHERE id = ?`, id)
}

func (q queries) importHashExists(ctx context.Context, hash string) (bool, error) {
	return q.exists(ctx, `SELECT 1 FROM transactions WHERE import_hash = ?`, hash)
}

func (q queries) exists(ctx context.Context, query string, arg any) (bool, error) {
	var one int
	err := q.db.QueryRowContext(ctx, query, arg).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// insertTransaction stores the header row of nt. A non-zero id is used as the
// row id (to bring a deleted transaction back under its old number); zero lets
// SQLite choose. It returns the id.
func (q queries) insertTransaction(ctx context.Context, id int64, nt NewTransaction) (int64, error) {
	var hash any // NULL for manual entries
	if nt.ImportHash != "" {
		hash = nt.ImportHash
	}
	var rowID any // NULL lets SQLite assign the next id
	if id != 0 {
		rowID = id
	}
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO transactions (id, uuid, date, payee, description, import_hash) VALUES (?, ?, ?, ?, ?, ?)`,
		rowID, uuid.NewV4().String(), nt.Date.Format(ledger.DateLayout), nt.Payee, nt.Description, hash)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// updateTransaction rewrites date, payee and description; the import hash and
// uuid stay. It counts as an edit, so version goes up.
func (q queries) updateTransaction(ctx context.Context, id int64, nt NewTransaction) error {
	_, err := q.db.ExecContext(ctx, `UPDATE transactions
		SET date = ?, payee = ?, description = ?, version = version + 1, updated_at = `+nowSQL+`
		WHERE id = ?`,
		nt.Date.Format(ledger.DateLayout), nt.Payee, nt.Description, id)
	return err
}

// deleteTransaction removes a transaction and (by cascade) its postings. It
// reports whether the transaction existed.
func (q queries) deleteTransaction(ctx context.Context, id int64) (bool, error) {
	res, err := q.db.ExecContext(ctx, `DELETE FROM transactions WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (q queries) insertPostings(ctx context.Context, txID int64, postings []ledger.Posting, accountIDs []int64) error {
	for i, p := range postings {
		if _, err := q.db.ExecContext(ctx,
			`INSERT INTO postings (transaction_id, account_id, amount, currency) VALUES (?, ?, ?, ?)`,
			txID, accountIDs[i], p.Amount.Minor, string(p.Amount.Currency)); err != nil {
			return err
		}
	}
	return nil
}

func (q queries) deletePostings(ctx context.Context, txID int64) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM postings WHERE transaction_id = ?`, txID)
	return err
}

// categoryPosting is a posting on an Income, Expenses or Equity account.
type categoryPosting struct {
	id       int64
	currency ledger.Currency
}

func (q queries) categoryPostings(ctx context.Context, txID int64) ([]categoryPosting, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT p.id, p.currency
		FROM postings p JOIN accounts a ON a.id = p.account_id
		WHERE p.transaction_id = ? AND a.type IN ('Income', 'Expenses', 'Equity')`, txID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []categoryPosting
	for rows.Next() {
		var (
			cp  categoryPosting
			cur string
		)
		if err := rows.Scan(&cp.id, &cur); err != nil {
			return nil, err
		}
		cp.currency = ledger.Currency(cur)
		out = append(out, cp)
	}
	return out, rows.Err()
}

// moveStoredPosting books a posting on another account. That edits the
// transaction it belongs to, so its version goes up.
func (q queries) moveStoredPosting(ctx context.Context, postingID, accountID int64) error {
	if _, err := q.db.ExecContext(ctx, `UPDATE transactions
		SET version = version + 1, updated_at = `+nowSQL+`
		WHERE id = (SELECT transaction_id FROM postings WHERE id = ?)`, postingID); err != nil {
		return err
	}
	_, err := q.db.ExecContext(ctx, `UPDATE postings SET account_id = ? WHERE id = ?`, accountID, postingID)
	return err
}

// listTransactions returns the matching transactions ordered oldest to newest.
// With a Limit, the newest Limit transactions are selected first and then
// ordered.
func (q queries) listTransactions(ctx context.Context, f TxFilter) ([]TxView, error) {
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

	query := `SELECT t.id, t.date, t.payee, t.description, COALESCE(t.import_hash, ''), COALESCE(t.transfer_with, 0), a.name, p.amount, p.currency
	      FROM transactions t
	      JOIN postings p ON p.transaction_id = t.id
	      JOIN accounts a ON a.id = p.account_id
	      WHERE t.id IN (SELECT t2.id FROM transactions t2 WHERE ` + cond + `
	                     ORDER BY t2.date DESC, t2.id DESC LIMIT ?)
	      ORDER BY t.date DESC, t.id DESC, p.id`
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TxView
	for rows.Next() {
		var (
			id, transferWith                   int64
			date, payee, desc, hash, acct, cur string
			amount                             int64
		)
		if err := rows.Scan(&id, &date, &payee, &desc, &hash, &transferWith, &acct, &amount, &cur); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, TxView{ID: id, Date: date, Payee: payee, Description: desc, ImportHash: hash, TransferWith: transferWith})
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

// balances returns non-zero balances per account and currency. With a non-empty
// asOf (YYYY-MM-DD) only transactions up to and including that day count.
func (q queries) balances(ctx context.Context, asOf string) ([]Balance, error) {
	return q.sums(ctx, `
		SELECT a.name, a.type, p.currency, SUM(p.amount)
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		JOIN transactions t ON t.id = p.transaction_id
		WHERE (? = '' OR t.date <= ?)
		GROUP BY a.id, p.currency
		HAVING SUM(p.amount) <> 0
		ORDER BY a.name, p.currency`, asOf, asOf)
}

// categoryTotals sums the postings on Income and Expenses accounts per account
// and currency, for transactions dated from..to (inclusive; an empty bound is
// open).
func (q queries) categoryTotals(ctx context.Context, from, to string) ([]Balance, error) {
	return q.sums(ctx, `
		SELECT a.name, a.type, p.currency, SUM(p.amount)
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		JOIN transactions t ON t.id = p.transaction_id
		WHERE a.type IN ('Income', 'Expenses')
		  AND (? = '' OR t.date >= ?)
		  AND (? = '' OR t.date <= ?)
		GROUP BY a.id, p.currency
		HAVING SUM(p.amount) <> 0
		ORDER BY a.name, p.currency`, from, from, to, to)
}

func (q queries) sums(ctx context.Context, query string, args ...any) ([]Balance, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
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

func (q queries) putRate(ctx context.Context, date string, base, quote ledger.Currency, rate string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO rates (date, base, quote, rate) VALUES (?, ?, ?, ?)
		 ON CONFLICT (date, base, quote) DO UPDATE SET rate = excluded.rate`,
		date, string(base), string(quote), rate)
	return err
}

func (q queries) rates(ctx context.Context) ([]ledger.RateEntry, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT date, base, quote, rate FROM rates ORDER BY date, base, quote`)
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

// ---- transfers between your own accounts -----------------------------------

// transferCandidate is a transaction that could be the other half of a transfer.
type transferCandidate struct {
	id       int64
	date     string
	account  string // the own account the money moved on
	category string // where its other leg is booked now
}

// transferCandidates finds the unpaired two-posting transactions on other own
// accounts that moved exactly the opposite of amount within days of date, and
// whose other leg is still uncategorized or already waiting in
// Equity:Transfers. The closest in time comes first.
func (q queries) transferCandidates(ctx context.Context, accountID int64, amount ledger.Money, date time.Time, days int) ([]transferCandidate, error) {
	from := date.AddDate(0, 0, -days).Format(ledger.DateLayout)
	to := date.AddDate(0, 0, days).Format(ledger.DateLayout)
	rows, err := q.db.QueryContext(ctx, `
		SELECT t.id, t.date, own.name, cat.name
		FROM transactions t
		JOIN postings pm ON pm.transaction_id = t.id
		JOIN accounts own ON own.id = pm.account_id AND own.type IN ('Assets', 'Liabilities') AND own.id <> ?
		JOIN postings pc ON pc.transaction_id = t.id AND pc.id <> pm.id
		JOIN accounts cat ON cat.id = pc.account_id AND cat.name IN (?, ?, ?)
		WHERE t.transfer_with IS NULL
		  AND pm.amount = ? AND pm.currency = ?
		  AND t.date BETWEEN ? AND ?
		  AND (SELECT COUNT(*) FROM postings WHERE transaction_id = t.id) = 2
		ORDER BY ABS(julianday(t.date) - julianday(?)), t.id`,
		accountID, ledger.UncategorizedExpense, ledger.UncategorizedIncome, ledger.TransferAccount,
		-amount.Minor, string(amount.Currency), from, to, date.Format(ledger.DateLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []transferCandidate
	for rows.Next() {
		var c transferCandidate
		if err := rows.Scan(&c.id, &c.date, &c.account, &c.category); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// adoptionCandidate is a hand-entered transaction that an imported bank row
// could be the other record of.
type adoptionCandidate struct {
	id       int64
	date     string
	payee    string
	category string // where its other leg is booked
}

// adoptionCandidates finds the transactions entered by hand (no import hash)
// on an account that moved exactly amount within days of date: plain entries of
// two postings, an account and a category. The category must not be another of
// your own accounts (a transfer that spans two accounts is not one bank row)
// nor the opening balance. The closest in time comes first.
func (q queries) adoptionCandidates(ctx context.Context, accountID int64, amount ledger.Money, date time.Time, days int) ([]adoptionCandidate, error) {
	from := date.AddDate(0, 0, -days).Format(ledger.DateLayout)
	to := date.AddDate(0, 0, days).Format(ledger.DateLayout)
	rows, err := q.db.QueryContext(ctx, `
		SELECT t.id, t.date, t.payee, cat.name
		FROM transactions t
		JOIN postings pm ON pm.transaction_id = t.id AND pm.account_id = ? AND pm.amount = ? AND pm.currency = ?
		JOIN postings pc ON pc.transaction_id = t.id AND pc.id <> pm.id
		JOIN accounts cat ON cat.id = pc.account_id AND cat.type NOT IN ('Assets', 'Liabilities') AND cat.name <> ?
		WHERE t.import_hash IS NULL
		  AND t.date BETWEEN ? AND ?
		  AND (SELECT COUNT(*) FROM postings WHERE transaction_id = t.id) = 2
		ORDER BY ABS(julianday(t.date) - julianday(?)), t.id`,
		accountID, amount.Minor, string(amount.Currency), ledger.OpeningBalanceAccount, from, to, date.Format(ledger.DateLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adoptionCandidate
	for rows.Next() {
		var c adoptionCandidate
		if err := rows.Scan(&c.id, &c.date, &c.payee, &c.category); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// setImportHash gives a hand-entered transaction the import hash of the bank
// row it turned out to be, so importing that row again finds it. It fails if
// the transaction already has one.
func (q queries) setImportHash(ctx context.Context, id int64, hash string) error {
	res, err := q.db.ExecContext(ctx, `
		UPDATE transactions SET import_hash = ?, version = version + 1, updated_at = `+nowSQL+`
		WHERE id = ? AND import_hash IS NULL`, hash, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("transaction #%d does not exist or is already an imported one", id)
	}
	return nil
}

// clearImportHash makes a transaction a hand-entered one again.
func (q queries) clearImportHash(ctx context.Context, id int64) error {
	_, err := q.db.ExecContext(ctx, `
		UPDATE transactions SET import_hash = NULL, version = version + 1, updated_at = `+nowSQL+`
		WHERE id = ?`, id)
	return err
}

// linkTransfer makes a and b the two halves of one transfer.
func (q queries) linkTransfer(ctx context.Context, a, b int64) error {
	_, err := q.db.ExecContext(ctx, `
		UPDATE transactions
		SET transfer_with = CASE id WHEN ? THEN ? ELSE ? END, version = version + 1, updated_at = `+nowSQL+`
		WHERE id IN (?, ?)`, a, b, a, a, b)
	return err
}

// unlinkTransfer clears the link between a and b, whichever way round.
func (q queries) unlinkTransfer(ctx context.Context, a, b int64) error {
	_, err := q.db.ExecContext(ctx, `
		UPDATE transactions SET transfer_with = NULL, version = version + 1, updated_at = `+nowSQL+`
		WHERE id IN (?, ?)`, a, b)
	return err
}

// transferPartner is the other half of id's transfer, or 0.
func (q queries) transferPartner(ctx context.Context, id int64) (int64, error) {
	var partner int64
	err := q.db.QueryRowContext(ctx, `SELECT COALESCE(transfer_with, 0) FROM transactions WHERE id = ?`, id).Scan(&partner)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return partner, err
}

// brokenTransferLinks describes every link that is not mutual.
func (q queries) brokenTransferLinks(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT a.id, a.transfer_with
		FROM transactions a LEFT JOIN transactions b ON b.id = a.transfer_with
		WHERE a.transfer_with IS NOT NULL AND (b.id IS NULL OR b.transfer_with IS NOT a.id)
		ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a, b int64
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("transaction #%d is linked to #%d as a transfer, but #%d does not link back", a, b, b))
	}
	return out, rows.Err()
}

// ---- opening balances ------------------------------------------------------

// openingTransaction is the id of the transaction that books an account's
// opening balance: exactly two postings, one on the account and one on
// Equity:Opening. It is 0 when there is none.
func (q queries) openingTransaction(ctx context.Context, accountID int64) (int64, error) {
	var id int64
	err := q.db.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(t.id), 0)
		FROM transactions t
		JOIN postings mine ON mine.transaction_id = t.id AND mine.account_id = ?
		JOIN postings other ON other.transaction_id = t.id
		JOIN accounts eq ON eq.id = other.account_id AND eq.name = ?
		WHERE (SELECT COUNT(*) FROM postings WHERE transaction_id = t.id) = 2`,
		accountID, ledger.OpeningBalanceAccount).Scan(&id)
	return id, err
}

// accountSum is what the postings on an account add up to, up to and including
// the day asOf, leaving out the transaction skip (0 = leave out none).
func (q queries) accountSum(ctx context.Context, accountID int64, asOf string, skip int64) (int64, error) {
	var sum int64
	err := q.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(p.amount), 0)
		FROM postings p JOIN transactions t ON t.id = p.transaction_id
		WHERE p.account_id = ? AND t.date <= ? AND t.id <> ?`, accountID, asOf, skip).Scan(&sum)
	return sum, err
}

// firstPostingDate is the day of the oldest transaction on an account, leaving
// out the transaction skip; empty when there is none.
func (q queries) firstPostingDate(ctx context.Context, accountID, skip int64) (string, error) {
	var day string
	err := q.db.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(t.date), '')
		FROM postings p JOIN transactions t ON t.id = p.transaction_id
		WHERE p.account_id = ? AND t.id <> ?`, accountID, skip).Scan(&day)
	return day, err
}

// ---- maintenance -----------------------------------------------------------

func (q queries) integrityCheck(ctx context.Context) (string, error) {
	var result string
	err := q.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result)
	return result, err
}

// unbalanced describes every transaction whose postings do not sum to zero in
// some currency.
func (q queries) unbalanced(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `
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

// currencyMismatches describes every posting whose currency differs from the
// currency its account is bound to.
func (q queries) currencyMismatches(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `
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

// missingIdentity describes every account and transaction without a uuid. The
// column cannot be NOT NULL (SQLite cannot add such a column to a populated
// table), so this is what catches a writer that forgot to set it.
func (q queries) missingIdentity(ctx context.Context) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT 'account ' || name FROM accounts WHERE uuid IS NULL
		UNION ALL
		SELECT 'transaction #' || id FROM transactions WHERE uuid IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var what string
		if err := rows.Scan(&what); err != nil {
			return nil, err
		}
		out = append(out, what+" has no uuid")
	}
	return out, rows.Err()
}

// vacuumInto writes a consistent snapshot of the database to dest.
func (q queries) vacuumInto(ctx context.Context, dest string) error {
	literal := "'" + strings.ReplaceAll(dest, "'", "''") + "'"
	_, err := q.db.ExecContext(ctx, "VACUUM INTO "+literal)
	return err
}
