package i18n

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const englishCatalog = `{
  "hello": "Hello {{.Name}}",
  "files": {"one": "{{.Count}} file", "other": "{{.Count}} files"},
  "bye": "Bye"
}`

func Test_untranslated(t *testing.T) {
	tcs := []struct {
		name  string
		other string
		want  []Entry
	}{
		{name: "nothing is translated", other: "{}", want: []Entry{
			{ID: "hello", Forms: []Form{{"other", "Hello {{.Name}}"}}},
			{ID: "files", Forms: []Form{{"one", "{{.Count}} file"}, {"other", "{{.Count}} files"}}},
			{ID: "bye", Forms: []Form{{"other", "Bye"}}},
		}},
		{name: "one message is", other: `{"bye": "Tschüss"}`, want: []Entry{
			{ID: "hello", Forms: []Form{{"other", "Hello {{.Name}}"}}},
			{ID: "files", Forms: []Form{{"one", "{{.Count}} file"}, {"other", "{{.Count}} files"}}},
		}},
		{name: "an empty text is not a translation", other: `{"hello": "", "bye": "Tschüss", "files": {"one": "x", "other": "y"}}`, want: []Entry{
			{ID: "hello", Forms: []Form{{"other", "Hello {{.Name}}"}}},
		}},
		{name: "everything is translated", other: `{"hello": "a", "files": {"other": "b"}, "bye": "c"}`},
		{name: "a message that English does not have is not asked for", other: `{"extra": "x", "hello": "a", "files": {"other": "b"}, "bye": "c"}`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			english, err := parseCatalog("en", []byte(englishCatalog))
			if err != nil {
				t.Fatal(err)
			}
			other, err := parseCatalog("de", []byte(tc.other))
			if err != nil {
				t.Fatal(err)
			}

			// when
			got := untranslated(english, other)

			// then
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Untranslated(t *testing.T) {
	tcs := []struct {
		name    string
		code    string
		wantErr string
	}{
		{name: "English is the source", code: "en", wantErr: "en is the source language, so its messages are written, not translated"},
		{name: "an unknown language", code: "fr", wantErr: `unknown language "fr": use one of en, es, de`},
		{name: "a language of Thaler", code: "es"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.code

			// when
			_, err := Untranslated(tc.code)

			// then
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tc.wantErr, got); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Stubs(t *testing.T) {
	tcs := []struct {
		name    string
		entries []Entry
		want    string
	}{
		{name: "nothing to translate", want: "{\n}\n"},
		{
			name: "a message with one text, and one with plural forms",
			entries: []Entry{
				{ID: "hello", Forms: []Form{{"other", "Hello {{.Name}}"}}},
				{ID: "files", Forms: []Form{{"one", "{{.Count}} file"}, {"other", "{{.Count}} files"}}},
			},
			want: "{\n" +
				"  \"hello\": \"\",\n" +
				"  \"files\": {\n    \"one\": \"\",\n    \"other\": \"\"\n  }\n" +
				"}\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.entries

			// when
			got := Stubs(tc.entries)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if !json.Valid([]byte(got)) {
				t.Errorf("the stubs are not valid JSON:\n%s", got)
			}
		})
	}
}

func Test_Reference(t *testing.T) {
	tcs := []struct {
		name    string
		entries []Entry
		want    string
	}{
		{name: "nothing"},
		{
			name: "one line per form",
			entries: []Entry{
				{ID: "hello", Forms: []Form{{"other", "Hello {{.Name}}"}}},
				{ID: "files", Forms: []Form{{"one", "{{.Count}} file"}, {"other", "{{.Count}} files"}}},
			},
			want: "hello.other: Hello {{.Name}}\nfiles.one: {{.Count}} file\nfiles.other: {{.Count}} files\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.entries

			// when
			got := Reference(tc.entries)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_parseCatalog(t *testing.T) {
	tcs := []struct {
		name    string
		text    string
		want    []string // the ids, in file order
		wantErr string
	}{
		{name: "the order of the file is kept", text: `{"b": {"other": "x"}, "a": {"other": "y"}, "c": {"other": "z"}}`, want: []string{"b", "a", "c"}},
		{name: "an empty catalog", text: `{}`},
		{name: "a message that is a number", text: `{"a": 1}`, wantErr: `x.json: message "a" is neither a text nor an object of plural forms`},
		{name: "a message with a text and one with forms", text: `{"a": "x", "b": {"one": "y", "other": "z"}}`, want: []string{"a", "b"}},
		{name: "not an object", text: `[]`, wantErr: "x.json: a catalog is a JSON object"},
		{name: "a syntax error", text: `{"a": {"other": }`, wantErr: `x.json: message "a": invalid character '}' looking for beginning of value`},
		{name: "nothing at all", text: ``, wantErr: "x.json: a catalog is a JSON object"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.text

			// when
			got, err := parseCatalog("x.json", []byte(tc.text))

			// then
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if diff := cmp.Diff(tc.wantErr, gotErr); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got.order, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("ids mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
