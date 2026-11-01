package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_SetLanguage(t *testing.T) {
	const header = "# Created by Thaler. The sample below is switched off: remove the leading # from a line to use it.\n#\n"
	tcs := []struct {
		name     string
		existing *string // nil: no file yet
		example  string
		code     string
		mode     os.FileMode
		invalid  bool   // the file is not a valid config to begin with, so it cannot be loaded after
		want     string // the file after
		wantErr  string
	}{
		{
			name:    "no file yet: it starts from the example, and the line goes under the sample of the setting",
			example: "# The language.\n# language = \"de\"\n\n# keep_backups = 20\n",
			code:    "es",
			want:    header + "# The language.\n# language = \"de\"\nlanguage = \"es\"\n\n# keep_backups = 20\n",
		},
		{
			name:     "no setting and no sample: above the first table",
			existing: ptr("import_dir = \"/x\"\n\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n"),
			code:     "de",
			want:     "import_dir = \"/x\"\n\nlanguage = \"de\"\n\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n",
		},
		{
			name:     "an existing setting is replaced where it stands, with its comment",
			existing: ptr("# mine\n  language = \"en\"   # since the move\nkeep_backups = 3\n"),
			code:     "de",
			want:     "# mine\n  language = \"de\"   # since the move\nkeep_backups = 3\n",
		},
		{
			name:     "a setting in single quotes",
			existing: ptr("language = 'en'\n"),
			code:     "es",
			want:     "language = \"es\"\n",
		},
		{
			name:     "the same language again changes nothing",
			existing: ptr("language = \"de\"\nkeep_backups = 3\n"),
			code:     "de",
			want:     "language = \"de\"\nkeep_backups = 3\n",
		},
		{
			name:     "a setting inside a table is not the language: the line goes above the table",
			invalid:  true,
			existing: ptr("[features]\nlanguage = true\n"),
			code:     "de",
			want:     "language = \"de\"\n\n[features]\nlanguage = true\n",
		},
		{
			name:     "no tables: at the end, after text without a newline",
			existing: ptr("keep_backups = 3"),
			code:     "es",
			want:     "keep_backups = 3\nlanguage = \"es\"\n",
		},
		{
			name:     "no tables: at the end, after text with a newline",
			existing: ptr("keep_backups = 3\n"),
			code:     "es",
			want:     "keep_backups = 3\nlanguage = \"es\"\n",
		},
		{
			name:     "an empty file",
			existing: ptr(""),
			code:     "en",
			want:     "language = \"en\"\n",
		},
		{
			name:     "the permissions of the file stay",
			existing: ptr("keep_backups = 3\n"),
			code:     "de",
			mode:     0o640,
			want:     "keep_backups = 3\nlanguage = \"de\"\n",
		},
		{
			name:     "a language Thaler does not speak is refused, and the file is left alone",
			existing: ptr("keep_backups = 3\n"),
			code:     "fr",
			want:     "keep_backups = 3\n",
			wantErr:  "unknown_language(Code=fr)",
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
				if tc.mode != 0 {
					if err := os.Chmod(path, tc.mode); err != nil {
						t.Fatal(err)
					}
				}
			}

			// when
			err := SetLanguage(path, tc.example, tc.code)

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
			if tc.wantErr != "" || tc.invalid {
				return
			}
			cfg, err := Load(path, true)
			if err != nil {
				t.Fatalf("the file no longer loads: %v", err)
			}
			if diff := cmp.Diff(tc.code, cfg.Language); diff != "" {
				t.Errorf("language mismatch (-want +got):\n%s", diff)
			}
			if tc.mode != 0 {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(tc.mode, info.Mode().Perm()); diff != "" {
					t.Errorf("mode mismatch (-want +got):\n%s", diff)
				}
			}
			left, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".thaler-config-*"))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff([]string(nil), left); diff != "" {
				t.Errorf("temporary files left behind (-want +got):\n%s", diff)
			}
		})
	}
}
