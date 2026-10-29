package core

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/config"
)

// Restoring puts a backup in place of the database. It works on files and not on
// an open Service, for two reasons: it has to work when the current database is
// damaged or missing, and opening a database upgrades it, which is not wanted for
// one that is about to be replaced.

var (
	sqliteMagic = []byte("SQLite format 3\x00")
	ageMagic    = []byte("age-encryption.org/v1")
)

// RestoreResult says what a restore did.
type RestoreResult struct {
	// Replaced is where the database that was in the way is kept, or "" if there
	// was none.
	Replaced string
	// Upgrade is true when the backup was made by an older version, so the next
	// time the database is opened it is upgraded (after a copy of it, see Open).
	Upgrade bool
}

// BackupIsEncrypted says whether the file at path is an encrypted backup, so
// that a passphrase can be asked for before it is restored. It is an error for
// the file not to be a backup at all.
func BackupIsEncrypted(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("%s does not exist", path)
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	head := make([]byte, len(ageMagic))
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, ageMagic):
		return true, nil
	case bytes.HasPrefix(head, sqliteMagic):
		return false, nil
	}
	return false, fmt.Errorf("%s is not a backup: it is neither a SQLite database nor an age file", path)
}

// RestoreBackup replaces the database at dbPath with the backup at src, which is
// a plain backup or, with its passphrase, an encrypted one.
//
// The backup is read into a new file next to the database and checked first, so a
// backup that is damaged, that is not a Thaler database, or that was made by a
// newer version is refused with nothing changed. Only then is the database in the
// way moved aside, never deleted, into backups/replaced next to it; and only then
// is the new one put in its place. If any step fails, what was moved is moved
// back. Restoring the file that was set aside undoes a restore.
//
// Nothing else may be using the database: close every other Thaler first.
func RestoreBackup(ctx context.Context, src, dbPath, passphrase string) (RestoreResult, error) {
	var res RestoreResult
	if dbPath == ":memory:" || strings.ContainsAny(dbPath, "?#") {
		return res, fmt.Errorf("cannot restore into %q", dbPath)
	}
	encrypted, err := BackupIsEncrypted(src)
	if err != nil {
		return res, err
	}
	if encrypted && passphrase == "" {
		return res, fmt.Errorf("%s is encrypted: a passphrase is needed", src)
	}
	if a, errA := os.Stat(src); errA == nil {
		if b, errB := os.Stat(dbPath); errB == nil && os.SameFile(a, b) {
			return res, fmt.Errorf("cannot restore %s from itself", dbPath)
		}
	}
	if !encrypted {
		if wal, err := os.Stat(src + "-wal"); err == nil && wal.Size() > 0 {
			return res, fmt.Errorf("%s holds changes that are not in %s yet: open that database with Thaler once, or restore from a backup made with thaler cli backup", src+"-wal", src)
		}
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, err
	}
	staged, err := os.CreateTemp(dir, ".thaler-restore-*.db")
	if err != nil {
		return res, err
	}
	stagedPath := staged.Name()
	defer func() { // gone after the rename; this is for every way out before it
		for _, suffix := range dbFiles {
			os.Remove(stagedPath + suffix)
		}
	}()
	if err := fillStaged(staged, src, encrypted, passphrase); err != nil {
		return res, err
	}
	if res.Upgrade, err = checkRestorable(ctx, stagedPath, src); err != nil {
		return res, err
	}

	if _, err := os.Stat(dbPath); err == nil {
		if res.Replaced, err = setAside(ctx, dbPath); err != nil {
			return RestoreResult{}, err
		}
	}
	if err := os.Rename(stagedPath, dbPath); err != nil {
		if res.Replaced == "" {
			return RestoreResult{}, fmt.Errorf("nothing was changed: %w", err)
		}
		if back := moveFiles(res.Replaced, dbPath, dbFiles); back != nil {
			return RestoreResult{}, fmt.Errorf("could not put the backup in place (%w), and could not put the old database back either: it is at %s: %w", err, res.Replaced, back)
		}
		return RestoreResult{}, fmt.Errorf("nothing was changed: %w", err)
	}
	return res, nil
}

