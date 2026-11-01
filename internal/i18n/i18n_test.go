package i18n

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"
)

func Test_Parse(t *testing.T) {
	tcs := []struct {
		name   string
		code   string
		want   language.Tag
		wantOK bool
	}{
		{name: "English", code: "en", want: language.English, wantOK: true},
		{name: "Spanish", code: "es", want: language.Spanish, wantOK: true},
		{name: "German in capitals, with spaces", code: " DE ", want: language.German, wantOK: true},
		{name: "a language Thaler does not speak", code: "fr", want: language.English},
		{name: "a language with a region is not a code", code: "de-DE", want: language.English},
		{name: "nothing", code: "", want: language.English},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.code

			// when
			got, ok := Parse(tc.code)

			// then
			if diff := cmp.Diff(tc.want.String(), got.String()); diff != "" {
				t.Errorf("tag mismatch (-want +got):\n%s", diff)
			}
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

func Test_Detect(t *testing.T) {
	tcs := []struct {
		name    string
		setting string
		env     map[string]string
		want    language.Tag
	}{
		{name: "English with nothing set", want: language.English},
		{name: "the setting", setting: "de", want: language.German},
		{name: "the setting wins over the environment", setting: "en", env: map[string]string{"LANG": "de_DE.UTF-8"}, want: language.English},
		{name: "a setting that is no language is English, not the environment", setting: "xx", env: map[string]string{"LANG": "de_DE.UTF-8"}, want: language.English},
		{name: "LANG with a country and a character set", env: map[string]string{"LANG": "de_DE.UTF-8"}, want: language.German},
		{name: "LANG with a modifier", env: map[string]string{"LANG": "es_ES@euro"}, want: language.Spanish},
		{name: "LANG as a bare language", env: map[string]string{"LANG": "es"}, want: language.Spanish},
		{name: "Mexican Spanish is Spanish", env: map[string]string{"LANG": "es_MX.UTF-8"}, want: language.Spanish},
		{name: "LC_ALL wins over LC_MESSAGES and LANG", env: map[string]string{"LC_ALL": "es_ES.UTF-8", "LC_MESSAGES": "de_DE.UTF-8", "LANG": "en_US.UTF-8"}, want: language.Spanish},
		{name: "LC_MESSAGES wins over LANG", env: map[string]string{"LC_MESSAGES": "de_DE.UTF-8", "LANG": "es_ES.UTF-8"}, want: language.German},
		{name: "the first one set decides, even if Thaler does not speak it", env: map[string]string{"LC_ALL": "fr_FR.UTF-8", "LANG": "de_DE.UTF-8"}, want: language.English},
		{name: "C is English", env: map[string]string{"LANG": "C"}, want: language.English},
		{name: "POSIX is English", env: map[string]string{"LANG": "POSIX"}, want: language.English},
		{name: "something that is no locale", env: map[string]string{"LANG": "!!"}, want: language.English},
		{name: "a language Thaler does not speak", env: map[string]string{"LANG": "fr_FR.UTF-8"}, want: language.English},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			getenv := func(name string) string { return tc.env[name] }

			// when
			got := Detect(tc.setting, getenv)

			// then
			if diff := cmp.Diff(tc.want.String(), got.String()); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Localizer_T(t *testing.T) {
	tcs := []struct {
		name string
		tag  language.Tag
		id   string
		args map[string]any
		want string
	}{
		{name: "English", tag: language.English, id: "no_accounts_yet", want: "No accounts yet. Create one with: thaler cli account add Assets:Sparkasse --currency EUR"},
		{name: "Spanish", tag: language.Spanish, id: "no_accounts_yet", want: "Todavía no hay cuentas. Crea una con: thaler cli account add Assets:Sparkasse --currency EUR"},
		{name: "German", tag: language.German, id: "no_accounts_yet", want: "Noch keine Konten. Lege eines an mit: thaler cli account add Assets:Sparkasse --currency EUR"},
		{name: "an argument in English", tag: language.English, id: "upgrade_notice", args: map[string]any{"Copy": "/d/x.db"}, want: "Upgraded the database to this version of Thaler. A copy of it from before is at /d/x.db (not encrypted)"},
		{name: "an argument in German", tag: language.German, id: "upgrade_notice", args: map[string]any{"Copy": "/d/x.db"}, want: "Die Datenbank wurde auf diese Version von Thaler aktualisiert. Eine Kopie von vorher liegt unter /d/x.db (nicht verschlüsselt)"},
		{name: "an id nobody defined comes out as the id", tag: language.English, id: "no_such_message", want: "no_such_message"},
		{name: "a region of a language Thaler speaks", tag: language.MustParse("de-AT"), id: "no_accounts_yet", want: "Noch keine Konten. Lege eines an mit: thaler cli account add Assets:Sparkasse --currency EUR"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got := loc.T(tc.id, tc.args)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A language falls back to English for a message it has not translated.
func Test_Localizer_T_fallback(t *testing.T) {
	tcs := []struct {
		name string
		text string // the catalog of the language, in place of its file
		id   string
		want string
	}{
		{name: "a message the language has", text: `{"upgrade_notice": "Hallo {{.Copy}}"}`, id: "upgrade_notice", want: "Hallo /x"},
		{name: "a message it lacks", text: `{"upgrade_notice": "Hallo {{.Copy}}"}`, id: "no_accounts_yet", want: "No accounts yet. Create one with: thaler cli account add Assets:Sparkasse --currency EUR"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := newFromCatalogs(language.German, map[string]string{"de": tc.text})

			// when
			got := loc.T(tc.id, map[string]any{"Copy": "/x"})

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The catalogs that ship load, and every language has the file it needs.
func Test_Languages_catalogs(t *testing.T) {
	var tcs []struct{ name, code string }
	for _, lang := range Languages {
		tcs = append(tcs, struct{ name, code string }{lang.Name, lang.Code})
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.code

			// when
			_, err := readCatalog(tc.code)

			// then
			if err != nil {
				t.Errorf("readCatalog(%q): %v", tc.code, err)
			}
		})
	}
}
