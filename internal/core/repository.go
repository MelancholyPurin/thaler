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

	"github.com/melancholypurin/thaler/internal/coded"
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
	db   *sql.DB
	path string // ":memory:" or the database file

	// migrationBackup is the copy made before upgrading the schema while opening,
	// or "" when none was needed.
	migrationBackup string
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
			return nil, coded.New("database_path_invalid", map[string]any{"Path": path})
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
		return nil, fmt.Errorf("%w: %w", coded.New("opening", map[string]any{"Path": path}), err)
	}
	r := &repository{db: db, path: path}
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
			return Account{}, coded.New("account_exists", map[string]any{"Name": name})
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
		return Account{}, coded.New("account_not_found", map[string]any{"Name": name})
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

// datedBalance is a Balance for one day.
type datedBalance struct {
	Balance
	date string
}

// categoryTotals sums the postings on Income and Expenses accounts per account,
// currency and day, for transactions dated from..to (inclusive; an empty bound is
// open). The day is there so that each sum can be converted at the rate of its own
// day.
func (q queries) categoryTotals(ctx context.Context, from, to string) ([]datedBalance, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT a.name, a.type, p.currency, t.date, SUM(p.amount)
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		JOIN transactions t ON t.id = p.transaction_id
		WHERE a.type IN ('Income', 'Expenses')
		  AND (? = '' OR t.date >= ?)
		  AND (? = '' OR t.date <= ?)
		GROUP BY a.id, p.currency, t.date
		HAVING SUM(p.amount) <> 0
		ORDER BY a.name, p.currency, t.date`, from, from, to, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []datedBalance
	for rows.Next() {
		var (
			d        datedBalance
			typ, cur string
		)
		if err := rows.Scan(&d.Account, &typ, &cur, &d.date, &d.Amount.Minor); err != nil {
			return nil, err
		}
		d.Type, d.Amount.Currency = ledger.AccountType(typ), ledger.Currency(cur)
		out = append(out, d)
	}
	return out, rows.Err()
}

// payeeSpend is what one transaction put on Expenses accounts, in one currency.
type payeeSpend struct {
	txID   int64
	payee  string
	date   string
	amount ledger.Money
}

// expensesByTransaction sums the postings on Expenses accounts of each transaction
// dated from..to (inclusive), per currency. A transaction whose expenses come to
// nothing is left out.
func (q queries) expensesByTransaction(ctx context.Context, from, to string) ([]payeeSpend, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT t.id, t.payee, t.date, p.currency, SUM(p.amount)
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		JOIN transactions t ON t.id = p.transaction_id
		WHERE a.type = 'Expenses'
		  AND t.date >= ? AND t.date <= ?
		GROUP BY t.id, p.currency
		HAVING SUM(p.amount) <> 0
		ORDER BY t.date, t.id, p.currency`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []payeeSpend
	for rows.Next() {
		var (
			s   payeeSpend
			cur string
		)
		if err := rows.Scan(&s.txID, &s.payee, &s.date, &cur, &s.amount.Minor); err != nil {
			return nil, err
		}
		s.amount.Currency = ledger.Currency(cur)
		out = append(out, s)
	}
	return out, rows.Err()
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
			return nil, coded.New("corrupt_rate", map[string]any{"Rate": rate, "Base": base, "Quote": quote, "Date": date})
		}
		out = append(out, ledger.RateEntry{Date: date, Base: ledger.Currency(base), Quote: ledger.Currency(quote), Rate: r})
	}
	return out, rows.Err()
}

// ---- budget targets --------------------------------------------------------

func (q queries) putBudgetTarget(ctx context.Context, accountID int64, month string, amount ledger.Money) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO budget_targets (account_id, month, amount, currency) VALUES (?, ?, ?, ?)
		 ON CONFLICT (account_id, month) DO UPDATE SET amount = excluded.amount, currency = excluded.currency`,
		accountID, month, amount.Minor, string(amount.Currency))
	return err
}

func (q queries) deleteBudgetTarget(ctx context.Context, accountID int64, month string) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM budget_targets WHERE account_id = ? AND month = ?`, accountID, month)
	return err
}

