package main

import "runtime/debug"

// version is set when the binary is built (make build passes it with -ldflags
// "-X main.version=..."), and later by the release workflow. It stays empty for
// a plain `go build` or `go install`: buildVersion then asks the Go toolchain.
var version string

// buildVersion is the version to print: the one set at build time, else the one the
// Go toolchain recorded (a tagged `go install thaler@v1.2.3` has it, and a build
// in a clone gets a pseudo-version with the commit), else "dev".
func buildVersion(set string, info *debug.BuildInfo, ok bool) string {
	switch {
	case set != "":
		return set
	case ok && info.Main.Version != "" && info.Main.Version != "(devel)":
		return info.Main.Version
	}
	return "dev"
}

// currentVersion is the version of the running binary.
func currentVersion() string {
	info, ok := debug.ReadBuildInfo()
	return buildVersion(version, info, ok)
}
