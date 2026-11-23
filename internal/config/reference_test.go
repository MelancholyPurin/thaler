package config

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// referenceRow is a row of a table in docs/config.md: its first cell is a key, in full.
var referenceRow = regexp.MustCompile("(?m)^\\| `([a-z_.<>]+)` \\|")

// Test_reference checks that docs/config.md and its translations document every key
// config.toml takes, and no key it does not: a key added to Config without a row
// there, or a row left behind by a key that is gone, fails here. (changed-paths.sh runs
// the Go checks for a change to those pages, so editing one alone cannot get past this.)
func Test_reference(t *testing.T) {
	tcs := []struct {
		name string
		page string
	}{
		{name: "the reference page", page: "../../docs/config.md"},
		{name: "its Spanish translation", page: "../../docs/config.es.md"},
		{name: "its German translation", page: "../../docs/config.de.md"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			text, err := os.ReadFile(tc.page)
			if err != nil {
				t.Fatal(err)
			}
			want := tomlKeys(reflect.TypeFor[Config](), "")
			slices.Sort(want)

			// when
			var got []string
			for _, m := range referenceRow.FindAllStringSubmatch(string(text), -1) {
				got = append(got, m[1])
			}
			slices.Sort(got)

			// then
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("the keys of %s (-config.toml takes +the page documents):\n%s", tc.page, diff)
			}
		})
	}
}

// tomlKeys lists the keys a struct takes from TOML, in full (profile.columns.date). A
// table of tables ([[rule]]) is named like a table; a map is any name under its key.
func tomlKeys(typ reflect.Type, prefix string) []string {
	var out []string
	for f := range typ.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
		if name == "" || name == "-" {
			continue
		}
		key := prefix + name
		inner := f.Type
		for inner.Kind() == reflect.Pointer || inner.Kind() == reflect.Slice {
			inner = inner.Elem()
		}
		switch inner.Kind() {
		case reflect.Struct:
			out = append(out, tomlKeys(inner, key+".")...)
		case reflect.Map:
			out = append(out, key+".<name>")
		default:
			out = append(out, key)
		}
	}
	return out
}