// fillStaged writes the contents of the backup src into the file staged and
// closes it.
func fillStaged(staged *os.File, src string, encrypted bool, passphrase string) error {
	var from io.Reader
	if encrypted {
		r, srcFile, err := openDecrypted(src, passphrase)
		if err != nil {
			staged.Close()
			return err
		}
		defer srcFile.Close()
		from = r
	} else {
		f, err := os.Open(src)
		if err != nil {
			staged.Close()
			return err
		}
		defer f.Close()
		from = f
	}
	if _, err := io.Copy(staged, from); err != nil {
		staged.Close()
		return fmt.Errorf("reading %s: %w", src, err)
	}
	return staged.Close()
}

// checkRestorable opens the staged copy without changing it and says whether it
// is a healthy database that this build can use, and whether it needs upgrading.
func checkRestorable(ctx context.Context, staged, src string) (upgrade bool, err error) {
	if err := verifySnapshot(ctx, staged); err != nil {
		return false, fmt.Errorf("%s is not a healthy database, and nothing was changed: %w", src, err)
	}
	files, err := migrationFiles()
	if err != nil {
		return false, err
	}
	db, err := sql.Open("sqlite", "file:"+uriPath.Replace(staged)+"?mode=ro&immutable=1")
	if err != nil {
		return false, err
	}
	defer db.Close()
	state, err := readMigrationState(ctx, db, files)
	if err != nil {
		return false, fmt.Errorf("%s cannot be read, and nothing was changed: %w", src, err)
	}
	// Other programs use user_version too, so a legacy-looking database also has
	// to have Thaler's own tables.
	var ours int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('accounts', 'transactions', 'postings')`).Scan(&ours); err != nil {
		return false, err
	}
	if !state.existing || ours != 3 {
		return false, fmt.Errorf("%s is a SQLite database but not a Thaler one, and nothing was changed", src)
	}
	known := map[string]bool{}
	for _, f := range files {
		known[f] = true
	}
	for name := range state.applied {
		if !known[name] {
			return false, fmt.Errorf("%s was made by a newer version of Thaler (it has migration %s): update thaler; nothing was changed", src, name)
		}
	}
	for _, f := range files {
		if !state.applied[f] {
			upgrade = true
		}
	}
	return upgrade, nil
}

// dbFiles are the files one SQLite database can be spread over.
var dbFiles = []string{"", "-wal", "-shm"}

// setAside moves the database at dbPath, and the files that go with it, into the
// replaced folder, and returns the new path of the database. The write-ahead log
// is first merged into the database where that is possible, so that the file
// kept is complete on its own.
func setAside(ctx context.Context, dbPath string) (string, error) {
	mergeLog(ctx, dbPath)
	dir := config.ReplacedDir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dest := freePath(filepath.Join(dir, fmt.Sprintf("thaler-%s-replaced.db", time.Now().Format("20060102-150405"))))
	if err := moveFiles(dbPath, dest, dbFiles); err != nil {
		// moveFiles has put back what it moved, unless it says otherwise.
		return "", err
	}
	return dest, nil
}

// moveFiles renames from+suffix to to+suffix for each suffix whose file exists. If
// one fails, the ones already moved are moved back, and the error says so if even
// that fails.
func moveFiles(from, to string, suffixes []string) error {
	var moved []string
	for _, suffix := range suffixes {
		if _, err := os.Stat(from + suffix); err != nil {
			continue
		}
		if err := os.Rename(from+suffix, to+suffix); err != nil {
			var stuck []error
			for _, m := range moved {
				if undo := os.Rename(to+m, from+m); undo != nil {
					stuck = append(stuck, undo)
				}
			}
			if len(stuck) > 0 {
				return fmt.Errorf("%w; and the files already moved could not be put back: %w", err, errors.Join(stuck...))
			}
			return fmt.Errorf("nothing was changed: %w", err)
		}
		moved = append(moved, suffix)
	}
	return nil
}

// mergeLog asks SQLite to fold the write-ahead log into the database file. It
// is best effort: a database that cannot be opened is set aside as it is.
func mergeLog(ctx context.Context, dbPath string) {
	db, err := sql.Open("sqlite", "file:"+uriPath.Replace(dbPath)+"?_pragma=busy_timeout(2000)")
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`) // best effort, as the comment says
}
