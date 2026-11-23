package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Test_Open_private checks that the database and the files SQLite keeps beside it are
// readable by you alone once it is open, however they were before.
func Test_Open_private(t *testing.T) {
	// The permissions of a new file come from the umask; the usual one lets others
	// read, which is what the database must not inherit.
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })

	tcs := []struct {
		name   string
		before map[string]fs.FileMode // the database ("") and its -wal and -shm, where they exist
		want   map[string]fs.FileMode
	}{
		{
			name: "a new database",
			want: map[string]fs.FileMode{"": 0o600, "-wal": 0o600, "-shm": 0o600},
		},
		{
			name:   "a database others can read",
			before: map[string]fs.FileMode{"": 0o644},
			want:   map[string]fs.FileMode{"": 0o600, "-wal": 0o600, "-shm": 0o600},
		},
		{
			name:   "a database and its log others can read and write",
			before: map[string]fs.FileMode{"": 0o666, "-wal": 0o666, "-shm": 0o644},
			want:   map[string]fs.FileMode{"": 0o600, "-wal": 0o600, "-shm": 0o600},
		},
		{
			name:   "a database your group can read",
			before: map[string]fs.FileMode{"": 0o640},
			want:   map[string]fs.FileMode{"": 0o600, "-wal": 0o600, "-shm": 0o600},
		},
		{
			name:   "a database that is already private",
			before: map[string]fs.FileMode{"": 0o600},
			want:   map[string]fs.FileMode{"": 0o600, "-wal": 0o600, "-shm": 0o600},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "thaler.db")
			if _, ok := tc.before[""]; ok {
				st, err := Open(t.Context(), path)
				if err != nil {
					t.Fatal(err)
				}
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for suffix, mode := range tc.before {
				if suffix != "" { // an empty -wal or -shm, as a crash can leave them
					if err := os.WriteFile(path+suffix, nil, mode); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Chmod(path+suffix, mode); err != nil {
					t.Fatal(err)
				}
			}

			// when
			st, err := Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { st.Close() })
			if _, err := st.AddAccount(t.Context(), "Assets:Bank", "EUR"); err != nil {
				t.Fatal(err)
			}

			// then
			got := map[string]fs.FileMode{}
			for _, suffix := range dbFiles {
				if info, err := os.Stat(path + suffix); err == nil {
					got[suffix] = info.Mode().Perm()
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("permissions (-want +got):\n%s", diff)
			}
		})
	}
}
