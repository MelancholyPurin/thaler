package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
)

// backupModel is a model whose database is "{dir}/thaler.db" and whose backups
// folder is "{dir}/backups", on a ledger with one transaction in it.
func backupModel(t *testing.T, dir string) *Model {
	t.Helper()
	st := openLedger(t)
	openAccounts(t, st, "Assets:Bank EUR")
	book(t, st, "2026-10-03", "Kino", "Assets:Bank", "Expenses:Fun", -1200)
	m, err := newWithClock(ctx, st, Options{
		LoadConfig:   testConfig,
		DatabasePath: filepath.Join(dir, "thaler.db"),
		BackupDir:    filepath.Join(dir, "backups"),
	}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return m
}

// openFile opens the database file at path, as a restore would.
func openFile(t *testing.T, path string) *core.Local {
	t.Helper()
	st, err := core.Open(ctx, path)
	if err != nil {
		t.Fatalf("the backup does not open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// filesUnder lists the files below dir, relative to it.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var (
	sizeInStatus = regexp.MustCompile(`\(\d+(\.\d)? (B|KB|MB),`)
	pathInStatus = regexp.MustCompile(`→ .*?([^/]+)$`)
)

// The S popup writes a snapshot of the database, checks it, and says where it is.
func Test_Update_backup(t *testing.T) {
	type observed struct {
		Mode   string
		Dest   string // what the popup would write to, while it is open
		Err    string
		Status string
		Files  []string // everything under the folder, afterwards
	}
	tcs := []struct {
		name     string
		existing map[string]string // files to create first, relative to the folder
		script   func(dir string) string
		want     observed
	}{
		{
			name: "S opens the popup on a timestamped file in the backups folder", script: func(string) string { return "S" },
			want: observed{Mode: "backup", Dest: "{dir}/backups/thaler-20261022-120000.db"},
		},
		{
			name: "enter writes it", script: func(string) string { return "S enter" },
			want: observed{
				Mode: "list", Status: "Backed up ({size}, checked, not encrypted) → …thaler-20261022-120000.db",
				Files: []string{"backups/thaler-20261022-120000.db"},
			},
		},
		{
			name: "ctrl+s does too", script: func(string) string { return "S ctrl+s" },
			want: observed{
				Mode: "list", Status: "Backed up ({size}, checked, not encrypted) → …thaler-20261022-120000.db",
				Files: []string{"backups/thaler-20261022-120000.db"},
			},
		},
		{
			name: "esc writes nothing", script: func(string) string { return "S esc" },
			want: observed{Mode: "list"},
		},
		{
			name: "a path can be typed instead",
			script: func(dir string) string {
				return "S ctrl+u raw:" + filepath.Join(dir, "elsewhere", "mine.db") + " enter"
			},
			want: observed{
				Mode: "list", Status: "Backed up ({size}, checked, not encrypted) → …mine.db",
				Files: []string{"elsewhere/mine.db"},
			},
		},
		{
			name:   "a folder gets the timestamped name inside it",
			script: func(dir string) string { return "S ctrl+u raw:" + dir + " enter" },
			want: observed{
				Mode: "list", Status: "Backed up ({size}, checked, not encrypted) → …thaler-20261022-120000.db",
				Files: []string{"thaler-20261022-120000.db"},
			},
		},
		{
			name:   "~ is the home folder",
			script: func(string) string { return "S ctrl+u raw:~/saved.db enter" },
			want: observed{
				Mode: "list", Status: "Backed up ({size}, checked, not encrypted) → …saved.db",
				Files: []string{"saved.db"},
			},
		},
		{
			name:     "a backup that exists is never overwritten",
			existing: map[string]string{"backups/thaler-20261022-120000.db": "precious"},
			script:   func(string) string { return "S enter" },
			want: observed{
				Mode: "backup", Dest: "{dir}/backups/thaler-20261022-120000.db",
				Err:   "{dir}/backups/thaler-20261022-120000.db already exists",
				Files: []string{"backups/thaler-20261022-120000.db"},
			},
		},
		{
			name: "an empty path is refused", script: func(string) string { return "S ctrl+u enter" },
			want: observed{Mode: "backup", Err: "say where to write the backup"},
		},
		{
			name:   "typing clears the message, so it can be tried again",
			script: func(dir string) string { return "S ctrl+u enter raw:" + filepath.Join(dir, "x.db") + " enter" },
			want: observed{
				Mode: "list", Status: "Backed up ({size}, checked, not encrypted) → …x.db",
				Files: []string{"x.db"},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			m := backupModel(t, dir)
			for name, content := range tc.existing {
				p := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			play(m, tc.script(dir))

			// then
			got := observed{Mode: modeNames[m.mode], Status: m.status}
			if m.bkp != nil {
				got.Dest, got.Err = m.bkp.dest.Value(), m.bkp.err
			}
			got.Status = sizeInStatus.ReplaceAllString(got.Status, "({size},")
			got.Status = pathInStatus.ReplaceAllString(got.Status, "→ …$1")
			for _, p := range []*string{&got.Dest, &got.Err} {
				*p = strings.ReplaceAll(*p, dir, "{dir}")
			}
			got.Files = filesUnder(t, dir)
			// the database itself is not on disk: it is in memory in these tests
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if tc.existing != nil {
				for name, content := range tc.existing {
					if diff := cmp.Diff(content, readFileString(t, filepath.Join(dir, name))); diff != "" {
						t.Errorf("the existing file was touched (-want +got):\n%s", diff)
					}
				}
			}
		})
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A backup that was written can be opened as a ledger holding the same data.
func Test_Update_backup_contents(t *testing.T) {
	// given
	dir := t.TempDir()
	m := backupModel(t, dir)

	// when
	play(m, "S enter")

	// then
	restored := openFile(t, filepath.Join(dir, "backups", "thaler-20261022-120000.db"))
	got := storedLines(t, restored)
	want := []string{"2026-10-03 Kino | Assets:Bank -12.00 | Expenses:Fun 12.00"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the backup does not hold the ledger (-want +got):\n%s", diff)
	}
}

// Without a known backups folder S says so instead of writing somewhere.
func Test_Update_backup_unknownFolder(t *testing.T) {
	// given
	m, _ := newModel(t)

	// when
	play(m, "S")

	// then
	got := Flow{Mode: modeNames[m.mode], Status: m.status, StatusIsError: m.statusIsError}
	want := Flow{Mode: "list", Status: "no backup location is known", StatusIsError: true}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

// The popup tells you when the last backup was made.
func Test_backupPopup(t *testing.T) {
	day := func(d int) time.Time { return fixedNow().AddDate(0, 0, -d) }
	tcs := []struct {
		name  string
		files map[string]time.Time // in the backups folder
		other map[string]time.Time // elsewhere in it, not backups
		shows []string
		hides []string
	}{
		{
			name:  "none yet",
			shows: []string{"Last backup", "never", "Back up the database", "NOT encrypted"},
			hides: []string{"1 backup", "· 0 backups"},
		},
		{
			name:  "one, from today",
			files: map[string]time.Time{"thaler-20261022-090000.db": day(0)},
			shows: []string{"· today ·", "· 1 backup"},
			hides: []string{"never", "2 backups"},
		},
		{
			name: "the newest of several is the last one",
			files: map[string]time.Time{
				"thaler-20261001-090000.db": day(21), "thaler-20261019-090000.db": day(3), "thaler-20260920-090000.db": day(32),
			},
			shows: []string{"· 3 days ago ·", "· 3 backups"},
			hides: []string{"21 days ago", "never"},
		},
		{
			name:  "yesterday",
			files: map[string]time.Time{"thaler-20261021-090000.db": day(1)},
			shows: []string{"· yesterday ·"},
		},
		{
			name:  "files that are not backups are not counted",
			files: map[string]time.Time{"thaler-20261019-090000.db": day(3)},
			other: map[string]time.Time{"notes.txt": day(0), "thaler-20261019-090000.db.bak": day(0)},
			shows: []string{"· 3 days ago ·", "· 1 backup"},
			hides: []string{"today"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			m := backupModel(t, dir)
			for _, group := range []map[string]time.Time{tc.files, tc.other} {
				for name, when := range group {
					p := filepath.Join(dir, "backups", name)
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
			}

			// when
			play(m, "S")

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

func Test_ago(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	tcs := []struct {
		name string
		t    time.Time
		want string
	}{
		{name: "this morning", t: time.Date(2026, 10, 22, 0, 5, 0, 0, time.UTC), want: "today"},
		{name: "later today", t: time.Date(2026, 10, 22, 23, 59, 0, 0, time.UTC), want: "today"},
		{name: "last night counts as yesterday", t: time.Date(2026, 10, 21, 23, 59, 0, 0, time.UTC), want: "yesterday"},
		{name: "the day before", t: time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC), want: "2 days ago"},
		{name: "across a month", t: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), want: "30 days ago"},
		{name: "a clock set ahead is not negative", t: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC), want: "today"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := ago(now, tc.t)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_humanSize(t *testing.T) {
	tcs := []struct {
		name string
		n    int64
		want string
	}{
		{name: "nothing", n: 0, want: "0 B"},
		{name: "bytes", n: 1023, want: "1023 B"},
		{name: "a kilobyte", n: 1024, want: "1.0 KB"},
		{name: "kilobytes", n: 80 * 1024, want: "80.0 KB"},
		{name: "just under a megabyte", n: 1024*1024 - 1, want: "1024.0 KB"},
		{name: "a megabyte", n: 1024 * 1024, want: "1.0 MB"},
		{name: "megabytes", n: 3*1024*1024 + 104858, want: "3.1 MB"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := humanSize(tc.n)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_tailFit(t *testing.T) {
	tcs := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{name: "fits", s: "/a/b.db", w: 20, want: "/a/b.db"},
		{name: "exactly fits", s: "/a/b.db", w: 7, want: "/a/b.db"},
		{name: "cut from the left, keeping the file name", s: "/home/me/backups/thaler.db", w: 12, want: "…s/thaler.db"},
		{name: "counts characters, not bytes", s: "/Bücher/Konto.db", w: 9, want: "…Konto.db"},
		{name: "no room at all", s: "/a/b.db", w: 1, want: "/a/b.db"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := tailFit(tc.s, tc.w)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
