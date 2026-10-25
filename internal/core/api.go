// Package core is Thaler's application layer: the one place that knows the
// bookkeeping rules and the only code that touches the database.
//
// Everything a front end can ask for is on the Service interface. The terminal
// UI and the command line depend on that interface and nothing else, so the
// SQLite-backed implementation (Local) can later be swapped for a client that
// talks to a server without touching them.
//
//	api.go         this file: the interface and the types that cross it
//	service.go     Local: the rules (validation, privacy later, import, reports)
//	repository.go  Local's SQL; unexported, so nothing outside can bypass the rules
//	migrations.go  schema versioning
package core

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

var (
	// ErrNoAccount means a named account does not exist.
	ErrNoAccount = errors.New("account does not exist")
	// ErrAccountExists means an account with that name is already present.
	ErrAccountExists = errors.New("account already exists")
	// ErrDuplicate means a transaction with the same import hash is already stored.
	ErrDuplicate = errors.New("transaction already imported")
	// ErrRowsRejected means an import contained rows that do not fit the target
	// account and the caller did not ask to skip them. Nothing was written.
	ErrRowsRejected = errors.New("some rows do not fit the account")
)

// Service is everything a front end can ask of Thaler. Amounts are always
// ledger.Money and dates are YYYY-MM-DD strings or time.Time, so every request
// and response can be encoded for a network later without changing meaning.
type Service interface {
	io.Closer

	// Settings and exchange rates.
	BaseCurrency(ctx context.Context) (ledger.Currency, error)
	SetBaseCurrency(ctx context.Context, c ledger.Currency) error
	// SetRate records that on date one unit of base is worth rate units of
	// quote; rate is a normalised decimal string from ledger.ParseRate.
	SetRate(ctx context.Context, date string, base, quote ledger.Currency, rate string) error
	Rates(ctx context.Context) ([]ledger.RateEntry, error)

	// Accounts. Assets and Liabilities need a currency.
	AddAccount(ctx context.Context, name string, currency ledger.Currency) (Account, error)
	GetAccount(ctx context.Context, name string) (Account, error)
	ListAccounts(ctx context.Context) ([]Account, error)

	// Transactions.
	AddTransaction(ctx context.Context, nt NewTransaction) (AddResult, error)
	GetTransaction(ctx context.Context, id int64) (TxView, error)
	ListTransactions(ctx context.Context, f TxFilter) ([]TxView, error)
	// ReplaceTransaction rewrites an existing transaction in place, keeping its
	// id and import hash. It returns the names of categories it created.
	ReplaceTransaction(ctx context.Context, id int64, nt NewTransaction) ([]string, error)
	DeleteTransaction(ctx context.Context, id int64) error
	// RestoreTransaction stores nt under a free id: undo for a delete.
	RestoreTransaction(ctx context.Context, id int64, nt NewTransaction) error
	// SetCategories re-books the category leg of each transaction, all or
	// nothing, and returns the names of categories it created.
	SetCategories(ctx context.Context, changes []CategoryChange) ([]string, error)
	Transfer(ctx context.Context, req TransferRequest) (TransferResult, error)

	// Import books parsed bank rows into one account, categorised by the rules
	// in the request. With DryRun nothing is written and every row's outcome is
	// listed. Importing is idempotent: rows already stored are skipped.
	Import(ctx context.Context, req ImportRequest) (ImportResult, error)
	// Recategorize applies rules to transactions still in an Uncategorized
	// account.
	Recategorize(ctx context.Context, req RecategorizeRequest) (RecategorizeResult, error)

	// Reports.
	BalanceReport(ctx context.Context, req BalanceRequest) (BalanceReport, error)
	BudgetReport(ctx context.Context, req BudgetRequest) (BudgetReport, error)

	// Maintenance.
	// Check returns a description of every database inconsistency found.
	Check(ctx context.Context) ([]string, error)
	// Backup writes a snapshot to dest, a path on the machine running the
	// service. dest must not exist yet.
	Backup(ctx context.Context, dest string) error
}

// ---- accounts and transactions ----------------------------------------------

// Account is a node in the chart of accounts.
type Account struct {
	ID       int64
	Name     string
	Type     ledger.AccountType
	Currency ledger.Currency // empty = any currency
}

// NewTransaction is the input for adding a transaction. Postings reference
// accounts by name. Income, Expenses and Equity accounts that do not exist yet
// are created on the fly; Assets and Liabilities must already exist.
type NewTransaction struct {
	Date        time.Time
	Payee       string
	Description string
	ImportHash  string // optional; a second transaction with the same hash is rejected
	Postings    []ledger.Posting
}

// AddResult describes a stored transaction.
type AddResult struct {
	ID      int64
	Created []string // names of accounts that were created implicitly
}

// CategoryChange moves the category leg of one transaction to another account.
type CategoryChange struct {
	TransactionID int64
	Category      string
}

// TxFilter narrows ListTransactions. Empty fields mean "no restriction".
type TxFilter struct {
	ID      int64  // a single transaction; 0 = no restriction
	Account string // matches the account itself and everything below it
	From    string // YYYY-MM-DD, inclusive
	To      string // YYYY-MM-DD, inclusive
	Limit   int    // newest N transactions; <= 0 means all
}

// PostingView is one leg of a stored transaction.
type PostingView struct {
	Account string
	Amount  ledger.Money
}

// TxView is a stored transaction with its postings.
type TxView struct {
	ID          int64
	Date        string
	Payee       string
	Description string
	ImportHash  string // empty for manual entries
	Postings    []PostingView
}

