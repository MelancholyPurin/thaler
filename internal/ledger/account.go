package ledger

import (
	"fmt"
	"strings"
	"unicode"
)

// AccountType is the top-level class of an account. It is derived from the
// first segment of the account name (beancount convention), so
// "Assets:Sparkasse:Giro" is an asset account.
type AccountType string

const (
	Assets      AccountType = "Assets"
	Liabilities AccountType = "Liabilities"
	Income      AccountType = "Income"
	Expenses    AccountType = "Expenses"
	Equity      AccountType = "Equity"
)

// Well-known accounts that thaler creates on demand.
const (
	UncategorizedExpense = "Expenses:Uncategorized"
	UncategorizedIncome  = "Income:Uncategorized"
	// ConversionAccount is the multi-currency account that sits between the two
	// legs of a cross-currency transfer, so every currency balances on its own.
	ConversionAccount = "Equity:Conversions"
	// TransferAccount is where both halves of a transfer between your own
	// accounts are booked, so that a transfer is neither income nor spending.
	// Its balance is what is still waiting for its other half.
	TransferAccount = "Equity:Transfers"
	// OpeningBalanceAccount is the other side of every account's opening balance.
	OpeningBalanceAccount = "Equity:Opening"
)

var accountTypes = []AccountType{Assets, Liabilities, Income, Expenses, Equity}

// NeedsCurrency reports whether accounts of this type are bound to a single
// currency. Income, expense and equity accounts may hold any currency.
func (t AccountType) NeedsCurrency() bool { return t == Assets || t == Liabilities }

// TypeOf returns the type implied by an account name's first segment.
func TypeOf(name string) (AccountType, error) {
	root, _, _ := strings.Cut(name, ":")
	for _, t := range accountTypes {
		if string(t) == root {
			return t, nil
		}
	}
	return "", fmt.Errorf("account %q must start with one of Assets, Liabilities, Income, Expenses, Equity", name)
}

// ValidateAccountName checks that name is a colon-separated path whose first
// segment is an account type and whose segments contain only letters, digits,
// '-' and '_'.
func ValidateAccountName(name string) error {
	if _, err := TypeOf(name); err != nil {
		return err
	}
	for _, seg := range strings.Split(name, ":") {
		if seg == "" {
			return fmt.Errorf("account %q has an empty segment", name)
		}
		for i, r := range seg {
			ok := unicode.IsLetter(r) || unicode.IsDigit(r) || (i > 0 && (r == '-' || r == '_'))
			if !ok {
				return fmt.Errorf("account %q: invalid character %q (use letters, digits, '-' and '_')", name, r)
			}
		}
	}
	return nil
}
