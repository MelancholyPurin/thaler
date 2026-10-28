package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
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

func Test_restore(t *testing.T) {
	timestamp := regexp.MustCompile(`thaler-\d{8}-\d{6}-replaced`)
	listing := []string{"cli", "account", "list"}
	tcs := []struct {
		name    string
		files   map[string]string
		answers []string
		setup   [][]string // after the backup is made and Savings is added
		noDB    bool       // delete the database before restoring
		args    []string
		want    string // the output, the folder of the database shown as {db} and the time as {time}
		wantErr string
		after   string // the account list afterwards
	}{
		{
			name: "a plain backup", args: []string{"cli", "restore", "{dir}/b.db"},
			want:  "Restored {dir}/b.db as {db}/t.db\nThe database it replaced is kept at {db}/backups/replaced/thaler-{time}-replaced.db\nTo undo this, run: thaler --db {db}/t.db cli restore {db}/backups/replaced/thaler-{time}-replaced.db\n",
			after: "Assets:Bank  Assets, EUR\n",
		},
		{
			name: "an encrypted backup, with a passphrase file", files: map[string]string{"pass.txt": "secret\n"},
			setup: [][]string{{"cli", "backup", "{dir}/s.db", "--passphrase-file", "{dir}/pass.txt"}},
			args:  []string{"cli", "restore", "{dir}/s.db.age", "--passphrase-file", "{dir}/pass.txt"},
			want:  "Restored {dir}/s.db.age as {db}/t.db\nThe database it replaced is kept at {db}/backups/replaced/thaler-{time}-replaced.db\nTo undo this, run: thaler --db {db}/t.db cli restore {db}/backups/replaced/thaler-{time}-replaced.db\n",
			after: "Assets:Bank     Assets, EUR\nAssets:Savings  Assets, EUR\n",
		},
		{
			name: "an encrypted backup, asked for the passphrase", files: map[string]string{"pass.txt": "secret\n"}, answers: []string{"secret"},
			setup: [][]string{{"cli", "backup", "{dir}/s.db", "--passphrase-file", "{dir}/pass.txt"}},
			args:  []string{"cli", "restore", "{dir}/s.db.age"},
			want:  "Restored {dir}/s.db.age as {db}/t.db\nThe database it replaced is kept at {db}/backups/replaced/thaler-{time}-replaced.db\nTo undo this, run: thaler --db {db}/t.db cli restore {db}/backups/replaced/thaler-{time}-replaced.db\n",
			after: "Assets:Bank     Assets, EUR\nAssets:Savings  Assets, EUR\n",
		},
		{
			name: "with no database in place", noDB: true, args: []string{"cli", "restore", "{dir}/b.db"},
			want:  "Restored {dir}/b.db as {db}/t.db\nThere was no database to replace.\n",
			after: "Assets:Bank  Assets, EUR\n",
		},
		{
			name: "the wrong passphrase", files: map[string]string{"pass.txt": "secret\n", "wrong.txt": "wrong\n"},
			setup:   [][]string{{"cli", "backup", "{dir}/s.db", "--passphrase-file", "{dir}/pass.txt"}},
			args:    []string{"cli", "restore", "{dir}/s.db.age", "--passphrase-file", "{dir}/wrong.txt"},
			wantErr: "decrypting {dir}/s.db.age: incorrect passphrase",
			after:   "Assets:Bank     Assets, EUR\nAssets:Savings  Assets, EUR\n",
		},
		{
			name: "no terminal to ask on", files: map[string]string{"pass.txt": "secret\n"},
			setup:   [][]string{{"cli", "backup", "{dir}/s.db", "--passphrase-file", "{dir}/pass.txt"}},
			args:    []string{"cli", "restore", "{dir}/s.db.age"},
			wantErr: "a passphrase is asked for on a terminal; for scripts use --passphrase-file",
			after:   "Assets:Bank     Assets, EUR\nAssets:Savings  Assets, EUR\n",
		},
		{
			name: "a file that is not a backup", files: map[string]string{"junk.txt": "text"}, args: []string{"cli", "restore", "{dir}/junk.txt"},
			wantErr: "{dir}/junk.txt is not a backup: it is neither a SQLite database nor an age file",
			after:   "Assets:Bank     Assets, EUR\nAssets:Savings  Assets, EUR\n",
		},
		{
			name: "a file that is not there", args: []string{"cli", "restore", "{dir}/nope.db"},
			wantErr: "{dir}/nope.db does not exist",
			after:   "Assets:Bank     Assets, EUR\nAssets:Savings  Assets, EUR\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given: a ledger with Bank, a backup of it, and then Savings added after the backup
			e := newCLIEnv(t, tc.files, addBank, []string{"cli", "backup", "{dir}/b.db"})
			if _, err := e.run(addSavings...); err != nil {
				t.Fatal(err)
			}
			for _, args := range tc.setup {
				if out, err := e.run(args...); err != nil {
					t.Fatalf("setup %v: %q, %v", args, out, err)
				}
			}
			if tc.noDB {
				for _, suffix := range []string{"", "-wal", "-shm"} {
					os.Remove(e.db + suffix)
				}
			}
			e.answers = tc.answers

			// when
			got, err := e.run(tc.args...)

			// then
			dbDir := filepath.Dir(e.db)
			got = timestamp.ReplaceAllString(strings.ReplaceAll(got, dbDir, "{db}"), "thaler-{time}-replaced")
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			e.answers = nil
			after, err := e.run(listing...)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.after, after); diff != "" {
				t.Errorf("the ledger afterwards is wrong (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_restoreMessage(t *testing.T) {
	tcs := []struct {
		name     string
		customDB bool
		res      core.RestoreResult
		want     string
	}{
		{
			name: "a database was replaced",
			res:  core.RestoreResult{Replaced: "/d/backups/replaced/old.db"},
			want: "Restored /b/x.db as /d/thaler.db\nThe database it replaced is kept at /d/backups/replaced/old.db\nTo undo this, run: thaler cli restore /d/backups/replaced/old.db\n",
		},
		{
			name: "with --db the undo says so", customDB: true,
			res:  core.RestoreResult{Replaced: "/d/backups/replaced/old.db"},
			want: "Restored /b/x.db as /d/thaler.db\nThe database it replaced is kept at /d/backups/replaced/old.db\nTo undo this, run: thaler --db /d/thaler.db cli restore /d/backups/replaced/old.db\n",
		},
		{name: "nothing was replaced", want: "Restored /b/x.db as /d/thaler.db\nThere was no database to replace.\n"},
		{
			name: "an old backup is upgraded later",
			res:  core.RestoreResult{Replaced: "/d/backups/replaced/old.db", Upgrade: true},
			want: "Restored /b/x.db as /d/thaler.db\nThe database it replaced is kept at /d/backups/replaced/old.db\nTo undo this, run: thaler cli restore /d/backups/replaced/old.db\nIt was made by an older version of Thaler, so it is upgraded, after a copy of it, the next time Thaler starts.\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := restoreMessage("/b/x.db", "/d/thaler.db", tc.customDB, tc.res)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The interface restores through swapDatabase: it must end up holding the service
// of whatever database is in place, also when the restore is refused.
func Test_swapDatabase(t *testing.T) {
	type observed struct {
		Accounts    []string
		Err         string
		Replaced    bool // the database that was in place is kept
		HoldsResult bool // the app closes the service it returned
	}
	tcs := []struct {
		name string
		src  string // "backup" is a backup made before the second account was added
		want observed
	}{
		{
			name: "a backup replaces the database", src: "backup",
			want: observed{Accounts: []string{"Assets:Bank"}, Replaced: true, HoldsResult: true},
		},
		{
			name: "a file that is not there is refused and the old database is open again", src: "{dir}/nothing.db",
			want: observed{Accounts: []string{"Assets:Bank", "Assets:Cash"}, Err: "{dir}/nothing.db does not exist", HoldsResult: true},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			dir := t.TempDir()
			db := filepath.Join(dir, "t.db")
			a := &app{dbFlag: db, notice: io.Discard}
			t.Cleanup(a.close)
			svc, err := a.service(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.AddAccount(t.Context(), "Assets:Bank", "EUR"); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(dir, "backups", "thaler-20261021-090000.db")
			if err := svc.Backup(t.Context(), backup); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.AddAccount(t.Context(), "Assets:Cash", "EUR"); err != nil {
				t.Fatal(err)
			}
			src := strings.ReplaceAll(strings.Replace(tc.src, "backup", backup, 1), "{dir}", dir)

			// when
			got, _, swapErr := a.swapDatabase(t.Context(), src, "")

			// then
			var o observed
			o.Accounts = accountNames(t, got)
			if swapErr != nil {
				o.Err = strings.ReplaceAll(swapErr.Error(), dir, "{dir}")
			}
			replaced, _ := filepath.Glob(filepath.Join(dir, "backups", "replaced", "thaler-*-replaced.db"))
			o.Replaced = len(replaced) == 1
			o.HoldsResult = a.svc == got
			if diff := cmp.Diff(tc.want, o); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func accountNames(t *testing.T, svc core.Service) []string {
	t.Helper()
	accs, err := svc.ListAccounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range accs {
		names = append(names, a.Name)
	}
	return names
}
