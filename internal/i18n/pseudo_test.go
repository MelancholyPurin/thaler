package i18n

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"
)

func Test_Pseudo(t *testing.T) {
	tcs := []struct {
		name string
		in   string
		want string
	}{
		{name: "a word", in: "Hello", want: "[Héllö ~~]"},
		{name: "a sentence grows by 40%", in: "Save the file now", want: "[Šávé thé fílé ñöw ~~~~~~~]"},
		{name: "an argument is left alone and does not count", in: "Copy at {{.Copy}} (ok)", want: "[Çöpý át {{.Copy}} (ök) ~~~~~~]"},
		{name: "two arguments", in: "{{.A}} and {{.B}}", want: "[{{.A}} áñd {{.B}} ~~]"},
		{name: "only an argument", in: "{{.Name}}", want: "[{{.Name}}]"},
		{name: "nothing", in: "", want: "[]"},
		{name: "characters without an accent stay", in: "1, 2 & 3: x", want: "[1, 2 & 3: x ~~~~~]"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			got := Pseudo(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Parse_pseudo(t *testing.T) {
	tcs := []struct {
		name     string
		code     string
		wantOK   bool
		inCodes  bool
		inDetect bool
	}{
		{name: "pseudo", code: "pseudo", wantOK: true, inDetect: true},
		{name: "in capitals", code: "Pseudo", wantOK: true, inDetect: true},
		{name: "a language", code: "de", wantOK: true},
		{name: "something else", code: "pseud", wantOK: false},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.code

			// when
			tag, ok := Parse(tc.code)
			detected := Detect(tc.code, func(string) string { return "" })

			// then
			if ok != tc.wantOK {
				t.Errorf("Parse ok = %v, want %v", ok, tc.wantOK)
			}
			if got := tag == PseudoTag; got != (ok && tc.inDetect) {
				t.Errorf("Parse gives the pseudo-language = %v, want %v", got, ok && tc.inDetect)
			}
			if got := detected == PseudoTag; got != tc.inDetect {
				t.Errorf("Detect gives the pseudo-language = %v, want %v", got, tc.inDetect)
			}
		})
	}
}

// The pseudo-language is not offered as a language to choose.
func Test_Codes_withoutPseudo(t *testing.T) {
	tcs := []struct {
		name string
		want []string
	}{{name: "the codes of Languages", want: []string{"en", "es", "de"}}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.want

			// when
			got := Codes()

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Every message of English comes out of the pseudo-language longer and with its
// arguments in place.
func Test_Localizer_T_pseudo(t *testing.T) {
	english, err := readCatalog("en")
	if err != nil {
		t.Fatal(err)
	}
	type messageCase struct{ name, id, text string }
	var tcs []messageCase
	for _, id := range english.order {
		for _, f := range english.entries[id].Forms {
			if f.Name == "other" {
				tcs = append(tcs, messageCase{name: id, id: id, text: f.Text})
			}
		}
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(PseudoTag)
			args := map[string]any{"Copy": "/c", "Text": "t", "Formats": "f", "Name": "n", "Count": "2"}

			// when
			got := loc.T(tc.id, args)

			// then
			english := New(language.English).T(tc.id, args)
			literal := utf8.RuneCountInString(template.ReplaceAllString(tc.text, ""))
			if grew := utf8.RuneCountInString(got) - utf8.RuneCountInString(english); grew < literal*4/10 {
				t.Errorf("the pseudo text is %d characters longer, not 40%% of the %d written ones:\n%s\n%s", grew, literal, got, english)
			}
			if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
				t.Errorf("the pseudo text is not in brackets: %s", got)
			}
			if strings.Contains(got, "{{") || strings.Contains(got, "<no value>") {
				t.Errorf("an argument was not filled in: %s", got)
			}
		})
	}
}

// Numbers and dates are written as English writes them in the pseudo-language.
func Test_Localizer_pseudo_formats(t *testing.T) {
	tcs := []struct {
		name string
		want string
	}{{name: "an amount", want: "1,234.56 EUR"}}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(PseudoTag)

			// when
			got := loc.Money(eur(123456))

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
