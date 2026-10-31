package core

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// accountNames lists the accounts in the SQLite file at path, without opening it
// as a ledger (which would upgrade it). A file that is not a database has none.
// The file is opened immutable, so that looking leaves no -wal or -shm file
// behind for the tests to trip over; nothing else has it open.
func accountNames(t *testing.T, path string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(), `SELECT name FROM accounts ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	return out
}

// everythingBelow lists every file below dir, relative to it, including the
// write-ahead files that filesBelow leaves out: a stray one is exactly what the
// restore tests have to catch.
func everythingBelow(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ledgerWith builds a database file at path holding one account.
func ledgerWith(t *testing.T, path, account string) {
	t.Helper()
	st, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddAccount(t.Context(), account, "EUR"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

func Test_RestoreBackup(t *testing.T) {
	type observed struct {
		Err       string
		Replaced  string   // where the old database was put, relative to the folder, the time shown as {time}
		Upgrade   bool     // the backup is older than this build
		Accounts  []string // in the database afterwards
		WasKept   []string // in the database that was put aside
		Leftovers []string // anything in the folder that is not the database or backups
	}
	const (
		tracked = `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`
		atInit  = `INSERT INTO schema_migrations (name) VALUES ('20261022_init.up.sql')`
		anAcct  = `INSERT INTO accounts (name, type, currency) VALUES ('Assets:Bank', 'Assets', 'EUR')`
		kept    = "backups/replaced/thaler-{time}-replaced.db"
	)
	live := []string{"Assets:Live"}
	old := []string{"Assets:Old"}
	// A source is built in its own folder, and given the database's folder and path.
	// A database as Open leaves it: in write-ahead-log mode, which is also what a
	// database that a restore set aside looks like.
	plain := func(t *testing.T, _, _ string) string {
		p := filepath.Join(t.TempDir(), "backup.db")
		ledgerWith(t, p, "Assets:Old")
		return p
	}
	sealedOld := func(t *testing.T, _, _ string) string {
		cheapScrypt(t)
		st, err := Open(t.Context(), ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		if _, err := st.AddAccount(t.Context(), "Assets:Old", "EUR"); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "backup.db.age")
		if err := st.BackupEncrypted(t.Context(), p, "right"); err != nil {
			t.Fatal(err)
		}
		return p
	}
	file := func(content []byte) func(*testing.T, string, string) string {
		return func(t *testing.T, _, _ string) string {
			p := filepath.Join(t.TempDir(), "backup.db")
			if err := os.WriteFile(p, content, 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	tcs := []struct {
		name       string
		hasLive    bool
		liveBroken bool // the database in place is not a database at all
		passphrase string
		source     func(t *testing.T, dir, dbPath string) string
		want       observed
		wantErr    string // {src} and {db} stand for the files
	}{
		{
			name: "a plain backup replaces the database, which is kept", hasLive: true, source: plain,
			want: observed{Replaced: kept, Accounts: old, WasKept: live},
		},
		{
			name: "an encrypted one, with its passphrase", hasLive: true, passphrase: "right", source: sealedOld,
			want: observed{Replaced: kept, Accounts: old, WasKept: live},
		},
		{
			name: "with no database in place there is nothing to keep", source: plain,
			want: observed{Accounts: old},
		},
		{
			name: "a database that is not even a database is kept as it is, and the restore still works", hasLive: true, liveBroken: true, source: plain,
			want: observed{Replaced: kept, Accounts: old},
		},
		{
			name: "a backup from an older version is restored and flagged for upgrade", hasLive: true,
			source: func(t *testing.T, _, _ string) string { return oldDatabase(t, tracked, atInit, anAcct) },
			want:   observed{Replaced: kept, Upgrade: true, Accounts: []string{"Assets:Bank"}, WasKept: live},
		},
		{
			name: "the wrong passphrase changes nothing", hasLive: true, passphrase: "wrong", source: sealedOld,
			wantErr: "decrypting {src}: incorrect passphrase",
			want:    observed{Accounts: live},
		},
		{
			name: "an encrypted backup needs a passphrase", hasLive: true, source: sealedOld,
			wantErr: "{src} is encrypted: a passphrase is needed",
			want:    observed{Accounts: live},
		},
		{
			name: "a file that is not there", hasLive: true,
			source:  func(t *testing.T, dir, _ string) string { return filepath.Join(dir, "nothing.db") },
			wantErr: "{src} does not exist",
			want:    observed{Accounts: live},
		},
		{
			name: "a file that is not a backup", hasLive: true, source: file([]byte("just some text")),
			wantErr: "{src} is not a backup: it is neither a SQLite database nor an age file",
			want:    observed{Accounts: live},
		},
		{
			name: "a SQLite database that is not Thaler's", hasLive: true,
			source: func(t *testing.T, _, _ string) string {
				p := filepath.Join(t.TempDir(), "other.db")
				db, err := sql.Open("sqlite", "file:"+p)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.ExecContext(t.Context(), `CREATE TABLE notes (x TEXT); PRAGMA user_version = 3`); err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: "{src} is a SQLite database but not a Thaler one, and nothing was changed",
			want:    observed{Accounts: live},
		},
		{
			name: "a backup made by a newer version", hasLive: true,
			source: func(t *testing.T, _, _ string) string {
				return oldDatabase(t, atVersion(29991231, false)...)
			},
			wantErr: "{src} was made by a newer version of Thaler (it has migration 29991231): update thaler; nothing was changed",
			want:    observed{Accounts: live},
		},
		{
			name: "a backup saved while an upgrade of it was stopped halfway", hasLive: true,
			source: func(t *testing.T, _, _ string) string {
				return oldDatabase(t, atVersion(20261025, true)...)
			},
			wantErr: "{src} was saved while an upgrade of it was stopped halfway (migration 20261025), and nothing was changed",
			want:    observed{Accounts: live},
		},
		{
			name: "a backup made by the current version needs no upgrade", hasLive: true,
			source: func(t *testing.T, _, _ string) string {
				return oldDatabase(t, append(atVersion(latestMigration(t), false), anAcct)...)
			},
			want: observed{Replaced: kept, Accounts: []string{"Assets:Bank"}, WasKept: live},
		},
		{
			name: "a damaged backup", hasLive: true, source: file(append(append([]byte{}, sqliteMagic...), bytes.Repeat([]byte{0xff}, 8192)...)),
			wantErr: "{src} is not a healthy database, and nothing was changed: file is not a database (26)",
			want:    observed{Accounts: live},
		},
		{
			name: "the database itself", hasLive: true,
			source:  func(t *testing.T, _, dbPath string) string { return dbPath },
			wantErr: "cannot restore {src} from itself",
			want:    observed{Accounts: live},
		},
		{
			name: "a backup with changes still waiting in its log", hasLive: true,
			source: func(t *testing.T, _, _ string) string {
				p := plain(t, "", "")
				if err := os.WriteFile(p+"-wal", []byte("pending"), 0o600); err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: "{src}-wal holds changes that are not in {src} yet: open that database with Thaler once, or restore from a backup made with thaler cli backup",
			want:    observed{Accounts: live},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "thaler.db")
			switch {
			case tc.liveBroken:
				if err := os.WriteFile(dbPath, []byte("this is not a database"), 0o600); err != nil {
					t.Fatal(err)
				}
			case tc.hasLive:
				ledgerWith(t, dbPath, "Assets:Live")
			}
			src := tc.source(t, dir, dbPath)

			// when
			res, err := RestoreBackup(t.Context(), src, dbPath, tc.passphrase)

			// then
			got := observed{Upgrade: res.Upgrade, Accounts: accountNames(t, dbPath)}
			if err != nil {
				got.Err = strings.NewReplacer(src, "{src}", dbPath, "{db}").Replace(err.Error())
			}
			if res.Replaced != "" {
				rel, _ := filepath.Rel(dir, res.Replaced)
				got.Replaced = regexp.MustCompile(`thaler-\d{8}-\d{6}-replaced`).ReplaceAllString(rel, "thaler-{time}-replaced")
				got.WasKept = accountNames(t, res.Replaced)
			}
			for _, f := range everythingBelow(t, dir) {
				if f != "thaler.db" && !strings.HasPrefix(f, "backups"+string(filepath.Separator)) {
					got.Leftovers = append(got.Leftovers, f)
				}
			}
			if tc.wantErr == "" {
				if got.Err != "" {
					t.Fatalf("unexpected error: %s", got.Err)
				}
			} else if diff := cmp.Diff(tc.wantErr, got.Err); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			got.Err = ""
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Restoring the database that a restore set aside puts things back as they were.
func Test_RestoreBackup_undo(t *testing.T) {
	tcs := []struct {
		name string
		live string // the account in the database in place
		old  string // the account in the backup
	}{
		{name: "one account each", live: "Assets:Live", old: "Assets:Old"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "thaler.db")
			ledgerWith(t, dbPath, tc.live)
			backup := filepath.Join(t.TempDir(), "backup.db")
			ledgerWith(t, backup, tc.old)
			first, err := RestoreBackup(t.Context(), backup, dbPath, "")
			if err != nil {
				t.Fatal(err)
			}

			// when
			second, err := RestoreBackup(t.Context(), first.Replaced, dbPath, "")

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff([]string{tc.live}, accountNames(t, dbPath)); diff != "" {
				t.Errorf("the database is not back as it was (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]string{tc.old}, accountNames(t, second.Replaced)); diff != "" {
				t.Errorf("the restored backup was not kept (-want +got):\n%s", diff)
			}
		})
	}
}

// A restored database is a working one: it opens, upgrades if it is old, and
// keeps what the backup held.
func Test_RestoreBackup_opens(t *testing.T) {
	tcs := []struct {
		name  string
		setup []string // builds the old database, after the initial schema
		want  string   // the account that must still be there
	}{
		{
			name: "a database from before names were tracked by file",
			setup: []string{
				`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`,
				`INSERT INTO schema_migrations (name) VALUES ('20261022_init.up.sql')`,
				`INSERT INTO accounts (name, type, currency) VALUES ('Assets:Bank', 'Assets', 'EUR')`,
			},
			want: "Assets:Bank",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "thaler.db")
			old := oldDatabase(t, tc.setup...)
			if _, err := RestoreBackup(t.Context(), old, dbPath, ""); err != nil {
				t.Fatal(err)
			}

			// when
			st, err := Open(t.Context(), dbPath)

			// then
			if err != nil {
				t.Fatalf("the restored database does not open: %v", err)
			}
			defer st.Close()
			acct, err := st.GetAccount(t.Context(), tc.want)
			if err != nil {
				t.Fatalf("the data is not there: %v", err)
			}
			if diff := cmp.Diff(tc.want, acct.Name); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if st.PreMigrationBackup() == "" {
				t.Error("an old backup is upgraded after a copy of it, but no copy was made")
			}
			checkConsistent(t, st)
		})
	}
}

func Test_BackupIsEncrypted(t *testing.T) {
	tcs := []struct {
		name    string
		content []byte // nil: the file does not exist
		want    bool
		wantErr string
	}{
		{name: "a plain backup", content: append([]byte{}, sqliteMagic...), want: false},
		{name: "an encrypted one", content: []byte("age-encryption.org/v1\n-> scrypt"), want: true},
		{name: "something else", content: []byte("hello, this is text"), wantErr: "{path} is not a backup: it is neither a SQLite database nor an age file"},
		{name: "an empty file", content: []byte{}, wantErr: "{path} is not a backup: it is neither a SQLite database nor an age file"},
		{name: "a file that is not there", wantErr: "{path} does not exist"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "f")
			if tc.content != nil {
				if err := os.WriteFile(path, tc.content, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			got, err := BackupIsEncrypted(path)

			// then
			checkErr(t, err, strings.ReplaceAll(tc.wantErr, "{path}", path), nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