func (q queries) budgetTargets(ctx context.Context, month string) ([]BudgetTarget, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT a.name, t.amount, t.currency FROM budget_targets t JOIN accounts a ON a.id = t.account_id
		 WHERE t.month = ? ORDER BY a.name`, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BudgetTarget
	for rows.Next() {
		var (
			t   BudgetTarget
			cur string
		)
		if err := rows.Scan(&t.Category, &t.Amount.Minor, &cur); err != nil {
			return nil, err
		}
		t.Amount.Currency = ledger.Currency(cur)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (q queries) putBudgetRollover(ctx context.Context, accountID int64, from string) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO budget_rollover (account_id, from_month) VALUES (?, ?)
		 ON CONFLICT (account_id) DO UPDATE SET from_month = excluded.from_month`, accountID, from)
	return err
}

func (q queries) deleteBudgetRollover(ctx context.Context, accountID int64) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM budget_rollover WHERE account_id = ?`, accountID)
	return err
}

func (q queries) budgetRollovers(ctx context.Context) ([]BudgetRollover, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT a.name, r.from_month FROM budget_rollover r JOIN accounts a ON a.id = r.account_id ORDER BY a.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BudgetRollover
	for rows.Next() {
		var r BudgetRollover
		if err := rows.Scan(&r.Category, &r.From); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (q queries) putMinimumBalance(ctx context.Context, accountID, minor int64) error {
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO account_minimums (account_id, amount) VALUES (?, ?)
		 ON CONFLICT (account_id) DO UPDATE SET amount = excluded.amount`, accountID, minor)
	return err
}

func (q queries) deleteMinimumBalance(ctx context.Context, accountID int64) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM account_minimums WHERE account_id = ?`, accountID)
	return err
}

func (q queries) minimumBalances(ctx context.Context) ([]MinimumBalance, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT a.name, a.currency, m.amount FROM account_minimums m JOIN accounts a ON a.id = m.account_id ORDER BY a.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MinimumBalance
	for rows.Next() {
		var m MinimumBalance
		var cur string
		if err := rows.Scan(&m.Account, &cur, &m.Amount.Minor); err != nil {
			return nil, err
		}
		m.Amount.Currency = ledger.Currency(cur)
		out = append(out, m)
	}
	return out, rows.Err()
}

// monthlyAmount is a sum for one category in one month (a target) or on one day
// (spending), in the currency it was kept in.
type monthlyAmount struct {
	category, month string // month is YYYY-MM for a target, YYYY-MM-DD for spending
	amount          ledger.Money
}

// carriedSpending is what the categories that carry over spent on each day from
// the month they started to the one before month, per currency.
func (q queries) carriedSpending(ctx context.Context, month string) ([]monthlyAmount, error) {
	return q.monthlyAmounts(ctx, `
		SELECT a.name, t.date, p.currency, SUM(p.amount)
		FROM postings p
		JOIN accounts a ON a.id = p.account_id
		JOIN transactions t ON t.id = p.transaction_id
		JOIN budget_rollover r ON r.account_id = a.id
		WHERE t.date >= r.from_month || '-01' AND substr(t.date, 1, 7) < ?
		GROUP BY a.id, t.date, p.currency
		ORDER BY a.name, t.date, p.currency`, month)
}

// carriedTargets is the targets of the categories that carry over, for each month
// from the month they started to the one before month.
func (q queries) carriedTargets(ctx context.Context, month string) ([]monthlyAmount, error) {
	return q.monthlyAmounts(ctx, `
		SELECT a.name, t.month, t.currency, t.amount
		FROM budget_targets t
		JOIN accounts a ON a.id = t.account_id
		JOIN budget_rollover r ON r.account_id = t.account_id
		WHERE t.month >= r.from_month AND t.month < ?
		ORDER BY a.name, t.month`, month)
}

func (q queries) monthlyAmounts(ctx context.Context, query string, args ...any) ([]monthlyAmount, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []monthlyAmount
	for rows.Next() {
		var (
			a   monthlyAmount
			cur string
		)
		if err := rows.Scan(&a.category, &a.month, &cur, &a.amount.Minor); err != nil {
			return nil, err
		}
		a.amount.Currency = ledger.Currency(cur)
		out = append(out, a)
	}
	return out, rows.Err()
}

// targetToCopy is a target of one month and the category it is for.
type targetToCopy struct {
	accountID int64
	target    BudgetTarget
}

// targetsToCopy are the targets of month from whose category has none in month to,
// by category name, and how many the other way round: the ones that month has.
func (q queries) targetsToCopy(ctx context.Context, from, to string) (missing []targetToCopy, kept int, err error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT t.account_id, a.name, t.amount, t.currency,
		        EXISTS (SELECT 1 FROM budget_targets o WHERE o.account_id = t.account_id AND o.month = ?)
		 FROM budget_targets t JOIN accounts a ON a.id = t.account_id
		 WHERE t.month = ? ORDER BY a.name`, to, from)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			c      targetToCopy
			cur    string
			exists bool
		)
		if err := rows.Scan(&c.accountID, &c.target.Category, &c.target.Amount.Minor, &cur, &exists); err != nil {
			return nil, 0, err
		}
		c.target.Amount.Currency = ledger.Currency(cur)
		if exists {
			kept++
			continue
		}
		missing = append(missing, c)
	}
	return missing, kept, rows.Err()
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
		return coded.New("transaction_not_found_or_imported", map[string]any{"ID": id})
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

