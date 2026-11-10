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
	"io"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

var (
	// ErrNoAccount means a named account does not exist.
	ErrNoAccount = coded.New("account_not_found", nil)
	// ErrAccountExists means an account with that name is already present.
	ErrAccountExists = coded.New("account_exists", nil)
	// ErrDuplicate means a transaction with the same import hash is already stored.
	ErrDuplicate = coded.New("already_imported", nil)
	// ErrRowsRejected means an import contained rows that do not fit the target
	// account and the caller did not ask to skip them. Nothing was written.
	ErrRowsRejected = coded.New("rows_rejected", nil)
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
	// SetOpeningBalance books (or corrects) the transaction that makes an asset
	// or liability account hold the balance you read off your bank.
	SetOpeningBalance(ctx context.Context, req OpeningBalanceRequest) (OpeningBalanceResult, error)

	// Import books parsed bank rows into one account, categorised by the rules
	// in the request. With DryRun nothing is written and every row's outcome is
	// listed. Importing is idempotent: rows already stored are skipped.
	Import(ctx context.Context, req ImportRequest) (ImportResult, error)
	// DetachImport makes imported transactions hand-entered ones again by
	// forgetting which bank row they stand for. It undoes the adoption an import
	// made; the transactions themselves stay.
	DetachImport(ctx context.Context, ids []int64) error
	// Recategorize applies rules to transactions still in an Uncategorized
	// account.
	Recategorize(ctx context.Context, req RecategorizeRequest) (RecategorizeResult, error)

	// Reports.
	BalanceReport(ctx context.Context, req BalanceRequest) (BalanceReport, error)
	BudgetReport(ctx context.Context, req BudgetRequest) (BudgetReport, error)

	// MonthBudget is what each expense category spent in a month against its
	// target, and what is left.
	MonthBudget(ctx context.Context, req MonthBudgetRequest) (MonthBudget, error)

	// Budget targets: what an expense category may spend in a month (YYYY-MM),
	// in the base currency. A month keeps its own targets.
	// SetBudgetTarget sets or replaces the target of category for month: what an
	// Expenses category may spend, or what an Income category is planned to bring in
	// (MonthBudget compares the plan with the targets). Uncategorized has none.
	SetBudgetTarget(ctx context.Context, month, category string, amount ledger.Money) error
	// ClearBudgetTarget removes it; a category with no target is left as it is.
	ClearBudgetTarget(ctx context.Context, month, category string) error
	// BudgetTargets lists the targets of month, by category name.
	BudgetTargets(ctx context.Context, month string) ([]BudgetTarget, error)
	// SetBudgetRollover makes category carry what is left of its target, or what it
	// overspent, into the next month, from month `from` on (that month itself starts
	// with nothing carried in). Setting it again moves the start.
	SetBudgetRollover(ctx context.Context, category, from string) error
	// ClearBudgetRollover stops the carrying; a category that has none is left as it is.
	ClearBudgetRollover(ctx context.Context, category string) error
	// BudgetRollovers lists the categories that carry over, by name.
	BudgetRollovers(ctx context.Context) ([]BudgetRollover, error)
	// CopyBudgetTargets starts the month `to` from the targets of `from`: it adds
	// those `to` lacks and keeps the ones it has, so that nothing is overwritten.
	CopyBudgetTargets(ctx context.Context, from, to string) (CopiedTargets, error)

	// Schedules: transactions that repeat. A schedule is a template and a rhythm;
	// defining one posts nothing (what is due, and asking first, come with REC-2
	// and REC-3).
	// CreateSchedule stores a schedule and returns its id. A category it names that
	// does not exist yet is created, as for a transaction.
	CreateSchedule(ctx context.Context, ns NewSchedule) (int64, error)
	// ReplaceSchedule rewrites the schedule id in place.
	ReplaceSchedule(ctx context.Context, id int64, ns NewSchedule) error
	// DeleteSchedule removes a schedule; the transactions it was confirmed into stay.
	DeleteSchedule(ctx context.Context, id int64) error
	// PauseSchedule stops the schedule proposing anything from today on, and
	// ResumeSchedule starts it again from today. The dates in between are dropped,
	// not held back; the ones before the pause that nobody answered stay due.
	// Pausing a paused schedule, or resuming one that runs, leaves it as it is.
	PauseSchedule(ctx context.Context, id int64) error
	ResumeSchedule(ctx context.Context, id int64) error
	// Schedules lists them by name.
	Schedules(ctx context.Context) ([]Schedule, error)
	// Due lists the occurrences of every schedule from its start date up to and
	// including upTo (YYYY-MM-DD), oldest first, a schedule's name breaking a tie
	// on the same day. It only proposes: nothing is posted. An occurrence that was
	// confirmed or skipped is not listed again.
	Due(ctx context.Context, upTo string) ([]Occurrence, error)
	// ScheduleStatuses lists the schedules with when each is next due, as of asOf
	// (YYYY-MM-DD, today): the soonest first, a name breaking a tie, and the ones
	// with nothing coming (paused, or past their end) last.
	ScheduleStatuses(ctx context.Context, asOf string) ([]ScheduleStatus, error)
	// ConfirmOccurrence books the occurrence of schedule id on date as a transaction
	// dated that day, and returns it. postings, when given, replace the template's
	// (to change an amount); they must balance. Only a date Due lists today can be
	// confirmed. Deleting the transaction later makes the occurrence due again.
	ConfirmOccurrence(ctx context.Context, id int64, date string, postings []ledger.Posting) (ConfirmResult, error)
	// SkipOccurrence passes on the occurrence: nothing is booked, and it is not
	// listed as due again.
	SkipOccurrence(ctx context.Context, id int64, date string) error
	// UnskipOccurrence takes a skip back: the occurrence is due again. One that was
	// confirmed, or not answered, is an error; deleting what it booked is how a
	// confirm is taken back.
	UnskipOccurrence(ctx context.Context, id int64, date string) error

	// Maintenance.
	// Check returns a description of every database inconsistency found.
	Check(ctx context.Context) ([]string, error)
	// Backup writes a snapshot to dest, a path on the machine running the
	// service. dest must not exist yet.
	Backup(ctx context.Context, dest string) error
	// BackupEncrypted is Backup with the snapshot sealed under a passphrase
	// (an age file), checked by decrypting it once before it returns.
	BackupEncrypted(ctx context.Context, dest, passphrase string) error
}

