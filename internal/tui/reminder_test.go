package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/config"
)

// reminderModel is a model on a ledger with one transaction (none if empty is
// set) whose backups folder is "{dir}/backups", with the config the loader returns.
func reminderModel(t *testing.T, dir string, empty bool, load ConfigLoader) *Model {
	t.Helper()
	st := openLedger(t)
	openAccounts(t, st, "Assets:Bank EUR")
	if !empty {
		book(t, st, "2026-10-03", "Kino", "Assets:Bank", "Expenses:Fun", -1200)
	}
	m, err := newWithClock(ctx, st, Options{
		LoadConfig:   load,
		DatabasePath: filepath.Join(dir, "thaler.db"),
		BackupDir:    filepath.Join(dir, "backups"),
	}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	return m
}

func daysSetting(n int) ConfigLoader {
	return func() (*config.Config, error) { return &config.Config{BackupReminderDays: &n}, nil }
}

// touch makes an empty file below dir that was last changed d days before the
// clock of the tests.
func touch(t *testing.T, dir, name string, d int) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := fixedNow().AddDate(0, 0, -d)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

func Test_backupReminder(t *testing.T) {
	const (
		never = "No backup yet: S makes one"
		week  = "Last backup 7 days ago: S makes a new one"
	)
	tcs := []struct {
		name  string
		files map[string]int // below the folder, and how many days old
		empty bool           // the ledger has no transactions
		load  ConfigLoader
		want  string
	}{
		{name: "no backup yet, and there is data", load: testConfig, want: never},
		{name: "no backup yet, but nothing to lose", empty: true, load: testConfig},
		{name: "a backup from today", files: map[string]int{"backups/thaler-20261022-090000.db": 0}, load: testConfig},
		{name: "six days is fine by default", files: map[string]int{"backups/thaler-20261016-090000.db": 6}, load: testConfig},
		{name: "seven days is a week", files: map[string]int{"backups/thaler-20261015-090000.db": 7}, load: testConfig, want: week},
		{
			name:  "the newest of several counts",
			files: map[string]int{"backups/thaler-20261001-090000.db": 21, "backups/thaler-20261020-090000.db": 2},
			load:  testConfig,
		},
		{name: "an encrypted backup counts", files: map[string]int{"backups/thaler-20261022-090000.db.age": 0}, load: testConfig},
		{
			name:  "what a restore replaced and what an upgrade copied are not backups",
			files: map[string]int{"backups/replaced/thaler-20261022-090000-replaced.db": 0, "backups/pre-migration/thaler-20261022-090000-before-x.db": 0},
			load:  testConfig, want: never,
		},
		{name: "a longer interval waits", files: map[string]int{"backups/thaler-20261012-090000.db": 10}, load: daysSetting(30)},
		{
			name: "a shorter one does not", files: map[string]int{"backups/thaler-20261019-090000.db": 3}, load: daysSetting(3),
			want: "Last backup 3 days ago: S makes a new one",
		},
		{name: "0 switches it off", load: daysSetting(0)},
		{name: "0 switches it off, old backup or not", files: map[string]int{"backups/thaler-20260901-090000.db": 51}, load: daysSetting(0)},
		{
			name: "a config that cannot be read does not silence it", load: func() (*config.Config, error) { return nil, errors.New("broken") },
			want: never,
		},
		{name: "without a config loader", load: nil, want: never},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			for name, days := range tc.files {
				touch(t, dir, name, days)
			}

			// when
			m := reminderModel(t, dir, tc.empty, tc.load)

			// then
			if diff := cmp.Diff(tc.want, m.backupReminder()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if tc.want != "" {
				checkView(t, m.View(), []string{tc.want}, nil)
			} else {
				checkView(t, m.View(), nil, []string{"Last backup", "No backup yet"})
			}
		})
	}
}

func Test_backupReminder_followsWhatYouDo(t *testing.T) {
	// given
	dir := t.TempDir()
	touch(t, dir, "backups/thaler-20261001-090000.db", 21)
	days := 30
	m := reminderModel(t, dir, false, func() (*config.Config, error) { return &config.Config{BackupReminderDays: &days}, nil })
	type observed struct{ Start, Shorter, Backed string }
	var got observed

	// when
	got.Start = m.backupReminder()
	days = 7
	m.configEdited(nil)
	got.Shorter = m.backupReminder()
	play(m, "S enter")
	got.Backed = m.backupReminder()

	// then
	want := observed{Shorter: "Last backup 21 days ago: S makes a new one"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func Test_daysAgo(t *testing.T) {
	now := time.Date(2026, 10, 22, 0, 30, 0, 0, time.UTC)
	tcs := []struct {
		name string
		t    time.Time
		want int
	}{
		{name: "just now", t: now, want: 0},
		{name: "late yesterday is a day, not an hour", t: time.Date(2026, 10, 21, 23, 59, 0, 0, time.UTC), want: 1},
		{name: "a week", t: time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC), want: 7},
		{name: "across a month", t: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), want: 30},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := daysAgo(now, tc.t)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A backup made by something else while the interface is open ends the reminder,
// but the folder is not read on every key.
func Test_backupReminder_noticesOutsideBackups(t *testing.T) {
	// given
	dir := t.TempDir()
	m := reminderModel(t, dir, false, testConfig)
	clock := fixedNow()
	m.now = func() time.Time { return clock }
	m.refreshBackupAge()
	type observed struct{ Before, Soon, Later string }
	var got observed
	got.Before = m.backupReminder()

	// when
	touch(t, dir, "backups/thaler-20261022-110000.db", 0)
	clock = clock.Add(10 * time.Second)
	press(m, "t")
	got.Soon = m.backupReminder()
	clock = clock.Add(time.Minute)
	press(m, "t")
	got.Later = m.backupReminder()

	// then
	want := observed{Before: "No backup yet: S makes one", Soon: "No backup yet: S makes one"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}
