package core

import (
	"context"
	"errors"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// Your own accounts are the Assets and Liabilities ones. They can be renamed, and set aside
// when the bank account itself is gone, without losing what happened on them: everything
// refers to an account by its id, so a rename changes one name, and a closed account is the
// same row with the time it was closed. A closed account holds nothing and takes no new money
// (see guardOpen), so the balances and the reports of any month stay what they were.

// moneyAccount is the account called name, once it is known to be an Assets or Liabilities
// one.
func moneyAccount(ctx context.Context, q queries, name string) (Account, error) {
	acc, err := q.account(ctx, name)
	if err != nil {
		return Account{}, err
	}
	if !acc.Type.NeedsCurrency() {
		return Account{}, coded.New("account_not_money", map[string]any{"Name": acc.Name, "Type": acc.Type})
	}
	return acc, nil
}

// guardOpen refuses a closed account.
func guardOpen(acc Account) error {
	if acc.Closed {
		return coded.New("account_closed", map[string]any{"Name": acc.Name})
	}
	return nil
}

// guardTransactionOpen refuses to change or remove a transaction that has money on a closed
// account: that would change what the account holds.
func guardTransactionOpen(ctx context.Context, q queries, txID int64) error {
	ids, err := q.transactionAccounts(ctx, txID)
	if err != nil {
		return err
	}
	name, err := q.closedAccountIn(ctx, ids)
	if err != nil {
		return err
	}
	if name != "" {
		return coded.New("account_closed", map[string]any{"Name": name})
	}
	return nil
}

// RenameAccount gives the Assets or Liabilities account from the name to. The new name keeps
// the type and must be free.
func (s *Local) RenameAccount(ctx context.Context, from, to string) (Account, error) {
	var out Account
	err := s.repo.inTx(ctx, func(q queries) error {
		acc, err := moneyAccount(ctx, q, from)
		if err != nil {
			return err
		}
		if err := ledger.ValidateAccountName(to); err != nil {
			return err
		}
		if typ, _ := ledger.TypeOf(to); typ != acc.Type {
			return coded.New("account_type_change", map[string]any{"From": from, "To": to, "Type": acc.Type})
		}
		if to == from {
			return coded.New("account_same_name", map[string]any{"Name": from})
		}
		if _, err := q.account(ctx, to); err == nil {
			return coded.New("account_exists", map[string]any{"Name": to})
		} else if !errors.Is(err, ErrNoAccount) {
			return err
		}
		if err := q.renameAccount(ctx, acc.ID, to); err != nil {
			return err
		}
		out = acc
		out.Name = to
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return out, nil
}

// CloseAccount sets an account aside. It has to hold nothing, in any currency, and no
// schedule may book to it: those are the person's to move or end first, so that closing
// never hides money or leaves a schedule that cannot be confirmed.
func (s *Local) CloseAccount(ctx context.Context, name string) (Account, error) {
	var out Account
	err := s.repo.inTx(ctx, func(q queries) error {
		acc, err := moneyAccount(ctx, q, name)
		if err != nil {
			return err
		}
		if acc.Closed {
			return coded.New("account_already_closed", map[string]any{"Name": acc.Name})
		}
		held, err := q.accountHoldings(ctx, acc.ID)
		if err != nil {
			return err
		}
		for _, m := range held {
			return coded.New("account_not_empty", map[string]any{"Name": acc.Name, "Balance": m.String()})
		}
		n, err := q.scheduleCount(ctx, acc.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return coded.New("account_has_schedules", map[string]any{"Name": acc.Name, "Count": n})
		}
		if err := q.setAccountClosed(ctx, acc.ID, true); err != nil {
			return err
		}
		out = acc
		out.Closed = true
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return out, nil
}

// ReopenAccount takes a closed account back into use.
func (s *Local) ReopenAccount(ctx context.Context, name string) (Account, error) {
	var out Account
	err := s.repo.inTx(ctx, func(q queries) error {
		acc, err := moneyAccount(ctx, q, name)
		if err != nil {
			return err
		}
		if !acc.Closed {
			return coded.New("account_not_closed", map[string]any{"Name": acc.Name})
		}
		if err := q.setAccountClosed(ctx, acc.ID, false); err != nil {
			return err
		}
		out = acc
		out.Closed = false
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return out, nil
}
