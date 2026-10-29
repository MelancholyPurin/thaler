package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
)

// pressRaw sends keys without finishing the work they start, as a person typing
// during a backup would.
func pressRaw(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(key(k))
	}
}

// busyModel is a model with a popup ready to start work: a backup popup, or a
// restore popup over the given backups. keys are pressed first, and settled.
func busyModel(t *testing.T, restore bool, setup string, seeds ...seeded) (*Model, string) {
	t.Helper()
	dir := t.TempDir()
	if restore {
		m := restoreModel(t, dir, seeds...)
		play(m, setup)
		return m, dir
	}
	m := backupModel(t, dir)
	play(m, setup)
	return m, dir
}

func Test_Update_busy(t *testing.T) {
	type observed struct {
		Mode         string
		Note         string // what the work in progress says it is doing, "" for none
		QuitWhenDone bool
		Dest         string // what the backup popup holds in Save to
		Files        []string
	}
	locked := seeded{"thaler-20261021-090000.db.age", "Gärtner", day21}
	newest := seeded{"thaler-20261021-090000.db", "Metzger", day21}
	tcs := []struct {
		name    string
		restore bool
		seeds   []seeded
		setup   string
		then    string
		want    observed
	}{
		{
			name: "a plain backup says so while it is written", setup: "S", then: "enter",
			want: observed{Mode: "backup", Note: "Writing the backup, then checking it", Dest: "{dir}/backups/thaler-20261022-120000.db"},
		},
		{
			name: "an encrypted one says it is slow on purpose", setup: "S tab text:pw tab text:pw", then: "enter",
			want: observed{Mode: "backup", Note: "Encrypting the backup (slow on purpose: a moment)", Dest: "{dir}/backups/thaler-20261022-120000.db"},
		},
		{
			name: "ctrl+s starts it too", setup: "S", then: "ctrl+s",
			want: observed{Mode: "backup", Note: "Writing the backup, then checking it", Dest: "{dir}/backups/thaler-20261022-120000.db"},
		},
		{
			name: "keys do nothing meanwhile: not esc, not typing, not another enter", setup: "S", then: "enter esc x tab enter",
			want: observed{Mode: "backup", Note: "Writing the backup, then checking it", Dest: "{dir}/backups/thaler-20261022-120000.db"},
		},
		{
			name: "ctrl+c waits for it", setup: "S", then: "enter ctrl+c",
			want: observed{Mode: "backup", Note: "Writing the backup, then checking it", QuitWhenDone: true, Dest: "{dir}/backups/thaler-20261022-120000.db"},
		},
		{
			name: "a restore says what it is doing", restore: true, seeds: []seeded{newest}, setup: "ctrl+r enter", then: "enter",
			want: observed{Mode: "restore", Note: "Checking the backup, then replacing the database"},
		},
		{
			name: "an encrypted one is being decrypted", restore: true, seeds: []seeded{locked}, setup: "ctrl+r tab text:pw enter", then: "enter",
			want: observed{Mode: "restore", Note: "Decrypting and checking the backup (slow on purpose)"},
		},
		{
			name: "and takes no keys either", restore: true, seeds: []seeded{newest}, setup: "ctrl+r enter", then: "enter esc n y enter",
			want: observed{Mode: "restore", Note: "Checking the backup, then replacing the database"},
		},
		{
			name: "ctrl+c waits for a restore", restore: true, seeds: []seeded{newest}, setup: "ctrl+r enter", then: "enter ctrl+c",
			want: observed{Mode: "restore", Note: "Checking the backup, then replacing the database", QuitWhenDone: true},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, dir := busyModel(t, tc.restore, tc.setup, tc.seeds...)
			before := filesUnder(t, dir)

			// when
			pressRaw(m, keys(tc.then)...)

			// then
			got := observed{Mode: modeNames[m.mode], QuitWhenDone: m.quitWhenDone}
			if m.busy != nil {
				got.Note = m.busy.note
			}
			if m.bkp != nil {
				got.Dest = strings.ReplaceAll(m.bkp.dest.Value(), dir, "{dir}")
			}
			got.Files = filesUnder(t, dir)
			tc.want.Files = before // the work has not run yet, so nothing is written
			if diff := cmp.Diff(tc.want, got, equalEmpty, cmpSorted); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// What the work ends with is shown as before, and ctrl+c pressed meanwhile quits then.
func Test_Update_busy_finish(t *testing.T) {
	type observed struct {
		Mode   string
		Busy   bool
		Status string
		Quit   bool
		Backup bool // the backup was written
	}
	tcs := []struct {
		name string
		then string
		want observed
	}{
		{
			name: "it ends in the list, and does not quit",
			then: "enter",
			want: observed{Mode: "list", Status: "Backed up ({size}, checked, NOT encrypted) → …thaler-20261022-120000.db", Backup: true},
		},
		{
			name: "ctrl+c pressed meanwhile quits after the backup is written",
			then: "enter ctrl+c",
			want: observed{Mode: "list", Status: "Backed up ({size}, checked, NOT encrypted) → …thaler-20261022-120000.db", Quit: true, Backup: true},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			m := backupModel(t, dir)
			play(m, "S")
			pressRaw(m, keys(tc.then)...)

			// when
			cmd := settle(m)

			// then
			got := observed{Mode: modeNames[m.mode], Busy: m.busy != nil, Status: m.status}
			got.Quit = cmd != nil && cmd() == tea.Quit()
			got.Status = sizeInStatus.ReplaceAllString(got.Status, "({size},")
			got.Status = pathInStatus.ReplaceAllString(got.Status, "→ …$1")
			got.Backup = len(filesUnder(t, dir)) == 1
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A backup that fails leaves the popup open, ready for another try.
func Test_Update_busy_failure(t *testing.T) {
	// given
	dir := t.TempDir()
	m := backupModel(t, dir)
	play(m, "S enter") // writes the backup

	// when
	play(m, "S enter") // the same name again: refused

	// then
	type observed struct {
		Mode string
		Busy bool
		Err  string
	}
	got := observed{Mode: modeNames[m.mode], Busy: m.busy != nil, Err: m.bkp.err}
	got.Err = strings.ReplaceAll(got.Err, dir, "{dir}")
	want := observed{Mode: "backup", Err: "{dir}/backups/thaler-20261022-120000.db already exists"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func Test_Update_spinner(t *testing.T) {
	type observed struct {
		Frame int
		Again bool // asks for the next tick
	}
	tcs := []struct {
		name  string
		start bool
		ticks int
		want  observed
	}{
		{name: "it turns while busy", start: true, ticks: 3, want: observed{Frame: 3, Again: true}},
		{name: "a late tick, after the work, is dropped", start: false, ticks: 1, want: observed{}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m := backupModel(t, t.TempDir())
			play(m, "S")
			if tc.start {
				pressRaw(m, "enter")
			}

			// when
			var cmd tea.Cmd
			for range tc.ticks {
				_, cmd = m.Update(spinMsg{})
			}

			// then
			got := observed{Again: cmd != nil}
			if m.busy != nil {
				got.Frame = m.busy.frame
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_busyView(t *testing.T) {
	tcs := []struct {
		name    string
		restore bool
		seeds   []seeded
		setup   string
		then    string
		shows   []string
		hides   []string
	}{
		{
			name: "the backup popup shows the spinner and the wait", setup: "S tab text:pw tab text:pw", then: "enter",
			shows: []string{"⠋ Encrypting the backup", "please wait · ctrl+c quits when done"},
			hides: []string{"enter back up"},
		},
		{
			name: "and what ctrl+c will do", setup: "S", then: "enter ctrl+c",
			shows: []string{"⠋ Writing the backup", "quitting as soon as this is done"},
		},
		{
			name: "before and after, there is none", setup: "S", then: "",
			shows: []string{"enter back up"},
			hides: []string{"Writing the backup", "please wait"},
		},
		{
			name: "the restore popup too", restore: true, seeds: []seeded{{"thaler-20261021-090000.db", "Metzger", day21}}, setup: "ctrl+r enter", then: "enter",
			shows: []string{"⠋ Checking the backup, then replacing", "please wait"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := busyModel(t, tc.restore, tc.setup, tc.seeds...)

			// when
			pressRaw(m, keys(tc.then)...)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
