package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_SetTheme(t *testing.T) {
	const header = "# Created by Thaler. The sample below is switched off: remove the leading # from a line to use it.\n#\n"
	tcs := []struct {
		name     string
		existing *string // nil: no file yet
		example  string
		theme    string
		want     string // the file after
		wantErr  string
	}{
		{
			name:    "no file yet: it starts from the example, and the line goes under the sample of the setting",
			example: "# The theme.\n# theme = \"light\"\n\n# keep_backups = 20\n",
			theme:   "dark",
			want:    header + "# The theme.\n# theme = \"light\"\ntheme = \"dark\"\n\n# keep_backups = 20\n",
		},
		{
			name:     "an existing setting is replaced where it stands, with its comment",
			existing: ptr("# mine\n  theme = \"dark\"   # for the evening\nkeep_backups = 3\n"),
			theme:    "light",
			want:     "# mine\n  theme = \"light\"   # for the evening\nkeep_backups = 3\n",
		},
		{
			name:     "the language is not the theme, nor the theme the language",
			existing: ptr("language = \"de\"\ntheme = \"dark\"\n"),
			theme:    "auto",
			want:     "language = \"de\"\ntheme = \"auto\"\n",
		},
		{
			name:     "the sample of the language is not the sample of the theme",
			existing: ptr("# language = \"de\"\nkeep_backups = 3\n"),
			theme:    "dark",
			want:     "# language = \"de\"\nkeep_backups = 3\ntheme = \"dark\"\n",
		},
		{
			name:     "no setting and no sample: above the first table, the rest as it was",
			existing: ptr("language = \"es\"\n\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n"),
			theme:    "light",
			want:     "language = \"es\"\n\ntheme = \"light\"\n\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n",
		},
		{
			name:     "a theme there is not is refused, and the file is left alone",
			existing: ptr("keep_backups = 3\n"),
			theme:    "solarized",
			want:     "keep_backups = 3\n",
			wantErr:  "unknown_theme(Name=solarized)",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "thaler", "config.toml")
			if tc.existing != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, *tc.existing)
			}

			// when
			err := SetTheme(path, tc.example, tc.theme)

			// then
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if diff := cmp.Diff(tc.wantErr, gotErr); diff != "" {
				t.Fatalf("error mismatch (-want +got):\n%s", diff)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, string(body)); diff != "" {
				t.Errorf("file mismatch (-want +got):\n%s", diff)
			}
			if tc.wantErr != "" {
				return
			}
			cfg, err := Load(path, true)
			if err != nil {
				t.Fatalf("the file no longer loads: %v", err)
			}
			if diff := cmp.Diff(tc.theme, cfg.Theme); diff != "" {
				t.Errorf("theme mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
