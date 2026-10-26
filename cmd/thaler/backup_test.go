package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_backup(t *testing.T) {
	tcs := []struct {
		name    string
		files   map[string]string
		answers []string // what the passphrase prompts are answered with
		args    []string
		want    string
		wantErr string
		made    string // the file that should exist afterwards, "" for none
	}{
		{
			name: "plain, to a path", args: []string{"cli", "backup", "{dir}/b.db"},
			want: "Backup written to {dir}/b.db (not encrypted)\n", made: "b.db",
		},
		{
			name: "with a passphrase file", files: map[string]string{"pass.txt": "secret\n"},
			args: []string{"cli", "backup", "{dir}/b.db", "--passphrase-file", "{dir}/pass.txt"},
			want: "Encrypted backup written to {dir}/b.db.age\n", made: "b.db.age",
		},
		{
			name: "a passphrase file with Windows line endings", files: map[string]string{"pass.txt": "secret\r\nsecond line\r\n"},
			args: []string{"cli", "backup", "{dir}/b.db", "--passphrase-file", "{dir}/pass.txt"},
			want: "Encrypted backup written to {dir}/b.db.age\n", made: "b.db.age",
		},
		{
			name: "asked for twice with --encrypt", answers: []string{"secret", "secret"},
			args: []string{"cli", "backup", "{dir}/b.db.age", "--encrypt"},
			want: "Encrypted backup written to {dir}/b.db.age\n", made: "b.db.age",
		},
		{
			name: "asked twice, answered differently", answers: []string{"secret", "sekret"},
			args: []string{"cli", "backup", "{dir}/b.db", "--encrypt"}, wantErr: "the two passphrases differ",
		},
		{
			name: "asked, answered with nothing", answers: []string{""},
			args: []string{"cli", "backup", "{dir}/b.db", "--encrypt"}, wantErr: "the passphrase is empty",
		},
		{
			name: "no terminal to ask on", args: []string{"cli", "backup", "{dir}/b.db", "--encrypt"},
			wantErr: "a passphrase is asked for on a terminal; for scripts use --passphrase-file",
		},
		{
			name: "an empty passphrase file", files: map[string]string{"pass.txt": "\nsecond line\n"},
			args:    []string{"cli", "backup", "{dir}/b.db", "--passphrase-file", "{dir}/pass.txt"},
			wantErr: "{dir}/pass.txt has no passphrase on its first line",
		},
		{
			name: "a passphrase file that is not there", args: []string{"cli", "backup", "{dir}/b.db", "--passphrase-file", "{dir}/nope.txt"},
			wantErr: "open {dir}/nope.txt: no such file or directory",
		},
		{
			name: "a file that exists is not overwritten", files: map[string]string{"b.db": "precious"},
			args: []string{"cli", "backup", "{dir}/b.db"}, wantErr: "{dir}/b.db already exists",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, addBank)
			e.answers = tc.answers

			// when
			got, err := e.run(tc.args...)

			// then
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			if tc.made != "" {
				if _, err := os.Stat(filepath.Join(e.dir, tc.made)); err != nil {
					t.Errorf("the backup was not written: %v", err)
				}
			}
		})
	}
}

func Test_decrypt(t *testing.T) {
	sealedBy := func(pass string) [][]string {
		return [][]string{addBank, {"cli", "backup", "{dir}/b.db", "--passphrase-file", "{dir}/" + pass}}
	}
	tcs := []struct {
		name    string
		files   map[string]string
		answers []string
		setup   [][]string
		args    []string
		want    string
		wantErr string
		made    string
	}{
		{
			name: "with a passphrase file", files: map[string]string{"right.txt": "right\n"}, setup: sealedBy("right.txt"),
			args: []string{"cli", "decrypt", "{dir}/b.db.age", "--passphrase-file", "{dir}/right.txt"},
			want: "Decrypted to {dir}/b.db (use it with: thaler --db {dir}/b.db)\n", made: "b.db",
		},
		{
			name: "asked for the passphrase", files: map[string]string{"right.txt": "right\n"}, setup: sealedBy("right.txt"),
			answers: []string{"right"}, args: []string{"cli", "decrypt", "{dir}/b.db.age", "{dir}/out.db"},
			want: "Decrypted to {dir}/out.db (use it with: thaler --db {dir}/out.db)\n", made: "out.db",
		},
		{
			name: "the wrong passphrase", files: map[string]string{"right.txt": "right\n", "wrong.txt": "wrong\n"}, setup: sealedBy("right.txt"),
			args:    []string{"cli", "decrypt", "{dir}/b.db.age", "--passphrase-file", "{dir}/wrong.txt"},
			wantErr: "decrypting {dir}/b.db.age: incorrect passphrase",
		},
		{
			name: "a destination that exists is not overwritten, not even by the backup itself", files: map[string]string{"right.txt": "right\n"}, setup: sealedBy("right.txt"),
			args:    []string{"cli", "decrypt", "{dir}/b.db.age", "--passphrase-file", "{dir}/right.txt", "{dir}/b.db.age"},
			wantErr: "{dir}/b.db.age already exists",
		},
		{
			name: "no ending to take off", files: map[string]string{"right.txt": "right\n", "plain": "x"},
			args:    []string{"cli", "decrypt", "{dir}/plain", "--passphrase-file", "{dir}/right.txt"},
			wantErr: "{dir}/plain does not end in .age: say where to write the result",
		},
		{
			name: "no terminal to ask on", files: map[string]string{"right.txt": "right\n"}, setup: sealedBy("right.txt"),
			args:    []string{"cli", "decrypt", "{dir}/b.db.age"},
			wantErr: "a passphrase is asked for on a terminal; for scripts use --passphrase-file",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)
			e.answers = tc.answers

			// when
			got, err := e.run(tc.args...)

			// then
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			if tc.made != "" {
				if _, err := os.Stat(filepath.Join(e.dir, tc.made)); err != nil {
					t.Errorf("the result was not written: %v", err)
				}
			}
		})
	}
}