// ---- accounts and transactions ----------------------------------------------

// Account is a node in the chart of accounts.
type Account struct {
	ID       int64
	Name     string
	Type     ledger.AccountType
	Currency ledger.Currency // empty = any currency
}

// LedgerBalance converts a balance as people read it into the ledger's sign:
// the amount a bank shows for a credit card is what you owe, so a positive
// number on a Liabilities account becomes negative. Assets keep their sign.
func (a Account) LedgerBalance(shown ledger.Money) ledger.Money {
	if a.Type == ledger.Liabilities {
		return shown.Neg()
	}
	return shown
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
	// TransferWith is the other half when this is one half of a transfer between
	// your own accounts (both booked to Equity:Transfers); 0 otherwise.
	TransferWith int64
	Postings     []PostingView
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
	// AddResult is the half that leaves the source account; InID is the half
	// that arrives, linked to it. Each is an ordinary transaction on its own
	// account, as an import of the two bank statements would produce.
	AddResult
	InID int64
	// In is what arrived on the destination account (equal to the request's
	// Amount for a same-currency transfer).
	In ledger.Money
	// CrossCurrency is true when the transfer went through Equity:Conversions.
	CrossCurrency bool
}

// OpeningBalanceRequest says what an account held at the end of a day, as the
// bank shows it. The service works out the opening balance from that: whatever
// the transactions already booked do not explain.
type OpeningBalanceRequest struct {
	Account string
	// Balance is in the account's currency and the ledger's sign: positive for
	// an asset that holds money, negative for a liability that owes it.
	Balance ledger.Money
	AsOf    time.Time
}

