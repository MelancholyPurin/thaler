package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/importer"
)

func Test_editorCommand(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("not found") }
	only := func(name string) func(string) (string, error) {
		return func(n string) (string, error) {
			if n == name {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("not found")
		}
	}
	tcs := []struct {
		name    string
		vars    map[string]string
		look    func(string) (string, error)
		want    []string
		wantErr string
	}{
		{name: "VISUAL wins", vars: map[string]string{"VISUAL": "hx", "EDITOR": "nano"}, look: none, want: []string{"hx", "/c.toml"}},
		{name: "EDITOR", vars: map[string]string{"EDITOR": "nano"}, look: none, want: []string{"nano", "/c.toml"}},
		{name: "with arguments", vars: map[string]string{"EDITOR": "code --wait"}, look: none, want: []string{"code", "--wait", "/c.toml"}},
		{name: "a blank VISUAL is unset", vars: map[string]string{"VISUAL": "  ", "EDITOR": "vim"}, look: none, want: []string{"vim", "/c.toml"}},
		{name: "vi when it is all there is", look: only("vi"), want: []string{"vi", "/c.toml"}},
		{name: "nano before vi", look: func(string) (string, error) { return "x", nil }, want: []string{"nano", "/c.toml"}},
		{name: "nothing at all", look: none, wantErr: "no_editor"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			getenv := func(k string) string { return tc.vars[k] }

			// when
			cmd, err := editorCommand(getenv, tc.look, "/c.toml")

			// then
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if diff := cmp.Diff(tc.wantErr, gotErr); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			var got []string
			if cmd != nil {
				got = cmd.Args
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("command mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_configKey(t *testing.T) {
	const (
		template = "[[rule]]\ncontains = \"x\"\ncategory = \"Expenses:X\"\n"
		header   = "# Created by Thaler. The sample below is switched off: remove the leading # from a line to use it.\n#\n"
	)
	type observed struct {
		EditorStarted bool
		Status        string
		StatusIsError bool
		File          string // the config afterwards; "" when there is no file
	}
	tcs := []struct {
		name     string
		existing *string
		visual   string
		editor   string
		noPath   bool // the editor cannot be found
		script   string
		want     observed
	}{
		{
			name: "C creates the file from the example, switched off, and starts the editor", editor: "true", script: "L",
			want: observed{EditorStarted: true, File: header + "# [[rule]]\n# contains = \"x\"\n# category = \"Expenses:X\"\n"},
		},
		{
			name: "on the budget screen too", editor: "true", script: "B",
			want: observed{EditorStarted: true, File: header + "# [[rule]]\n# contains = \"x\"\n# category = \"Expenses:X\"\n"},
		},
		{
			name: "on the forecast", editor: "true", script: "F",
			want: observed{EditorStarted: true, File: header + "# [[rule]]\n# contains = \"x\"\n# category = \"Expenses:X\"\n"},
		},
		{
			name: "on the reports", editor: "true", script: "R",
			want: observed{EditorStarted: true, File: header + "# [[rule]]\n# contains = \"x\"\n# category = \"Expenses:X\"\n"},
		},
		{
			name: "an existing file is left alone", existing: ptr("# mine\n"), editor: "true", script: "L",
			want: observed{EditorStarted: true, File: "# mine\n"},
		},
		{
			name: "with no editor it says what to set and creates nothing", noPath: true, script: "L",
			want: observed{Status: "no editor found: set $VISUAL or $EDITOR, e.g. export EDITOR=nano", StatusIsError: true},
		},
		{
			name: "VISUAL is tried first", visual: "true", editor: "false", script: "L",
			want: observed{EditorStarted: true, File: header + "# [[rule]]\n# contains = \"x\"\n# category = \"Expenses:X\"\n"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			path := filepath.Join(t.TempDir(), "thaler", "config.toml")
			m.opts = Options{ConfigPath: path, ConfigTemplate: template}
			if tc.existing != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(*tc.existing), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("VISUAL", tc.visual)
			t.Setenv("EDITOR", tc.editor)
			if tc.noPath {
				t.Setenv("PATH", "")
			}
			play(m, tc.script)

			// when
			_, cmd := m.Update(key("C"))

			// then
			got := observed{EditorStarted: cmd != nil, Status: m.status, StatusIsError: m.statusIsError}
			if body, err := os.ReadFile(path); err == nil {
				got.File = string(body)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func ptr(s string) *string { return &s }

// C is just a letter while a popup has the keyboard.
func Test_Update_configKey_inAPopup(t *testing.T) {
	type observed struct {
		Mode          string
		Typed         string // what the popup's focused field holds
		ConfigCreated bool
	}
	tcs := []struct {
		name   string
		script string
		typed  func(m *Model) string
		want   observed
	}{
		{name: "the add form", script: "a", typed: func(m *Model) string { return m.form.amount.Value() }, want: observed{"form", "C", false}},
		{name: "the transfer popup", script: "x", typed: func(m *Model) string { return m.xfer.amount.Value() }, want: observed{"transfer", "C", false}},
		{name: "the search box", script: "/", typed: func(m *Model) string { return m.filter.Value() }, want: observed{"filter", "C", false}},
		{name: "the category picker", script: "3 u c", typed: func(m *Model) string { return m.picker.Value() }, want: observed{"pick", "C", false}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			m.opts = Options{ConfigPath: filepath.Join(t.TempDir(), "config.toml")}
			play(m, tc.script)

			// when
			m.Update(key("C"))

			// then
			_, err := os.Stat(m.opts.ConfigPath)
			got := observed{modeNames[m.mode], tc.typed(m), err == nil}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("the letter belongs to the popup (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_configEdited(t *testing.T) {
	rules := func() (*config.Config, error) {
		return &config.Config{Rules: make([]importer.Rule, 3), Profiles: make([]importer.Profile, 1)}, nil
	}
	broken := func() (*config.Config, error) {
		return nil, errors.New("config.toml: unknown key(s): [rule.typo]")
	}
	tcs := []struct {
		name      string
		load      ConfigLoader
		editorErr error
		wantMsg   string
		wantErr   bool
	}{
		{name: "saved", load: rules, wantMsg: "Config saved (rules: 3, bank profiles: 1); used by the next import"},
		{
			name: "saved with a mistake", load: broken,
			wantMsg: "The config has a problem, imports will refuse it until it is fixed (press C): config.toml: unknown key(s): [rule.typo]", wantErr: true,
		},
		{name: "the editor failed", load: rules, editorErr: errors.New("exit status 1"), wantMsg: "the editor failed: exit status 1", wantErr: true},
		{name: "no way to read it", load: nil, wantMsg: "Config closed"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			m.loadConfig = tc.load

			// when
			m.Update(configEditedMsg{err: tc.editorErr})

			// then
			type observed struct {
				Status        string
				StatusIsError bool
			}
			if diff := cmp.Diff(observed{tc.wantMsg, tc.wantErr}, observed{m.status, m.statusIsError}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
