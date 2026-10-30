package core

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/google/go-cmp/cmp"
)

// cheapScrypt makes encrypting fast for the rest of the test: it matters here
// that a backup can be opened again, not how slow guessing a passphrase is.
func cheapScrypt(t *testing.T) {
	t.Helper()
	old := ScryptWorkFactor
	ScryptWorkFactor = 8
	t.Cleanup(func() { ScryptWorkFactor = old })
}

func Test_BackupEncrypted(t *testing.T) {
	type observed struct {
		Files      []string // everything under the folder, relative to it
		IsAge      bool     // the file starts like an age file
		LeaksData  bool     // the file contains the database's own text
		Mode       fs.FileMode
		Restored   string // the account found in the decrypted backup
		BackupGone bool   // the file that was there before is untouched
	}
	tcs := []struct {
		name       string
		dest       string // relative to the folder
		passphrase string
		existing   bool
		wantErr    string // {dest} stands for the file
		want       observed
	}{
		{
			name: "a new file in a new folder", dest: "backups/thaler.db.age", passphrase: "correct horse",
			want: observed{Files: []string{"backups/thaler.db.age", "thaler.db"}, IsAge: true, Mode: 0o600, Restored: "Assets:Bank"},
		},
		{
			name: "a passphrase with unusual characters", dest: "backups/ü.age", passphrase: "pässwörd ✓ with spaces",
			want: observed{Files: []string{"backups/ü.age", "thaler.db"}, IsAge: true, Mode: 0o600, Restored: "Assets:Bank"},
		},
		{
			name: "no passphrase", dest: "backups/thaler.db.age", wantErr: "an encrypted backup needs a passphrase",
			want: observed{Files: []string{"thaler.db"}},
		},
		{
			name: "a file that exists is never overwritten", dest: "taken.age", passphrase: "x", existing: true, wantErr: "{dest} already exists",
			want: observed{Files: []string{"taken.age", "thaler.db"}, BackupGone: false},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			cheapScrypt(t)
			dir := t.TempDir()
			st, err := Open(t.Context(), filepath.Join(dir, "thaler.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			accounts("Assets:Bank EUR")(t, st)
			dest := filepath.Join(dir, tc.dest)
			if tc.existing {
				if err := os.WriteFile(dest, []byte("precious"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			err = st.BackupEncrypted(t.Context(), dest, tc.passphrase)

			// then
			checkErr(t, err, strings.ReplaceAll(tc.wantErr, "{dest}", dest), nil)
			got := observed{Files: filesBelow(t, dir)}
			if tc.existing {
				got.BackupGone = readFile(t, dest) != "precious"
			}
			if tc.wantErr == "" {
				b, err := os.ReadFile(dest)
				if err != nil {
					t.Fatal(err)
				}
				got.IsAge = bytes.HasPrefix(b, []byte("age-encryption.org/v1"))
				got.LeaksData = bytes.Contains(b, []byte("SQLite format 3")) || bytes.Contains(b, []byte("Assets:Bank"))
				info, err := os.Stat(dest)
				if err != nil {
					t.Fatal(err)
				}
				got.Mode = info.Mode().Perm()

				plain := filepath.Join(dir, "restored.db")
				if err := DecryptBackup(t.Context(), dest, plain, tc.passphrase); err != nil {
					t.Fatalf("the backup does not decrypt: %v", err)
				}
				restored, err := Open(t.Context(), plain)
				if err != nil {
					t.Fatal(err)
				}
				defer restored.Close()
				acct, err := restored.GetAccount(t.Context(), "Assets:Bank")
				if err != nil {
					t.Fatalf("the backup does not hold the data: %v", err)
				}
				got.Restored = acct.Name
				got.Files = filesBelow(t, dir) // the restored copy is not part of what is being checked
				got.Files = withoutFile(got.Files, "restored.db")
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func withoutFile(files []string, name string) []string {
	var out []string
	for _, f := range files {
		if f != name && !strings.HasPrefix(f, name+"-") {
			out = append(out, f)
		}
	}
	return out
}

// filesBelow lists the files below dir, relative to it. The database's own
// write-ahead files are left out.
func filesBelow(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if !strings.HasSuffix(rel, "-wal") && !strings.HasSuffix(rel, "-shm") {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func Test_DecryptBackup(t *testing.T) {
	type observed struct {
		Files []string // under the folder, apart from the backup and the database
		Err   string
	}
	tcs := []struct {
		name       string
		passphrase string
		src        func(t *testing.T, st *Local, dir string) string // the file to open
		dest       string
		existing   bool
		want       observed
	}{
		{
			name: "the right passphrase", passphrase: "right",
			src:  func(t *testing.T, st *Local, dir string) string { return sealed(t, st, dir, "right") },
			dest: "out.db", want: observed{Files: []string{"out.db"}},
		},
		{
			name: "the wrong passphrase leaves nothing behind", passphrase: "wrong",
			src:  func(t *testing.T, st *Local, dir string) string { return sealed(t, st, dir, "right") },
			dest: "out.db", want: observed{Err: "decrypting {src}: incorrect passphrase"},
		},
		{
			name: "a file that is not an age file", passphrase: "right",
			src: func(t *testing.T, st *Local, dir string) string {
				p := filepath.Join(dir, "plain.txt")
				if err := os.WriteFile(p, []byte("just some text"), 0o600); err != nil {
					t.Fatal(err)
				}
				return p
			},
			dest: "out.db", want: observed{Err: "decrypting {src}: failed to read header: parsing age header: unexpected EOF reading intro: \"just some text\""},
		},
		{
			name: "a file that opens but is not a database", passphrase: "right",
			src: func(t *testing.T, st *Local, dir string) string {
				p := filepath.Join(dir, "notadb.age")
				f, err := os.Create(p)
				if err != nil {
					t.Fatal(err)
				}
				rcpt, err := age.NewScryptRecipient("right")
				if err != nil {
					t.Fatal(err)
				}
				w, err := age.Encrypt(f, rcpt)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte(strings.Repeat("not a database\n", 300))); err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				return p
			},
			dest: "out.db", want: observed{Err: "decrypting {src}: the result is not a healthy database: file is not a database (26)"},
		},
		{
			name: "a missing file", passphrase: "right",
			src:  func(t *testing.T, st *Local, dir string) string { return filepath.Join(dir, "nothing.age") },
			dest: "out.db", want: observed{Err: "open {src}: no such file or directory"},
		},
		{
			name: "an existing result is never overwritten", passphrase: "right", existing: true,
			src:  func(t *testing.T, st *Local, dir string) string { return sealed(t, st, dir, "right") },
			dest: "out.db", want: observed{Files: []string{"out.db"}, Err: "{dest} already exists"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			cheapScrypt(t)
			dir := t.TempDir()
			st, err := Open(t.Context(), ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			accounts("Assets:Bank EUR")(t, st)
			src := tc.src(t, st, dir)
			dest := filepath.Join(dir, tc.dest)
			if tc.existing {
				if err := os.WriteFile(dest, []byte("precious"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			err = DecryptBackup(t.Context(), src, dest, tc.passphrase)

			// then
			got := observed{Err: strings.ReplaceAll(strings.ReplaceAll(errText(err), src, "{src}"), dest, "{dest}")}
			for _, f := range filesBelow(t, dir) {
				if f != filepath.Base(src) {
					got.Files = append(got.Files, f)
				}
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if tc.existing {
				if diff := cmp.Diff("precious", readFile(t, dest)); diff != "" {
					t.Errorf("the existing file was touched (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// sealed writes an encrypted backup of st into dir and returns its path.
func sealed(t *testing.T, st *Local, dir, passphrase string) string {
	t.Helper()
	p := filepath.Join(dir, "sealed.age")
	if err := st.BackupEncrypted(t.Context(), p, passphrase); err != nil {
		t.Fatal(err)
	}
	return p
}
