package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_WriteNewFile(t *testing.T) {
	tcs := []struct {
		name    string
		prior   *string // what the file holds already, if it is there
		dir     string  // a folder inside the temporary one, left missing
		want    string
		wantErr string
	}{
		{name: "a new file", want: "a,b\n"},
		{name: "a file that is there is kept", prior: new("keep"), want: "keep", wantErr: "FILE is already there: give another name"},
		{name: "a folder that is not there", dir: "missing", want: "", wantErr: "no such file or directory"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), tc.dir, "out.csv")
			if tc.prior != nil {
				if err := os.WriteFile(path, []byte(*tc.prior), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			err := WriteNewFile(path, []byte("a,b\n"))

			// then
			got := ""
			if err != nil {
				got = errText(err)
			}
			got = strings.ReplaceAll(got, path, "FILE")
			if tc.wantErr == "" {
				if diff := cmp.Diff(tc.wantErr, got); diff != "" {
					t.Errorf("error mismatch (-want +got):\n%s", diff)
				}
			} else if !strings.Contains(got, tc.wantErr) {
				t.Errorf("error %q lacks %q", got, tc.wantErr)
			}
			body, readErr := os.ReadFile(path)
			if tc.dir == "" {
				if readErr != nil {
					t.Fatal(readErr)
				}
				if diff := cmp.Diff(tc.want, string(body)); diff != "" {
					t.Errorf("file mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func Test_WriteNewFile_private(t *testing.T) {
	tcs := []struct {
		name string
		want os.FileMode
	}{
		{name: "readable by you alone", want: 0o600},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "out.csv")

			// when
			if err := WriteNewFile(path, []byte("x")); err != nil {
				t.Fatal(err)
			}

			// then
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, info.Mode().Perm()); diff != "" {
				t.Errorf("mode mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
