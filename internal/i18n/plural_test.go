package i18n

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"
)

// "1 backup" and "2 backups", in the way each language counts.
func Test_Localizer_T_plural(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		count any
		want  string
	}{
		{name: "English, one", tag: language.English, count: 1, want: "Removed 1 backup (5 B)."},
		{name: "English, two", tag: language.English, count: 2, want: "Removed 2 backups (5 B)."},
		{name: "English, zero", tag: language.English, count: 0, want: "Removed 0 backups (5 B)."},
		{name: "Spanish, one", tag: language.Spanish, count: 1, want: "Se eliminó 1 copia de seguridad (5 B)."},
		{name: "Spanish, many", tag: language.Spanish, count: 3, want: "Se eliminaron 3 copias de seguridad (5 B)."},
		{name: "Spanish, zero", tag: language.Spanish, count: 0, want: "Se eliminaron 0 copias de seguridad (5 B)."},
		{name: "German, one", tag: language.German, count: 1, want: "1 Sicherung entfernt (5 B)."},
		{name: "German, many", tag: language.German, count: 12, want: "12 Sicherungen entfernt (5 B)."},
		{name: "a count of another integer type", tag: language.English, count: int64(1), want: "Removed 1 backup (5 B)."},
		{name: "an unsigned count", tag: language.English, count: uint8(1), want: "Removed 1 backup (5 B)."},
		{name: "a region of German", tag: language.MustParse("de-AT"), count: 1, want: "1 Sicherung entfernt (5 B)."},
		{name: "the pseudo-language keeps the forms", tag: PseudoTag, count: 1, want: "[Rémövéd 1 báçkúp (5 B). ~~~~~~~~]"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got := loc.T("backups_removed", map[string]any{"Count": tc.count, "Size": "5 B"})

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A count that is not a whole number does not choose a form: the general one is used.
func Test_Localizer_T_plural_notACount(t *testing.T) {
	tcs := []struct {
		name  string
		count any
		want  string
	}{
		{name: "a string", count: "1", want: "Removed 1 backups (5 B)."},
		{name: "a float", count: 1.0, want: "Removed 1 backups (5 B)."},
		{name: "no count at all", count: nil, want: "Removed <no value> backups (5 B)."},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(language.English)
			args := map[string]any{"Size": "5 B"}
			if tc.count != nil {
				args["Count"] = tc.count
			}

			// when
			got := loc.T("backups_removed", args)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Plural forms of a language that has more than one and other: Spanish says "many"
// for a million, and falls back to "other" where a catalog has no text for a form.
func Test_Localizer_T_plural_forms(t *testing.T) {
	const english = `{"files": {"one": "{{.Count}} file", "other": "{{.Count}} files"}}`
	tcs := []struct {
		name    string
		spanish string
		count   int
		want    string
	}{
		{name: "one", spanish: `{"files": {"one": "{{.Count}} archivo", "many": "{{.Count}} de archivos", "other": "{{.Count}} archivos"}}`, count: 1, want: "1 archivo"},
		{name: "other", spanish: `{"files": {"one": "{{.Count}} archivo", "many": "{{.Count}} de archivos", "other": "{{.Count}} archivos"}}`, count: 5, want: "5 archivos"},
		{name: "many, for a million", spanish: `{"files": {"one": "{{.Count}} archivo", "many": "{{.Count}} de archivos", "other": "{{.Count}} archivos"}}`, count: 1000000, want: "1000000 de archivos"},
		{name: "a form the catalog lacks falls back to other", spanish: `{"files": {"one": "{{.Count}} archivo", "other": "{{.Count}} archivos"}}`, count: 1000000, want: "1000000 archivos"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := newFromCatalogs(language.Spanish, map[string]string{"en": english, "es": tc.spanish})

			// when
			got := loc.T("files", map[string]any{"Count": tc.count})

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
