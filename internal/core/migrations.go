package core

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.up.sql
var migrationFS embed.FS

// migrate applies migrations/YYYYMMDD_name.up.sql files in name order. Applied
// migrations are recorded by file name in schema_migrations, so a migration
// that lands on a branch with an earlier date than one already applied is still
// picked up. Each migration runs in its own transaction.
func (r *repository) migrate(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
	)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		date, _, ok := strings.Cut(e.Name(), "_")
		if _, convErr := strconv.Atoi(date); !ok || convErr != nil || len(date) != 8 || !strings.HasSuffix(e.Name(), ".up.sql") {
			return fmt.Errorf("bad migration file name %q (want YYYYMMDD_name.up.sql)", e.Name())
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	// Databases created before schema_migrations existed tracked their version
	// in PRAGMA user_version; the only migration then was the initial schema.
	var legacy int
	if err := r.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&legacy); err != nil {
		return err
	}
	if legacy > 0 && len(files) > 0 {
		if _, err := r.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (name) VALUES (?)`, files[0]); err != nil {
			return err
		}
	}

	applied := map[string]bool{}
	rows, err := r.db.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		applied[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	known := map[string]bool{}
	for _, f := range files {
		known[f] = true
	}
	for name := range applied {
		if !known[name] {
			return fmt.Errorf("database has migration %s that this build does not know; update thaler", name)
		}
	}

	for _, f := range files {
		if applied[f] {
			continue
		}
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
