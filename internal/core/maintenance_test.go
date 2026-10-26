package core

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/ledger"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func migrationNames(t *testing.T) []string {
	t.Helper()
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func Test_Open(t *testing.T) {
	tcs := []struct {
		name       string
		path       func(dir string) string
		reopen     bool // close and open again before looking
		wantFolder bool // the database's folder exists afterwards, and is private
		wantErr    string
	}{
		{name: "in memory", path: func(string) string { return ":memory:" }},
		{name: "a file in a folder that does not exist yet", path: func(dir string) string { return filepath.Join(dir, "sub", "thaler.db") }, wantFolder: true},
		{name: "opened a second time it keeps the data", path: func(dir string) string { return filepath.Join(dir, "thaler.db") }, reopen: true},
		{
			name:    "a path with a question mark",
			path:    func(dir string) string { return filepath.Join(dir, "a?b.db") },
			wantErr: "database path \"{dir}/a?b.db\" must not contain '?' or '#'",
		},
		{
			name:    "a path with a hash",
			path:    func(dir string) string { return filepath.Join(dir, "a#b.db") },
			wantErr: "database path \"{dir}/a#b.db\" must not contain '?' or '#'",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			path := tc.path(dir)
			if tc.reopen {
				first, err := Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := first.AddAccount(ctx, "Assets:Bank", "EUR"); err != nil {
					t.Fatal(err)
				}
				if err := first.Close(); err != nil {
					t.Fatal(err)
				}
			}

			// when
			st, err := Open(ctx, path)

			// then
			checkErr(t, errWithDir(err, dir), tc.wantErr, nil)
			if err != nil {
				return
			}
			defer st.Close()
			if tc.reopen {
				got, err := st.GetAccount(ctx, "Assets:Bank")
				if err != nil {
					t.Fatalf("data lost after reopening: %v", err)
				}
				if diff := cmp.Diff(Account{ID: 1, Name: "Assets:Bank", Type: ledger.Assets, Currency: "EUR"}, got); diff != "" {
					t.Errorf("account mismatch (-want +got):\n%s", diff)
				}
			}
			if tc.wantFolder {
				info, err := os.Stat(filepath.Dir(path))
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(fs.FileMode(0o700), info.Mode().Perm()); diff != "" {
					t.Errorf("the folder holds financial data and must be private (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// errWithDir shows a temporary folder in an error as {dir}.
func errWithDir(err error, dir string) error {
	if err == nil {
		return nil
	}
	return &dirError{strings.ReplaceAll(err.Error(), dir, "{dir}")}
}

type dirError struct{ msg string }

func (e *dirError) Error() string { return e.msg }

func Test_Backup(t *testing.T) {
	tcs := []struct {
		name         string
		dest         string // relative to a temporary folder
		existing     bool   // a file is already there
		wantErr      string // {dest} stands for the file
		wantRestored bool   // the backup opens and holds the data
	}{
		{name: "a new file in a new folder", dest: "backups/thaler.db", wantRestored: true},
		{name: "a name with a quote in it", dest: "backups/it's a backup.db", wantRestored: true},
		{name: "a name with characters that mean something in a URI", dest: "backups/what? 100% #1.db"}, // Open refuses such a path, so it is only checked to exist
		{name: "a file that exists is never overwritten", dest: "thaler-backup.db", existing: true, wantErr: "{dest} already exists"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			st, err := Open(ctx, filepath.Join(dir, "thaler.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			accounts("Assets:Bank EUR")(t, st)
			dest := filepath.Join(dir, tc.dest)
			if tc.existing {
				if err := os.WriteFile(dest, []byte("precious"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			err = st.Backup(ctx, dest)

			// then
			if tc.wantErr != "" {
				tc.wantErr = strings.ReplaceAll(tc.wantErr, "{dest}", dest)
			}
			checkErr(t, err, tc.wantErr, nil)
			if tc.wantErr == "" {
				if _, err := os.Stat(dest); err != nil {
					t.Errorf("the backup was not written: %v", err)
				}
			}
			if tc.existing {
				if diff := cmp.Diff("precious", readFile(t, dest)); diff != "" {
					t.Errorf("the existing file was touched (-want +got):\n%s", diff)
				}
			}
			if !tc.wantRestored {
				return
			}
			restored, err := Open(ctx, dest)
			if err != nil {
				t.Fatalf("the backup does not open: %v", err)
			}
			defer restored.Close()
			got, err := restored.GetAccount(ctx, "Assets:Bank")
			if err != nil {
				t.Fatalf("the backup does not contain the data: %v", err)
			}
			if diff := cmp.Diff("Assets:Bank", got.Name); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A snapshot is checked after it is written: one that is not a healthy
// database is an error, not a backup.
func Test_verifySnapshot(t *testing.T) {
	tcs := []struct {
		name    string
		prepare func(t *testing.T, path string) // writes the file
		wantErr string                          // a part of the message; "" for none
	}{
		{
			name: "a healthy database",
			prepare: func(t *testing.T, path string) {
				st, err := Open(ctx, ":memory:")
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				if err := st.Backup(ctx, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "a file that is not a database",
			prepare: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte(strings.Repeat("this is not sqlite\n", 200)), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "file is not a database",
		},
		{
			name:    "no file at all",
			prepare: func(*testing.T, string) {},
			wantErr: "unable to open database file",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "snapshot.db")
			tc.prepare(t, path)

			// when
			err := verifySnapshot(ctx, path)

			// then
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("error = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// oldDatabase builds, outside the repository, a database as an earlier version
// of Thaler left it, and returns its path.
func oldDatabase(t *testing.T, statements ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	initSQL, err := migrationFS.ReadFile("migrations/20261022_init.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range append([]string{string(initSQL)}, statements...) {
		if _, err := old.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("building the old database: %v", err)
		}
	}
	return path
}

func Test_migrate(t *testing.T) {
	all := migrationNames(t)
	const tracked = `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`
	tcs := []struct {
		name       string
		statements []string // on top of the initial schema
		want       []string // the migrations recorded afterwards
		wantErr    string
	}{
		{
			name:       "a database from before migrations were tracked is adopted at the initial schema, then brought up to date",
			statements: []string{`PRAGMA user_version = 1`},
			want:       all,
		},
		{
			name:       "a database at the initial schema is brought up to date",
			statements: []string{tracked, `INSERT INTO schema_migrations (name) VALUES ('20261022_init.up.sql')`},
			want:       all,
		},
		{
			name:       "a database that has a migration this build does not know is refused",
			statements: []string{tracked, `INSERT INTO schema_migrations (name) VALUES ('20261022_init.up.sql'), ('29991231_from_the_future.up.sql')`},
			wantErr:    "database has migration 29991231_from_the_future.up.sql that this build does not know; update thaler",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := oldDatabase(t, tc.statements...)

			// when
			st, err := Open(ctx, path)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if err != nil {
				return
			}
			defer st.Close()
			rows, err := st.repo.db.QueryContext(ctx, `SELECT name FROM schema_migrations ORDER BY name`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var got []string
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatal(err)
				}
				got = append(got, name)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("recorded migrations mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Rows written before accounts and transactions had identities get them, and
// nobody is invented: no user, no owner.
func Test_migrate_existingRows(t *testing.T) {
	type table struct {
		Rows, DistinctUUIDs, WellFormed, Untouched int
	}
	type outcome struct {
		Accounts, Transactions table
		Users                  int
		Problems               []string
		Listed                 int
	}
	const tracked = `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`
	tcs := []struct {
		name       string
		statements []string
		want       outcome
	}{
		{
			name: "two accounts and two transactions",
			statements: []string{
				tracked, `INSERT INTO schema_migrations (name) VALUES ('20261022_init.up.sql')`,
				`INSERT INTO accounts (name, type, currency) VALUES ('Assets:Bank', 'Assets', 'EUR'), ('Expenses:Food', 'Expenses', NULL)`,
				`INSERT INTO transactions (date, payee) VALUES ('2026-10-01', 'a'), ('2026-10-02', 'b')`,
				`INSERT INTO postings (transaction_id, account_id, amount, currency)
				 VALUES (1, 1, -100, 'EUR'), (1, 2, 100, 'EUR'), (2, 1, -200, 'EUR'), (2, 2, 200, 'EUR')`,
			},
			want: outcome{Accounts: table{2, 2, 2, 2}, Transactions: table{2, 2, 2, 2}, Listed: 2},
		},
		{name: "an empty database", statements: []string{tracked, `INSERT INTO schema_migrations (name) VALUES ('20261022_init.up.sql')`}, want: outcome{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := oldDatabase(t, tc.statements...)

			// when
			st, err := Open(ctx, path)

			// then
			if err != nil {
				t.Fatalf("migrating: %v", err)
			}
			defer st.Close()
			summary := func(name, untouched string) table {
				var tb table
				err := st.repo.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT uuid),
					COALESCE(SUM(uuid GLOB '????????-????-4???-[89ab]???-????????????'), 0), COALESCE(SUM(`+untouched+`), 0) FROM `+name).
					Scan(&tb.Rows, &tb.DistinctUUIDs, &tb.WellFormed, &tb.Untouched)
				if err != nil {
					t.Fatal(err)
				}
				return tb
			}
			var got outcome
			got.Accounts = summary("accounts", "version = 1 AND updated_at IS NULL AND created_by IS NULL AND owner_id IS NULL")
			got.Transactions = summary("transactions", "version = 1 AND updated_at IS NULL AND created_by IS NULL")
			if err := st.repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&got.Users); err != nil {
				t.Fatal(err)
			}
			if got.Problems, err = st.Check(ctx); err != nil {
				t.Fatal(err)
			}
			txs, err := st.ListTransactions(ctx, TxFilter{})
			if err != nil {
				t.Fatal(err)
			}
			got.Listed = len(txs)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("outcome mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Check(t *testing.T) {
	healthy := []given{accounts("Assets:Bank EUR"), spending("2026-10-01", "Expenses:Food", 1000)}
	pair := []given{accounts("Assets:Bank EUR", "Assets:Savings EUR"), moved("Assets:Bank", "Assets:Savings", 500, "2026-10-01")}
	tcs := []struct {
		name  string
		setup []given
		want  []string
	}{
		{name: "an empty ledger"},
		{name: "a healthy ledger", setup: healthy},
		{name: "a healthy pair of transfer halves", setup: pair},
		{
			name:  "postings that do not add up",
			setup: append(append([]given{}, healthy...), rawSQL(`UPDATE postings SET amount = amount + 1 WHERE id = 1`)),
			want:  []string{"transaction #1 does not balance: EUR is off by 0.01"},
		},
		{
			name:  "a posting in another currency than its account",
			setup: append(append([]given{}, healthy...), rawSQL(`UPDATE postings SET currency = 'USD' WHERE transaction_id = 1`)),
			want:  []string{"transaction #1 posts USD to Assets:Bank, which holds EUR"},
		},
		{
			name:  "an account without an identity",
			setup: append(append([]given{}, healthy...), rawSQL(`UPDATE accounts SET uuid = NULL WHERE name = 'Expenses:Food'`)),
			want:  []string{"account Expenses:Food has no uuid"},
		},
		{
			name:  "a transaction without an identity",
			setup: append(append([]given{}, healthy...), rawSQL(`UPDATE transactions SET uuid = NULL`)),
			want:  []string{"transaction #1 has no uuid"},
		},
		{
			name:  "a transfer link that is not mutual",
			setup: append(append([]given{}, pair...), rawSQL(`UPDATE transactions SET transfer_with = NULL WHERE id = 2`)),
			want:  []string{"transaction #1 is linked to #2 as a transfer, but #2 does not link back"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			got, err := st.Check(ctx)

			// then
			checkErr(t, err, "", nil)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("problems mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Every account and transaction has a random identity of its own, and every
// edit raises the version of the transaction it touches.
func Test_identity(t *testing.T) {
	type identity struct {
		UUIDOK     bool // a version 4 uuid
		UUIDKept   bool // the same as before the edit
		Version    int
		EditedTime bool
	}
	newEntry := NewTransaction{
		Date: day("2026-10-01"), Payee: "shop",
		Postings: []ledger.Posting{{Account: "Assets:Bank", Amount: eur(-500)}, {Account: "Expenses:Food", Amount: eur(500)}},
	}
	replace := func(t *testing.T, st *Local) {
		nt := newEntry
		nt.Payee = "market"
		if _, err := st.ReplaceTransaction(ctx, 1, nt); err != nil {
			t.Fatal(err)
		}
	}
	recategorize := func(t *testing.T, st *Local) {
		if _, err := st.SetCategories(ctx, []CategoryChange{{TransactionID: 1, Category: "Expenses:Dining"}}); err != nil {
			t.Fatal(err)
		}
	}
	tcs := []struct {
		name  string
		edits []given
		want  identity
	}{
		{name: "never edited", want: identity{true, true, 1, false}},
		{name: "replaced", edits: []given{replace}, want: identity{true, true, 2, true}},
		{name: "recategorized", edits: []given{recategorize}, want: identity{true, true, 2, true}},
		{name: "replaced and recategorized", edits: []given{replace, recategorize}, want: identity{true, true, 3, true}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, accounts("Assets:Bank EUR"), stored(newEntry))
			read := func() (uuid string, version int, updated sql.NullString) {
				err := st.repo.db.QueryRowContext(ctx, `SELECT uuid, version, updated_at FROM transactions WHERE id = 1`).Scan(&uuid, &version, &updated)
				if err != nil {
					t.Fatal(err)
				}
				return uuid, version, updated
			}
			before, _, _ := read()

			// when
			for _, edit := range tc.edits {
				edit(t, st)
			}

			// then
			uuid, version, updated := read()
			got := identity{UUIDOK: uuidV4.MatchString(uuid), UUIDKept: uuid == before, Version: version, EditedTime: updated.Valid}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("identity mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("accounts and transactions all get different uuids", func(t *testing.T) {
		// given
		st := newLedger(t, accounts("Assets:Bank EUR", "Assets:Cash EUR"), stored(newEntry), stored(newEntry))

		// when
		rows, err := st.repo.db.QueryContext(ctx, `SELECT uuid FROM accounts UNION ALL SELECT uuid FROM transactions`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		// then
		seen := map[string]bool{}
		var malformed []string
		for rows.Next() {
			var u string
			if err := rows.Scan(&u); err != nil {
				t.Fatal(err)
			}
			if !uuidV4.MatchString(u) {
				malformed = append(malformed, u)
			}
			seen[u] = true
		}
		if diff := cmp.Diff([]string(nil), malformed); diff != "" {
			t.Errorf("malformed uuids (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(5, len(seen)); diff != "" {
			t.Errorf("distinct uuids for 2 accounts, 2 transactions and the Expenses:Food created on the fly (-want +got):\n%s", diff)
		}
	})
}

// The users table is groundwork: nothing reads it yet, but its rules are
// already the database's.
func Test_usersTable(t *testing.T) {
	user := rawSQL(`INSERT INTO users (name) VALUES ('Santiago')`)
	tcs := []struct {
		name    string
		setup   []given
		stmt    string
		wantErr bool
	}{
		{name: "a user", stmt: `INSERT INTO users (name) VALUES ('Santiago')`},
		{name: "a name that differs only in case", setup: []given{user}, stmt: `INSERT INTO users (name) VALUES ('santiago')`, wantErr: true},
		{name: "a blank name", stmt: `INSERT INTO users (name) VALUES ('  ')`, wantErr: true},
		{name: "no name", stmt: `INSERT INTO users DEFAULT VALUES`, wantErr: true},
		{name: "an account owned by a user", setup: []given{user, accounts("Assets:Bank EUR")}, stmt: `UPDATE accounts SET owner_id = 1`},
		{name: "an account owned by nobody", setup: []given{accounts("Assets:Bank EUR")}, stmt: `UPDATE accounts SET owner_id = NULL`},
		{name: "an account owned by a user that does not exist", setup: []given{accounts("Assets:Bank EUR")}, stmt: `UPDATE accounts SET owner_id = 99`, wantErr: true},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := newLedger(t, tc.setup...)

			// when
			_, err := st.repo.db.ExecContext(ctx, tc.stmt)

			// then
			if diff := cmp.Diff(tc.wantErr, err != nil); diff != "" {
				t.Errorf("failure mismatch (-want +got):\n%s (err = %v)", diff, err)
			}
		})
	}

	t.Run("a user gets a version 4 uuid of their own", func(t *testing.T) {
		// given
		st := newLedger(t, user)

		// when
		var u string
		err := st.repo.db.QueryRowContext(ctx, `SELECT uuid FROM users`).Scan(&u)

		// then
		checkErr(t, err, "", nil)
		if diff := cmp.Diff(true, uuidV4.MatchString(u)); diff != "" {
			t.Errorf("uuid %q is not a version 4 uuid (-want +got):\n%s", u, diff)
		}
	})
}
