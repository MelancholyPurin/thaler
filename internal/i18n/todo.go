package i18n

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// pluralForms are the keys of a message that hold text, in the order they are written.
var pluralForms = []string{"zero", "one", "two", "few", "many", "other"}

// Entry is a message of the English catalog, as a translator is handed it.
type Entry struct {
	ID    string
	Forms []Form // the plural forms English has: other alone, or one and other
}

// Form is the text of one plural form.
type Form struct {
	Name string
	Text string
}

// catalog is the messages of one language file, by id, with the ids in file order.
type catalog struct {
	order   []string
	entries map[string]Entry
}

func readCatalog(code string) (catalog, error) {
	name := "locales/" + code + ".json"
	text, err := locales.ReadFile(name)
	if err != nil {
		return catalog{}, fmt.Errorf("no catalog for %q", code)
	}
	return parseCatalog(name, text)
}

// parseCatalog reads a catalog, keeping the order of the messages in the file,
// which a decoding into a map would lose.
func parseCatalog(name string, text []byte) (catalog, error) {
	c := catalog{entries: map[string]Entry{}}
	dec := json.NewDecoder(bytes.NewReader(text))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return catalog{}, fmt.Errorf("%s: a catalog is a JSON object", name)
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return catalog{}, fmt.Errorf("%s: %w", name, err)
		}
		id, _ := key.(string)
		var value any
		if err := dec.Decode(&value); err != nil {
			return catalog{}, fmt.Errorf("%s: message %q: %w", name, id, err)
		}
		e := Entry{ID: id}
		switch v := value.(type) {
		case string: // "id": "text" is the text of the general form
			e.Forms = []Form{{Name: "other", Text: v}}
		case map[string]any:
			for _, form := range pluralForms {
				if t, ok := v[form].(string); ok {
					e.Forms = append(e.Forms, Form{Name: form, Text: t})
				}
			}
		default:
			return catalog{}, fmt.Errorf("%s: message %q is neither a text nor an object of plural forms", name, id)
		}
		c.order = append(c.order, id)
		c.entries[id] = e
	}
	if _, err := dec.Token(); err != nil {
		return catalog{}, fmt.Errorf("%s: %w", name, err)
	}
	return c, nil
}

// Untranslated is the English messages that the language with this code lacks, or
// has with an empty text, in the order of the English file.
func Untranslated(code string) ([]Entry, error) {
	if code == "en" {
		return nil, fmt.Errorf("en is the source language, so its messages are written, not translated")
	}
	if _, ok := Parse(code); !ok {
		return nil, fmt.Errorf("unknown language %q: use one of %s", code, strings.Join(Codes(), ", "))
	}
	english, err := readCatalog("en")
	if err != nil {
		return nil, err
	}
	other, err := readCatalog(code)
	if err != nil {
		return nil, err
	}
	return untranslated(english, other), nil
}

// untranslated is the entries of english that other lacks or leaves empty.
func untranslated(english, other catalog) []Entry {
	var todo []Entry
	for _, id := range english.order {
		have, ok := other.entries[id]
		if ok && hasText(have) {
			continue
		}
		todo = append(todo, english.entries[id])
	}
	return todo
}

func hasText(e Entry) bool {
	for _, f := range e.Forms {
		if strings.TrimSpace(f.Text) != "" {
			return true
		}
	}
	return false
}

// Stubs writes the entries as a JSON object for a translator to fill in and merge
// into the language's file: the text of each message, or of each of its plural
// forms, is empty. The English to translate
// is in Reference, since JSON has no comments to put it in.
func Stubs(entries []Entry) string {
	var b strings.Builder
	b.WriteString("{\n")
	for i, e := range entries {
		if len(e.Forms) == 1 && e.Forms[0].Name == "other" {
			fmt.Fprintf(&b, "  %s: \"\"", quote(e.ID))
		} else {
			var lines []string
			for _, f := range e.Forms {
				lines = append(lines, fmt.Sprintf("    %s: \"\"", quote(f.Name)))
			}
			fmt.Fprintf(&b, "  %s: {\n%s\n  }", quote(e.ID), strings.Join(lines, ",\n"))
		}
		if i < len(entries)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// Reference writes the English text of the entries, one form to a line, for the
// translator to read next to Stubs.
func Reference(entries []Entry) string {
	var b strings.Builder
	for _, e := range entries {
		for _, f := range e.Forms {
			fmt.Fprintf(&b, "%s.%s: %s\n", e.ID, f.Name, f.Text)
		}
	}
	return b.String()
}

// quote is s as a JSON string.
func quote(s string) string {
	b, _ := json.Marshal(s) // a string always marshals
	return string(b)
}
