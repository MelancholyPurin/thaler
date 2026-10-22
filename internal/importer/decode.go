package importer

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"
)

// cp1252High maps bytes 0x80..0x9F of Windows-1252 to Unicode. Bytes 0xA0..0xFF
// are identical to Latin-1, and bytes below 0x80 are ASCII.
var cp1252High = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// decodeToUTF8 converts a bank export to UTF-8 and strips a leading BOM.
// German banks often still deliver Windows-1252, so "auto" falls back to it
// whenever the bytes are not valid UTF-8.
func decodeToUTF8(b []byte, encoding string) (string, error) {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	switch encoding {
	case "utf-8", "utf8":
		if !utf8.Valid(b) {
			return "", errors.New("file is not valid UTF-8 (try encoding = \"windows-1252\")")
		}
		return string(b), nil
	case "auto", "":
		if utf8.Valid(b) {
			return string(b), nil
		}
	}
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
	return sb.String(), nil
}
