package i18n

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func mustCatalog(t *testing.T, text string) catalog {
	t.Helper()
	c, err := parseCatalog("x.json", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const checkEnglish = `{
  "hello": "Hello {{.Name}}",
  "files": {"one": "{{.Count}} file", "other": "{{.Count}} files"},
  "bye": "Bye"
}`

func Test_catalogProblems(t *testing.T) {
	tcs := []struct {
		name     string
		other    string
		complete bool
		want     []string
	}{
		{name: "a whole translation", other: `{"hello": "Hallo {{.Name}}", "files": {"one": "{{.Count}} Datei", "other": "{{.Count}} Dateien"}, "bye": "Tschüss"}`, complete: true},
		{name: "arguments in another order", other: `{"hello": "{{.Name}}, hallo", "files": {"other": "{{.Count}} Dateien"}, "bye": "Tschüss"}`, complete: true},
		{name: "a missing message when the language is complete", other: `{"hello": "Hallo {{.Name}}", "files": {"other": "{{.Count}} Dateien"}}`, complete: true, want: []string{`de: "bye" is not translated, but the language is marked Complete`}},
		{name: "a missing message while it is still being translated", other: `{"hello": "Hallo {{.Name}}"}`},
		{name: "an empty text counts as missing", other: `{"hello": "Hallo {{.Name}}", "files": {"other": "{{.Count}} Dateien"}, "bye": ""}`, complete: true, want: []string{`de: "bye" is not translated, but the language is marked Complete`}},
		{name: "a message English no longer has", other: `{"hello": "Hallo {{.Name}}", "old": "Alt"}`, want: []string{`de: "old" is not a message of English: it was removed or renamed there`}},
		{name: "a translated argument", other: `{"hello": "Hallo {{.Nombre}}"}`, want: []string{`de: "hello" takes the arguments [Nombre] but English has [Name]: arguments are not translated or dropped`}},
		{name: "a dropped argument", other: `{"hello": "Hallo"}`, want: []string{`de: "hello" takes the arguments [] but English has [Name]: arguments are not translated or dropped`}},
		{name: "a plural form without other", other: `{"files": {"one": "{{.Count}} Datei"}}`, want: []string{`de: "files" has no "other" text, which every message needs`}},
		{name: "one form of a plural left empty", other: `{"files": {"one": "", "other": "{{.Count}} Dateien"}}`, want: []string{`de: "files" has an empty "one" text`}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			english, other := mustCatalog(t, checkEnglish), mustCatalog(t, tc.other)

			// when
			got := catalogProblems("de", english, other, tc.complete)

			// then
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_englishProblems(t *testing.T) {
	tcs := []struct {
		name string
		text string
		want []string
	}{
		{name: "a sound catalog", text: checkEnglish},
		{name: "an id in capitals", text: `{"Hello": "x"}`, want: []string{`en: "Hello" is not a lower case id with underscores`}},
		{name: "an id with a dash", text: `{"no-accounts": "x"}`, want: []string{`en: "no-accounts" is not a lower case id with underscores`}},
		{name: "a plural without other", text: `{"files": {"one": "x"}}`, want: []string{`en: "files" has no "other" text, which every message needs`}},
		{name: "an empty message", text: `{"a": ""}`, want: []string{`en: "a" has no "other" text, which every message needs`}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.text

			// when
			got := englishProblems(mustCatalog(t, tc.text))

			// then
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_usageProblems(t *testing.T) {
	tcs := []struct {
		name string
		uses []use
		want []string
	}{
		{name: "every message used once or twice", uses: []use{{"hello", "a.go:1"}, {"files", "a.go:2"}, {"bye", "b.go:3"}, {"bye", "c.go:4"}}},
		{name: "a message nobody uses", uses: []use{{"hello", "a.go:1"}, {"files", "a.go:2"}}, want: []string{`en: "bye" is not used by any call of T or coded.New`}},
		{name: "an id that does not exist", uses: []use{{"hello", "a.go:1"}, {"files", "a.go:2"}, {"bye", "a.go:3"}, {"helo", "a.go:9"}}, want: []string{`a.go:9: "helo" is not a message of English`}},
		{name: "an id that is not a literal", uses: []use{{"hello", "a.go:1"}, {"files", "a.go:2"}, {"bye", "a.go:3"}, {"", "a.go:8"}}, want: []string{"a.go:8: the id given to T or coded.New is not a string literal, so it cannot be checked"}},
		{name: "nothing is used", want: []string{`en: "hello" is not used by any call of T or coded.New`, `en: "files" is not used by any call of T or coded.New`, `en: "bye" is not used by any call of T or coded.New`}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			english := mustCatalog(t, checkEnglish)

			// when
			got := usageProblems(english, tc.uses)

			// then
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_usesIn(t *testing.T) {
	files := map[string]string{
		"a/a.go":        "package a\n\nfunc f(l interface{ T(string, map[string]any) string }) {\n\t_ = l.T(\"hello\", nil)\n\t_ = l.T(\"bye\", nil)\n}\n",
		"b.go":          "package b\n\nvar id = \"x\"\n\nfunc g(l interface{ T(string, map[string]any) string }) { _ = l.T(id, nil) }\n",
		"a/a_test.go":   "package a\n\nfunc h(l interface{ T(string, map[string]any) string }) { _ = l.T(\"only_in_a_test\", nil) }\n",
		"vendor/v/v.go": "package v\n\nfunc v(l interface{ T(string, map[string]any) string }) { _ = l.T(\"vendored\", nil) }\n",
		"testdata/t.go": "package t\n\nfunc t(l interface{ T(string, map[string]any) string }) { _ = l.T(\"fixture\", nil) }\n",
		"notes.txt":     "l.T(\"in_a_text_file\")",
		"c.go":          "package c\n\nfunc k() { T() }\n",
		"d.go":          "package d\n\nfunc k(x struct{ T func() }) { x.T() }\n",
		"e.go":          "package e\n\nfunc k(l interface{ T(string) }) { l.T(\"with_one_arg\") }\n",
	}
	want := []use{{"hello", "a/a.go:4"}, {"bye", "a/a.go:5"}, {"", "b.go:5"}, {"with_one_arg", "e.go:3"}}
	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tcs := []struct {
		name string
		want []use
	}{{name: "calls in sources, but not in tests, vendored code or other files", want: want}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given root

			// when
			got, err := usesIn(root)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.SortSlices(func(a, b use) bool { return a.Where < b.Where })); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The catalogs that ship are sound: English on its own, and each language against
// English.
func Test_Catalogs(t *testing.T) {
	type languageCase struct {
		name     string
		code     string
		complete bool
	}
	var tcs []languageCase
	for _, l := range Languages[1:] { // English is the source
		tcs = append(tcs, languageCase{name: l.Name, code: l.Code, complete: l.Complete})
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			english, err := readCatalog("en")
			if err != nil {
				t.Fatal(err)
			}
			other, err := readCatalog(tc.code)
			if err != nil {
				t.Fatal(err)
			}

			// when
			got := append(englishProblems(english), catalogProblems(tc.code, english, other, tc.complete)...)

			// then
			if diff := cmp.Diff([]string(nil), got); diff != "" {
				t.Errorf("the catalogs have problems (-want +got):\n%s", diff)
			}
		})
	}
}

// Every message is used by the code, and the code asks only for messages that exist.
func Test_Catalogs_usage(t *testing.T) {
	tcs := []struct {
		name string
		root string
	}{{name: "the sources of the repository", root: "../.."}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			english, err := readCatalog("en")
			if err != nil {
				t.Fatal(err)
			}

			// when
			uses, err := usesIn(tc.root)
			if err != nil {
				t.Fatal(err)
			}
			got := usageProblems(english, uses)

			// then
			if diff := cmp.Diff([]string(nil), got); diff != "" {
				t.Errorf("the messages and the code disagree (-want +got):\n%s", diff)
			}
		})
	}
}
