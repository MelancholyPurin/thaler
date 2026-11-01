package i18n

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/coded"
)

func Test_Localizer_Error(t *testing.T) {
	notFound := coded.New("account_not_found", map[string]any{"Name": "Assets:Nope"})
	tcs := []struct {
		name string
		tag  language.Tag
		err  error
		want string
	}{
		{name: "nothing", tag: language.English, err: nil, want: ""},
		{name: "a plain error is its own text", tag: language.German, err: errors.New("disk full"), want: "disk full"},
		{name: "a coded error in English", tag: language.English, err: notFound, want: "account does not exist: Assets:Nope"},
		{name: "in Spanish", tag: language.Spanish, err: notFound, want: "la cuenta no existe: Assets:Nope"},
		{name: "in German", tag: language.German, err: notFound, want: "Das Konto existiert nicht: Assets:Nope"},
		{name: "without arguments", tag: language.German, err: coded.New("already_imported", nil), want: "Die Buchung wurde bereits importiert"},
		{name: "wrapped in more text", tag: language.German, err: fmt.Errorf("importing x.csv: %w", notFound), want: "importing x.csv: Das Konto existiert nicht: Assets:Nope"},
		{name: "wrapped twice", tag: language.English, err: fmt.Errorf("a: %w", fmt.Errorf("b: %w", notFound)), want: "a: b: account does not exist: Assets:Nope"},
		{name: "joined", tag: language.English, err: errors.Join(notFound, coded.New("already_imported", nil)), want: "account does not exist: Assets:Nope\ntransaction already imported"},
		{name: "a number as an argument", tag: language.English, err: coded.New("rows_rejected", map[string]any{"Count": 3}), want: "3 rows do not fit the account"},
		{name: "a code nobody defined shows as the code", tag: language.English, err: coded.New("no_such_code", nil), want: "no_such_code"},
		{name: "the pseudo-language", tag: PseudoTag, err: coded.New("already_imported", nil), want: "[tráñšáçtíöñ álréádý ímpörtéd " + strings.Repeat("~", 12) + "]"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got := loc.Error(tc.err)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
