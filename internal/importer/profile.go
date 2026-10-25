// Package importer turns bank CSV exports into ledger rows. It depends only on
// the standard library and the ledger package; reading config files and
// writing to the database happen elsewhere.
package importer

import (
	"fmt"
	"sort"
	"strings"
)

// Columns lists, per field, the header names that may carry it. The first
// candidate present in the file wins. Matching ignores case and treats
// ä/ö/ü/ß as ae/oe/ue/ss.
type Columns struct {
	Date        []string `toml:"date"`
	Payee       []string `toml:"payee"`
	Purpose     []string `toml:"purpose"`
	IBAN        []string `toml:"iban"`
	BookingText []string `toml:"booking_text"`
	Amount      []string `toml:"amount"`
	Currency    []string `toml:"currency"`
}

// Profile describes one bank's CSV layout.
type Profile struct {
	Name string `toml:"name"`
	// Delimiter is a single character; default ";".
	Delimiter string `toml:"delimiter"`
	// Encoding is "auto" (default: UTF-8 if valid, else Windows-1252), "utf-8"
	// or "windows-1252" (also accepts latin1 / iso-8859-1).
	Encoding string `toml:"encoding"`
	// DecimalSep is "," or "."; empty means detect per amount.
	DecimalSep string `toml:"decimal_separator"`
	// DateFormats use DD, MM, YY and YYYY, e.g. "DD.MM.YYYY". Default:
	// "YYYY-MM-DD" and "DD.MM.YYYY". Go layouts such as "2006-01-02" also work.
	DateFormats []string `toml:"date_formats"`
	Columns     Columns  `toml:"columns"`
	// Rows whose SkipColumn contains SkipContains (case-insensitive) are
	// ignored, e.g. pending transactions that have not been booked yet.
	SkipColumn   string `toml:"skip_column"`
	SkipContains string `toml:"skip_contains"`
}

// Sparkasse is the built-in profile for the "CSV-CAMT" export that Sparkasse
// online banking offers (semicolon separated, Windows-1252 or UTF-8, German
// number format, two-digit years). The "CSV-MT940" export uses the same header
// names for every column this profile needs.
var Sparkasse = Profile{
	Name:        "sparkasse",
	Delimiter:   ";",
	Encoding:    "auto",
	DecimalSep:  ",",
	DateFormats: []string{"DD.MM.YY", "DD.MM.YYYY"},
	Columns: Columns{
		Date:        []string{"Buchungstag"},
		Payee:       []string{"Beguenstigter/Zahlungspflichtiger"},
		Purpose:     []string{"Verwendungszweck"},
		IBAN:        []string{"Kontonummer/IBAN", "Kontonummer"},
		BookingText: []string{"Buchungstext"},
		Amount:      []string{"Betrag"},
		Currency:    []string{"Waehrung"},
	},
	SkipColumn:   "Info",
	SkipContains: "vorgemerkt",
}

var builtin = map[string]Profile{"sparkasse": Sparkasse}

// FindProfile looks a profile up by name (case-insensitive). Custom profiles
// from the config file take precedence over built-in ones.
func FindProfile(name string, custom []Profile) (Profile, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, p := range custom {
		if strings.ToLower(p.Name) == key {
			return p, nil
		}
	}
	if p, ok := builtin[key]; ok {
		return p, nil
	}
	return Profile{}, fmt.Errorf("unknown import profile %q (available: %s)", name, strings.Join(ProfileNames(custom), ", "))
}

// ProfileNames lists the built-in and custom profile names, lower-cased and
// sorted.
func ProfileNames(custom []Profile) []string {
	set := make(map[string]bool)
	for k := range builtin {
		set[k] = true
	}
	for _, p := range custom {
		set[strings.ToLower(p.Name)] = true
	}
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

type compiled struct {
	delim    rune
	encoding string
	sep      byte
	layouts  []string
}

func (p Profile) compile() (compiled, error) {
	c := compiled{delim: ';', encoding: "auto"}
	if p.Delimiter != "" {
		rs := []rune(p.Delimiter)
		if len(rs) != 1 {
			return c, fmt.Errorf("profile %q: delimiter must be a single character", p.Name)
		}
		c.delim = rs[0]
	}
	if p.Encoding != "" {
		c.encoding = strings.ToLower(p.Encoding)
	}
	switch c.encoding {
	case "auto", "utf-8", "utf8", "windows-1252", "cp1252", "latin1", "iso-8859-1":
	default:
		return c, fmt.Errorf("profile %q: unsupported encoding %q", p.Name, p.Encoding)
	}
	switch p.DecimalSep {
	case "":
	case ",":
		c.sep = ','
	case ".":
		c.sep = '.'
	default:
		return c, fmt.Errorf("profile %q: decimal_separator must be \",\" or \".\"", p.Name)
	}
	formats := p.DateFormats
	if len(formats) == 0 {
		formats = []string{"YYYY-MM-DD", "DD.MM.YYYY"}
	}
	for _, f := range formats {
		c.layouts = append(c.layouts, toLayout(f))
	}
	if len(p.Columns.Date) == 0 || len(p.Columns.Amount) == 0 {
		return c, fmt.Errorf("profile %q: columns.date and columns.amount are required", p.Name)
	}
	return c, nil
}

// toLayout converts DD/MM/YY/YYYY tokens to a Go time layout. Strings that
// already are Go layouts contain none of the tokens and pass through.
func toLayout(f string) string {
	return strings.NewReplacer("YYYY", "2006", "YY", "06", "MM", "01", "DD", "02").Replace(f)
}