// OpeningBalanceResult describes the opening-balance transaction. Calling
// SetOpeningBalance again corrects the same transaction instead of adding one.
type OpeningBalanceResult struct {
	// ID is the opening-balance transaction; 0 when the booked transactions
	// already explain the balance and none was needed.
	ID     int64
	Date   string       // the day it is booked on: before the account's first transaction
	Amount ledger.Money // what it books on the account
	// Before is the transaction as it was when this call corrected it; nil when
	// the call created it. It is what an undo puts back.
	Before  *TxView
	Created []string // Equity:Opening, the first time
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
	// Transfer is set when the row is the other half of a transfer that is
	// already stored on another of your accounts; Category is then
	// Equity:Transfers.
	Transfer *TransferMatch
	// Adopts is set when the row is the bank's record of a transaction you
	// entered by hand: that entry takes over the row instead of a new
	// transaction being added, and Category is the entry's own category.
	Adopts *AdoptedEntry
	// Settles is set when the row is the bank's booking of an occurrence of a
	// schedule that nobody answered yet: a real import then answers it, and
	// Category is the schedule's unless a rule chose one.
	Settles *SettledOccurrence
}

// ConfirmResult is what confirming an occurrence booked. ID is the transaction;
// for a scheduled transfer it is the half that leaves, and InID the half that
// arrives (0 for anything else).
type ConfirmResult struct {
	AddResult
	InID int64
}

// SettledOccurrence is the occurrence of a schedule that an imported row stands
// for.
type SettledOccurrence struct {
	ScheduleID    int64
	Schedule      string // its name
	Date          string // the occurrence's own date
	TransactionID int64  // the transaction the row became; 0 in a dry run
}

// AdoptedEntry is the hand-entered transaction a bank row was matched with.
type AdoptedEntry struct {
	ID       int64
	Date     string
	Payee    string
	Category string
}

// TransferMatch is the stored transaction an imported row was paired with.
type TransferMatch struct {
	ID      int64
	Account string // the own account the money moved on
	Date    string
	// Was is where that transaction's other leg was booked before: an
	// Uncategorized account, or Equity:Transfers when a rule had marked it.
	Was string
}

// TransferLink records a pairing a real import made, so that it can be undone:
// transaction ID was imported as the other half of OtherID, which was booked to
// OtherWas until then.
type TransferLink struct {
	ID, OtherID int64
	OtherWas    string
}

// RejectedRow is a row that does not fit the target account.
type RejectedRow struct {
	Line int
	Err  error // why, as a coded error
}

// ImportResult summarises an import.
type ImportResult struct {
	Inserted      int                 // new transactions (would be, in a dry run)
	IDs           []int64             // the new transactions, so the import can be undone; empty in a dry run
	Transfers     int                 // of those, how many were paired with a stored transaction as a transfer
	Linked        []TransferLink      // the pairings made; empty in a dry run
	Adopted       int                 // rows that matched an entry made by hand and were not added again
	AdoptedIDs    []int64             // those entries, so the import can be undone; empty in a dry run
	Settled       []SettledOccurrence // occurrences answered by a row (would be, in a dry run)
	Duplicates    int                 // rows already stored
	Uncategorized int                 // rows no rule matched and no transfer explained
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
	// RateDate is the latest day exchange rates are looked up on; empty = today.
	// Each amount is converted at the rate of the day it was booked (never a later
	// one than RateDate), so a rate entered afterwards leaves earlier days alone. A
	// day before its currency's first rate takes the rate at the end of the period.
	RateDate string
}

