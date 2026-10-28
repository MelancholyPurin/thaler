package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/melancholypurin/thaler/internal/core"
)

// seeded is a backup to put in the backups folder before a test: a ledger with
// one transaction by payee, written to name there. A name ending in .age is
// encrypted with the passphrase "pw"; payee "damaged" makes a file that is not a
// database.
type seeded struct {
	name  string
	payee string
	when  time.Time
}

var (
	day20 = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	day21 = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
)

// makeBackup writes b under dir/backups, with its modification time set.
func makeBackup(t *testing.T, dir string, b seeded) {
	t.Helper()
	dest := filepath.Join(dir, "backups", b.name)
	if b.payee == "damaged" {
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte("this is not a database, at all, not even close"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else {
		st, err := core.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
		if err != nil {
			t.Fatal(err)
		}
		openAccounts(t, st, "Assets:Bank EUR")
		book(t, st, "2026-10-01", b.payee, "Assets:Bank", "Expenses:Fun", -500)
		if strings.HasSuffix(b.name, ".age") {
			err = st.BackupEncrypted(ctx, dest, "pw")
		} else {
			err = st.Backup(ctx, dest)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dest, b.when, b.when); err != nil {
		t.Fatal(err)
	}
}

// restoreModel is a model on the file "{dir}/thaler.db" holding one transaction
// by Kino, whose Restore does what the command does: close the database, restore,
// open what is in place. The service it ends with is closed after the test.
func restoreModel(t *testing.T, dir string, backups ...seeded) *Model {
	t.Helper()
	cheapScrypt(t)
	dbPath := filepath.Join(dir, "thaler.db")
	st, err := core.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	openAccounts(t, st, "Assets:Bank EUR")
	book(t, st, "2026-10-03", "Kino", "Assets:Bank", "Expenses:Fun", -1200)
	var current core.Service = st
	t.Cleanup(func() { current.Close() })
	for _, b := range backups {
		makeBackup(t, dir, b)
	}
	m, err := newWithClock(ctx, st, Options{
		LoadConfig:   testConfig,
		DatabasePath: dbPath,
		BackupDir:    filepath.Join(dir, "backups"),
		Restore: func(ctx context.Context, src, passphrase string) (core.Service, core.RestoreResult, error) {
			current.Close()
			res, restoreErr := core.RestoreBackup(ctx, src, dbPath, passphrase)
			svc, err := core.Open(ctx, dbPath)
			if err != nil {
				return nil, res, err
			}
			current = svc
			return svc, res, restoreErr
		},
	}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

var cmpSorted = cmpopts.SortSlices(func(a, b string) bool { return a < b })

var (
	replacedName = regexp.MustCompile(`thaler-\d{8}-\d{6}-replaced(-\d+)?`)
	sideFile     = regexp.MustCompile(`-(wal|shm)$`)
)

func Test_Update_restore(t *testing.T) {
	type observed struct {
		Mode     string
		Selected string // the backup the cursor is on, while the popup is open
		Confirm  bool
		Err      string
		Status   string
		Ledger   []string // the transactions in the database the interface is using
		Files    []string // under the folder, except the files SQLite keeps beside an open database
	}
	kino := []string{"2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Fun 12.00"}
	paid := func(payee string) []string {
		return []string{"2026-10-01 " + payee + " | Assets:Bank -5.00 | Expenses:Fun 5.00"}
	}
	oldest := seeded{"thaler-20261020-090000.db", "Bäcker", day20}
	newest := seeded{"thaler-20261021-090000.db", "Metzger", day21}
	locked := seeded{"thaler-20261021-090000.db.age", "Gärtner", day21}
	plain := []string{"backups/thaler-20261020-090000.db", "backups/thaler-20261021-090000.db", "thaler.db"}
	tcs := []struct {
		name   string
		seeds  []seeded
		script string
		noHook bool
		want   observed
	}{
		{
			name: "ctrl+r opens the popup on the newest backup", seeds: []seeded{oldest, newest}, script: "ctrl+r",
			want: observed{Mode: "restore", Selected: "thaler-20261021-090000.db", Ledger: kino, Files: plain},
		},
		{
			name: "down chooses an older one", seeds: []seeded{oldest, newest}, script: "ctrl+r down",
			want: observed{Mode: "restore", Selected: "thaler-20261020-090000.db", Ledger: kino, Files: plain},
		},
		{
			name: "down stops at the last", seeds: []seeded{oldest, newest}, script: "ctrl+r down down down",
			want: observed{Mode: "restore", Selected: "thaler-20261020-090000.db", Ledger: kino, Files: plain},
		},
		{
			name: "enter asks before anything changes", seeds: []seeded{oldest, newest}, script: "ctrl+r enter",
			want: observed{Mode: "restore", Selected: "thaler-20261021-090000.db", Confirm: true, Ledger: kino, Files: plain},
		},
		{
			name: "esc at the question goes back to the list", seeds: []seeded{oldest, newest}, script: "ctrl+r enter esc",
			want: observed{Mode: "restore", Selected: "thaler-20261021-090000.db", Ledger: kino, Files: plain},
		},
		{
			name: "esc closes the popup", seeds: []seeded{oldest, newest}, script: "ctrl+r esc",
			want: observed{Mode: "list", Ledger: kino, Files: plain},
		},
		{
			name: "enter twice restores it, and keeps the database it replaced", seeds: []seeded{oldest, newest}, script: "ctrl+r enter enter",
			want: observed{
				Mode: "list", Status: "Restored thaler-20261021-090000.db · the database before is kept: ctrl+r lists it",
				Ledger: paid("Metzger"),
				Files:  append(plain, "backups/replaced/thaler-{time}-replaced.db"),
			},
		},
		{
			name: "y answers the question too", seeds: []seeded{oldest, newest}, script: "ctrl+r enter y",
			want: observed{
				Mode: "list", Status: "Restored thaler-20261021-090000.db · the database before is kept: ctrl+r lists it",
				Ledger: paid("Metzger"),
				Files:  append(plain, "backups/replaced/thaler-{time}-replaced.db"),
			},
		},
		{
			name: "n does not", seeds: []seeded{oldest, newest}, script: "ctrl+r enter n",
			want: observed{Mode: "restore", Selected: "thaler-20261021-090000.db", Ledger: kino, Files: plain},
		},
		{
			name: "an older backup can be chosen", seeds: []seeded{oldest, newest}, script: "ctrl+r down enter enter",
			want: observed{
				Mode: "list", Status: "Restored thaler-20261020-090000.db · the database before is kept: ctrl+r lists it",
				Ledger: paid("Bäcker"),
				Files:  append(plain, "backups/replaced/thaler-{time}-replaced.db"),
			},
		},
		{
			name: "no backups: there is nothing to restore", script: "ctrl+r enter",
			want: observed{Mode: "restore", Err: "there is no backup to restore: S makes one", Ledger: kino, Files: []string{"thaler.db"}},
		},
		{
			name: "an encrypted backup asks for its passphrase first", seeds: []seeded{locked}, script: "ctrl+r enter",
			want: observed{
				Mode: "restore", Selected: "thaler-20261021-090000.db.age", Err: "thaler-20261021-090000.db.age is encrypted: type its passphrase",
				Ledger: kino, Files: []string{"backups/thaler-20261021-090000.db.age", "thaler.db"},
			},
		},
		{
			name: "and restores with it", seeds: []seeded{locked}, script: "ctrl+r tab text:pw enter enter",
			want: observed{
				Mode: "list", Status: "Restored thaler-20261021-090000.db.age · the database before is kept: ctrl+r lists it",
				Ledger: paid("Gärtner"),
				Files:  []string{"backups/replaced/thaler-{time}-replaced.db", "backups/thaler-20261021-090000.db.age", "thaler.db"},
			},
		},
		{
			name: "a wrong passphrase changes nothing, and can be retyped", seeds: []seeded{locked}, script: "ctrl+r tab text:nope enter enter",
			want: observed{
				Mode: "restore", Selected: "thaler-20261021-090000.db.age", Err: "decrypting {dir}/backups/thaler-20261021-090000.db.age: incorrect passphrase",
				Ledger: kino, Files: []string{"backups/thaler-20261021-090000.db.age", "thaler.db"},
			},
		},
		{
			name: "retyped, it restores", seeds: []seeded{locked}, script: "ctrl+r tab text:nope enter enter ctrl+u text:pw enter enter",
			want: observed{
				Mode: "list", Status: "Restored thaler-20261021-090000.db.age · the database before is kept: ctrl+r lists it",
				Ledger: paid("Gärtner"),
				Files:  []string{"backups/replaced/thaler-{time}-replaced.db", "backups/thaler-20261021-090000.db.age", "thaler.db"},
			},
		},
		{
			name:  "a damaged backup is refused, and the database stays",
			seeds: []seeded{{"thaler-20261021-090000.db", "damaged", day21}}, script: "ctrl+r enter enter",
			want: observed{
				Mode: "restore", Selected: "thaler-20261021-090000.db", Err: "{dir}/backups/thaler-20261021-090000.db is not a backup: it is neither a SQLite database nor an age file",
				Ledger: kino, Files: []string{"backups/thaler-20261021-090000.db", "thaler.db"},
			},
		},
		{
			name: "a restore can be taken back from the replaced database", seeds: []seeded{newest}, script: "ctrl+r enter enter ctrl+r down enter enter",
			want: observed{
				Mode: "list", Status: "Restored thaler-{time}-replaced.db · the database before is kept: ctrl+r lists it",
				Ledger: kino,
				Files: []string{
					"backups/replaced/thaler-{time}-replaced.db", "backups/replaced/thaler-{time}-replaced-2.db",
					"backups/thaler-20261021-090000.db", "thaler.db",
				},
			},
		},
		{
			name: "undo does not reach back across a restore", seeds: []seeded{newest}, script: "3 d y ctrl+r enter enter z",
			want: observed{
				Mode: "list", Status: "nothing to undo", Ledger: paid("Metzger"),
				Files: []string{"backups/replaced/thaler-{time}-replaced.db", "backups/thaler-20261021-090000.db", "thaler.db"},
			},
		},
		{
			name: "without a way to restore, it says so", seeds: []seeded{newest}, script: "ctrl+r", noHook: true,
			want: observed{Mode: "list", Status: "restoring is not possible here: this database is not a file", Ledger: kino, Files: []string{"backups/thaler-20261021-090000.db", "thaler.db"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			m := restoreModel(t, dir, tc.seeds...)
			if tc.noHook {
				m.opts.Restore = nil
			}

			// when
			play(m, tc.script)

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status, Ledger: storedLines(t, m.svc)}
			if m.rst != nil {
				got.Err, got.Confirm = m.rst.err, m.rst.confirm
				if len(m.rst.files) > 0 {
					got.Selected = filepath.Base(m.rst.selected().path)
				}
			}
			got.Err = strings.ReplaceAll(got.Err, dir, "{dir}")
			got.Status = replacedName.ReplaceAllString(strings.ReplaceAll(got.Status, dir, "{dir}"), "thaler-{time}-replaced$1")
			for _, f := range filesUnder(t, dir) {
				if !sideFile.MatchString(f) {
					got.Files = append(got.Files, replacedName.ReplaceAllString(f, "thaler-{time}-replaced$1"))
				}
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty, cmpSorted); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// What the screen shows follows the database that was put in place.
func Test_Update_restore_refreshes(t *testing.T) {
	// given
	dir := t.TempDir()
	m := restoreModel(t, dir, seeded{"thaler-20261021-090000.db", "Metzger", day21})
	before := visibleRows(m)

	// when
	play(m, "ctrl+r enter enter")

	// then
	got := map[string][]string{"before": before, "after": visibleRows(m)}
	want := map[string][]string{
		"before": {"2026-10-03 Kino -12.00"},
		"after":  {"2026-10-01 Metzger -5.00"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
	if m.undo != nil || m.lastAccount != "" {
		t.Errorf("what was done before the restore can still be undone: %v", m.undo)
	}
}

// If nothing can be opened after a restore there is no database to show, so the
// interface ends with the reason.
func Test_Update_restore_ends(t *testing.T) {
	// given
	dir := t.TempDir()
	m := restoreModel(t, dir, seeded{"thaler-20261021-090000.db", "Metzger", day21})
	m.opts.Restore = func(context.Context, string, string) (core.Service, core.RestoreResult, error) {
		return nil, core.RestoreResult{}, errors.New("disk on fire")
	}

	// when
	play(m, "ctrl+r enter")
	_, cmd := m.Update(key("enter"))

	// then
	type observed struct {
		Quit  bool
		Fatal string
	}
	got := observed{Quit: cmd != nil && cmd() == tea.Quit(), Fatal: strings.ReplaceAll(m.fatal.Error(), dir, "{dir}")}
	want := observed{Quit: true, Fatal: "restoring {dir}/backups/thaler-20261021-090000.db: disk on fire"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func Test_restorePopup(t *testing.T) {
	day := func(d int) time.Time { return fixedNow().AddDate(0, 0, -d) }
	tcs := []struct {
		name   string
		files  map[string]time.Time // below the folder, as empty files
		script string
		shows  []string
		hides  []string
	}{
		{
			name:   "none yet",
			script: "ctrl+r",
			shows:  []string{"Restore a backup", "No backups yet", "Passphrase", "kept, not deleted"},
		},
		{
			name: "backups, newest first, and what is plain or encrypted",
			files: map[string]time.Time{
				"backups/thaler-20261019-090000.db": day(3), "backups/thaler-20261021-090000.db.age": day(1),
			},
			script: "ctrl+r",
			shows:  []string{"▸ ", "· yesterday · 1 B · encrypted", "· 3 days ago · 1 B"},
			hides:  []string{"No backups yet", "replaced by a restore"},
		},
		{
			name: "what a restore replaced comes last, marked",
			files: map[string]time.Time{
				"backups/thaler-20261019-090000.db":                    day(3),
				"backups/replaced/thaler-20261022-110000-replaced.db":  day(0),
				"backups/pre-migration/thaler-20261022-before-0001.db": day(0),
				"backups/thaler-20261022-090000.db.bak":                day(0),
			},
			script: "ctrl+r",
			shows:  []string{"▸ ", "· 3 days ago · 1 B", "· today · 1 B · replaced by a restore"},
			hides:  []string{"No backups yet", "before-0001"},
		},
		{
			name:   "the question names the file and where the old database goes",
			files:  map[string]time.Time{"backups/thaler-20261019-090000.db": day(3)},
			script: "ctrl+r enter",
			shows:  []string{"Replace the database with", "thaler-20261019-090000.db", "backups/replaced", "can be restored from here the same way", "enter yes, restore"},
			hides:  []string{"Passphrase"},
		},
		{
			name:   "the passphrase is dots",
			files:  map[string]time.Time{"backups/thaler-20261019-090000.db.age": day(3)},
			script: "ctrl+r tab text:secret",
			shows:  []string{"▸ Passphrase", "••••••"},
			hides:  []string{"secret"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			m := restoreModel(t, dir)
			for name, when := range tc.files {
				p := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(p, when, when); err != nil {
					t.Fatal(err)
				}
			}

			// when
			play(m, tc.script)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
