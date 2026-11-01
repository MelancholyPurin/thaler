package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func Test_prune(t *testing.T) {
	when := regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}`)
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	backups := map[string]int{ // below the folder of the database, and how many days old
		"backups/thaler-20261022-090000.db": 0, "backups/thaler-20261021-090000.db": 1, "backups/thaler-20261020-090000.db.age": 2,
		"backups/thaler-20261015-090000.db": 7, "backups/notes.txt": 90,
		"backups/replaced/thaler-20261001-100000-replaced.db": 21,
	}
	tcs := []struct {
		name    string
		config  string
		args    []string
		env     map[string]string
		want    string // the output, the folder of the database as {db} and times as {when}
		wantErr string
		left    []string // below the folder, afterwards
	}{
		{
			name: "it only lists, by default",
			args: []string{"cli", "prune", "--keep", "2"},
			want: "Keeping the newest 2 in each folder; 2 backups (10 B) would go:\n\n" +
				"backups: {db}/backups, 2 of 4\n  {when}  thaler-20261020-090000.db.age  5 B\n  {when}  thaler-20261015-090000.db  5 B\n" +
				"\nNothing was removed. Run it again with --yes to remove these.\n",
			left: []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261015-090000.db", "backups/thaler-20261020-090000.db.age", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name: "--yes removes them",
			args: []string{"cli", "prune", "--keep", "2", "--yes"},
			want: "Keeping the newest 2 in each folder; 2 backups (10 B) would go:\n\n" +
				"backups: {db}/backups, 2 of 4\n  {when}  thaler-20261020-090000.db.age  5 B\n  {when}  thaler-20261015-090000.db  5 B\n" +
				"\nRemoved 2 backups (10 B).\n",
			left: []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name:   "keep_backups in the config is used",
			config: "keep_backups = 3\n",
			args:   []string{"cli", "prune", "--yes"},
			want: "Keeping the newest 3 in each folder; 1 backup (5 B) would go:\n\n" +
				"backups: {db}/backups, 1 of 4\n  {when}  thaler-20261015-090000.db  5 B\n" +
				"\nRemoved 1 backup (5 B).\n",
			left: []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261020-090000.db.age", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name:   "--keep wins over the config",
			config: "keep_backups = 1\n",
			args:   []string{"cli", "prune", "--keep", "4"},
			want:   "Nothing to remove: no folder has more than 4 backups.\n",
			left:   []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261015-090000.db", "backups/thaler-20261020-090000.db.age", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name: "ten are kept by default, so here nothing goes",
			args: []string{"cli", "prune", "--yes"},
			want: "Nothing to remove: no folder has more than 10 backups.\n",
			left: []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261015-090000.db", "backups/thaler-20261020-090000.db.age", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name:   "keep_backups = 0 keeps everything",
			config: "keep_backups = 0\n",
			args:   []string{"cli", "prune", "--yes"},
			want:   "keep_backups is 0 in config.toml: all backups are kept. Say how many to keep with --keep N.\n",
			left:   []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261015-090000.db", "backups/thaler-20261020-090000.db.age", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name:    "--keep 0 is refused, not taken to mean everything",
			args:    []string{"cli", "prune", "--keep", "0", "--yes"},
			wantErr: "keep 0 backups: it must be at least 1",
			left:    []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261015-090000.db", "backups/thaler-20261020-090000.db.age", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name: "English on a German machine, in the plural",
			args: []string{"cli", "prune", "--keep", "2", "--yes"},
			env:  map[string]string{"LANG": "de_DE.UTF-8"},
			want: "Keeping the newest 2 in each folder; 2 backups (10 B) would go:\n\n" +
				"backups: {db}/backups, 2 of 4\n  {when}  thaler-20261020-090000.db.age  5 B\n  {when}  thaler-20261015-090000.db  5 B\n" +
				"\nRemoved 2 backups (10 B).\n",
			left: []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name:   "English on a Spanish machine, in the singular",
			config: "keep_backups = 3\n",
			args:   []string{"cli", "prune", "--yes"},
			env:    map[string]string{"LANG": "es_ES.UTF-8"},
			want: "Keeping the newest 3 in each folder; 1 backup (5 B) would go:\n\n" +
				"backups: {db}/backups, 1 of 4\n  {when}  thaler-20261015-090000.db  5 B\n" +
				"\nRemoved 1 backup (5 B).\n",
			left: []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261020-090000.db.age", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name: "English on a German machine, nothing to remove",
			args: []string{"cli", "prune", "--keep", "5"},
			env:  map[string]string{"LANG": "de_DE.UTF-8"},
			want: "Nothing to remove: no folder has more than 5 backups.\n",
			left: []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261015-090000.db", "backups/thaler-20261020-090000.db.age", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"},
		},
		{
			name: "a folder of its own is counted on its own",
			args: []string{"cli", "prune", "--keep", "1", "--yes"},
			want: "Keeping the newest 1 in each folder; 3 backups (15 B) would go:\n\n" +
				"backups: {db}/backups, 3 of 4\n  {when}  thaler-20261021-090000.db  5 B\n  {when}  thaler-20261020-090000.db.age  5 B\n  {when}  thaler-20261015-090000.db  5 B\n" +
				"\nRemoved 3 backups (15 B).\n",
			left: []string{"backups/notes.txt", "backups/replaced/thaler-20261001-100000-replaced.db", "backups/thaler-20261022-090000.db"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, nil)
			e.env = tc.env
			if err := os.WriteFile(e.cfg, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			dbDir := filepath.Dir(e.db)
			for name, days := range backups {
				p := filepath.Join(dbDir, name)
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
					t.Fatal(err)
				}
				mtime := base.AddDate(0, 0, -days)
				if err := os.Chtimes(p, mtime, mtime); err != nil {
					t.Fatal(err)
				}
			}

			// when
			got, err := e.run(tc.args...)

			// then
			got = when.ReplaceAllString(strings.ReplaceAll(got, dbDir, "{db}"), "{when}")
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			var left []string
			for name := range backups {
				if _, err := os.Stat(filepath.Join(dbDir, name)); err == nil {
					left = append(left, name)
				}
			}
			if diff := cmp.Diff(tc.left, left, cmpopts.SortSlices(func(a, b string) bool { return a < b }), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("files left mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
