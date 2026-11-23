// Package importer turns bank exports (CSV, OFX, QIF and CAMT.053) into ledger rows. It depends only on
// the standard library and the ledger package; reading config files and
// writing to the database happen elsewhere.
package importer

import (
	"sort"
	"strings"

	"github.com/melancholypurin/thaler/internal/coded"
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

// Profile describes one bank's export: the layout of its CSV, or that it is an OFX,
// a QIF or a CAMT.053 file.
type Profile struct {
	Name string `toml:"name"`
	// Format is "csv" (the default), "ofx", "qif" or "camt". An OFX or CAMT file
	// needs nothing else from the profile; a QIF file takes DateFormats and DecimalSep. Columns,
	// Delimiter and the skip rule are for CSV alone.
	Format string `toml:"format"`
	// Delimiter is a single character, or "auto" (the default): found from the
	// header, see Delimiters.
	Delimiter string `toml:"delimiter"`
	// Encoding is "auto" (the default: UTF-16 or UTF-8 when the bytes say so, else
	// Windows-1252), "utf-8", "utf-16", "utf-16le", "utf-16be" or "windows-1252"
	// (also latin1 / iso-8859-1). See decodeToUTF8.
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
// names for every column this profile needs. The delimiter and the encoding are
// found from the file, so the export still reads after a spreadsheet has saved
// it again with commas or as UTF-16.
var Sparkasse = Profile{
	Name:        "sparkasse",
	Delimiter:   "auto",
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

// The formats of a profile.
const (
	FormatCSV  = "csv"
	FormatOFX  = "ofx"
	FormatQIF  = "qif"
	FormatCAMT = "camt"
)

// OFX is the built-in profile for OFX statements (also QFX, Quicken's name for them):
// version 1, the SGML one, and version 2, the XML one. Each transaction of the statement
// is a row, with its booking date, amount, name, memo and type; the currency is the one
// the statement names.
var OFX = Profile{Name: FormatOFX, Format: FormatOFX, Encoding: "auto"}

// QIF is the built-in profile for QIF files (Quicken Interchange Format) of a bank,
// cash or credit card account. A QIF date has no fixed order, so the first of these
// formats that reads every date of the file is used: the European ones first, then the
// American ones Quicken writes (1/31'26 is read as 01/31/26). A file whose days are all
// 12 or less reads either way; a profile of yours with format = "qif" and the
// date_formats of your bank settles it.
var QIF = Profile{
	Name: FormatQIF, Format: FormatQIF, Encoding: "auto",
	DateFormats: []string{"DD.MM.YYYY", "DD.MM.YY", "YYYY-MM-DD", "MM/DD/YYYY", "MM/DD/YY", "DD/MM/YYYY", "DD/MM/YY"},
}

// CAMT is the built-in profile for CAMT.053 statements (ISO 20022, camt.053.001.02 and
// later): each booked entry is a row, with its booking day, amount and currency, the
// other side and its IBAN, the remittance text and the bank's additional information.
var CAMT = Profile{Name: FormatCAMT, Format: FormatCAMT, Encoding: "auto"}

var builtin = map[string]Profile{"sparkasse": Sparkasse, FormatOFX: OFX, FormatQIF: QIF, FormatCAMT: CAMT}

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
	return Profile{}, coded.New("unknown_profile", map[string]any{"Name": name, "Available": strings.Join(ProfileNames(custom), ", ")})
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

// Delimiters are the characters an "auto" delimiter is chosen from, the first
// preferred when two read the header equally well.
var Delimiters = []rune{';', ',', '\t', '|'}

type compiled struct {
	format   string // FormatCSV, FormatOFX, FormatQIF or FormatCAMT
	delim    rune   // 0: found from the header
	encoding string
	sep      byte
	layouts  []string
}

func (p Profile) compile() (compiled, error) {
	c := compiled{encoding: "auto", format: strings.ToLower(strings.TrimSpace(p.Format))}
	switch c.format {
	case "":
		c.format = FormatCSV
	case FormatCSV, FormatOFX, FormatQIF, FormatCAMT:
	default:
		return c, coded.New("profile_format", map[string]any{"Name": p.Name, "Format": p.Format})
	}
	if p.Delimiter != "" && !strings.EqualFold(p.Delimiter, "auto") {
		rs := []rune(p.Delimiter)
		if len(rs) != 1 {
			return c, coded.New("profile_delimiter", map[string]any{"Name": p.Name})
		}
		c.delim = rs[0]
	}
	if p.Encoding != "" {
		c.encoding = strings.ToLower(p.Encoding)
	}
	switch c.encoding {
	case "auto", "utf-8", "utf8", "utf-16", "utf16", "utf-16le", "utf16le", "utf-16be", "utf16be",
		"windows-1252", "cp1252", "latin1", "iso-8859-1":
	default:
		return c, coded.New("profile_encoding", map[string]any{"Name": p.Name, "Encoding": p.Encoding})
	}
	switch p.DecimalSep {
	case "":
	case ",":
		c.sep = ','
	case ".":
		c.sep = '.'
	default:
		return c, coded.New("profile_decimal_separator", map[string]any{"Name": p.Name})
	}
	formats := p.DateFormats
	if len(formats) == 0 {
		formats = []string{"YYYY-MM-DD", "DD.MM.YYYY"}
	}
	for _, f := range formats {
		c.layouts = append(c.layouts, toLayout(f))
	}
	if c.format == FormatCSV && (len(p.Columns.Date) == 0 || len(p.Columns.Amount) == 0) {
		return c, coded.New("profile_columns", map[string]any{"Name": p.Name})
	}
	return c, nil
}

// toLayout converts DD/MM/YY/YYYY tokens to a Go time layout. Strings that
// already are Go layouts contain none of the tokens and pass through.
func toLayout(f string) string {
	return strings.NewReplacer("YYYY", "2006", "YY", "06", "MM", "01", "DD", "02").Replace(f)
}
