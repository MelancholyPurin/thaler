package core

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	msqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/melancholypurin/thaler/internal/config"
)

//go:embed dbmigrations/*.up.sql
var migrationFS embed.FS

// The schema is changed by dbmigrations/<version>_<name>.up.sql files, applied
// in version order by golang-migrate. A version is a timestamp: the existing
// files are dated (YYYYMMDD), new ones use YYYYMMDDHHMMSS, which sorts after every
// date because it is a bigger number. The database remembers the last version
// applied, and nothing else, so a file added with an earlier version than that
// would never run; a test lists every version for that reason (see
// migrationScriptTimestamps in the tests).
//
// There are no down migrations: a database is never taken back a version. Before
// an upgrade changes a database that holds data it is copied aside, and that copy
// is how to go back (see backupBeforeMigrating).

const migrationsDir = "dbmigrations"

// migrationFile is one file in the migrations folder.
type migrationFile struct {
	version uint
	name    string // the file name, e.g. 20261022_init.up.sql
}

var migrationFileName = regexp.MustCompile(`^(\d{8}|\d{14})_[a-z0-9_]+\.up\.sql$`)

// migrationFilesIn lists the migrations in dir of fsys, oldest version first.
func migrationFilesIn(fsys fs.FS, dir string) ([]migrationFile, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var files []migrationFile
	seen := map[uint]string{}
	for _, e := range entries {
		if !migrationFileName.MatchString(e.Name()) {
			return nil, fmt.Errorf("bad migration file name %q (want YYYYMMDDHHMMSS_name.up.sql, in lower case)", e.Name())
		}
		version, _ := strconv.ParseUint(e.Name()[:strings.IndexByte(e.Name(), '_')], 10, 64)
		if other, dup := seen[uint(version)]; dup {
			return nil, fmt.Errorf("migrations %s and %s have the same version", other, e.Name())
		}
		seen[uint(version)] = e.Name()
		files = append(files, migrationFile{version: uint(version), name: e.Name()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files, nil
}

// migrationFiles are the migrations built into this binary.
func migrationFiles() ([]migrationFile, error) { return migrationFilesIn(migrationFS, migrationsDir) }

// migrate brings the schema up to date with the migrations in this binary.
func (r *repository) migrate(ctx context.Context) error {
	return r.migrateFrom(ctx, migrationFS, migrationsDir)
}

// migrateFrom is migrate with the migrations of dir in fsys.
func (r *repository) migrateFrom(ctx context.Context, fsys fs.FS, dir string) error {
	files, err := migrationFilesIn(fsys, dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	state, err := readMigrationState(ctx, r.db)
	if err != nil {
		return err
	}
	if latest := files[len(files)-1].version; state.version > latest {
		return fmt.Errorf("database has migration %d that this build does not know; update thaler", state.version)
	}
	if state.dirty {
		return r.interruptedUpgrade(state)
	}
	var pending []migrationFile
	for _, f := range files {
		if f.version > state.version {
			pending = append(pending, f)
		}
	}

	// Nothing above has written anything: the copy is of the database as it was.
	if state.existing && len(pending) > 0 {
		if err := r.backupBeforeMigrating(ctx, pending[0].name); err != nil {
			return err
		}
	}
	// Only the table that records migrations changes, in one transaction, so a
	// database that is up to date needs no copy for it.
	if state.legacy {
		if err := convertMigrationTable(ctx, r.db, state.version); err != nil {
			return err
		}
	}
	if len(pending) == 0 {
		return nil
	}
	return r.applyMigrations(ctx, fsys, dir, files)
}

// applyMigrations runs the pending migrations with golang-migrate.
func (r *repository) applyMigrations(ctx context.Context, fsys fs.FS, dir string, files []migrationFile) error {
	src, err := iofs.New(fsys, dir)
	if err != nil {
		return err
	}
	drv, err := msqlite.WithInstance(r.db, &msqlite.Config{})
	if err != nil {
		return err
	}
	// m.Close is not called: it would close the connection pool, which is ours.
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		return err
	}
	err = m.Up()
	if err == nil || errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return r.recoverFailedMigration(fsys, dir, files, m, err)
}

// recoverFailedMigration deals with a migration that failed. golang-migrate marks
// the version it was applying as dirty before it runs a migration, and leaves it
// so if the migration fails, which would refuse every later start. When the error
// comes from the migration's own SQL, SQLite has rolled that migration back (each
// runs in one transaction), the schema is as it was before it, and the version
// can safely go back to the one before. Any other failure leaves the database
// marked as it is, and the next start refuses it and points to the copy.
func (r *repository) recoverFailedMigration(fsys fs.FS, dir string, files []migrationFile, m *migrate.Migrate, upErr error) error {
	version, dirty, err := m.Version()
	if err != nil || !dirty {
		return upErr
	}
	var dbErr *database.Error
	if !errors.As(upErr, &dbErr) {
		return upErr
	}
	for i, f := range files {
		if f.version != version {
			continue
		}
		body, readErr := fs.ReadFile(fsys, path.Join(dir, f.name))
		if readErr != nil || !bytes.Equal(dbErr.Query, body) {
			return upErr
		}
		previous := database.NilVersion
		if i > 0 {
			previous = int(files[i-1].version)
		}
		if err := m.Force(previous); err != nil {
			return fmt.Errorf("migration %s: %w (and the version could not be reset: %w)", f.name, dbErr.OrigErr, err)
		}
		return fmt.Errorf("migration %s: %w", f.name, dbErr.OrigErr)
	}
	return upErr
}

// interruptedUpgrade is the error for a database whose last upgrade did not
// finish. It says where the copy made before it is.
func (r *repository) interruptedUpgrade(state migrationState) error {
	msg := fmt.Sprintf("the last upgrade of the database stopped halfway, at migration %d, and it is not safe to go on", state.version)
	if r.path != ":memory:" {
		if copies, _ := ListBackups(config.PreMigrationDir(r.path)); len(copies) > 0 {
			return fmt.Errorf("%s; restore the copy made before it: thaler cli restore %s", msg, copies[0].Path)
		}
	}
	return errors.New(msg)
}

// migrationState is what a database says about its own schema.
type migrationState struct {
	version  uint // the last migration applied, 0 for none
	dirty    bool // a migration was started and not finished
	legacy   bool // tracked by file name, as before golang-migrate: converted when upgraded
	existing bool // it already has a schema, so it may hold data
}

// readMigrationState reads, and never writes, which migration the database
// behind db is at.
func readMigrationState(ctx context.Context, db dbtx) (migrationState, error) {
	var st migrationState
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('schema_migrations')`)
	if err != nil {
		return st, err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return st, err
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		return st, err
	}
	if err := rows.Err(); err != nil {
		return st, err
	}

	switch {
	case columns["name"]:
		return readLegacyMigrationState(ctx, db)
	case columns["version"]:
		var version int64
		err := db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &st.dirty)
		if errors.Is(err, sql.ErrNoRows) {
			return st, nil
		}
		if err != nil {
			return st, err
		}
		st.version = uint(max(version, 0))
		st.existing = version > 0 || st.dirty
	}
	return st, nil
}

// readLegacyMigrationState reads the table that recorded migrations by file
// name, before golang-migrate: the version is the newest of those files.
func readLegacyMigrationState(ctx context.Context, db dbtx) (migrationState, error) {
	st := migrationState{legacy: true}
	rows, err := db.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return st, err
		}
		prefix, _, _ := strings.Cut(name, "_")
		version, err := strconv.ParseUint(prefix, 10, 64)
		if err != nil {
			return st, fmt.Errorf("schema_migrations has %q, which is not a migration file name", name)
		}
		st.version = max(st.version, uint(version))
	}
	st.existing = st.version > 0
	return st, rows.Err()
}

// convertMigrationTable replaces the table that recorded migrations by file name
// with the one golang-migrate keeps: the number of the last migration applied.
func convertMigrationTable(ctx context.Context, db *sql.DB, version uint) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DROP TABLE schema_migrations`,
		`CREATE TABLE schema_migrations (version uint64, dirty bool)`,
		`CREATE UNIQUE INDEX version_unique ON schema_migrations (version)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, dirty) VALUES (?, 0)`, version); err != nil {
		return err
	}
	return tx.Commit()
}

// backupBeforeMigrating copies the database aside before the migration named
// first changes it. If the copy cannot be made, the migration does not run: an
// upgrade that cannot be undone is worse than one that waits.
func (r *repository) backupBeforeMigrating(ctx context.Context, first string) error {
	if r.path == ":memory:" {
		return nil
	}
	name := fmt.Sprintf("thaler-%s-before-%s.db", time.Now().Format("20060102-150405"), strings.TrimSuffix(first, ".up.sql"))
	dest := freePath(filepath.Join(config.PreMigrationDir(r.path), name))
	if err := snapshot(ctx, queries{r.db}, dest); err != nil {
		return fmt.Errorf("cannot upgrade the database without a copy of it first, and nothing was changed: %w", err)
	}
	r.migrationBackup = dest
	return nil
}

// freePath is path, or the first of path-2, path-3, ... (before the extension)
// that does not exist yet.
func freePath(path string) string {
	if _, err := os.Stat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
}
