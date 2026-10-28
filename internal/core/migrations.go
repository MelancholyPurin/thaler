package core

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.up.sql
var migrationFS embed.FS

// migrate applies migrations/YYYYMMDD_name.up.sql files in name order. Applied
// migrations are recorded by file name in schema_migrations, so a migration
// that lands on a branch with an earlier date than one already applied is still
// picked up. Each migration runs in its own transaction.
//
// A database that already holds data is copied aside before the first pending
// migration touches it (see backupBeforeMigrating).
func (r *repository) migrate(ctx context.Context) error {
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	state, err := readMigrationState(ctx, r.db, files)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, f := range files {
		known[f] = true
	}
	for name := range state.applied {
		if !known[name] {
			return fmt.Errorf("database has migration %s that this build does not know; update thaler", name)
		}
	}
	var pending []string
	for _, f := range files {
		if !state.applied[f] {
			pending = append(pending, f)
		}
	}

	// Nothing above has written anything: the copy is of the database as it was.
	if state.existing && len(pending) > 0 {
		if err := r.backupBeforeMigrating(ctx, pending[0]); err != nil {
			return err
		}
	}

	if _, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
	)`); err != nil {
		return err
	}
	if state.legacy && len(files) > 0 {
		if _, err := r.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (name) VALUES (?)`, files[0]); err != nil {
			return err
		}
	}
	for _, f := range pending {
		body, err := migrationFS.ReadFile("migrations/" + f)
		if err != nil {
			return err
		}
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", f, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES (?)`, f); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", f, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s: %w", f, err)
		}
	}
	return nil
}

// migrationFiles is the embedded migrations, in the order they are applied.
func migrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		date, _, ok := strings.Cut(e.Name(), "_")
		if _, convErr := strconv.Atoi(date); !ok || convErr != nil || len(date) != 8 || !strings.HasSuffix(e.Name(), ".up.sql") {
			return nil, fmt.Errorf("bad migration file name %q (want YYYYMMDD_name.up.sql)", e.Name())
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}

// migrationState is what a database says about its own schema.
type migrationState struct {
	applied  map[string]bool
	legacy   bool // from before migrations were tracked: adopted at the initial schema
	existing bool // it already has a schema, so it may hold data
}

// readMigrationState reads, and never writes, which migrations the database
// behind db has applied.
func readMigrationState(ctx context.Context, db dbtx, files []string) (migrationState, error) {
	st := migrationState{applied: map[string]bool{}}

	// Databases created before schema_migrations existed tracked their version
	// in PRAGMA user_version; the only migration then was the initial schema.
	var legacy int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&legacy); err != nil {
		return st, err
	}
	if legacy > 0 && len(files) > 0 {
		st.legacy = true
		st.applied[files[0]] = true
	}

	var tracked int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&tracked); err != nil {
		return st, err
	}
	if tracked > 0 {
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
			st.applied[name] = true
		}
		if err := rows.Err(); err != nil {
			return st, err
		}
	}
	st.existing = len(st.applied) > 0
	return st, nil
}

// preMigrationDir is where the copies made before an upgrade go: a folder of its
// own inside the backups folder next to the database (see config.BackupDir), so
// that they are not mistaken for the backups you made yourself.
func preMigrationDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "backups", "pre-migration")
}

// backupBeforeMigrating copies the database aside before the migration named
// first changes it. If the copy cannot be made, the migration does not run: an
// upgrade that cannot be undone is worse than one that waits.
func (r *repository) backupBeforeMigrating(ctx context.Context, first string) error {
	if r.path == ":memory:" {
		return nil
	}
	name := fmt.Sprintf("thaler-%s-before-%s.db", time.Now().Format("20060102-150405"), strings.TrimSuffix(first, ".up.sql"))
	dest := freePath(filepath.Join(preMigrationDir(r.path), name))
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
