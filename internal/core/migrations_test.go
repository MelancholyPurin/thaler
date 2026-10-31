package core

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/go-cmp/cmp"
)

// migrationScriptTimestamps are the versions of the files in dbmigrations/, in
// order. golang-migrate only remembers the last version applied, so a file whose
// version is lower than one already applied would be skipped, silently. Every new
// migration is added here, which is also what turns two branches that each add
// one into a merge conflict: the one merged second renames its file to a newer
// version, and then adds it here.
var migrationScriptTimestamps = []uint{
	20261022,
	20261025,
	20261026,
}

func Test_migrationScriptTimestamps(t *testing.T) {
	versionsOf := func(t *testing.T) []uint {
		files, err := migrationFiles()
		if err != nil {
			t.Fatal(err)
		}
		var versions []uint
		for _, f := range files {
			versions = append(versions, f.version)
		}
		return versions
	}
	onDisk := func(t *testing.T) (names []string) {
		entries, err := os.ReadDir(migrationsDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".down.sql") {
				names = append(names, e.Name())
			}
		}
		return names
	}
	tcs := []struct {
		name string
		got  func(t *testing.T) any
		want any
	}{
		{name: "every migration file is registered, and nothing else is", got: func(t *testing.T) any { return versionsOf(t) }, want: migrationScriptTimestamps},
		{
			name: "the registered versions are in ascending order",
			got: func(*testing.T) any {
				return sort.SliceIsSorted(migrationScriptTimestamps, func(i, j int) bool { return migrationScriptTimestamps[i] < migrationScriptTimestamps[j] })
			},
			want: true,
		},
		{name: "there are no down migrations", got: func(t *testing.T) any { return onDisk(t) }, want: []string(nil)},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := tc.got(t)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_migrationFilesIn(t *testing.T) {
	file := func(names ...string) fstest.MapFS {
		fsys := fstest.MapFS{"m": &fstest.MapFile{Mode: fs.ModeDir}}
		for _, n := range names {
			fsys["m/"+n] = &fstest.MapFile{Data: []byte("SELECT 1;")}
		}
		return fsys
	}
	tcs := []struct {
		name    string
		fsys    fs.FS
		want    []migrationFile
		wantErr string
	}{
		{name: "none", fsys: file(), want: nil},
		{
			name: "oldest version first, whatever the order of the names",
			fsys: file("20261026_b.up.sql", "20261022_a.up.sql"),
			want: []migrationFile{{20261022, "20261022_a.up.sql"}, {20261026, "20261026_b.up.sql"}},
		},
		{
			name: "a timestamp sorts after every date",
			fsys: file("20261101120000_c.up.sql", "29991231_future.up.sql", "20261022_a.up.sql"),
			want: []migrationFile{{20261022, "20261022_a.up.sql"}, {29991231, "29991231_future.up.sql"}, {20261101120000, "20261101120000_c.up.sql"}},
		},
		{name: "two files with one version", fsys: file("20261022_a.up.sql", "20261022_b.up.sql"), wantErr: "migrations 20261022_a.up.sql and 20261022_b.up.sql have the same version"},
		{name: "a down migration", fsys: file("20261022_a.down.sql"), wantErr: `bad migration file name "20261022_a.down.sql" (want YYYYMMDDHHMMSS_name.up.sql, in lower case)`},
		{name: "a name in capitals", fsys: file("20261022_Init.up.sql"), wantErr: `bad migration file name "20261022_Init.up.sql" (want YYYYMMDDHHMMSS_name.up.sql, in lower case)`},
		{name: "a version that is neither a date nor a timestamp", fsys: file("202610_a.up.sql"), wantErr: `bad migration file name "202610_a.up.sql" (want YYYYMMDDHHMMSS_name.up.sql, in lower case)`},
		{name: "a folder that is not there", fsys: fstest.MapFS{}, wantErr: "open m: file does not exist"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got, err := migrationFilesIn(tc.fsys, "m")

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(migrationFile{}), equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_readMigrationState(t *testing.T) {
	tcs := []struct {
		name       string
		statements []string
		want       migrationState
		wantErr    string
	}{
		{name: "no table at all: a new database, or one that is not Thaler's"},
		{name: "golang-migrate's table, with nothing in it", statements: []string{`CREATE TABLE schema_migrations (version uint64, dirty bool)`}},
		{name: "at a version", statements: atVersion(20261025, false), want: migrationState{version: 20261025, existing: true}},
		{name: "in the middle of a migration", statements: atVersion(20261025, true), want: migrationState{version: 20261025, dirty: true, existing: true}},
		{
			name: "the table of file names: the newest file is the version",
			statements: []string{
				`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`,
				`INSERT INTO schema_migrations (name) VALUES ('20261025_users_and_identity.up.sql'), ('20261022_init.up.sql')`,
			},
			want: migrationState{version: 20261025, legacy: true, existing: true},
		},
		{
			name:       "the table of file names, with nothing in it",
			statements: []string{`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`},
			want:       migrationState{legacy: true},
		},
		{
			name: "a file name that is not one",
			statements: []string{
				`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT '')`,
				`INSERT INTO schema_migrations (name) VALUES ('hello')`,
			},
			want:    migrationState{legacy: true},
			wantErr: `schema_migrations has "hello", which is not a migration file name`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "s.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			for _, stmt := range tc.statements {
				if _, err := db.ExecContext(t.Context(), stmt); err != nil {
					t.Fatal(err)
				}
			}

			// when
			got, err := readMigrationState(t.Context(), db)

			// then
			checkErr(t, err, tc.wantErr, nil)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(migrationState{})); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A migration that fails is rolled back, and the database is not left marked as
// half upgraded: the next start must not be refused for it.
func Test_migrateFrom_failure(t *testing.T) {
	type observed struct {
		Err     string
		Version uint
		Dirty   bool
		Tables  []string // of those the migrations below create
	}
	embedded := func(t *testing.T, extra map[string]string) fstest.MapFS {
		fsys := fstest.MapFS{}
		entries, err := migrationFS.ReadDir(migrationsDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			body, err := migrationFS.ReadFile(migrationsDir + "/" + e.Name())
			if err != nil {
				t.Fatal(err)
			}
			fsys["m/"+e.Name()] = &fstest.MapFile{Data: body}
		}
		for name, body := range extra {
			fsys["m/"+name] = &fstest.MapFile{Data: []byte(body)}
		}
		return fsys
	}
	const (
		good1 = "20261101120000_one.up.sql"
		good2 = "20261101120001_two.up.sql"
		bad   = "20261101120002_bad.up.sql"
	)
	tcs := []struct {
		name  string
		extra map[string]string
		fixed map[string]string // replaces the failing file, for a second try
		want  observed
		again observed // after the second try, when there is one
	}{
		{
			name:  "a failing migration changes nothing, and the version stays where it was",
			extra: map[string]string{bad: "CREATE TABLE half_done (x INT); THIS IS NOT SQL;"},
			fixed: map[string]string{bad: "CREATE TABLE fixed (x INT);"},
			want: observed{
				Err:     `migration 20261101120002_bad.up.sql: SQL logic error: near "THIS": syntax error (1)`,
				Version: 20261026,
			},
			again: observed{Version: 20261101120002, Tables: []string{"fixed"}},
		},
		{
			name:  "the ones before it stay applied",
			extra: map[string]string{good1: "CREATE TABLE one (x INT);", good2: "CREATE TABLE two (x INT);", bad: "CREATE TABLE half_done (x INT); NOPE;"},
			fixed: map[string]string{bad: "CREATE TABLE three (x INT);"},
			want: observed{
				Err:     `migration 20261101120002_bad.up.sql: SQL logic error: near "NOPE": syntax error (1)`,
				Version: 20261101120001, Tables: []string{"one", "two"},
			},
			again: observed{Version: 20261101120002, Tables: []string{"one", "three", "two"}},
		},
		{
			// The trigger makes recording the version fail, after the migration itself
			// has been committed: the schema has changed, so the version is not reset.
			name: "a migration that was applied but could not be recorded stays marked, and the next try is refused",
			extra: map[string]string{
				bad: "CREATE TABLE half_done (x INT); CREATE TRIGGER no_more BEFORE INSERT ON schema_migrations BEGIN SELECT RAISE(ABORT, 'no'); END;",
			},
			want: observed{
				Err:     "constraint failed: no (1811) in line 0: INSERT INTO schema_migrations (version, dirty) VALUES (?, ?)",
				Version: 20261101120002, Dirty: true, Tables: []string{"half_done"},
			},
			again: observed{
				Err:     "the last upgrade of the database stopped halfway, at migration 20261101120002, and it is not safe to go on",
				Version: 20261101120002, Dirty: true, Tables: []string{"half_done"},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st, err := Open(t.Context(), ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			look := func(err error) observed {
				var o observed
				if err != nil {
					o.Err = err.Error()
				}
				var dirty bool
				if err := st.repo.db.QueryRowContext(t.Context(), `SELECT version, dirty FROM schema_migrations`).Scan(&o.Version, &dirty); err != nil {
					t.Fatal(err)
				}
				o.Dirty = dirty
				rows, err := st.repo.db.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('one','two','three','half_done','fixed') ORDER BY name`)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				for rows.Next() {
					var name string
					if err := rows.Scan(&name); err != nil {
						t.Fatal(err)
					}
					o.Tables = append(o.Tables, name)
				}
				return o
			}

			// when
			got := look(st.repo.migrateFrom(t.Context(), embedded(t, tc.extra), "m"))
			again := look(st.repo.migrateFrom(t.Context(), embedded(t, mergeStrings(tc.extra, tc.fixed)), "m"))

			// then
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("the failed try (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.again, again, equalEmpty); diff != "" {
				t.Errorf("the second try (-want +got):\n%s", diff)
			}
		})
	}
}

func mergeStrings(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// A database that is marked as half upgraded is not opened, and the error says
// where the copy made before that upgrade is.
func Test_Open_interruptedUpgrade(t *testing.T) {
	tcs := []struct {
		name    string
		copies  []string // files in the pre-migration folder, below the database's folder
		wantErr string   // {dir} stands for the folder of the database
	}{
		{
			name:    "no copy to point to",
			wantErr: "the last upgrade of the database stopped halfway, at migration 20261025, and it is not safe to go on",
		},
		{
			name:    "the newest copy is named",
			copies:  []string{"backups/pre-migration/thaler-20261024-090000-before-a.db", "backups/pre-migration/thaler-20261025-090000-before-b.db"},
			wantErr: "the last upgrade of the database stopped halfway, at migration 20261025, and it is not safe to go on; restore the copy made before it: thaler cli restore {dir}/backups/pre-migration/thaler-20261025-090000-before-b.db",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := oldDatabase(t, atVersion(20261025, true)...)
			dir := filepath.Dir(path)
			base := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
			for i, c := range tc.copies {
				aged(t, dir, c, base, len(tc.copies)-i)
			}

			// when
			st, err := Open(t.Context(), path)

			// then
			if err == nil {
				st.Close()
			}
			checkErr(t, err, strings.ReplaceAll(tc.wantErr, "{dir}", dir), nil)
		})
	}
}