// BudgetLine is the total of one category over the period, converted into the
// base currency.
type BudgetLine struct {
	Name   string
	Amount ledger.Money
}

// BudgetTarget is what a category may spend in a month.
type BudgetTarget struct {
	Category string
	Amount   ledger.Money
}

// BudgetRollover says a category carries its leftover into the next month, from
// month From (YYYY-MM) on.
type BudgetRollover struct {
	Category string
	From     string
}

// ScheduleUnit is what a schedule counts in.
type ScheduleUnit string

const (
	EveryWeek  ScheduleUnit = "week"
	EveryMonth ScheduleUnit = "month"
	EveryYear  ScheduleUnit = "year"
)

// NewSchedule is the input for defining a schedule: the transaction that repeats
// (Payee, Description and Postings, as for NewTransaction) every Every Units,
// the first time on Start. A monthly or yearly schedule keeps Start's day of the
// month, and falls on the last day of a month that is shorter. End, when set, is
// the last day it may fall on.
//
// With Transfer set the schedule moves money between two of your own accounts:
// Postings are then the amount that leaves one (negative) and the amount that
// arrives in the other (positive, in that account's currency; equal to the first
// when both hold the same one), and Description is the memo.
//
// Tolerance makes the amount an estimate: a bank row whose amount is within that
// many percent of it, on either side and in the same direction, still counts as
// the schedule's occurrence (see ImportRequest). 0 is an exact amount. A transfer
// has no tolerance.
type NewSchedule struct {
	Name        string // unique, whatever the case
	Every       int    // 1 or more: 2 with EveryWeek is every other week
	Unit        ScheduleUnit
	Start       time.Time
	End         time.Time // zero: no end
	Transfer    bool
	Tolerance   int // percent of the amount, 0 to 100
	Payee       string
	Description string
	Postings    []ledger.Posting
}

// Schedule is a stored schedule.
type Schedule struct {
	ID          int64
	Name        string
	Every       int
	Unit        ScheduleUnit
	Start       string // YYYY-MM-DD
	End         string // YYYY-MM-DD, the last day it may fall on; empty: no end
	Transfer    bool   // moves money between two of your accounts, see NewSchedule
	Tolerance   int    // percent the amount may differ and still match a bank row; 0: exact
	Payee       string
	Description string
	Postings    []PostingView
	Pauses      []SchedulePause // oldest first
}

// SchedulePause is a span in which a schedule proposed nothing: from the day it
// was paused up to, not including, the day it was resumed. To is empty while it
// is still paused.
type SchedulePause struct {
	From, To string // YYYY-MM-DD
}

// NewSchedule converts a stored schedule back into input, for editing it or for
// putting it back as it was. Pauses are not part of it: they belong to the stored
// schedule and survive an edit.
func (s Schedule) NewSchedule() (NewSchedule, error) {
	start, err := ledger.ParseDate(s.Start)
	if err != nil {
		return NewSchedule{}, err
	}
	ns := NewSchedule{
		Name: s.Name, Every: s.Every, Unit: s.Unit, Start: start, Transfer: s.Transfer, Tolerance: s.Tolerance,
		Payee: s.Payee, Description: s.Description,
	}
	if s.End != "" {
		if ns.End, err = ledger.ParseDate(s.End); err != nil {
			return NewSchedule{}, err
		}
	}
	for _, p := range s.Postings {
		ns.Postings = append(ns.Postings, ledger.Posting{Account: p.Account, Amount: p.Amount})
	}
	return ns, nil
}

// Paused says whether the schedule is paused now.
func (s Schedule) Paused() bool {
	return len(s.Pauses) > 0 && s.Pauses[len(s.Pauses)-1].To == ""
}

// ScheduleStatus is a schedule and when it is next due.
type ScheduleStatus struct {
	Schedule
	// Next is the earliest date nobody has answered yet: in the past when
	// something is waiting, else the next one to come. It is empty for a paused
	// schedule with nothing waiting, and for one that has run out.
	Next string
	// Overdue counts the dates up to the day asked that nobody has answered.
	Overdue int
}