// Legs splits a simple two-posting transaction into the leg on your own account
// (source) and the leg on its category. ok is false for transfers, splits and
// anything else that is not exactly one category posting plus one other.
func (t TxView) Legs() (source, category PostingView, ok bool) {
	if len(t.Postings) != 2 {
		return PostingView{}, PostingView{}, false
	}
	for i, p := range t.Postings {
		typ, err := ledger.TypeOf(p.Account)
		if err != nil || typ.NeedsCurrency() {
			continue
		}
		other := t.Postings[1-i]
		if otherTyp, err := ledger.TypeOf(other.Account); err != nil || !otherTyp.NeedsCurrency() {
			return PostingView{}, PostingView{}, false
		}
		return other, p, true
	}
	return PostingView{}, PostingView{}, false
}

// NewTransaction converts a stored transaction back into input, for undoing a
// delete or an edit.
func (t TxView) NewTransaction() (NewTransaction, error) {
	d, err := ledger.ParseDate(t.Date)
	if err != nil {
		return NewTransaction{}, err
	}
	nt := NewTransaction{Date: d, Payee: t.Payee, Description: t.Description, ImportHash: t.ImportHash}
	for _, p := range t.Postings {
		nt.Postings = append(nt.Postings, ledger.Posting{Account: p.Account, Amount: p.Amount})
	}
	return nt, nil
}

// TransferRequest moves money between two of your own accounts.
type TransferRequest struct {
	From, To string
	// Amount leaves From, in From's currency.
	Amount ledger.Money
	// ToAmount arrives in To, in To's currency. It is required when the
	// accounts hold different currencies (it pins down the rate you got) and
	// must be left zero when they hold the same one.
	ToAmount ledger.Money
	Date     time.Time
	Memo     string
}

// TransferResult describes a stored transfer.
type TransferResult struct {
	AddResult
	// In is what arrived on the destination account (equal to the request's
	// Amount for a same-currency transfer).
	In ledger.Money
	// CrossCurrency is true when the transfer went through Equity:Conversions.
	CrossCurrency bool
}

// ---- import and recategorisation ---------------------------------------------

// ImportRequest books rows parsed from a bank export into Account.
type ImportRequest struct {
	Account string
	Rows    []importer.Row
	Rules   []importer.Rule // first match wins; unmatched rows go to Uncategorized
	DryRun  bool
	// IgnoreRejected imports the rows that fit even if others were rejected
	// (see ImportResult.Rejected). Without it any rejected row aborts the import.
	IgnoreRejected bool
}

// ImportedRow is the outcome for one row of a dry run.
type ImportedRow struct {
	Row       importer.Row
	Category  string
	Duplicate bool // already stored, so a real import would skip it
}

// RejectedRow is a row that does not fit the target account.
type RejectedRow struct {
	Line   int
	Reason string
}

// ImportResult summarises an import.
type ImportResult struct {
	Inserted      int     // new transactions (would be, in a dry run)
	IDs           []int64 // the new transactions, so the import can be undone; empty in a dry run
	Duplicates    int     // rows already stored
	Uncategorized int     // rows no rule matched
	Created       []string
	Rejected      []RejectedRow
	Rows          []ImportedRow // dry run only
}

// RecategorizeRequest applies Rules to every uncategorized transaction.
type RecategorizeRequest struct {
	Rules  []importer.Rule
	DryRun bool
}

// Move is one transaction a rule re-books.
type Move struct {
	TransactionID int64
	Date          string
	Payee         string
	Category      string       // the new category
	Amount        ledger.Money // on your own account
}

// RecategorizeResult lists what moved (or, in a dry run, would move).
type RecategorizeResult struct {
	Moves         []Move
	Uncategorized int // how many transactions were uncategorized before
	Created       []string
}

// ---- reports -----------------------------------------------------------------

// Balance is the sum of all postings of one account in one currency. Postings
// keep the ledger's sign convention: assets are positive; liabilities, income
// and equity are negative when they hold a credit balance.
type Balance struct {
	Account string
	Type    ledger.AccountType
	Amount  ledger.Money
}

// BalanceRequest asks for balances and net worth.
type BalanceRequest struct {
	AsOf       string // YYYY-MM-DD; empty = today
	IncludeAll bool   // also list income, expense and equity accounts
}

// NetWorth is the sum of every asset and liability balance converted into the
// base currency.
type NetWorth struct {
	Base  ledger.Currency
	Total ledger.Money
	// Missing lists currencies that have no usable exchange rate to Base. Their
	// accounts are left out of Total instead of being guessed.
	Missing []ledger.Currency
}

// BalanceReport is the answer to a BalanceRequest.
type BalanceReport struct {
	Date     string // the day it is as of
	Lines    []Balance
	NetWorth NetWorth
}

// BudgetRequest asks where money came from and went over a period.
type BudgetRequest struct {
	From, To string // YYYY-MM-DD, inclusive; an empty bound is open
	// RateDate is the day exchange rates are looked up on; empty = today. It is
	// capped at To so that a past month uses the rates of that month.
	RateDate string
}

// BudgetLine is the total of one category over the period, converted into the
// base currency.
type BudgetLine struct {
	Name   string
	Amount ledger.Money
}

// BudgetReport is the answer to a BudgetRequest. Expenses are positive; income
// is positive too (it is flipped from the ledger's credit sign).
type BudgetReport struct {
	Base                      ledger.Currency
	Income, Expenses          []BudgetLine // largest first
	IncomeTotal, ExpenseTotal ledger.Money
	Missing                   []ledger.Currency // no exchange rate, so left out
}
