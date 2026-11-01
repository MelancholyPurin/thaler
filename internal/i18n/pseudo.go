package i18n

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

// pseudoGrowth is how much longer the pseudo-language makes a text: German and
// Finnish run to about 35% and 40% longer than English.
const pseudoGrowth = 0.4

// accent keeps text readable while changing it: a vowel or a few consonants
// become the same letter with a mark, which shows a font or an encoding that loses
// anything but ASCII.
var accent = strings.NewReplacer(
	"a", "á", "e", "é", "i", "í", "o", "ö", "u", "ú", "y", "ý", "c", "ç", "n", "ñ", "s", "š", "z", "ž",
	"A", "Å", "E", "É", "I", "Í", "O", "Ö", "U", "Ú", "Y", "Ý", "C", "Ç", "N", "Ñ", "S", "Š", "Z", "Ž",
)

// template is an argument or any other action of a message, which is left as it is.
var template = regexp.MustCompile(`\{\{.*?\}\}`)

// Pseudo is text as the pseudo-language shows it: accented, 40% longer, and inside
// brackets so that a cut-off end is seen, as in [Ûpgrádéd ~~~~]. Arguments such as
// {{.Copy}} are not changed, and do not count towards the length.
func Pseudo(text string) string {
	var b strings.Builder
	b.WriteString("[")
	visible := 0
	rest := text
	for {
		loc := template.FindStringIndex(rest)
		if loc == nil {
			b.WriteString(accent.Replace(rest))
			visible += utf8.RuneCountInString(rest)
			break
		}
		b.WriteString(accent.Replace(rest[:loc[0]]))
		visible += utf8.RuneCountInString(rest[:loc[0]])
		b.WriteString(rest[loc[0]:loc[1]])
		rest = rest[loc[1]:]
	}
	if pad := int(float64(visible)*pseudoGrowth + 0.999); pad > 0 {
		b.WriteString(" " + strings.Repeat("~", pad))
	}
	b.WriteString("]")
	return b.String()
}

// addPseudo adds the pseudo-language to the bundle: every message of English, put
// through Pseudo.
func addPseudo(bundle *goi18n.Bundle) error {
	text, err := locales.ReadFile("locales/en.json")
	if err != nil {
		return err
	}
	english, err := parseCatalog("locales/en.json", text)
	if err != nil {
		return err
	}
	var msgs []*goi18n.Message
	for _, id := range english.order {
		m := &goi18n.Message{ID: id}
		for _, f := range english.entries[id].Forms {
			p := Pseudo(f.Text)
			switch f.Name {
			case "zero":
				m.Zero = p
			case "one":
				m.One = p
			case "two":
				m.Two = p
			case "few":
				m.Few = p
			case "many":
				m.Many = p
			case "other":
				m.Other = p
			default:
				return fmt.Errorf("message %q has the plural form %q, which is not one of CLDR's", id, f.Name)
			}
		}
		msgs = append(msgs, m)
	}
	return bundle.AddMessages(PseudoTag, msgs...)
}