// Occurrence is one date a schedule falls on.
type Occurrence struct {
	Schedule Schedule
	Date     string // YYYY-MM-DD
}

// CopiedTargets is what CopyBudgetTargets did: the targets it added, and how many
// of the source month's it left alone because the month already had one.
type CopiedTargets struct {
	Copied []BudgetTarget // by category name
	Kept   int
}

// MonthBudgetRequest asks how a month's spending stands against its targets.
type MonthBudgetRequest struct {
	Month string // YYYY-MM
	// RateDate is the latest day exchange rates are looked up on, as for a
	// BudgetRequest; empty = today. Spending, the carried months' too, is converted
	// at the rate of its own day; a target at the rate of the end of its month.
	RateDate string
}

// MonthBudgetLine is one expense category in a month. Everything is in the base
// currency.
type MonthBudgetLine struct {
	Name      string
	Spent     ledger.Money
	HasTarget bool
	Target    ledger.Money // zero without a target
	// Rollover says the category carries over this month. Carried is what came in
	// from the months before: positive if they left money, negative if they
	// overspent. It is zero without a rollover, in the first month of one, and when
	// the months before came out exactly.
	Rollover bool
	Carried  ledger.Money
	// Left is Target plus Carried minus Spent: negative when the category overspent.
	// Zero for a category with neither a target nor anything carried, which Planned
	// tells.
	Left ledger.Money
}

// Planned says the category has money set aside for the month: a target, or
// something carried in. The others spent without a plan.
func (l MonthBudgetLine) Planned() bool { return l.HasTarget || l.Carried.Minor != 0 }

// MonthBudget is the answer to a MonthBudgetRequest. Lines are the expense
// categories that spent something or have a target, the largest spending first.
// Income is not part of it.
type MonthBudget struct {
	Month string
	Base  ledger.Currency
	Lines []MonthBudgetLine
	// SpentTotal is all spending; TargetTotal the sum of the targets, and
	// CarriedTotal the sum of what is carried in.
	SpentTotal, TargetTotal, CarriedTotal ledger.Money
	// LeftTotal is TargetTotal plus CarriedTotal minus what the planned categories
	// spent, so that spending nobody planned for does not hide what is left of what
	// was. That spending is UnbudgetedTotal, and SpentTotal is the two added.
	LeftTotal, UnbudgetedTotal ledger.Money
	// Income is the Income categories that brought something in or have a planned
	// amount, the largest first. IncomeTotal is what came in, PlannedIncomeTotal the
	// sum of the plans. Unassigned is PlannedIncomeTotal less TargetTotal: what is
	// planned to come in and has no target to go to, negative when the targets add up
	// to more than the plan. It is zero, and HasPlannedIncome false, when no income is
	// planned, since there is then nothing to assign.
	Income                          []MonthIncomeLine
	IncomeTotal, PlannedIncomeTotal ledger.Money
	HasPlannedIncome                bool
	Unassigned                      ledger.Money
	// Missing are the currencies with no exchange rate, so left out: spending in
	// them, or a target set in a base currency that has since changed.
	Missing []ledger.Currency
}

// MonthIncomeLine is one Income category in a month, in the base currency. Planned
// is what a target said would come in, and is zero without one (HasPlan false).
type MonthIncomeLine struct {
	Name     string
	Received ledger.Money
	HasPlan  bool
	Planned  ledger.Money
}

// BudgetReport is the answer to a BudgetRequest. Expenses are positive; income
// is positive too (it is flipped from the ledger's credit sign).
type BudgetReport struct {
	Base                      ledger.Currency
	Income, Expenses          []BudgetLine // largest first
	IncomeTotal, ExpenseTotal ledger.Money
	Missing                   []ledger.Currency // no exchange rate, so left out
}
