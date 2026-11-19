package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/config"
)

// look is one look at the import folder, as the runtime does it: the command that
// reads the folder runs, and what it found is taken in. It returns the command for
// the next look.
func look(t *testing.T, m *Model) {
	t.Helper()
	msg := m.scanExports()()
	if _, ok := msg.(exportsSeen); !ok {
		t.Fatalf("a look at the folder answered %T", msg)
	}
	if _, cmd := m.Update(msg); cmd == nil {
		t.Fatal("after a look, the next one is asked for")
	}
}

// arrive puts a file in the folder, modified at a time of its own.
func arrive(t *testing.T, dir, name, content string, minutes int) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	when := fixedNow().Add(-time.Duration(minutes) * time.Minute)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func Test_Update_exports(t *testing.T) {
	type observed struct {
		Notes     []string // what the bottom bar offers about exports
		Listed    []string // the files the import popup lists, if it is open
		Highlight string
	}
	off := func(m *Model) {
		m.loadConfig = func() (*config.Config, error) {
			no := false
			return &config.Config{WatchImportDir: &no}, nil
		}
	}
	tcs := []struct {
		name   string
		steps  func(t *testing.T, m *Model, dir string)
		before func(m *Model)
		want   observed
		shows  []string
		hides  []string
	}{
		{
			name:  "what was there at the start is not new",
			steps: func(t *testing.T, m *Model, dir string) { look(t, m); look(t, m) },
			hides: []string{"new export"},
		},
		{
			name: "a file that arrives is offered",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
			},
			want:  observed{Notes: []string{"new export neu.csv · i imports it"}},
			shows: []string{"new export neu.csv · i imports it"},
		},
		{
			name: "seen again, it is still one",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
				look(t, m)
			},
			want: observed{Notes: []string{"new export neu.csv · i imports it"}},
		},
		{
			name: "several are counted, the newest named",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "a.csv", "a", 1)
				arrive(t, dir, "b.csv", "a", 2)
				look(t, m)
			},
			want: observed{Notes: []string{"2 new exports · i imports them"}},
		},
		{
			name: "ones that arrive at different looks are all counted",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "a.csv", "a", 1)
				look(t, m)
				arrive(t, dir, "b.csv", "a", 2)
				look(t, m)
			},
			want: observed{Notes: []string{"2 new exports · i imports them"}},
		},
		{
			name: "opening the import takes the offer away",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
				play(m, "i esc")
			},
			hides: []string{"new export"},
		},
		{
			name: "once the import was opened, the same file is not offered again",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
				play(m, "i esc")
				look(t, m)
			},
			hides: []string{"new export"},
		},
		{
			name: "a file saved twice before anyone looked is one",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "neu.csv", "a", 2)
				look(t, m)
				arrive(t, dir, "neu.csv", "abc", 1)
				look(t, m)
			},
			want: observed{Notes: []string{"new export neu.csv · i imports it"}},
		},
		{
			name: "a file saved again under its name is new, by its time alone",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "umsaetze.csv", exportCSV, 5)
				look(t, m)
			},
			want: observed{Notes: []string{"new export umsaetze.csv · i imports it"}},
		},
		{
			name: "or by its size alone",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "umsaetze.csv", exportCSV+"more\n", 60)
				look(t, m)
			},
			want: observed{Notes: []string{"new export umsaetze.csv · i imports it"}},
		},
		{
			name: "a file saved again under its name is new",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "umsaetze.csv", exportCSV+"more\n", 5)
				look(t, m)
			},
			want: observed{Notes: []string{"new export umsaetze.csv · i imports it"}},
		},
		{
			name: "a file that is gone again is not offered",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
				if err := os.Remove(filepath.Join(dir, "neu.csv")); err != nil {
					t.Fatal(err)
				}
				look(t, m)
			},
		},
		{
			name: "what is not a CSV file is not an export",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "notes.txt", "a", 1)
				arrive(t, dir, "umsaetze.csv.crdownload", "a", 1)
				look(t, m)
			},
		},
		{
			name:   "switched off, nothing is said",
			before: off,
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
			},
		},
		{
			name: "switched on again, what arrived while it was off is not new",
			steps: func(t *testing.T, m *Model, dir string) {
				on := m.loadConfig
				off(m)
				look(t, m)
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
				m.loadConfig = on
				look(t, m)
			},
		},
		{
			name: "another folder starts afresh, then notices",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				other := t.TempDir()
				arrive(t, other, "old.csv", "a", 9)
				m.loadConfig = func() (*config.Config, error) { return &config.Config{ImportDir: other}, nil }
				look(t, m)
				arrive(t, other, "neu.csv", "a", 1)
				look(t, m)
			},
			want: observed{Notes: []string{"new export neu.csv · i imports it"}},
		},
		{
			name: "a folder that is not there says nothing",
			steps: func(t *testing.T, m *Model, dir string) {
				m.downloads = filepath.Join(dir, "missing")
				look(t, m)
				look(t, m)
			},
		},
		{
			name: "with the list of files open, the file is on it, and chosen",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				play(m, "i")
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
			},
			want: observed{Listed: []string{"neu.csv", "umsaetze.csv"}, Highlight: "neu.csv"},
		},
		{
			name: "with a name typed, the list still fits what was typed",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				play(m, "i raw:umsa")
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
			},
			want: observed{Listed: []string{"umsaetze.csv"}, Highlight: "umsaetze.csv"},
		},
		{
			name: "further down the list, the choice stays where it is",
			steps: func(t *testing.T, m *Model, dir string) {
				arrive(t, dir, "alt.csv", "a", 90)
				look(t, m)
				play(m, "i down")
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
			},
			want: observed{Listed: []string{"umsaetze.csv", "alt.csv"}, Highlight: "alt.csv"},
		},
		{
			name: "with the review open, it is left as it is",
			steps: func(t *testing.T, m *Model, dir string) {
				look(t, m)
				play(m, "i enter")
				arrive(t, dir, "neu.csv", "a", 1)
				look(t, m)
			},
			want: observed{Notes: []string{"new export neu.csv · i imports it"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			dir := filepath.Dir(writeExport(t, m, "umsaetze.csv", exportCSV))
			if err := os.Chtimes(filepath.Join(dir, "umsaetze.csv"), fixedNow().Add(-time.Hour), fixedNow().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			if tc.before != nil {
				tc.before(m)
			}

			// when
			tc.steps(t, m, dir)

			// then
			got := observed{Notes: m.exportNotes()}
			if f := m.imp; f != nil && f.step == stepChoose {
				for _, c := range f.matches {
					got.Listed = append(got.Listed, c.name)
				}
				if f.fileCur >= 0 && f.fileCur < len(f.matches) {
					got.Highlight = f.matches[f.fileCur].name
				}
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// The runtime asks for a look when the interface starts and every few seconds after.
func Test_Update_exports_schedule(t *testing.T) {
	tcs := []struct {
		name string
		msg  func(m *Model) any
		want string
	}{
		{name: "the start looks at once", msg: func(m *Model) any { return m.Init()() }, want: "tui.exportsSeen"},
		{name: "a tick looks again", msg: func(m *Model) any { _, cmd := m.Update(exportTick{}); return cmd() }, want: "tui.exportsSeen"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			writeExport(t, m, "umsaetze.csv", exportCSV)

			// when
			got := tc.msg(m)

			// then
			seen, ok := got.(exportsSeen)
			if diff := cmp.Diff(true, ok); diff != "" {
				t.Fatalf("answer (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]string{"umsaetze.csv"}, namesOf(seen.files)); diff != "" {
				t.Errorf("what was seen (-want +got):\n%s", diff)
			}
		})
	}
}

func namesOf(files []bankFile) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.name)
	}
	return out
}

// newExports makes n exports arrive in the downloads folder of the model, seen by the
// interface as they would be: one look before, one after.
func newExports(t *testing.T, m *Model, n int) {
	t.Helper()
	if n == 0 {
		return
	}
	look(t, m)
	for i := range n {
		arrive(t, m.downloads, fmt.Sprintf("export%d.csv", i+1), "a", i+1)
	}
	look(t, m)
}
