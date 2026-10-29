package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// aged writes a small file below dir, last changed days days before base.
func aged(t *testing.T, dir, name string, base time.Time, days int) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := base.AddDate(0, 0, -days)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

func Test_PlanPrune(t *testing.T) {
	type observed struct {
		Folders map[string][]string // label -> what would be removed, newest first
		Totals  map[string]int
		Count   int
		Size    int64
	}
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	tcs := []struct {
		name    string
		files   map[string]int // below the folder holding the database, and how many days old
		keep    int
		want    observed
		wantErr string
	}{
		{
			name:  "nothing to remove while there are no more than are kept",
			files: map[string]int{"backups/thaler-20261020-090000.db": 2, "backups/thaler-20261021-090000.db": 1},
			keep:  2,
			want:  observed{Folders: map[string][]string{}, Totals: map[string]int{}},
		},
		{
			name: "the oldest go, the newest stay",
			files: map[string]int{
				"backups/thaler-20261001-090000.db": 21, "backups/thaler-20261019-090000.db": 3,
				"backups/thaler-20261010-090000.db": 12, "backups/thaler-20261021-090000.db": 1,
			},
			keep: 2,
			want: observed{
				Folders: map[string][]string{"backups": {"thaler-20261010-090000.db", "thaler-20261001-090000.db"}},
				Totals:  map[string]int{"backups": 4}, Count: 2, Size: 10,
			},
		},
		{
			name: "age is the modification time, not the name",
			files: map[string]int{
				"backups/thaler-20260101-090000.db": 0, "backups/thaler-20261021-090000.db": 40,
			},
			keep: 1,
			want: observed{
				Folders: map[string][]string{"backups": {"thaler-20261021-090000.db"}},
				Totals:  map[string]int{"backups": 2}, Count: 1, Size: 5,
			},
		},
		{
			name: "plain and encrypted backups count together",
			files: map[string]int{
				"backups/thaler-20261021-090000.db.age": 1, "backups/thaler-20261020-090000.db": 2, "backups/thaler-20261019-090000.db.age": 3,
			},
			keep: 2,
			want: observed{
				Folders: map[string][]string{"backups": {"thaler-20261019-090000.db.age"}},
				Totals:  map[string]int{"backups": 3}, Count: 1, Size: 5,
			},
		},
		{
			name: "each folder keeps its own newest",
			files: map[string]int{
				"backups/thaler-20261021-090000.db": 1, "backups/thaler-20261020-090000.db": 2,
				"backups/replaced/thaler-20261021-100000-replaced.db": 1, "backups/replaced/thaler-20261019-100000-replaced.db": 3,
				"backups/pre-migration/thaler-20261021-110000-before-a.db": 1, "backups/pre-migration/thaler-20261001-110000-before-b.db": 21,
			},
			keep: 1,
			want: observed{
				Folders: map[string][]string{
					"backups":                  {"thaler-20261020-090000.db"},
					"replaced by a restore":    {"thaler-20261019-100000-replaced.db"},
					"copies before an upgrade": {"thaler-20261001-110000-before-b.db"},
				},
				Totals: map[string]int{"backups": 2, "replaced by a restore": 2, "copies before an upgrade": 2}, Count: 3, Size: 15,
			},
		},
		{
			name: "only files named like a backup are listed",
			files: map[string]int{
				"backups/thaler-20261021-090000.db": 1, "backups/thaler-20261020-090000.db": 2,
				"backups/notes.txt": 50, "backups/thaler-20261019-090000.db.bak": 60, "backups/mine.db": 70, "backups/thaler-x.db/inside.db": 80,
			},
			keep: 1,
			want: observed{
				Folders: map[string][]string{"backups": {"thaler-20261020-090000.db"}},
				Totals:  map[string]int{"backups": 2}, Count: 1, Size: 5,
			},
		},
		{name: "keeping none is refused", keep: 0, wantErr: "keep 0 backups: it must be at least 1"},
		{name: "so is a negative number", keep: -1, wantErr: "keep -1 backups: it must be at least 1"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			for name, days := range tc.files {
				aged(t, dir, name, base, days)
			}

			// when
			plan, err := PlanPrune(filepath.Join(dir, "thaler.db"), tc.keep)

			// then
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if diff := cmp.Diff(tc.wantErr, gotErr); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			if err != nil {
				return
			}
			got := observed{Folders: map[string][]string{}, Totals: map[string]int{}, Count: plan.Count(), Size: plan.Size()}
			for _, f := range plan.Folders {
				got.Totals[f.Label] = f.Total
				for _, file := range f.Remove {
					got.Folders[f.Label] = append(got.Folders[f.Label], filepath.Base(file.Path))
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Prune(t *testing.T) {
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	type observed struct {
		Removed int
		Err     string
		Left    []string
	}
	tcs := []struct {
		name  string
		keep  int
		extra func(dir string) // changes made after planning
		want  observed
	}{
		{
			name: "the oldest are removed and the rest stays",
			keep: 2,
			want: observed{
				Removed: 3,
				Left: []string{
					"backups/notes.txt", "backups/replaced/thaler-20261020-100000-replaced.db", "backups/replaced/thaler-20261021-100000-replaced.db",
					"backups/thaler-20261020-090000.db", "backups/thaler-20261021-090000.db",
				},
			},
		},
		{
			name: "a file that is already gone does not stop it",
			keep: 2,
			extra: func(dir string) {
				_ = os.Remove(filepath.Join(dir, "backups", "thaler-20261001-090000.db"))
			},
			want: observed{
				Removed: 3,
				Left: []string{
					"backups/notes.txt", "backups/replaced/thaler-20261020-100000-replaced.db", "backups/replaced/thaler-20261021-100000-replaced.db",
					"backups/thaler-20261020-090000.db", "backups/thaler-20261021-090000.db",
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			for name, days := range map[string]int{
				"backups/thaler-20261021-090000.db": 1, "backups/thaler-20261020-090000.db": 2,
				"backups/thaler-20261010-090000.db": 12, "backups/thaler-20261001-090000.db": 21,
				"backups/notes.txt": 99,
				// a database kept by a restore may have its log files with it
				"backups/replaced/thaler-20261021-100000-replaced.db":     1,
				"backups/replaced/thaler-20261020-100000-replaced.db":     2,
				"backups/replaced/thaler-20261001-100000-replaced.db":     21,
				"backups/replaced/thaler-20261001-100000-replaced.db-wal": 21,
				"backups/replaced/thaler-20261001-100000-replaced.db-shm": 21,
			} {
				aged(t, dir, name, base, days)
			}
			plan, err := PlanPrune(filepath.Join(dir, "thaler.db"), tc.keep)
			if err != nil {
				t.Fatal(err)
			}
			if tc.extra != nil {
				tc.extra(dir)
			}

			// when
			removed, err := Prune(plan)

			// then
			got := observed{Removed: removed}
			if err != nil {
				got.Err = err.Error()
			}
			got.Left = everythingBelow(t, dir)
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Prune_refusesWhatIsNotABackup(t *testing.T) {
	// given
	dir := t.TempDir()
	precious := filepath.Join(dir, "thaler.db")
	aged(t, dir, "thaler.db", time.Now(), 0)
	aged(t, dir, "backups/thaler-20261021-090000.db", time.Now(), 0)
	plan := PrunePlan{Folders: []PruneFolder{{Remove: []BackupFile{
		{Path: filepath.Join(dir, "backups", "thaler-20261021-090000.db")}, {Path: precious},
	}}}}

	// when
	removed, err := Prune(plan)

	// then
	type observed struct {
		Removed int
		Err     bool
		Left    []string
	}
	got := observed{Removed: removed, Err: err != nil, Left: everythingBelow(t, dir)}
	want := observed{Err: true, Left: []string{"backups/thaler-20261021-090000.db", "thaler.db"}}
	if diff := cmp.Diff(want, got, equalEmpty); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}
