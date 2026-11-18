package importer

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_decodeToUTF8(t *testing.T) {
	type decoded struct {
		Text, Encoding, Err string
	}
	tcs := []struct {
		name     string
		in       string
		encoding string
		want     decoded
	}{
		{name: "plain ASCII is UTF-8", in: "Datum;Betrag\n", encoding: "auto", want: decoded{"Datum;Betrag\n", EncodingUTF8, ""}},
		{name: "no encoding is auto", in: "Datum\n", encoding: "", want: decoded{"Datum\n", EncodingUTF8, ""}},
		{name: "UTF-8 with umlauts", in: "Müller\n", encoding: "auto", want: decoded{"Müller\n", EncodingUTF8, ""}},
		{name: "the UTF-8 byte order mark goes", in: "\xef\xbb\xbfDatum", encoding: "auto", want: decoded{"Datum", EncodingUTF8, ""}},
		{name: "bytes that are not UTF-8 are Windows-1252", in: "M\xfcller \x80", encoding: "auto", want: decoded{"Müller €", EncodingWindows1252, ""}},
		{name: "UTF-16 with its mark, little-endian", in: utf16Text("Müller;1", false, true), encoding: "auto", want: decoded{"Müller;1", EncodingUTF16LE, ""}},
		{name: "UTF-16 with its mark, big-endian", in: utf16Text("Müller;1", true, true), encoding: "auto", want: decoded{"Müller;1", EncodingUTF16BE, ""}},
		{name: "UTF-16 without a mark, little-endian", in: utf16Text("Datum;Betrag", false, false), encoding: "auto", want: decoded{"Datum;Betrag", EncodingUTF16LE, ""}},
		{name: "UTF-16 without a mark, big-endian", in: utf16Text("Datum;Betrag", true, false), encoding: "auto", want: decoded{"Datum;Betrag", EncodingUTF16BE, ""}},
		{name: "a stray zero in UTF-8 does not make it UTF-16", in: "Datum;Betrag\x00\n", encoding: "auto", want: decoded{"Datum;Betrag\x00\n", EncodingUTF8, ""}},
		{name: "a file of one byte is not UTF-16", in: "a", encoding: "auto", want: decoded{"a", EncodingUTF8, ""}},
		{name: "an empty file is UTF-8", in: "", encoding: "auto", want: decoded{"", EncodingUTF8, ""}},
		{name: "a last byte without its other half is dropped", in: utf16Text("ab", false, true) + "\x00", encoding: "auto", want: decoded{"ab", EncodingUTF16LE, ""}},
		{name: "UTF-8 that is set refuses other bytes", in: "M\xfcller", encoding: "utf-8", want: decoded{"", "", `file is not valid UTF-8 (try encoding = "windows-1252")`}},
		{name: "UTF-8 that is set drops the mark", in: "\xef\xbb\xbfa", encoding: "UTF8", want: decoded{"a", EncodingUTF8, ""}},
		{name: "UTF-16 that is set follows the mark", in: utf16Text("ab", true, true), encoding: "utf-16", want: decoded{"ab", EncodingUTF16BE, ""}},
		{name: "UTF-16 that is set, with nothing to go by, is little-endian", in: "\x41\x42", encoding: "utf-16", want: decoded{"䉁", EncodingUTF16LE, ""}},
		{name: "UTF-16LE that is set", in: utf16Text("ab", false, true), encoding: "utf-16le", want: decoded{"ab", EncodingUTF16LE, ""}},
		{name: "UTF-16BE that is set", in: utf16Text("ab", true, false), encoding: "utf16be", want: decoded{"ab", EncodingUTF16BE, ""}},
		{name: "Windows-1252 that is set reads UTF-8 bytes as it says", in: "M\xc3\xbcller", encoding: "windows-1252", want: decoded{"MÃ¼ller", EncodingWindows1252, ""}},
		{name: "Windows-1252 that is set drops a UTF-8 mark", in: "\xef\xbb\xbf\xfc", encoding: "windows-1252", want: decoded{"ü", EncodingWindows1252, ""}},
		{name: "latin1 is Windows-1252", in: "\xfc", encoding: "latin1", want: decoded{"ü", EncodingWindows1252, ""}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			text, enc, err := decodeToUTF8([]byte(tc.in), strings.ToLower(tc.encoding))

			// then
			if diff := cmp.Diff(tc.want, decoded{text, enc, errText(err)}); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_utf16Order(t *testing.T) {
	tcs := []struct {
		name string
		in   string
		want string
	}{
		{name: "the little-endian mark", in: "\xff\xfe", want: EncodingUTF16LE},
		{name: "the big-endian mark", in: "\xfe\xff", want: EncodingUTF16BE},
		{name: "ASCII as little-endian", in: utf16Text("Buchungstag;Betrag", false, false), want: EncodingUTF16LE},
		{name: "ASCII as big-endian", in: utf16Text("Buchungstag;Betrag", true, false), want: EncodingUTF16BE},
		{name: "UTF-8", in: "Buchungstag;Betrag", want: ""},
		{name: "mostly, but not enough, zeros", in: "a\x00b\x00c\x00dedf", want: ""},
		{name: "zeros on both sides", in: "\x00\x00\x00\x00a\x00", want: ""},
		{name: "zeros on both sides, more on the even one", in: "\x00\x00\x00\x00\x00a", want: ""},
		{name: "too short to say", in: "a\x00", want: ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := utf16Order([]byte(tc.in))

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
