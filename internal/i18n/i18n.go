// Package i18n looks up Thaler's messages in the language of the person using it.
//
// The messages are in locales/<language>.json, embedded in the binary. English is
// the source: every message has an id there, and another language translates the
// ids it has; a message it lacks is shown in English. The lookup and the plural
// forms are go-i18n's; writing amounts and dates in the way of a language is the
// job of golang.org/x/text, whose results are passed in as arguments.
//
// A catalog is one JSON object. Each message is a member named by its id: lower
// case with underscores, and not changed once it is used, because it is also what
// an error code will be. Its value is the text: "id": "the text". A message that
// counts ("1 file", "2 files") is an object with a text per plural form instead,
// {"one": "{{.Count}} file", "other": "{{.Count}} files"} (the forms are CLDR's;
// "other" is the general one, and see go-i18n for the rest). {{.Name}} is an argument:
// arguments are never translated, and a message may move them around. There is no
// "description" for the translator: an id that says what the message is for does
// that better, and go-i18n reads one if a message ever needs it. JSON has no comments, so the notes live here. The Spanish and German
// texts wait for a native speaker to read them (TLC-7 in the roadmap).
//
// A message a language lacks is shown in English; `make i18n-todo LOCALE=es` lists
// what a language still lacks.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var locales embed.FS

// Language is one of the languages Thaler speaks.
type Language struct {
	Code string // as written in config.toml: en, es or de
	Tag  language.Tag
	Name string // in the language itself

	// DateLayout is how a date is written (a Go layout). English keeps ISO, which
	// is the same everywhere and cannot be read as month first or day first.
	DateLayout string
	// DateInput are the layouts typed dates are read in besides ISO and
	// DD.MM.YYYY, which every language reads. Day and month may have one digit.
	DateInput []string
	// DateFormats names what is accepted, for the message about a date that is not.
	DateFormats string
	// Complete says that the language has every message of English. The tests then
	// fail when it lacks one, and for a language that is not complete they accept
	// that it does, because it is still being translated. Set it when the
	// translation is whole, and clear it when a story adds messages before the
	// translators have them.
	Complete bool
}

// Languages are the languages of Thaler, the source language first.
var Languages = []Language{
	{Code: "en", Tag: language.English, Name: "English",
		DateLayout: "2006-01-02", DateFormats: "YYYY-MM-DD or DD.MM.YYYY"},
	{Code: "es", Tag: language.Spanish, Name: "Español",
		DateLayout: "02/01/2006", DateInput: []string{"2/1/2006"}, DateFormats: "DD/MM/YYYY, YYYY-MM-DD o DD.MM.YYYY", Complete: true},
	{Code: "de", Tag: language.German, Name: "Deutsch",
		DateLayout: "02.01.2006", DateInput: []string{"2.1.2006"}, DateFormats: "DD.MM.YYYY oder YYYY-MM-DD", Complete: true},
}

// Codes is the codes of Languages, for messages that list them.
func Codes() []string {
	codes := make([]string, len(Languages))
	for i, l := range Languages {
		codes[i] = l.Code
	}
	return codes
}

// PseudoTag is the pseudo-language: English made longer and accented, so that text
// that will not fit in German, or is cut off, or loses a character, shows before a
// translator is involved. It is not a language to use, so it is not in Languages and
// is not offered in messages, but `language = "pseudo"` in config.toml selects it.
var PseudoTag = language.MustParse("en-XA")

// pseudoCode is what config.toml says to get PseudoTag.
const pseudoCode = "pseudo"

// Parse is the language that a code written in config.toml names.
func Parse(code string) (language.Tag, bool) {
	if strings.EqualFold(strings.TrimSpace(code), pseudoCode) {
		return PseudoTag, true
	}
	for _, l := range Languages {
		if strings.EqualFold(l.Code, strings.TrimSpace(code)) {
			return l.Tag, true
		}
	}
	return language.English, false
}

// Detect picks the language: the one set in config.toml, else the one the
// environment asks for (LC_ALL, then LC_MESSAGES, then LANG, the first that is set),
// else English. An environment language that Thaler does not speak is English too.
// getenv is os.Getenv, or a stand-in in a test.
func Detect(setting string, getenv func(string) string) language.Tag {
	if strings.TrimSpace(setting) != "" {
		tag, _ := Parse(setting)
		return tag
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(name); v != "" {
			return fromLocale(v)
		}
	}
	return language.English
}

// languageOf is the Language that tag belongs to, English if none does.
func languageOf(tag language.Tag) Language {
	tags := make([]language.Tag, len(Languages))
	for i, l := range Languages {
		tags[i] = l.Tag
	}
	_, i, confidence := language.NewMatcher(tags).Match(tag)
	if confidence == language.No {
		return Languages[0]
	}
	return Languages[i]
}

// fromLocale reads a POSIX locale name such as de_DE.UTF-8 or es_MX@euro.
func fromLocale(locale string) language.Tag {
	if i := strings.IndexAny(locale, ".@"); i >= 0 {
		locale = locale[:i]
	}
	tag, err := language.Parse(strings.ReplaceAll(locale, "_", "-")) // "C" and "POSIX" do not parse
	if err != nil {
		return language.English
	}
	tags := make([]language.Tag, len(Languages))
	for i, l := range Languages {
		tags[i] = l.Tag
	}
	_, i, confidence := language.NewMatcher(tags).Match(tag)
	if confidence == language.No {
		return language.English
	}
	return tags[i]
}

// Localizer says things in one language.
type Localizer struct {
	tag     language.Tag
	lang    Language
	decimal string // the decimal separator of the language: "." or ","
	loc     *goi18n.Localizer
}

// New is the Localizer for tag; messages it does not have are in English. It panics
// if an embedded catalog cannot be read: that is a mistake in the build, which the
// tests of this package catch.
func New(tag language.Tag) *Localizer { return newFromCatalogs(tag, nil) }

// newFromCatalogs is New, with the text of some catalogs (by language code) taken
// from instead of the embedded files, for a test.
func newFromCatalogs(tag language.Tag, instead map[string]string) *Localizer {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, l := range Languages {
		name := "locales/" + l.Code + ".json"
		text, err := locales.ReadFile(name)
		if err != nil {
			panic(fmt.Sprintf("i18n: %v", err))
		}
		if s, ok := instead[l.Code]; ok {
			text = []byte(s)
		}
		if _, err := bundle.ParseMessageFileBytes(text, l.Code+".json"); err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", name, err))
		}
	}
	if tag == PseudoTag {
		if err := addPseudo(bundle); err != nil {
			panic(fmt.Sprintf("i18n: %v", err))
		}
	}
	lang := languageOf(tag)
	return &Localizer{
		tag:     tag,
		lang:    lang,
		decimal: decimalSeparator(lang.Tag),
		loc:     goi18n.NewLocalizer(bundle, tag.String(), language.English.String()),
	}
}

// Tag is the language this Localizer speaks.
func (l *Localizer) Tag() language.Tag { return l.tag }

// T is the message id with its arguments filled in, such as {"Copy": path} for
// {{.Copy}}. A message the language lacks is shown in English. An id nobody defined
// comes out as the id itself, so that the mistake is visible instead of a blank.
func (l *Localizer) T(id string, args map[string]any) string {
	// An error with a text is a message that came from English because the language
	// lacks it, which is what is wanted; an error without one is a message nobody has.
	text, _ := l.loc.Localize(&goi18n.LocalizeConfig{MessageID: id, TemplateData: args})
	if text == "" {
		return id
	}
	return text
}
