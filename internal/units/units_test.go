package units

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_HumanSize(t *testing.T) {
	tcs := []struct {
		name string
		n    int64
		want string
	}{
		{name: "nothing", n: 0, want: "0 B"},
		{name: "bytes", n: 1023, want: "1023 B"},
		{name: "a kilobyte", n: 1024, want: "1.0 KB"},
		{name: "kilobytes", n: 80 * 1024, want: "80.0 KB"},
		{name: "just under a megabyte", n: 1024*1024 - 1, want: "1024.0 KB"},
		{name: "a megabyte", n: 1024 * 1024, want: "1.0 MB"},
		{name: "megabytes", n: 3*1024*1024 + 104858, want: "3.1 MB"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := HumanSize(tc.n)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Plural(t *testing.T) {
	tcs := []struct {
		name string
		n    int
		want string
	}{
		{name: "none", n: 0, want: "backups"},
		{name: "one", n: 1, want: "backup"},
		{name: "two", n: 2, want: "backups"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := Plural(tc.n, "backup", "backups")

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