// ---- schedules -------------------------------------------------------------

// scheduleNameTaken says whether another schedule than except has the name.
func (q queries) scheduleNameTaken(ctx context.Context, name string, except int64) (bool, error) {
	var n int
	err := q.db.QueryRowContext(ctx, `SELECT count(*) FROM schedules WHERE name = ? AND id <> ?`, name, except).Scan(&n)
	return n > 0, err
}

func (q queries) scheduleExists(ctx context.Context, id int64) (bool, error) {
	var n int
	err := q.db.QueryRowContext(ctx, `SELECT count(*) FROM schedules WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

// endDate is the end of ns for the database: NULL when it has none.
func endDate(ns NewSchedule) any {
	if ns.End.IsZero() {
		return nil
	}
	return ns.End.Format(ledger.DateLayout)
}

func (q queries) insertSchedule(ctx context.Context, ns NewSchedule) (int64, error) {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO schedules (name, every, unit, start_date, end_date, transfer, tolerance, payee, description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ns.Name, ns.Every, string(ns.Unit), ns.Start.Format(ledger.DateLayout), endDate(ns), ns.Transfer, ns.Tolerance, ns.Payee, ns.Description)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (q queries) updateSchedule(ctx context.Context, id int64, ns NewSchedule) error {
	_, err := q.db.ExecContext(ctx,
		`UPDATE schedules SET name = ?, every = ?, unit = ?, start_date = ?, end_date = ?, transfer = ?, tolerance = ?, payee = ?, description = ? WHERE id = ?`,
		ns.Name, ns.Every, string(ns.Unit), ns.Start.Format(ledger.DateLayout), endDate(ns), ns.Transfer, ns.Tolerance, ns.Payee, ns.Description, id)
	return err
}

func (q queries) insertSchedulePostings(ctx context.Context, id int64, postings []ledger.Posting, accountIDs []int64) error {
	for i, p := range postings {
		if _, err := q.db.ExecContext(ctx,
			`INSERT INTO schedule_postings (schedule_id, position, account_id, amount, currency) VALUES (?, ?, ?, ?, ?)`,
			id, i, accountIDs[i], p.Amount.Minor, string(p.Amount.Currency)); err != nil {
			return err
		}
	}
	return nil
}

func (q queries) deleteSchedulePostings(ctx context.Context, id int64) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM schedule_postings WHERE schedule_id = ?`, id)
	return err
}

