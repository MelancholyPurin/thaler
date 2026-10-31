package releasenotes

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var testEpics = []Epic{{Key: "PLT", Name: "Installing"}, {Key: "REL", Name: "The release train"}, {Key: "ENG", Name: "Build and test"}}

func Test_ParseEpics(t *testing.T) {
	tcs := []struct {
		name    string
		roadmap string
		want    []Epic
	}{
		{
			name:    "the epic table, in its order",
			roadmap: "| Key | Epic | Milestone |\n| --- | --- | --- |\n| `LDG` | Ledger domain | `M-TUI` |\n| `PLT` | Installing and running on macOS and Linux | `M-TUI` |\n",
			want:    []Epic{{Key: "LDG", Name: "Ledger domain"}, {Key: "PLT", Name: "Installing and running on macOS and Linux"}},
		},
		{name: "rows of other tables are not epics", roadmap: "| `M-TUI` | Feature-complete TUI | One person |\n| `LDG-1` | x | y |\n"},
		{name: "no table", roadmap: "# Roadmap\n"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.roadmap

			// when
			got := ParseEpics(tc.roadmap)

			// then
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Parse(t *testing.T) {
	tcs := []struct {
		name  string
		title string
		want  Change
	}{
		{name: "a story", title: "[PLT-1] cmd/thaler: Add `--version` (#34)", want: Change{Story: "PLT-1", Text: "cmd/thaler: Add `--version` (#34)"}},
		{name: "maintenance, in capitals", title: "[MAINTENANCE] github/ci: Add CI", want: Change{Maintenance: true, Text: "github/ci: Add CI"}},
		{name: "maintenance, as it was written before", title: "[maintenance] internal/core: Drop windows support (#33)", want: Change{Maintenance: true, Text: "internal/core: Drop windows support (#33)"}},
		{name: "a colon after the bracket", title: "[maintenance]: Consolidate `plural` (#27)", want: Change{Maintenance: true, Text: "Consolidate `plural` (#27)"}},
		{name: "spaces around the title", title: "  [REL-4] internal/config: Add feature flags  ", want: Change{Story: "REL-4", Text: "internal/config: Add feature flags"}},
		{name: "no bracket", title: "build(deps): bump actions/setup-go from 5 to 7 (#17)", want: Change{Text: "build(deps): bump actions/setup-go from 5 to 7 (#17)"}},
		{name: "a bracket that is no ID", title: "[wip] try something", want: Change{Text: "[wip] try something"}},
		{name: "an ID in lower case is no ID", title: "[plt-1] x", want: Change{Text: "[plt-1] x"}},
		{name: "a story with the wrong number of letters", title: "[PL-1] x", want: Change{Text: "[PL-1] x"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.title

			// when
			got := Parse(tc.title)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Notes(t *testing.T) {
	tcs := []struct {
		name   string
		titles []string
		want   string
	}{
		{
			name: "grouped by epic in the roadmap's order, then maintenance, then the rest",
			titles: []string{
				"[maintenance] internal/core: Rename (#29)",
				"[ENG-12] internal/core: Use migrate (#31)",
				"[REL-4] internal/config: Add flags (#37)",
				"[PLT-1] cmd/thaler: Add version (#34)",
				"[PLT-6] readme: Install (#35)",
				"build(deps): bump x (#17)",
				"[ZZZ-1] nobody: Unknown epic (#40)",
			},
			want: "## v0.3.0 (2026-11-16)\n" +
				"\n### Installing (PLT)\n\n- `PLT-1` cmd/thaler: Add version (#34)\n- `PLT-6` readme: Install (#35)\n" +
				"\n### The release train (REL)\n\n- `REL-4` internal/config: Add flags (#37)\n" +
				"\n### Build and test (ENG)\n\n- `ENG-12` internal/core: Use migrate (#31)\n" +
				"\n### Maintenance\n\n- internal/core: Rename (#29)\n" +
				"\n### Other changes\n\n- build(deps): bump x (#17)\n- `ZZZ-1` nobody: Unknown epic (#40)\n",
		},
		{name: "a story that took two pull requests is listed twice", titles: []string{"[REL-1] a (#1)", "[REL-1] b (#2)"}, want: "## v0.3.0 (2026-11-16)\n\n### The release train (REL)\n\n- `REL-1` a (#1)\n- `REL-1` b (#2)\n"},
		{name: "nothing changed", want: "## v0.3.0 (2026-11-16)\n\nNothing changed.\n"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.titles

			// when
			got := Notes("v0.3.0", "2026-11-16", tc.titles, testEpics)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Row(t *testing.T) {
	tcs := []struct {
		name   string
		titles []string
		want   string
	}{
		{
			name:   "each story once, in the order of the notes",
			titles: []string{"[ENG-12] a", "[REL-4] b", "[PLT-6] c", "[REL-4] d", "[maintenance] e", "build(deps): f"},
			want:   "| `v0.3.0` | 2026-11-16 | `PLT-6`, `REL-4`, `ENG-12` |",
		},
		{name: "a story of no epic is not recorded", titles: []string{"[ZZZ-1] a", "[PLT-1] b"}, want: "| `v0.3.0` | 2026-11-16 | `PLT-1` |"},
		{name: "only maintenance", titles: []string{"[maintenance] a"}, want: "| `v0.3.0` | 2026-11-16 | none |"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.titles

			// when
			got := Row("v0.3.0", "2026-11-16", tc.titles, testEpics)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
