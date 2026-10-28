package tui

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/config"
)

func keepSetting(n int) ConfigLoader {
	return func() (*config.Config, error) { return &config.Config{KeepBackups: &n}, nil }
}

func Test_Update_prune(t *testing.T) {
	type observed struct {
		Mode   string
		Notes  []string
		Status string
		Err    string
		Files  []string
	}
	four := map[string]int{
		"backups/thaler-20261022-090000.db": 0, "backups/thaler-20261021-090000.db": 1,
		"backups/thaler-20261020-090000.db": 2, "backups/thaler-20261019-090000.db": 3,
	}
	newest2 := []string{"backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db"}
	all4 := []string{
		"backups/thaler-20261019-090000.db", "backups/thaler-20261020-090000.db",
		"backups/thaler-20261021-090000.db", "backups/thaler-20261022-090000.db",
	}
	tcs := []struct {
		name   string
		files  map[string]int
		keep   int
		script string
		want   observed
	}{
		{
			name: "the bar offers when there are more than are kept", files: four, keep: 2, script: "",
			want: observed{Mode: "list", Notes: []string{"2 old backups can be removed: ctrl+p"}, Files: all4},
		},
		{
			name: "one is one", files: four, keep: 3, script: "",
			want: observed{Mode: "list", Notes: []string{"1 old backup can be removed: ctrl+p"}, Files: all4},
		},
		{
			name: "not while there are no more than are kept", files: four, keep: 4, script: "",
			want: observed{Mode: "list", Files: all4},
		},
		{
			name: "0 keeps everything, and says so", files: four, keep: 0, script: "ctrl+p",
			want: observed{Mode: "list", Status: "keep_backups is 0 in config.toml: all backups are kept", Files: all4},
		},
		{
			name: "ctrl+p with nothing to remove says so", files: four, keep: 4, script: "ctrl+p",
			want: observed{Mode: "list", Status: "nothing to remove: no folder has more than 4 backups", Files: all4},
		},
		{
			name: "ctrl+p lists them and removes nothing yet", files: four, keep: 2, script: "ctrl+p",
			want: observed{Mode: "prune", Notes: []string{"2 old backups can be removed: ctrl+p"}, Files: all4},
		},
		{
			name: "esc leaves them", files: four, keep: 2, script: "ctrl+p esc",
			want: observed{Mode: "list", Notes: []string{"2 old backups can be removed: ctrl+p"}, Files: all4},
		},
		{
			name: "enter removes the oldest", files: four, keep: 2, script: "ctrl+p enter",
			want: observed{Mode: "list", Status: "Removed 2 old backups (2 B)", Files: newest2},
		},
		{
			name: "y does too", files: four, keep: 2, script: "ctrl+p y",
			want: observed{Mode: "list", Status: "Removed 2 old backups (2 B)", Files: newest2},
		},
		{
			name: "and n does not", files: four, keep: 2, script: "ctrl+p n",
			want: observed{Mode: "list", Notes: []string{"2 old backups can be removed: ctrl+p"}, Files: all4},
		},
		{
			name: "what restores replaced and upgrades copied are counted on their own",
			files: map[string]int{
				"backups/thaler-20261022-090000.db":                   0,
				"backups/replaced/thaler-20261022-100000-replaced.db": 0, "backups/replaced/thaler-20261001-100000-replaced.db": 21,
				"backups/pre-migration/thaler-20261022-110000-before-a.db": 0,
			},
			keep: 1, script: "ctrl+p enter",
			want: observed{
				Mode: "list", Status: "Removed 1 old backup (1 B)",
				Files: []string{
					"backups/pre-migration/thaler-20261022-110000-before-a.db",
					"backups/replaced/thaler-20261022-100000-replaced.db", "backups/thaler-20261022-090000.db",
				},
			},
		},
		{
			name: "a backup made now can tip it over", files: map[string]int{"backups/thaler-20261021-090000.db": 1, "backups/thaler-20261020-090000.db": 2},
			keep: 2, script: "S enter",
			want: observed{
				Mode: "list", Status: "Backed up", Notes: []string{"1 old backup can be removed: ctrl+p"},
				Files: []string{"backups/thaler-20261020-090000.db", "backups/thaler-20261021-090000.db", "backups/thaler-20261022-120000.db"},
			},
		},
		{
			name:  "it comes after the reminder to back up",
			files: map[string]int{"backups/thaler-20261001-090000.db": 21, "backups/thaler-20261002-090000.db": 20},
			keep:  1, script: "",
			want: observed{
				Mode:  "list",
				Notes: []string{"Last backup 20 days ago: S makes a new one", "1 old backup can be removed: ctrl+p"},
				Files: []string{"backups/thaler-20261001-090000.db", "backups/thaler-20261002-090000.db"},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			for name, days := range tc.files {
				touch(t, dir, name, days)
			}
			m := reminderModel(t, dir, false, keepSetting(tc.keep))

			// when
			play(m, tc.script)

			// then
			got := observed{Mode: modeNames[m.mode], Notes: m.backupNotes(), Status: m.status}
			if m.prn != nil {
				got.Err = m.prn.err
			}
			if strings.HasPrefix(got.Status, "Backed up") {
				got.Status = "Backed up"
			}
			got.Files = filesUnder(t, dir)
			if diff := cmp.Diff(tc.want, got, equalEmpty, cmpSorted); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if len(got.Notes) > 0 && m.mode == modeList && m.status == "" {
				checkView(t, m.View(), got.Notes, nil)
			}
		})
	}
}

func Test_prunePopup(t *testing.T) {
	// given
	dir := t.TempDir()
	touch(t, dir, "backups/thaler-20261022-090000.db", 0)
	for i, n := range []string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12"} {
		touch(t, dir, "backups/replaced/thaler-202610"+n+"-100000-replaced.db", 10+i) // 01 is the newest
	}
	m := reminderModel(t, dir, false, keepSetting(1))

	// when
	play(m, "ctrl+p")
	first := m.View()
	play(m, "down down down down down down down down down down down down down down")
	last := m.View()

	// then
	checkView(t, first, []string{"Remove old backups", "Keeping the newest 1 in each folder", "These 11 would go", "replaced by a restore · 11 of 12", "enter remove 11 · ↑/↓ scroll"}, []string{"backups · "})
	checkView(t, last, []string{"thaler-20261012-100000-replaced.db"}, []string{"thaler-20261002-100000-replaced.db"})
	if m.prn.top != len(m.prn.lines)-pruneRows {
		t.Errorf("scrolled to line %d, want the last window from %d", m.prn.top, len(m.prn.lines)-pruneRows)
	}
}
