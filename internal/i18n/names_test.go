package i18n

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// The accounts a person made keep their names, and the parts Thaler made are written
// in the language.
func Test_Localizer_AccountName(t *testing.T) {
	tcs := []struct {
		name    string
		tag     language.Tag
		account string
		want    string
	}{
		{name: "English is the stored name", tag: language.English, account: "Assets:Sparkasse", want: "Assets:Sparkasse"},
		{name: "a Spanish type", tag: language.Spanish, account: "Assets:Sparkasse", want: "Activos:Sparkasse"},
		{name: "a German type", tag: language.German, account: "Liabilities:Visa", want: "Verbindlichkeiten:Visa"},
		{name: "a region of German", tag: language.MustParse("de-AT"), account: "Income:Salary", want: "Einnahmen:Salary"},
		{name: "a deeper name keeps every segment", tag: language.German, account: "Expenses:Food:Groceries", want: "Ausgaben:Food:Groceries"},
		{name: "uncategorized spending", tag: language.German, account: ledger.UncategorizedExpense, want: "Ausgaben:Ohne Kategorie"},
		{name: "uncategorized income", tag: language.Spanish, account: ledger.UncategorizedIncome, want: "Ingresos:Sin categoría"},
		{name: "transfers", tag: language.German, account: ledger.TransferAccount, want: "Eigenkapital:Umbuchungen"},
		{name: "conversions", tag: language.Spanish, account: ledger.ConversionAccount, want: "Patrimonio:Conversiones"}, //nolint:misspell // Spanish
		{name: "opening balances", tag: language.Spanish, account: ledger.OpeningBalanceAccount, want: "Patrimonio:Saldo inicial"},
		{name: "a category of the same name elsewhere is the person's", tag: language.German, account: "Expenses:Opening", want: "Ausgaben:Opening"},
		{name: "a type alone", tag: language.German, account: "Equity", want: "Eigenkapital"},
		{name: "not an account", tag: language.German, account: "Groceries", want: "Groceries"},
		{name: "a type in the wrong case is not a type", tag: language.German, account: "assets:Bank", want: "assets:Bank"},
		{name: "nothing", tag: language.German, account: "", want: ""},
		{name: "the pseudo-language", tag: PseudoTag, account: ledger.UncategorizedExpense, want: "[Éxpéñšéš ~~~~]:[Úñçátégörížéd ~~~~~~]"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got := loc.AccountName(tc.account)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The short name drops the type, as the lists do.
func Test_Localizer_ShortName(t *testing.T) {
	tcs := []struct {
		name    string
		tag     language.Tag
		account string
		want    string
	}{
		{name: "an account of the person", tag: language.German, account: "Assets:Sparkasse", want: "Sparkasse"},
		{name: "a deeper name", tag: language.Spanish, account: "Expenses:Food:Groceries", want: "Food:Groceries"},
		{name: "uncategorized spending", tag: language.German, account: ledger.UncategorizedExpense, want: "Ohne Kategorie"},
		{name: "uncategorized income", tag: language.Spanish, account: ledger.UncategorizedIncome, want: "Sin categoría"},
		{name: "transfers", tag: language.German, account: ledger.TransferAccount, want: "Umbuchungen"},
		{name: "transfers in English", tag: language.English, account: ledger.TransferAccount, want: "Transfers"},
		{name: "opening balances in English", tag: language.English, account: ledger.OpeningBalanceAccount, want: "Opening balance"},
		{name: "a name without a type", tag: language.German, account: "Sparkasse", want: "Sparkasse"},
		{name: "nothing", tag: language.German, account: "", want: ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got := loc.ShortName(tc.account)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Every type the ledger knows and every account it makes has a name in every language,
// so that none of them reaches the screen as an id.
func Test_Localizer_AccountName_everyBuiltin(t *testing.T) {
	tcs := []struct {
		name    string
		account string
	}{
		{name: "assets", account: "Assets:X"},
		{name: "liabilities", account: "Liabilities:X"},
		{name: "income", account: "Income:X"},
		{name: "expenses", account: "Expenses:X"},
		{name: "equity", account: "Equity:X"},
		{name: "uncategorized spending", account: ledger.UncategorizedExpense},
		{name: "uncategorized income", account: ledger.UncategorizedIncome},
		{name: "transfers", account: ledger.TransferAccount},
		{name: "conversions", account: ledger.ConversionAccount},
		{name: "opening balance", account: ledger.OpeningBalanceAccount},
	}
	for _, tc := range tcs {
		for _, l := range Languages {
			t.Run(tc.name+" in "+l.Code, func(t *testing.T) {
				// given
				loc := New(l.Tag)

				// when
				got := loc.AccountName(tc.account)

				// then
				if got == "" || got == tc.account && l.Code != "en" {
					t.Errorf("%s is %q in %s, which is not translated", tc.account, got, l.Code)
				}
				if strings.Contains(got, "account_") {
					t.Errorf("%s shows a message id in %s: %q", tc.account, l.Code, got)
				}
			})
		}
	}
}

// The folders of backups are named by the kind core gives them.
func Test_Localizer_BackupFolder(t *testing.T) {
	tcs := []struct {
		name string
		tag  language.Tag
		kind string
		want string
	}{
		{name: "backups", tag: language.English, kind: "backups", want: "backups"},
		{name: "replaced", tag: language.English, kind: "replaced", want: "replaced by a restore"},
		{name: "before an upgrade", tag: language.English, kind: "pre_migration", want: "copies before an upgrade"},
		{name: "a kind nobody knows", tag: language.English, kind: "elsewhere", want: "elsewhere"},
		{name: "German", tag: language.German, kind: "replaced", want: "durch eine Wiederherstellung ersetzt"},
		{name: "Spanish", tag: language.Spanish, kind: "pre_migration", want: "copias previas a una actualización"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			loc := New(tc.tag)

			// when
			got := loc.BackupFolder(tc.kind)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
