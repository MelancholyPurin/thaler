package core

import (
	"context"
	"strings"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The categories are the Income and Expenses accounts. Everything refers to an account
// by its id, so renaming one changes a name and merging one moves what is on it.
// Uncategorized (one of each type) is Thaler's, and stays as it is.

// isUncategorized says whether name is one of the two accounts Thaler keeps for what has
// no category.
func isUncategorized(name string) bool {
	return name == ledger.UncategorizedExpense || name == ledger.UncategorizedIncome
}

// categoryAccount is the account called name, once it is known to be an Income or Expenses
// one. With editable, Uncategorized is refused too.
func categoryAccount(ctx context.Context, q queries, name string, editable bool) (Account, error) {
	acc, err := q.account(ctx, name)
	if err != nil {
		return Account{}, err
	}
	if acc.Type != ledger.Income && acc.Type != ledger.Expenses {
		return Account{}, coded.New("category_not_income_expense", map[string]any{"Name": acc.Name})
	}
	if editable && isUncategorized(acc.Name) {
		return Account{}, coded.New("category_protected", map[string]any{"Name": acc.Name})
	}
	return acc, nil
}

// below are the names of the accounts under name (Expenses:Food:Out is under Expenses:Food).
func below(accounts []Account, name string) []Account {
	var out []Account
	for _, a := range accounts {
		if strings.HasPrefix(a.Name, name+":") {
			out = append(out, a)
		}
	}
	return out
}

// RenameCategory gives the category from, and every category below it, a new name: the
// part of the name that is from becomes to. The new name keeps the type, and none of the
// new names may be taken. Everything follows the category, and all of it or none is done.
func (s *Local) RenameCategory(ctx context.Context, from, to string) (CategoryRename, error) {
	var out CategoryRename
	err := s.repo.inTx(ctx, func(q queries) error {
		acc, err := categoryAccount(ctx, q, from, true)
		if err != nil {
			return err
		}
		if err := ledger.ValidateAccountName(to); err != nil {
			return err
		}
		if typ, _ := ledger.TypeOf(to); typ != acc.Type {
			return coded.New("category_type_change", map[string]any{"From": from, "To": to, "Type": acc.Type})
		}
		if to == from {
			return coded.New("category_same_name", map[string]any{"Name": from})
		}
		if isUncategorized(to) {
			return coded.New("category_protected", map[string]any{"Name": to})
		}
		all, err := q.accounts(ctx)
		if err != nil {
			return err
		}
		taken := make(map[string]bool, len(all))
		for _, a := range all {
			taken[a.Name] = true
		}
		renamed := append([]Account{acc}, below(all, from)...)
		for _, a := range renamed {
			name := to + strings.TrimPrefix(a.Name, from)
			if taken[name] {
				return coded.New("account_exists", map[string]any{"Name": name})
			}
			if err := q.renameAccount(ctx, a.ID, name); err != nil {
				return err
			}
			out.Renamed = append(out.Renamed, name)
		}
		return nil
	})
	if err != nil {
		return CategoryRename{}, err
	}
	return out, nil
}

// MergeCategory moves everything on the category from to the category into, which must
// be of the same type, and removes from. Its transactions and schedules now book to
// into. Its monthly targets move too, except those for a month into has a target for
// (into keeps its own), and its carry-over, unless into has one. Merging into
// Uncategorized moves the transactions and drops the targets, which it cannot have.
// A category with categories below it is refused: merge those first. All of it or none is done.
func (s *Local) MergeCategory(ctx context.Context, from, into string) (CategoryMerge, error) {
	var out CategoryMerge
	err := s.repo.inTx(ctx, func(q queries) error {
		src, err := categoryAccount(ctx, q, from, true)
		if err != nil {
			return err
		}
		dst, err := categoryAccount(ctx, q, into, false)
		if err != nil {
			return err
		}
		if src.ID == dst.ID {
			return coded.New("category_merge_self", map[string]any{"Name": src.Name})
		}
		if src.Type != dst.Type {
			return coded.New("category_type_mismatch", map[string]any{"From": src.Name, "Into": dst.Name, "FromType": src.Type, "IntoType": dst.Type})
		}
		out, err = mergeInto(ctx, q, src, dst)
		return err
	})
	if err != nil {
		return CategoryMerge{}, err
	}
	return out, nil
}

// DeleteCategory removes the category name: what is booked to it is moved to the
// Uncategorized account of its type (made if it is not there), as MergeCategory into
// Uncategorized does, so that no transaction is lost or left without a category.
func (s *Local) DeleteCategory(ctx context.Context, name string) (CategoryMerge, error) {
	var out CategoryMerge
	err := s.repo.inTx(ctx, func(q queries) error {
		src, err := categoryAccount(ctx, q, name, true)
		if err != nil {
			return err
		}
		uncategorized := ledger.UncategorizedExpense
		if src.Type == ledger.Income {
			uncategorized = ledger.UncategorizedIncome
		}
		dst, err := resolveAccount(ctx, q, uncategorized, "", new([]string))
		if err != nil {
			return err
		}
		out, err = mergeInto(ctx, q, src, dst)
		return err
	})
	if err != nil {
		return CategoryMerge{}, err
	}
	return out, nil
}

// mergeInto does the merge of two categories of one type that are known to be different,
// inside a database transaction.
func mergeInto(ctx context.Context, q queries, src, dst Account) (CategoryMerge, error) {
	all, err := q.accounts(ctx)
	if err != nil {
		return CategoryMerge{}, err
	}
	if sub := below(all, src.Name); len(sub) > 0 {
		return CategoryMerge{}, coded.New("category_has_subcategories", map[string]any{"Name": src.Name, "Sub": sub[0].Name})
	}
	if dst.Currency != "" {
		currencies, err := q.accountCurrencies(ctx, src.ID)
		if err != nil {
			return CategoryMerge{}, err
		}
		for _, c := range currencies {
			if c != dst.Currency {
				return CategoryMerge{}, coded.New("category_currency", map[string]any{"Name": src.Name, "Into": dst.Name, "Currency": dst.Currency})
			}
		}
	}
	var out CategoryMerge
	if out.Transactions, err = q.moveAccountPostings(ctx, src.ID, dst.ID); err != nil {
		return CategoryMerge{}, err
	}
	if out.Schedules, err = q.moveSchedulePostings(ctx, src.ID, dst.ID); err != nil {
		return CategoryMerge{}, err
	}
	if out.Targets, out.TargetsDropped, err = q.moveBudget(ctx, src.ID, dst.ID, !isUncategorized(dst.Name)); err != nil {
		return CategoryMerge{}, err
	}
	if err := q.deleteAccount(ctx, src.ID); err != nil {
		return CategoryMerge{}, err
	}
	return out, q.touchAccount(ctx, dst.ID)
}
