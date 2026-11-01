package i18n

import (
	"strings"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// The names Thaler gives accounts itself are stored in English (Assets:Bank,
// Expenses:Uncategorized): that is the ledger's language, what `thaler cli` prints
// and what a person types, and it never changes with the language. What a person
// reads in the interface goes through AccountName and ShortName, which write the
// built-in parts in the language and leave the names the person chose as they are.

// typeName is the name of an account type in the language.
func (l *Localizer) typeName(t ledger.AccountType) string {
	switch t {
	case ledger.Assets:
		return l.T("account_type_assets", nil)
	case ledger.Liabilities:
		return l.T("account_type_liabilities", nil)
	case ledger.Income:
		return l.T("account_type_income", nil)
	case ledger.Expenses:
		return l.T("account_type_expenses", nil)
	case ledger.Equity:
		return l.T("account_type_equity", nil)
	}
	return string(t)
}

// builtinName is the name in the language of an account Thaler creates itself, and
// whether name is one. Only the stored name of the account matches: Expenses:Opening
// is a category somebody made.
func (l *Localizer) builtinName(name string) (string, bool) {
	switch name {
	case ledger.UncategorizedExpense, ledger.UncategorizedIncome:
		return l.T("account_uncategorized", nil), true
	case ledger.TransferAccount:
		return l.T("account_transfers", nil), true
	case ledger.ConversionAccount:
		return l.T("account_conversions", nil), true
	case ledger.OpeningBalanceAccount:
		return l.T("account_opening", nil), true
	}
	return "", false
}

// ShortName is an account without its type, the way the lists show it: Savings for
// Assets:Savings, and for the accounts Thaler makes, its name in the language.
func (l *Localizer) ShortName(name string) string {
	if shown, ok := l.builtinName(name); ok {
		return shown
	}
	if _, rest, ok := strings.Cut(name, ":"); ok {
		return rest
	}
	return name
}

// AccountName is the full name of an account for reading: the type in the language
// (Activos:Sparkasse), and for the accounts Thaler makes, their names too
// (Gastos:Sin categoría). It is never what to type or store; that is the name as
// the ledger has it.
func (l *Localizer) AccountName(name string) string {
	typ, err := ledger.TypeOf(name)
	if err != nil {
		return name // not an account of a known type: shown as it is
	}
	if !strings.Contains(name, ":") {
		return l.typeName(typ)
	}
	return l.typeName(typ) + ":" + l.ShortName(name)
}
