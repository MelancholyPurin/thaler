package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func Test_ListBackups(t *testing.T) {
	type listed struct {
		Name      string
		Size      int64
		Encrypted bool
	}
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	tcs := []struct {
		name  string
		files map[string]int // below the folder, and how many days old
		want  []listed
	}{
		{name: "a folder that is not there has none"},
		{name: "an empty folder has none", files: map[string]int{}},
		{
			name:  "newest first, whatever the names say",
			files: map[string]int{"thaler-20260101-090000.db": 0, "thaler-20261021-090000.db": 40, "thaler-20261001-090000.db": 10},
			want: []listed{
				{Name: "thaler-20260101-090000.db", Size: 5}, {Name: "thaler-20261001-090000.db", Size: 5}, {Name: "thaler-20261021-090000.db", Size: 5},
			},
		},
		{
			name:  "encrypted ones are listed with the others, and marked",
			files: map[string]int{"thaler-20261021-090000.db.age": 1, "thaler-20261020-090000.db": 2},
			want:  []listed{{Name: "thaler-20261021-090000.db.age", Size: 5, Encrypted: true}, {Name: "thaler-20261020-090000.db", Size: 5}},
		},
		{
			name:  "the same time: the later name first",
			files: map[string]int{"thaler-20261020-090000.db": 3, "thaler-20261021-090000.db": 3},
			want:  []listed{{Name: "thaler-20261021-090000.db", Size: 5}, {Name: "thaler-20261020-090000.db", Size: 5}},
		},
		{
			name: "only what is named like a backup, and only in this folder",
			files: map[string]int{
				"thaler-20261021-090000.db": 1, "notes.txt": 0, "mine.db": 0, "thaler-20261021-090000.db.bak": 0,
				"replaced/thaler-20261001-100000-replaced.db": 0, "thaler-x.db/inside.db": 0,
			},
			want: []listed{{Name: "thaler-20261021-090000.db", Size: 5}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := filepath.Join(t.TempDir(), "backups")
			if tc.files != nil {
				for name, days := range tc.files {
					aged(t, dir, name, base, days)
				}
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}

			// when
			got, err := ListBackups(dir)

			// then
			if err != nil {
				t.Fatal(err)
			}
			var names []listed
			for _, f := range got {
				names = append(names, listed{filepath.Base(f.Path), f.Size, f.Encrypted})
			}
			if diff := cmp.Diff(tc.want, names, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ListBackups_modificationTime(t *testing.T) {
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	tcs := []struct {
		name string
		days int // how old the file is
		want time.Time
	}{
		{name: "three days old", days: 3, want: base.AddDate(0, 0, -3)},
		{name: "changed just now", days: 0, want: base},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			aged(t, dir, "thaler-20261021-090000.db", base, tc.days)

			// when
			got, err := ListBackups(dir)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("listed %d backups, want 1", len(got))
			}
			if diff := cmp.Diff(tc.want, got[0].When); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
