package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const testRoadmap = "| Key | Epic | Milestone |\n| --- | --- | --- |\n| `PLT` | Installing | `M-TUI` |\n| `REL` | The release train | `M-TUI` |\n"

// repoWith makes a repository whose history has these commit titles, oldest
// first, and returns its folder.
func repoWith(t *testing.T, titles ...string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
		cmd := exec.CommandContext(t.Context(), "git", full...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--quiet")
	for _, title := range titles {
		git("commit", "--quiet", "--allow-empty", "-m", title)
	}
	return dir
}

func Test_run(t *testing.T) {
	tcs := []struct {
		name    string
		history []string
		args    []string // {repo} and {roadmap} stand for the files
		want    string
		wantErr string
	}{
		{
			name:    "the notes of the commits in the range",
			history: []string{"[PLT-1] old (#1)", "[REL-1] a (#2)", "[maintenance] b (#3)", "[PLT-6] c (#4)"},
			args:    []string{"-C", "{repo}", "-roadmap", "{roadmap}", "-version", "v0.2.0", "-date", "2026-11-30", "HEAD~3..HEAD"},
			want:    "## v0.2.0 (2026-11-30)\n\n### Installing (PLT)\n\n- `PLT-6` c (#4)\n\n### The release train (REL)\n\n- `REL-1` a (#2)\n\n### Maintenance\n\n- b (#3)\n",
		},
		{
			name:    "the line for the roadmap",
			history: []string{"[PLT-1] old (#1)", "[REL-1] a (#2)", "[PLT-6] c (#3)"},
			args:    []string{"-C", "{repo}", "-roadmap", "{roadmap}", "-row", "-version", "v0.2.0", "-date", "2026-11-30", "HEAD~2..HEAD"},
			want:    "| `v0.2.0` | 2026-11-30 | `PLT-6`, `REL-1` |\n",
		},
		{
			name:    "a range with nothing in it",
			history: []string{"[PLT-1] a (#1)"},
			args:    []string{"-C", "{repo}", "-roadmap", "{roadmap}", "-version", "v0.2.0", "-date", "2026-11-30", "HEAD..HEAD"},
			want:    "## v0.2.0 (2026-11-30)\n\nNothing changed.\n",
		},
		{
			name:    "a range git does not know",
			history: []string{"[PLT-1] a (#1)"},
			args:    []string{"-C", "{repo}", "-roadmap", "{roadmap}", "-version", "v0.2.0", "-date", "2026-11-30", "v9.9.9..HEAD"},
			wantErr: "git log v9.9.9..HEAD: exit status 128",
		},
		{name: "the version is needed", history: []string{"[PLT-1] a (#1)"}, args: []string{"-C", "{repo}", "-roadmap", "{roadmap}", "-date", "2026-11-30", "HEAD"}, wantErr: "usage: releasenotes -version VERSION -date YYYY-MM-DD [-row] [-roadmap FILE] FROM..TO"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			repo := repoWith(t, tc.history...)
			roadmap := filepath.Join(t.TempDir(), "ROADMAP.md")
			if err := os.WriteFile(roadmap, []byte(testRoadmap), 0o600); err != nil {
				t.Fatal(err)
			}
			args := make([]string, len(tc.args))
			for i, a := range tc.args {
				switch a {
				case "{repo}":
					a = repo
				case "{roadmap}":
					a = roadmap
				}
				args[i] = a
			}
			var out bytes.Buffer

			// when
			err := run(t.Context(), args, &out)

			// then
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if diff := cmp.Diff(tc.wantErr, gotErr); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, out.String()); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