func (q queries) deleteSchedule(ctx context.Context, id int64) (bool, error) {
	if err := q.deleteSchedulePostings(ctx, id); err != nil {
		return false, err
	}
	res, err := q.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (q queries) schedules(ctx context.Context) ([]Schedule, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, name, every, unit, start_date, COALESCE(end_date, ''), transfer, tolerance, payee, description FROM schedules ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Schedule
	byID := map[int64]int{}
	for rows.Next() {
		var (
			s    Schedule
			unit string
		)
		if err := rows.Scan(&s.ID, &s.Name, &s.Every, &unit, &s.Start, &s.End, &s.Transfer, &s.Tolerance, &s.Payee, &s.Description); err != nil {
			return nil, err
		}
		s.Unit = ScheduleUnit(unit)
		byID[s.ID] = len(out)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prows, err := q.db.QueryContext(ctx,
		`SELECT p.schedule_id, a.name, p.amount, p.currency
		 FROM schedule_postings p JOIN accounts a ON a.id = p.account_id
		 ORDER BY p.schedule_id, p.position`)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var (
			id  int64
			p   PostingView
			cur string
		)
		if err := prows.Scan(&id, &p.Account, &p.Amount.Minor, &cur); err != nil {
			return nil, err
		}
		p.Amount.Currency = ledger.Currency(cur)
		i := byID[id]
		out[i].Postings = append(out[i].Postings, p)
	}
	if err := prows.Err(); err != nil {
		return nil, err
	}
	zrows, err := q.db.QueryContext(ctx,
		`SELECT schedule_id, from_date, COALESCE(to_date, '') FROM schedule_pauses ORDER BY schedule_id, from_date`)
	if err != nil {
		return nil, err
	}
	defer zrows.Close()
	for zrows.Next() {
		var (
			id int64
			z  SchedulePause
		)
		if err := zrows.Scan(&id, &z.From, &z.To); err != nil {
			return nil, err
		}
		out[byID[id]].Pauses = append(out[byID[id]].Pauses, z)
	}
	return out, zrows.Err()
}

// pauseSchedule starts a pause on day unless one is open.
func (q queries) pauseSchedule(ctx context.Context, id int64, day string) error {
	var open int
	if err := q.db.QueryRowContext(ctx, `SELECT count(*) FROM schedule_pauses WHERE schedule_id = ? AND to_date IS NULL`, id).Scan(&open); err != nil || open > 0 {
		return err
	}
	// Pausing again on a day it was resumed reopens that span instead of adding one.
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO schedule_pauses (schedule_id, from_date) VALUES (?, ?)
		 ON CONFLICT (schedule_id, from_date) DO UPDATE SET to_date = NULL`, id, day)
	return err
}

// resumeSchedule ends the open pause on day, if there is one.
func (q queries) resumeSchedule(ctx context.Context, id int64, day string) error {
	_, err := q.db.ExecContext(ctx, `UPDATE schedule_pauses SET to_date = ? WHERE schedule_id = ? AND to_date IS NULL`, day, id)
	return err
}

// handledOccurrences is the set of occurrences confirmed or skipped, keyed by
// schedule and date.
func (q queries) handledOccurrences(ctx context.Context) (map[handled]bool, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT schedule_id, date FROM schedule_occurrences`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[handled]bool{}
	for rows.Next() {
		var h handled
		if err := rows.Scan(&h.schedule, &h.date); err != nil {
			return nil, err
		}
		out[h] = true
	}
	return out, rows.Err()
}

// deleteSkip removes the record of a skip, and says whether there was one.
func (q queries) deleteSkip(ctx context.Context, h handled) (bool, error) {
	res, err := q.db.ExecContext(ctx, `DELETE FROM schedule_occurrences WHERE schedule_id = ? AND date = ? AND status = 'skipped'`, h.schedule, h.date)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// handled names an occurrence.
type handled struct {
	schedule int64
	date     string
}

// putOccurrence records the answer to an occurrence; txID is 0 for a skip.
func (q queries) putOccurrence(ctx context.Context, h handled, txID int64) error {
	status, tx := "skipped", any(nil)
	if txID != 0 {
		status, tx = "posted", txID
	}
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO schedule_occurrences (schedule_id, date, status, transaction_id) VALUES (?, ?, ?, ?)`,
		h.schedule, h.date, status, tx)
	return err
}
