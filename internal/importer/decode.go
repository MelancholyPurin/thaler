package importer

import (
	"bytes"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/melancholypurin/thaler/internal/coded"
)

// The encodings a file is read in, as ParseResult names them.
const (
	EncodingUTF8        = "utf-8"
	EncodingUTF16LE     = "utf-16le"
	EncodingUTF16BE     = "utf-16be"
	EncodingWindows1252 = "windows-1252"
)

// cp1252High maps bytes 0x80..0x9F of Windows-1252 to Unicode. Bytes 0xA0..0xFF
// are identical to Latin-1, and bytes below 0x80 are ASCII.
var cp1252High = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// decodeToUTF8 converts a bank export to UTF-8, without its byte order mark, and
// says which encoding it was read in.
//
// "auto" works it out. A byte order mark says it. Without one, a file in which every
// second byte is a zero is UTF-16 (what a spreadsheet saves as "Unicode text"), the
// zeros saying which half comes first. Otherwise a file that is valid UTF-8 is UTF-8,
// and any other is Windows-1252, which German banks still deliver: there every byte is
// a character, so it cannot fail. "utf-16" without le or be goes by the byte order mark,
// or the zeros, and is little-endian when neither says.
func decodeToUTF8(b []byte, encoding string) (string, string, error) {
	switch encoding {
	case "auto", "":
		switch {
		case bytes.HasPrefix(b, bomUTF8):
			return string(b[len(bomUTF8):]), EncodingUTF8, nil
		case bytes.HasPrefix(b, bomUTF16LE), bytes.HasPrefix(b, bomUTF16BE):
			return decodeUTF16(b, utf16Order(b))
		}
		if order := utf16Order(b); order != "" {
			return decodeUTF16(b, order)
		}
		if utf8.Valid(b) {
			return string(b), EncodingUTF8, nil
		}
		return decodeCP1252(b), EncodingWindows1252, nil
	case "utf-8", "utf8":
		b = bytes.TrimPrefix(b, bomUTF8)
		if !utf8.Valid(b) {
			return "", "", coded.New("file_not_utf8", nil)
		}
		return string(b), EncodingUTF8, nil
	case "utf-16", "utf16":
		order := utf16Order(b)
		if order == "" {
			order = EncodingUTF16LE
		}
		return decodeUTF16(b, order)
	case "utf-16le", "utf16le":
		return decodeUTF16(b, EncodingUTF16LE)
	case "utf-16be", "utf16be":
		return decodeUTF16(b, EncodingUTF16BE)
	}
	return decodeCP1252(bytes.TrimPrefix(b, bomUTF8)), EncodingWindows1252, nil
}

// utf16Order is the byte order of a UTF-16 text, from its byte order mark or, without
// one, from where its zero bytes are: the text of a bank export is mostly ASCII, whose
// characters have a zero for their high byte. It is "" for a text that is not UTF-16.
func utf16Order(b []byte) string {
	switch {
	case bytes.HasPrefix(b, bomUTF16LE):
		return EncodingUTF16LE
	case bytes.HasPrefix(b, bomUTF16BE):
		return EncodingUTF16BE
	}
	sample := b[:min(len(b), 4096)&^1]
	pairs := len(sample) / 2
	if pairs < 2 {
		return ""
	}
	var even, odd int // zero bytes at even and at odd positions
	for i := 0; i < len(sample); i += 2 {
		if sample[i] == 0 {
			even++
		}
		if sample[i+1] == 0 {
			odd++
		}
	}
	switch {
	case odd*10 >= pairs*7 && even*20 < pairs:
		return EncodingUTF16LE
	case even*10 >= pairs*7 && odd*20 < pairs:
		return EncodingUTF16BE
	}
	return ""
}

// decodeUTF16 reads b as UTF-16 in the given byte order. A byte order mark of that
// order is dropped, and so is a last byte without its other half.
func decodeUTF16(b []byte, order string) (string, string, error) {
	if order == EncodingUTF16BE {
		b = bytes.TrimPrefix(b, bomUTF16BE)
	} else {
		b = bytes.TrimPrefix(b, bomUTF16LE)
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		lo, hi := b[2*i], b[2*i+1]
		if order == EncodingUTF16BE {
			lo, hi = hi, lo
		}
		units[i] = uint16(lo) | uint16(hi)<<8
	}
	return string(utf16.Decode(units)), order, nil
}

func decodeCP1252(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + len(b)/8)
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case c < 0xA0:
			sb.WriteRune(cp1252High[c-0x80])
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}
