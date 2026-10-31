package main

import (
	"runtime/debug"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_buildVersion(t *testing.T) {
	tcs := []struct {
		name string
		set  string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{name: "the version set at build time wins", set: "v2.0.0", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.0.0"}}, ok: true, want: "v2.0.0"},
		{name: "the toolchain's version for go install", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.4.0"}}, ok: true, want: "v1.4.0"},
		{name: "a pseudo-version from a clone", info: &debug.BuildInfo{Main: debug.Module{Version: "v1.4.1-0.20261101120000-abcdef123456+dirty"}}, ok: true, want: "v1.4.1-0.20261101120000-abcdef123456+dirty"},
		{name: "a build with no version information", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, ok: true, want: "dev"},
		{name: "an empty version", info: &debug.BuildInfo{}, ok: true, want: "dev"},
		{name: "no build information at all", want: "dev"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := buildVersion(tc.set, tc.info, tc.ok)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
