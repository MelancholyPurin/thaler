package config

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/melancholypurin/thaler/internal/importer"
)

// withFeatures makes known the list of flags for the rest of the test.
func withFeatures(t *testing.T, known []Feature) {
	t.Helper()
	old := knownFeatures
	knownFeatures = known
	t.Cleanup(func() { knownFeatures = old })
}

var rulesX = []importer.Rule{{Contains: "X", Category: "Expenses:X"}}

var budgetFlag = Feature{Name: "budget_targets", Story: "TBU-2", About: "set a category's monthly target"}

func Test_Config_Enabled(t *testing.T) {
	tcs := []struct {
		name string
		c    *Config
		feat string
		want bool
	}{
		{name: "everything is off without a config", c: nil, feat: "budget_targets"},
		{name: "off when config.toml does not mention it", c: &Config{}, feat: "budget_targets"},
		{name: "off when it is set to false", c: &Config{Features: map[string]bool{"budget_targets": false}}, feat: "budget_targets"},
		{name: "on when it is set to true", c: &Config{Features: map[string]bool{"budget_targets": true}}, feat: "budget_targets", want: true},
		{name: "another feature being on does not switch it on", c: &Config{Features: map[string]bool{"other": true}}, feat: "budget_targets"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.c

			// when
			got := tc.c.Enabled(tc.feat)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_checkFeatures(t *testing.T) {
	second := Feature{Name: "forecast", Story: "TFC-4", About: "project the balances"}
	tcs := []struct {
		name    string
		set     map[string]bool
		known   []Feature
		wantErr string
	}{
		{name: "nothing set, nothing known"},
		{name: "nothing set", known: []Feature{budgetFlag}},
		{name: "a known feature, on", set: map[string]bool{"budget_targets": true}, known: []Feature{budgetFlag}},
		{name: "a known feature, off", set: map[string]bool{"budget_targets": false}, known: []Feature{budgetFlag}},
		{
			name: "a name when nothing is behind a flag", set: map[string]bool{"budget_targets": true},
			wantErr: `unknown feature "budget_targets" under [features]: nothing is behind a feature flag in this version`,
		},
		{
			name: "a typo lists what exists", set: map[string]bool{"budget_target": true}, known: []Feature{budgetFlag, second},
			wantErr: `unknown feature "budget_target" under [features]: the features are budget_targets (TBU-2: set a category's monthly target), forecast (TFC-4: project the balances)`,
		},
		{
			name: "the first unknown name alphabetically is the one named", set: map[string]bool{"zeta": true, "alpha": false, "budget_targets": true}, known: []Feature{budgetFlag},
			wantErr: `unknown feature "alpha" under [features]: the features are budget_targets (TBU-2: set a category's monthly target)`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.set and tc.known

			// when
			err := checkFeatures(tc.set, tc.known)

			// then
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tc.wantErr, got); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Load_features(t *testing.T) {
	tcs := []struct {
		name    string
		known   []Feature
		file    string
		want    Config
		wantErr string // {path} stands for the file
	}{
		{name: "no features table", known: []Feature{budgetFlag}, file: "import_dir = \"/x\"\n", want: Config{ImportDir: "/x"}},
		{
			name: "a feature switched on, before the rules", known: []Feature{budgetFlag},
			file: "[features]\nbudget_targets = true\n\n[[rule]]\ncontains = \"X\"\ncategory = \"Expenses:X\"\n",
			want: Config{Features: map[string]bool{"budget_targets": true}, Rules: rulesX},
		},
		{
			name: "a name nobody defined is refused", file: "[features]\nbudgets = true\n",
			wantErr: `{path}: unknown feature "budgets" under [features]: nothing is behind a feature flag in this version`,
		},
		{
			name: "a feature is on or off, not a word", known: []Feature{budgetFlag}, file: "[features]\nbudget_targets = \"yes\"\n",
			wantErr: "reading {path}: toml: line 2 (last key \"features.budget_targets\"): incompatible types: TOML value has type string; destination has type boolean",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			withFeatures(t, tc.known)
			path := filepath.Join(t.TempDir(), "config.toml")
			writeFile(t, path, tc.file)

			// when
			got, err := Load(path, true)

			// then
			if diff := cmp.Diff(tc.wantErr, errText(err, path)); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, *got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("config mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_registryProblems(t *testing.T) {
	tcs := []struct {
		name  string
		known []Feature
		want  []string
	}{
		{name: "the features of this build", known: knownFeatures},
		{name: "a sound list", known: []Feature{budgetFlag, {Name: "forecast", Story: "TFC-4", About: "x"}}},
		{name: "a name that is not a TOML-friendly key", known: []Feature{{Name: "Budget-Targets", Story: "TBU-2", About: "x"}}, want: []string{`"Budget-Targets" is not a lower case name with underscores`}},
		{name: "the same name twice", known: []Feature{budgetFlag, budgetFlag}, want: []string{`"budget_targets" is there twice`}},
		{name: "no story", known: []Feature{{Name: "forecast", About: "x"}}, want: []string{`"forecast": "" is not a story ID like TBU-2`}},
		{name: "a story that is not an ID", known: []Feature{{Name: "forecast", Story: "forecast", About: "x"}}, want: []string{`"forecast": "forecast" is not a story ID like TBU-2`}},
		{name: "no description", known: []Feature{{Name: "forecast", Story: "TFC-4", About: " "}}, want: []string{`"forecast" has no description`}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.known

			// when
			got := registryProblems(tc.known)

			// then
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
