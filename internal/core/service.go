package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// Local is the Service backed by a SQLite file on this machine. It owns every
// rule; the repository (repository.go) only runs the SQL it is told to.
type Local struct {
	repo *repository
	now  func() time.Time // for "today"; tests replace it
}

var _ Service = (*Local)(nil)

// Open opens (creating if needed) the database at path and applies pending
// migrations. Use ":memory:" for a throwaway database in tests.
func Open(ctx context.Context, path string) (*Local, error) {
	repo, err := openRepository(ctx, path)
	if err != nil {
		return nil, err
	}
	return &Local{repo: repo, now: time.Now}, nil
}

// Close releases the database.
func (s *Local) Close() error { return s.repo.close() }

func (s *Local) today() string { return s.now().Format(ledger.DateLayout) }

// ---- settings and rates ----------------------------------------------------

// BaseCurrency returns the currency that reports are converted into.
func (s *Local) BaseCurrency(ctx context.Context) (ledger.Currency, error) {
	v, err := s.repo.queries().setting(ctx, "base_currency")
	if err != nil {
		return "", err
	}
	return ledger.ParseCurrency(v)
}

// SetBaseCurrency changes the reporting currency. Stored amounts are untouched.
func (s *Local) SetBaseCurrency(ctx context.Context, c ledger.Currency) error {
	return s.repo.queries().putSetting(ctx, "base_currency", string(c))
}

// SetRate records that on date one unit of base is worth rate units of quote.
func (s *Local) SetRate(ctx context.Context, date string, base, quote ledger.Currency, rate string) error {
	if base == quote {
		return coded.New("currencies_must_differ", nil)
	}
	return s.repo.queries().putRate(ctx, date, base, quote, rate)
}

// Rates returns every stored exchange rate, oldest first.
func (s *Local) Rates(ctx context.Context) ([]ledger.RateEntry, error) {
	return s.repo.queries().rates(ctx)
}

func (s *Local) rateBook(ctx context.Context) (*ledger.RateBook, error) {
	entries, err := s.Rates(ctx)
	if err != nil {
		return nil, err
	}
	return ledger.NewRateBook(entries), nil
}

// ---- accounts --------------------------------------------------------------

// AddAccount creates an account. Assets and Liabilities require a currency.
func (s *Local) AddAccount(ctx context.Context, name string, currency ledger.Currency) (Account, error) {
	if err := ledger.ValidateAccountName(name); err != nil {
		return Account{}, err
	}
	typ, _ := ledger.TypeOf(name)
	if typ.NeedsCurrency() && currency == "" {
		return Account{}, coded.New("account_needs_currency", map[string]any{"Type": typ})
	}
	return s.repo.queries().insertAccount(ctx, name, typ, currency)
}

// GetAccount looks an account up by its full name.
func (s *Local) GetAccount(ctx context.Context, name string) (Account, error) {
	return s.repo.queries().account(ctx, name)
}

// ListAccounts returns all accounts ordered by name.
func (s *Local) ListAccounts(ctx context.Context) ([]Account, error) {
	return s.repo.queries().accounts(ctx)
}

// resolveAccount returns the account a posting in currency cur is booked to.
// Income, Expenses and Equity accounts that do not exist yet are created and
// appended to created; Assets and Liabilities must already exist. It fails if
// the account is bound to a different currency.
func resolveAccount(ctx context.Context, q queries, name string, cur ledger.Currency, created *[]string) (Account, error) {
	acc, err := q.account(ctx, name)
	if errors.Is(err, ErrNoAccount) {
		typ, terr := ledger.TypeOf(name)
		if terr != nil {
			return Account{}, terr
		}
		if verr := ledger.ValidateAccountName(name); verr != nil {
			return Account{}, verr
		}
		if typ.NeedsCurrency() {
			return Account{}, coded.New("account_must_exist", map[string]any{"Name": name, "Type": typ, "Currency": cur})
		}
		acc, err = q.insertAccount(ctx, name, typ, "")
		if err == nil {
			*created = append(*created, name)
		}
	}
	if err != nil {
		return Account{}, err
	}
	if acc.Currency != "" && acc.Currency != cur {
		return Account{}, coded.New("posting_wrong_currency", map[string]any{"Account": acc.Name, "Holds": acc.Currency, "Currency": cur})
	}
	return acc, nil
}

// ---- transactions ----------------------------------------------------------

// AddTransaction validates and stores one transaction atomically.
func (s *Local) AddTransaction(ctx context.Context, nt NewTransaction) (AddResult, error) {
	var res AddResult
	err := s.repo.inTx(ctx, func(q queries) error {
		id, err := storeTransaction(ctx, q, nt, 0, &res.Created)
		res.ID = id
		return err
	})
	if err != nil {
		return AddResult{}, err
	}
	return res, nil
}

// validate checks what can be checked without the database.
func (nt NewTransaction) validate() error {
	if nt.Date.IsZero() {
		return coded.New("no_date", nil)
	}
	return ledger.CheckBalanced(nt.Postings)
}

// resolvePostings maps each posting to its account id, creating categories that
// do not exist yet.
func resolvePostings(ctx context.Context, q queries, nt NewTransaction, created *[]string) ([]int64, error) {
	ids := make([]int64, len(nt.Postings))
	for i, p := range nt.Postings {
		acc, err := resolveAccount(ctx, q, p.Account, p.Amount.Currency, created)
		if err != nil {
			return nil, err
		}
		ids[i] = acc.ID
	}
	return ids, nil
}

// storeTransaction stores nt. A non-zero id is used as the row id (to bring a
// deleted transaction back under its old number). A transaction whose import
// hash is already stored fails with ErrDuplicate.
func storeTransaction(ctx context.Context, q queries, nt NewTransaction, id int64, created *[]string) (int64, error) {
	if err := nt.validate(); err != nil {
		return 0, err
	}
	if nt.ImportHash != "" {
		dup, err := q.importHashExists(ctx, nt.ImportHash)
		if err != nil {
			return 0, err
		}
		if dup {
			return 0, coded.New("already_imported", nil)
		}
	}
	accountIDs, err := resolvePostings(ctx, q, nt, created)
	if err != nil {
		return 0, err
	}
	newID, err := q.insertTransaction(ctx, id, nt)
	if err != nil {
		return 0, err
	}
	return newID, q.insertPostings(ctx, newID, nt.Postings, accountIDs)
}

// DeleteTransaction removes a transaction and its postings.
func (s *Local) DeleteTransaction(ctx context.Context, id int64) error {
	found, err := s.repo.queries().deleteTransaction(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return coded.New("transaction_not_found", map[string]any{"ID": id})
	}
	return nil
}

// ReplaceTransaction rewrites the date, payee, description and postings of an
// existing transaction in place. Its id and import hash are kept, so a bank
// row that was corrected here is still recognised as imported. Categories that
// do not exist yet are created, and their names are returned.
func (s *Local) ReplaceTransaction(ctx context.Context, id int64, nt NewTransaction) ([]string, error) {
	var created []string
	err := s.repo.inTx(ctx, func(q queries) error {
		return replaceTransaction(ctx, q, id, nt, &created)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func replaceTransaction(ctx context.Context, q queries, id int64, nt NewTransaction, created *[]string) error {
	found, err := q.transactionExists(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return coded.New("transaction_not_found", map[string]any{"ID": id})
	}
	if err := nt.validate(); err != nil {
		return err
	}
	accountIDs, err := resolvePostings(ctx, q, nt, created)
	if err != nil {
		return err
	}
	if err := q.updateTransaction(ctx, id, nt); err != nil {
		return err
	}
	if err := q.deletePostings(ctx, id); err != nil {
		return err
	}
	if err := q.insertPostings(ctx, id, nt.Postings, accountIDs); err != nil {
		return err
	}
	return reconcileTransferLink(ctx, q, id, created)
}

// RestoreTransaction stores nt under the given id, which must be free. It
// brings back a transaction deleted by mistake, with its old number.
func (s *Local) RestoreTransaction(ctx context.Context, id int64, nt NewTransaction) error {
	return s.repo.inTx(ctx, func(q queries) error {
		var created []string
		_, err := storeTransaction(ctx, q, nt, id, &created)
		return err
	})
}

// GetTransaction returns one transaction with its postings.
func (s *Local) GetTransaction(ctx context.Context, id int64) (TxView, error) {
	txs, err := s.repo.queries().listTransactions(ctx, TxFilter{ID: id})
	if err != nil {
		return TxView{}, err
	}
	if len(txs) == 0 {
		return TxView{}, coded.New("transaction_not_found", map[string]any{"ID": id})
	}
	return txs[0], nil
}

// ListTransactions returns the matching transactions ordered oldest to newest,
// so that the most recent one is printed last. With a Limit, the newest Limit
// transactions are selected first and then ordered.
func (s *Local) ListTransactions(ctx context.Context, f TxFilter) ([]TxView, error) {
	return s.repo.queries().listTransactions(ctx, f)
}

// SetCategories re-books the category leg of each listed transaction in one
// database transaction: either every change is applied or none is. A
// transaction's category leg is its only posting to an Income, Expenses or
// Equity account; transactions without exactly one such posting (transfers
// between your own accounts, say) are rejected. Categories that do not exist
// yet are created, and their names are returned.
func (s *Local) SetCategories(ctx context.Context, changes []CategoryChange) ([]string, error) {
	var created []string
	err := s.repo.inTx(ctx, func(q queries) error {
		for _, c := range changes {
			if err := setCategory(ctx, q, c, &created); err != nil {
				return fmt.Errorf("%w: %w", coded.New("transaction_n", map[string]any{"ID": c.TransactionID}), err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func setCategory(ctx context.Context, q queries, c CategoryChange, created *[]string) error {
	if typ, err := ledger.TypeOf(c.Category); err != nil {
		return err
	} else if typ.NeedsCurrency() {
		return coded.New("not_a_category", map[string]any{"Name": c.Category})
	}
	postings, err := q.categoryPostings(ctx, c.TransactionID)
	if err != nil {
		return err
	}
	if len(postings) != 1 {
		return coded.New("category_postings", map[string]any{"Count": len(postings)})
	}
	acc, err := resolveAccount(ctx, q, c.Category, postings[0].currency, created)
	if err != nil {
		return err
	}
	if err := q.moveStoredPosting(ctx, postings[0].id, acc.ID); err != nil {
		return err
	}
	return reconcileTransferLink(ctx, q, c.TransactionID, created)
}

// uncategorizedFor is the Uncategorized account for money that moved by m on
// your own account: income when it came in, an expense when it went out.
func uncategorizedFor(m ledger.Money) string {
	if m.Minor > 0 {
		return ledger.UncategorizedIncome
	}
	return ledger.UncategorizedExpense
}

// reconcileTransferLink keeps a transfer link honest after id was edited. A link
// means both halves are booked to Equity:Transfers on different own accounts
// and their amounts cancel. If the edit broke that, the link is removed, and a
// partner still sitting in Equity:Transfers goes back to Uncategorized so that
// it is noticed instead of being left to wait for a half that is not coming.
func reconcileTransferLink(ctx context.Context, q queries, id int64, created *[]string) error {
	partnerID, err := q.transferPartner(ctx, id)
	if err != nil || partnerID == 0 {
		return err
	}
	mine, err := q.listTransactions(ctx, TxFilter{ID: id})
	if err != nil {
		return err
	}
	theirs, err := q.listTransactions(ctx, TxFilter{ID: partnerID})
	if err != nil {
		return err
	}
	if len(mine) == 1 && len(theirs) == 1 && transferHalves(mine[0], theirs[0]) {
		return nil
	}
	if err := q.unlinkTransfer(ctx, id, partnerID); err != nil {
		return err
	}
	if len(theirs) == 1 {
		if source, category, ok := theirs[0].Legs(); ok && isClearingAccount(category.Account) {
			return setCategory(ctx, q, CategoryChange{TransactionID: partnerID, Category: uncategorizedFor(source.Amount)}, created)
		}
	}
	return nil
}

// transferHalves reports whether a and b still make one transfer: on different
// own accounts, booked to the same clearing account, and either the same amount
// the other way round (Equity:Transfers) or an amount out in one currency and in
// in another (Equity:Conversions).
func transferHalves(a, b TxView) bool {
	sa, ca, oka := a.Legs()
	sb, cb, okb := b.Legs()
	if !oka || !okb || sa.Account == sb.Account || ca.Account != cb.Account {
		return false
	}
	switch ca.Account {
	case ledger.TransferAccount:
		return sa.Amount.Currency == sb.Amount.Currency && sa.Amount.Minor == -sb.Amount.Minor
	case ledger.ConversionAccount:
		return sa.Amount.Currency != sb.Amount.Currency && sa.Amount.Minor != 0 && sb.Amount.Minor != 0 &&
			(sa.Amount.Minor > 0) != (sb.Amount.Minor > 0)
	}
	return false
}

// isClearingAccount reports whether a category is where a transfer half sits.
func isClearingAccount(name string) bool {
	return name == ledger.TransferAccount || name == ledger.ConversionAccount
}

// Transfer moves money between two accounts that hold their own currency. A
// cross-currency transfer is booked through Equity:Conversions so that every
// currency balances on its own.
func (s *Local) Transfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	var res TransferResult
	err := s.repo.inTx(ctx, func(q queries) error {
		plan, err := planTransfer(ctx, q, req)
		if err != nil {
			return err
		}
		res, err = plan.store(ctx, q)
		return err
	})
	if err != nil {
		return TransferResult{}, err
	}
	return res, nil
}

// transferPlan is a transfer that has been checked and not yet stored: its two
// halves, each an ordinary transaction on its own account.
type transferPlan struct {
	out, in NewTransaction
	res     TransferResult // what is known before storing: In and CrossCurrency
}

// planTransfer checks req against the accounts and builds the two halves.
func planTransfer(ctx context.Context, q queries, req TransferRequest) (transferPlan, error) {
	src, err := q.account(ctx, req.From)
	if err != nil {
		return transferPlan{}, err
	}
	dst, err := q.account(ctx, req.To)
	if err != nil {
		return transferPlan{}, err
	}
	if src.Currency == "" || dst.Currency == "" {
		return transferPlan{}, coded.New("transfer_fixed_currency", nil)
	}
	if src.ID == dst.ID {
		return transferPlan{}, coded.New("transfer_same_account", nil)
	}
	if req.Amount.Currency != src.Currency {
		return transferPlan{}, coded.New("amount_wrong_currency", map[string]any{"Currency": string(req.Amount.Currency), "Account": src.Name, "Holds": string(src.Currency)})
	}
	if req.Amount.Minor <= 0 {
		return transferPlan{}, coded.New("amount_not_positive", nil)
	}

	// Each side of the transfer is its own transaction on its own account,
	// booked against a clearing account and linked to the other, which is the
	// shape an import of the two bank statements produces. Within one currency
	// the clearing account is Equity:Transfers; across currencies it is
	// Equity:Conversions, which then holds both currencies.
	clearing, received := ledger.TransferAccount, req.Amount
	res := TransferResult{In: req.Amount}
	if src.Currency == dst.Currency {
		if req.ToAmount != (ledger.Money{}) {
			return transferPlan{}, coded.New("transfer_same_currency", map[string]any{"Currency": src.Currency})
		}
	} else {
		if req.ToAmount == (ledger.Money{}) {
			return transferPlan{}, coded.New("transfer_needs_received", map[string]any{"From": src.Currency, "To": dst.Currency, "Currency": dst.Currency})
		}
		if req.ToAmount.Currency != dst.Currency {
			return transferPlan{}, coded.New("received_wrong_currency", map[string]any{"Currency": string(req.ToAmount.Currency), "Account": dst.Name, "Holds": string(dst.Currency)})
		}
		if req.ToAmount.Minor <= 0 {
			return transferPlan{}, coded.New("received_not_positive", nil)
		}
		clearing, received = ledger.ConversionAccount, req.ToAmount
		res.In, res.CrossCurrency = req.ToAmount, true
	}
	return transferPlan{
		res: res,
		out: NewTransaction{
			Date: req.Date, Payee: "Transfer to " + shortAccountName(dst.Name), Description: req.Memo,
			Postings: []ledger.Posting{{Account: src.Name, Amount: req.Amount.Neg()}, {Account: clearing, Amount: req.Amount}},
		},
		in: NewTransaction{
			Date: req.Date, Payee: "Transfer from " + shortAccountName(src.Name), Description: req.Memo,
			Postings: []ledger.Posting{{Account: dst.Name, Amount: received}, {Account: clearing, Amount: received.Neg()}},
		},
	}, nil
}

// store writes both halves and links them, inside the caller's transaction.
func (p transferPlan) store(ctx context.Context, q queries) (TransferResult, error) {
	res := p.res
	var err error
	if res.ID, err = storeTransaction(ctx, q, p.out, 0, &res.Created); err != nil {
		return TransferResult{}, err
	}
	if res.InID, err = storeTransaction(ctx, q, p.in, 0, &res.Created); err != nil {
		return TransferResult{}, err
	}
	return res, q.linkTransfer(ctx, res.ID, res.InID)
}

// shortAccountName drops the type segment: "Assets:Savings" is "Savings".
func shortAccountName(name string) string {
	if _, rest, ok := strings.Cut(name, ":"); ok {
		return rest
	}
	return name
}

// ---- import and recategorisation -------------------------------------------

// Import books bank rows into one account. Each row becomes a transaction
// between the account and the category the first matching rule names; rows no
// rule matches go to Expenses:Uncategorized or Income:Uncategorized. Rows whose
// currency differs from the account's are rejected.
func (s *Local) Import(ctx context.Context, req ImportRequest) (ImportResult, error) {
	q := s.repo.queries()
	acc, err := q.account(ctx, req.Account)
	if err != nil {
		return ImportResult{}, err
	}
	if acc.Currency == "" {
		return ImportResult{}, coded.New("import_no_fixed_currency", map[string]any{"Name": acc.Name})
	}
	rules, err := importer.NewRuleset(req.Rules)
	if err != nil {
		return ImportResult{}, fmt.Errorf("%w: %w", coded.New("invalid_rules", nil), err)
	}

	var res ImportResult
	var rows []importer.Row
	for _, r := range req.Rows {
		if r.Amount.Currency != acc.Currency {
			res.Rejected = append(res.Rejected, RejectedRow{
				Line: r.Line,
				Err:  coded.New("row_currency_mismatch", map[string]any{"Currency": r.Amount.Currency, "Account": acc.Name, "Holds": acc.Currency}),
			})
			continue
		}
		rows = append(rows, r)
	}
	if len(res.Rejected) > 0 && !req.IgnoreRejected {
		return res, coded.New("rows_rejected", map[string]any{"Count": len(res.Rejected)})
	}

	dedupe := importer.NewDeduper(strconv.FormatInt(acc.ID, 10))
	txs := make([]NewTransaction, len(rows))
	ruled := make([]bool, len(rows)) // a rule chose the category
	for i, r := range rows {
		category, ok := rules.Match(r.Payee, r.Purpose, r.BookingText)
		ruled[i] = ok
		if !ok {
			category = ledger.UncategorizedExpense
			if r.Amount.Minor > 0 {
				category = ledger.UncategorizedIncome
			}
		}
		description := r.Purpose
		if description == "" {
			description = r.BookingText
		}
		txs[i] = NewTransaction{
			Date:        r.Date,
			Payee:       r.Payee,
			Description: description,
			ImportHash:  dedupe.Key(r),
			Postings: []ledger.Posting{
				{Account: acc.Name, Amount: r.Amount},
				{Account: category, Amount: r.Amount.Neg()},
			},
		}
	}

	if req.DryRun {
		plan, err := planImport(ctx, q, acc, rows, txs, ruled)
		if err != nil {
			return ImportResult{}, err
		}
		for i, p := range plan {
			switch {
			case p.duplicate:
				res.Duplicates++
			case p.adopts != nil:
				res.Adopted++
			default:
				res.Inserted++
				if p.partner != nil {
					res.Transfers++
				}
			}
			if p.uncategorized(ruled[i]) {
				res.Uncategorized++
			}
			row := ImportedRow{Row: rows[i], Category: p.tx.Postings[1].Account, Duplicate: p.duplicate}
			if p.settles != nil {
				settled := p.settles.settled(0)
				row.Settles = &settled
				res.Settled = append(res.Settled, settled)
			}
			if p.partner != nil {
				row.Transfer = &TransferMatch{ID: p.partner.id, Account: p.partner.account, Date: p.partner.date, Was: p.partner.category}
			}
			if p.adopts != nil {
				row.Category = p.adopts.category
				row.Adopts = &AdoptedEntry{ID: p.adopts.id, Date: p.adopts.date, Payee: p.adopts.payee, Category: p.adopts.category}
			}
			res.Rows = append(res.Rows, row)
		}
		return res, nil
	}

	err = s.repo.inTx(ctx, func(q queries) error {
		// Planned inside the transaction, so the pairs it finds are still free
		// when the rows are written.
		plan, err := planImport(ctx, q, acc, rows, txs, ruled)
		if err != nil {
			return err
		}
		for i, p := range plan {
			if p.uncategorized(ruled[i]) {
				res.Uncategorized++
			}
			if p.duplicate {
				res.Duplicates++
				continue
			}
			if p.adopts != nil {
				if err := q.setImportHash(ctx, p.adopts.id, p.tx.ImportHash); err != nil {
					return fmt.Errorf("%w: %w", coded.New("import_row_error", map[string]any{"N": i + 1, "Date": p.tx.Date.Format(ledger.DateLayout), "Payee": p.tx.Payee}), err)
				}
				res.Adopted++
				res.AdoptedIDs = append(res.AdoptedIDs, p.adopts.id)
				continue
			}
			id, err := storeTransaction(ctx, q, p.tx, 0, &res.Created)
			if errors.Is(err, ErrDuplicate) {
				res.Duplicates++
				continue
			}
			if err != nil {
				return fmt.Errorf("%w: %w", coded.New("import_row_error", map[string]any{"N": i + 1, "Date": p.tx.Date.Format(ledger.DateLayout), "Payee": p.tx.Payee}), err)
			}
			res.Inserted++
			res.IDs = append(res.IDs, id)
			if p.settles != nil {
				if err := q.putOccurrence(ctx, handled{p.settles.scheduleID, p.settles.date}, id); err != nil {
					return fmt.Errorf("%w: %w", coded.New("import_row_error", map[string]any{"N": i + 1, "Date": p.tx.Date.Format(ledger.DateLayout), "Payee": p.tx.Payee}), err)
				}
				res.Settled = append(res.Settled, p.settles.settled(id))
			}
			if p.partner == nil {
				continue
			}
			if p.partner.category != ledger.TransferAccount {
				change := CategoryChange{TransactionID: p.partner.id, Category: ledger.TransferAccount}
				if err := setCategory(ctx, q, change, &res.Created); err != nil {
					return fmt.Errorf("%w: %w", coded.New("transaction_n", map[string]any{"ID": p.partner.id}), err)
				}
			}
			if err := q.linkTransfer(ctx, id, p.partner.id); err != nil {
				return err
			}
			res.Transfers++
			res.Linked = append(res.Linked, TransferLink{ID: id, OtherID: p.partner.id, OtherWas: p.partner.category})
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return res, nil
}

// matchWindowDays is how many days apart two records of the same money may be
// dated: the two halves of a transfer, or a hand-entered transaction and the
// bank's booking of it, which for a card payment can come days later.
const matchWindowDays = 3

// plannedRow is what an import would do with one row.
type plannedRow struct {
	tx        NewTransaction
	duplicate bool
	adopts    *adoptionCandidate // the hand-entered transaction this row is the bank's record of, if any
	settles   *occurrenceMatch   // the occurrence of a schedule this row is the bank's booking of, if any
	partner   *transferCandidate // the stored half this row completes, if any
}

// uncategorized says whether the row ends up in an Uncategorized account for want
// of anyone choosing a category: no rule, no transfer, no hand-entered entry, and
// no schedule that names one.
func (p plannedRow) uncategorized(ruled bool) bool {
	if ruled || p.partner != nil || p.adopts != nil {
		return false
	}
	return p.settles == nil || p.settles.category == ledger.UncategorizedExpense || p.settles.category == ledger.UncategorizedIncome
}

// planImport decides, for each parsed row, whether it is already stored, whether
// it is the bank's record of something you entered by hand (it then adopts that
// entry, whatever category the rules chose), and failing that whether it is the
// other half of a transfer between two of your own accounts:
// a stored transaction on another account that moved exactly the opposite
// amount within a few days and has not been paired or categorized yet. A row a
// rule categorized stays where the rule put it, unless that is Equity:Transfers.
// Every stored transaction is paired at most once.
func planImport(ctx context.Context, q queries, acc Account, rows []importer.Row, txs []NewTransaction, ruled []bool) ([]plannedRow, error) {
	plan := make([]plannedRow, len(rows))
	taken := map[int64]bool{}
	adopted := map[int64]bool{}
	occurrences, err := newOccurrenceMatcher(ctx, q)
	if err != nil {
		return nil, err
	}
	for i, tx := range txs {
		p := plannedRow{tx: tx}
		dup, err := q.importHashExists(ctx, tx.ImportHash)
		if err != nil {
			return nil, err
		}
		p.duplicate = dup
		if !dup && rows[i].Amount.Minor != 0 {
			entries, err := q.adoptionCandidates(ctx, acc.ID, rows[i].Amount, rows[i].Date, matchWindowDays)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !adopted[e.id] {
					e := e
					p.adopts = &e
					adopted[e.id] = true
					break
				}
			}
		}
		if !dup && p.adopts == nil && rows[i].Amount.Minor != 0 {
			// A row a rule marked as a transfer can only be a scheduled transfer.
			p.settles = occurrences.find(acc, rows[i].Amount, rows[i].Date, tx.Postings[1].Account == ledger.TransferAccount)
			if p.settles != nil && !ruled[i] {
				p.tx.Postings = []ledger.Posting{tx.Postings[0], {Account: p.settles.category, Amount: tx.Postings[0].Amount.Neg()}}
			}
		}
		wanted := !ruled[i] || tx.Postings[1].Account == ledger.TransferAccount
		if !dup && p.adopts == nil && (p.settles == nil || p.settles.transfer) && wanted && rows[i].Amount.Minor != 0 {
			candidates, err := q.transferCandidates(ctx, acc.ID, rows[i].Amount, rows[i].Date, matchWindowDays)
			if err != nil {
				return nil, err
			}
			for _, c := range candidates {
				if !taken[c.id] {
					c := c
					p.partner = &c
					taken[c.id] = true
					p.tx.Postings = []ledger.Posting{tx.Postings[0], {Account: ledger.TransferAccount, Amount: tx.Postings[0].Amount.Neg()}}
					break
				}
			}
		}
		plan[i] = p
	}
	return plan, nil
}

// DetachImport clears the import hash of each transaction, so that the bank
// row it stood for is no longer recognised as stored and the transaction is a
// hand-entered one again.
func (s *Local) DetachImport(ctx context.Context, ids []int64) error {
	return s.repo.inTx(ctx, func(q queries) error {
		for _, id := range ids {
			found, err := q.transactionExists(ctx, id)
			if err != nil {
				return err
			}
			if !found {
				return coded.New("transaction_not_found", map[string]any{"ID": id})
			}
			if err := q.clearImportHash(ctx, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Recategorize runs rules over every transaction that sits in
// Expenses:Uncategorized or Income:Uncategorized and moves the ones that match.
// Transactions that were categorised by hand are never touched. Rules see the
// payee and the stored description, so a rule that only matched the booking
// text of a row with a purpose at import time may not match here.
func (s *Local) Recategorize(ctx context.Context, req RecategorizeRequest) (RecategorizeResult, error) {
	rules, err := importer.NewRuleset(req.Rules)
	if err != nil {
		return RecategorizeResult{}, fmt.Errorf("%w: %w", coded.New("invalid_rules", nil), err)
	}
	var (
		res     RecategorizeResult
		changes []CategoryChange
	)
	for _, uncategorized := range []string{ledger.UncategorizedExpense, ledger.UncategorizedIncome} {
		txs, err := s.repo.queries().listTransactions(ctx, TxFilter{Account: uncategorized})
		if err != nil {
			return RecategorizeResult{}, err
		}
		for _, tx := range txs {
			source, current, ok := tx.Legs()
			if !ok || current.Account != uncategorized {
				continue
			}
			res.Uncategorized++
			category, ok := rules.Match(tx.Payee, tx.Description)
			if !ok || category == uncategorized {
				continue
			}
			changes = append(changes, CategoryChange{TransactionID: tx.ID, Category: category})
			res.Moves = append(res.Moves, Move{
				TransactionID: tx.ID, Date: tx.Date, Payee: tx.Payee, Category: category, Amount: source.Amount,
			})
		}
	}
	if req.DryRun || len(changes) == 0 {
		return res, nil
	}
	if res.Created, err = s.SetCategories(ctx, changes); err != nil {
		return RecategorizeResult{}, err
	}
	return res, nil
}

// ---- reports ---------------------------------------------------------------

// BalanceReport returns the balance of every asset and liability account in its
// own currency (and, on request, of the other accounts), plus the net worth
// converted into the base currency. Conversion uses the most recent exchange
// rate on or before the report date; currencies without a usable rate are
// listed in NetWorth.Missing and left out of the total.
func (s *Local) BalanceReport(ctx context.Context, req BalanceRequest) (BalanceReport, error) {
	date, asOf := s.today(), ""
	if req.AsOf != "" {
		d, err := ledger.ParseDate(req.AsOf)
		if err != nil {
			return BalanceReport{}, err
		}
		date = d.Format(ledger.DateLayout)
		asOf = date
	}
	base, err := s.BaseCurrency(ctx)
	if err != nil {
		return BalanceReport{}, err
	}
	book, err := s.rateBook(ctx)
	if err != nil {
		return BalanceReport{}, err
	}
	balances, err := s.repo.queries().balances(ctx, asOf)
	if err != nil {
		return BalanceReport{}, err
	}

	report := BalanceReport{Date: date, NetWorth: NetWorth{Base: base, Total: ledger.Money{Currency: base}}}
	missing := make(map[ledger.Currency]bool)
	for _, b := range balances {
		countsTowardNetWorth := b.Type.NeedsCurrency()
		if !countsTowardNetWorth && !req.IncludeAll {
			continue
		}
		report.Lines = append(report.Lines, b)
		if !countsTowardNetWorth {
			continue
		}
		converted, ok := book.Convert(b.Amount, base, date)
		if !ok {
			missing[b.Amount.Currency] = true
			continue
		}
		if report.NetWorth.Total, err = report.NetWorth.Total.Add(converted); err != nil {
			return BalanceReport{}, err
		}
	}
	report.NetWorth.Missing = sortedCurrencies(missing)
	return report, nil
}

// BudgetReport totals income and spending per category over a period,
// converted into the base currency.
// monthLayout is how a month is written: 2026-10.
const monthLayout = "2006-01"

// checkMonth is month once it is known to be a month, or the error that says it is not.
func checkMonth(month string) error {
	if _, err := time.Parse(monthLayout, month); err != nil {
		return coded.New("invalid_month", map[string]any{"Text": month})
	}
	return nil
}

// expenseCategory is the account named, once it is known to exist and to be a category
// that can have a target and carry over: an Expenses account other than Uncategorized.
func (s *Local) expenseCategory(ctx context.Context, category string) (Account, error) {
	acc, err := s.repo.queries().account(ctx, category)
	if err != nil {
		return Account{}, err
	}
	if acc.Type != ledger.Expenses || acc.Name == ledger.UncategorizedExpense {
		return Account{}, coded.New("target_not_expense", map[string]any{"Name": acc.Name})
	}
	return acc, nil
}

// targetCategory is the category that month's target is about, once the month is known
// to be a month and the category to be one a target can be set on: an Expenses category
// (what it may spend) or an Income one (what is planned to come in), other than the
// Uncategorized two.
func (s *Local) targetCategory(ctx context.Context, month, category string) (Account, error) {
	if err := checkMonth(month); err != nil {
		return Account{}, err
	}
	acc, err := s.repo.queries().account(ctx, category)
	if err != nil {
		return Account{}, err
	}
	planned := acc.Type == ledger.Expenses || acc.Type == ledger.Income
	if !planned || acc.Name == ledger.UncategorizedExpense || acc.Name == ledger.UncategorizedIncome {
		return Account{}, coded.New("target_not_category", map[string]any{"Name": acc.Name})
	}
	return acc, nil
}

// SetBudgetRollover makes category carry its leftover from month `from` on.
func (s *Local) SetBudgetRollover(ctx context.Context, category, from string) error {
	if err := checkMonth(from); err != nil {
		return err
	}
	acc, err := s.expenseCategory(ctx, category)
	if err != nil {
		return err
	}
	return s.repo.queries().putBudgetRollover(ctx, acc.ID, from)
}

// ClearBudgetRollover stops category carrying over. One that does not is not an error:
// the result is what was asked for.
func (s *Local) ClearBudgetRollover(ctx context.Context, category string) error {
	acc, err := s.expenseCategory(ctx, category)
	if err != nil {
		return err
	}
	return s.repo.queries().deleteBudgetRollover(ctx, acc.ID)
}

// BudgetRollovers lists the categories that carry over.
func (s *Local) BudgetRollovers(ctx context.Context) ([]BudgetRollover, error) {
	return s.repo.queries().budgetRollovers(ctx)
}

// CopyBudgetTargets adds to month `to` the targets of month `from` that `to` lacks. A
// category that has a target in `to` keeps it, so copying twice does nothing the
// second time, and a month planned by hand is never overwritten. The source is not
// touched. It all happens or none of it does.
func (s *Local) CopyBudgetTargets(ctx context.Context, from, to string) (CopiedTargets, error) {
	for _, month := range []string{from, to} {
		if _, err := time.Parse(monthLayout, month); err != nil {
			return CopiedTargets{}, coded.New("invalid_month", map[string]any{"Text": month})
		}
	}
	if from == to {
		return CopiedTargets{}, coded.New("copy_same_month", map[string]any{"Month": to})
	}
	var out CopiedTargets
	err := s.repo.inTx(ctx, func(q queries) error {
		missing, kept, err := q.targetsToCopy(ctx, from, to)
		if err != nil {
			return err
		}
		for _, c := range missing {
			if err := q.putBudgetTarget(ctx, c.accountID, to, c.target.Amount); err != nil {
				return err
			}
			out.Copied = append(out.Copied, c.target)
		}
		out.Kept = kept
		return nil
	})
	return out, err
}

// SetBudgetTarget sets what category may spend in month (or, for an Income category,
// what is planned to come in), replacing a target already there.
func (s *Local) SetBudgetTarget(ctx context.Context, month, category string, amount ledger.Money) error {
	acc, err := s.targetCategory(ctx, month, category)
	if err != nil {
		return err
	}
	base, err := s.BaseCurrency(ctx)
	if err != nil {
		return err
	}
	switch {
	case amount.Minor <= 0:
		return coded.New("amount_not_positive", nil)
	case amount.Currency != base:
		return coded.New("target_not_base_currency", map[string]any{"Currency": amount.Currency, "Base": base})
	}
	return s.repo.queries().putBudgetTarget(ctx, acc.ID, month, amount)
}

// ClearBudgetTarget removes the target of category for month. A category that has none
// is not an error: the result is what was asked for.
func (s *Local) ClearBudgetTarget(ctx context.Context, month, category string) error {
	acc, err := s.targetCategory(ctx, month, category)
	if err != nil {
		return err
	}
	return s.repo.queries().deleteBudgetTarget(ctx, acc.ID, month)
}

// BudgetTargets lists the targets set for month.
func (s *Local) BudgetTargets(ctx context.Context, month string) ([]BudgetTarget, error) {
	if _, err := time.Parse(monthLayout, month); err != nil {
		return nil, coded.New("invalid_month", map[string]any{"Text": month})
	}
	return s.repo.queries().budgetTargets(ctx, month)
}

func (s *Local) BudgetReport(ctx context.Context, req BudgetRequest) (BudgetReport, error) {
	totals, err := s.repo.queries().categoryTotals(ctx, req.From, req.To)
	if err != nil {
		return BudgetReport{}, err
	}
	base, err := s.BaseCurrency(ctx)
	if err != nil {
		return BudgetReport{}, err
	}
	book, err := s.rateBook(ctx)
	if err != nil {
		return BudgetReport{}, err
	}

	b := BudgetReport{
		Base:         base,
		IncomeTotal:  ledger.Money{Currency: base},
		ExpenseTotal: ledger.Money{Currency: base},
	}
	byName := make(map[string]*BudgetLine)
	missing := make(map[ledger.Currency]bool)
	for _, t := range totals {
		amount, ok := s.convertOn(book, t.Amount, base, req.RateDate, t.date, req.To)
		if !ok {
			missing[t.Amount.Currency] = true
			continue
		}
		if t.Type == ledger.Income {
			amount = amount.Neg() // income is stored as credits
		}
		line := byName[t.Account]
		if line == nil {
			line = &BudgetLine{Name: t.Account, Amount: ledger.Money{Currency: base}}
			byName[t.Account] = line
		}
		line.Amount.Minor += amount.Minor
		if t.Type == ledger.Income {
			b.IncomeTotal.Minor += amount.Minor
		} else {
			b.ExpenseTotal.Minor += amount.Minor
		}
	}
	for _, line := range byName {
		if typ, _ := ledger.TypeOf(line.Name); typ == ledger.Income {
			b.Income = append(b.Income, *line)
		} else {
			b.Expenses = append(b.Expenses, *line)
		}
	}
	for _, list := range [][]BudgetLine{b.Income, b.Expenses} {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Amount.Minor != list[j].Amount.Minor {
				return list[i].Amount.Minor > list[j].Amount.Minor
			}
			return list[i].Name < list[j].Name
		})
	}
	b.Missing = sortedCurrencies(missing)
	return b, nil
}

// convertOn converts an amount that was booked on day at the rate of that day, so that
// what a transaction counts for does not change when a later rate is entered. A day
// before the first rate of its currency falls back to the rate of fallback (the end
// of the period), which is how a rate entered today still counts the days before it.
// asked caps both, as in rateDate.
func (s *Local) convertOn(book *ledger.RateBook, m ledger.Money, base ledger.Currency, asked, day, fallback string) (ledger.Money, bool) {
	if out, ok := book.Convert(m, base, s.rateDate(asked, day)); ok {
		return out, true
	}
	return book.Convert(m, base, s.rateDate(asked, fallback))
}

// rateDate is the day rates are read on: the one asked for (today when empty), but
// no later than the end of the period, so that a past period uses its own rates.
func (s *Local) rateDate(asked, periodEnd string) string {
	if asked == "" {
		asked = s.today()
	}
	if periodEnd != "" && periodEnd < asked {
		return periodEnd
	}
	return asked
}

// MonthBudget sets what each expense category spent in a month against its target.
func (s *Local) MonthBudget(ctx context.Context, req MonthBudgetRequest) (MonthBudget, error) {
	first, err := time.Parse(monthLayout, req.Month)
	if err != nil {
		return MonthBudget{}, coded.New("invalid_month", map[string]any{"Text": req.Month})
	}
	last := first.AddDate(0, 1, -1).Format(ledger.DateLayout)
	spending, err := s.BudgetReport(ctx, BudgetRequest{From: first.Format(ledger.DateLayout), To: last, RateDate: req.RateDate})
	if err != nil {
		return MonthBudget{}, err
	}
	targets, err := s.repo.queries().budgetTargets(ctx, req.Month)
	if err != nil {
		return MonthBudget{}, err
	}
	book, err := s.rateBook(ctx)
	if err != nil {
		return MonthBudget{}, err
	}
	rateDate := s.rateDate(req.RateDate, last)

	base := spending.Base
	zero := ledger.Money{Currency: base}
	out := MonthBudget{Month: req.Month, Base: base, SpentTotal: spending.ExpenseTotal,
		TargetTotal: zero, CarriedTotal: zero, LeftTotal: zero, UnbudgetedTotal: zero,
		IncomeTotal: spending.IncomeTotal, PlannedIncomeTotal: zero, Unassigned: zero}
	missing := make(map[ledger.Currency]bool)
	for _, c := range spending.Missing {
		missing[c] = true
	}
	byName := make(map[string]*MonthBudgetLine)
	lineOf := func(name string) *MonthBudgetLine {
		if byName[name] == nil {
			byName[name] = &MonthBudgetLine{Name: name, Spent: zero, Target: zero, Carried: zero, Left: zero}
		}
		return byName[name]
	}
	for _, e := range spending.Expenses {
		lineOf(e.Name).Spent = e.Amount
	}
	incomeByName := make(map[string]*MonthIncomeLine)
	incomeOf := func(name string) *MonthIncomeLine {
		if incomeByName[name] == nil {
			incomeByName[name] = &MonthIncomeLine{Name: name, Received: zero, Planned: zero}
		}
		return incomeByName[name]
	}
	for _, i := range spending.Income {
		incomeOf(i.Name).Received = i.Amount
	}
	for _, t := range targets {
		// A target keeps the currency it was set in; the base currency may have changed since.
		amount, ok := book.Convert(t.Amount, base, rateDate)
		if !ok {
			missing[t.Amount.Currency] = true
			continue
		}
		if typ, _ := ledger.TypeOf(t.Category); typ == ledger.Income {
			line := incomeOf(t.Category)
			line.HasPlan, line.Planned = true, amount
			continue
		}
		line := lineOf(t.Category)
		line.HasTarget, line.Target = true, amount
	}

	rollovers, err := s.repo.queries().budgetRollovers(ctx)
	if err != nil {
		return MonthBudget{}, err
	}
	carried, err := s.carriedIn(ctx, req, book, base, missing)
	if err != nil {
		return MonthBudget{}, err
	}
	for name, amount := range carried {
		if amount.Minor != 0 {
			lineOf(name).Carried = amount
		}
	}
	for _, r := range rollovers {
		if line := byName[r.Category]; line != nil && r.From <= req.Month {
			line.Rollover = true
		}
	}

	for _, line := range byName {
		out.TargetTotal.Minor += line.Target.Minor
		out.CarriedTotal.Minor += line.Carried.Minor
		if line.Planned() {
			line.Left = ledger.Money{Minor: line.Target.Minor + line.Carried.Minor - line.Spent.Minor, Currency: base}
			out.LeftTotal.Minor += line.Left.Minor
		} else {
			out.UnbudgetedTotal.Minor += line.Spent.Minor
		}
		out.Lines = append(out.Lines, *line)
	}
	sort.Slice(out.Lines, func(i, j int) bool {
		if out.Lines[i].Spent.Minor != out.Lines[j].Spent.Minor {
			return out.Lines[i].Spent.Minor > out.Lines[j].Spent.Minor
		}
		return out.Lines[i].Name < out.Lines[j].Name
	})

	for _, line := range incomeByName {
		if line.HasPlan {
			out.PlannedIncomeTotal.Minor += line.Planned.Minor
			out.HasPlannedIncome = true
		}
		out.Income = append(out.Income, *line)
	}
	sort.Slice(out.Income, func(i, j int) bool {
		if out.Income[i].Received.Minor != out.Income[j].Received.Minor {
			return out.Income[i].Received.Minor > out.Income[j].Received.Minor
		}
		return out.Income[i].Name < out.Income[j].Name
	})
	if out.HasPlannedIncome {
		out.Unassigned.Minor = out.PlannedIncomeTotal.Minor - out.TargetTotal.Minor
	}
	out.Missing = sortedCurrencies(missing)
	return out, nil
}

// carriedIn is what each category that carries over brings into the month: the targets
// of the months from where it started up to the one before, less what it spent in them,
// each month at the rates of that month. A currency with no rate is added to missing.
func (s *Local) carriedIn(ctx context.Context, req MonthBudgetRequest, book *ledger.RateBook, base ledger.Currency, missing map[ledger.Currency]bool) (map[string]ledger.Money, error) {
	q := s.repo.queries()
	spent, err := q.carriedSpending(ctx, req.Month)
	if err != nil {
		return nil, err
	}
	targeted, err := q.carriedTargets(ctx, req.Month)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ledger.Money)
	// A target is converted at the rates of the end of its month, spending at those of
	// the day it was made (the end of its month when its currency has no rate yet).
	add := func(rows []monthlyAmount, sign int64, daily bool) {
		for _, r := range rows {
			month := r.month
			if daily && len(month) >= len(monthLayout) {
				month = month[:len(monthLayout)]
			}
			first, err := time.Parse(monthLayout, month)
			if err != nil { // the database holds no such month: GLOB and substr(date) see to it
				continue
			}
			monthEnd := first.AddDate(0, 1, -1).Format(ledger.DateLayout)
			day := monthEnd
			if daily {
				day = r.month
			}
			amount, ok := s.convertOn(book, r.amount, base, req.RateDate, day, monthEnd)
			if !ok {
				missing[r.amount.Currency] = true
				continue
			}
			sum := out[r.category]
			sum.Currency = base
			sum.Minor += sign * amount.Minor
			out[r.category] = sum
		}
	}
	add(targeted, 1, false)
	add(spent, -1, true)
	return out, nil
}

func sortedCurrencies(set map[ledger.Currency]bool) []ledger.Currency {
	var out []ledger.Currency
	for c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ---- opening balances ------------------------------------------------------

// SetOpeningBalance makes the account hold req.Balance at the end of req.AsOf
// by booking the difference between that and what its other transactions add
// up to as one transaction against Equity:Opening. That transaction is dated the
// day before the account's first transaction (or on AsOf when the account has
// none), so the account's whole history adds up. Asking again with a new
// balance corrects the same transaction.
func (s *Local) SetOpeningBalance(ctx context.Context, req OpeningBalanceRequest) (OpeningBalanceResult, error) {
	var res OpeningBalanceResult
	err := s.repo.inTx(ctx, func(q queries) error {
		acc, err := q.account(ctx, req.Account)
		if err != nil {
			return err
		}
		if !acc.Type.NeedsCurrency() {
			return coded.New("balance_not_for_type", map[string]any{"Name": acc.Name, "Type": acc.Type})
		}
		if req.Balance.Currency != acc.Currency {
			return coded.New("balance_wrong_currency", map[string]any{"Account": acc.Name, "Holds": string(acc.Currency), "Currency": string(req.Balance.Currency)})
		}
		if req.AsOf.IsZero() {
			return coded.New("balance_no_date", nil)
		}
		asOf := req.AsOf.Format(ledger.DateLayout)

		existing, err := q.openingTransaction(ctx, acc.ID)
		if err != nil {
			return err
		}
		others, err := q.accountSum(ctx, acc.ID, asOf, existing)
		if err != nil {
			return err
		}
		first, err := q.firstPostingDate(ctx, acc.ID, existing)
		if err != nil {
			return err
		}
		amount := ledger.Money{Minor: req.Balance.Minor - others, Currency: acc.Currency}
		if existing == 0 && amount.Minor == 0 {
			return nil // nothing to explain
		}
		date := req.AsOf
		if first != "" && first <= asOf {
			firstDay, err := ledger.ParseDate(first)
			if err != nil {
				return err
			}
			date = firstDay.AddDate(0, 0, -1)
		}
		nt := NewTransaction{
			Date:  date,
			Payee: "Opening balance",
			Postings: []ledger.Posting{
				{Account: acc.Name, Amount: amount},
				{Account: ledger.OpeningBalanceAccount, Amount: amount.Neg()},
			},
		}
		if existing != 0 {
			views, err := q.listTransactions(ctx, TxFilter{ID: existing})
			if err != nil {
				return err
			}
			if len(views) != 1 {
				return coded.New("transaction_vanished", map[string]any{"ID": existing})
			}
			res.Before = &views[0]
			if err := replaceTransaction(ctx, q, existing, nt, &res.Created); err != nil {
				return err
			}
			res.ID = existing
		} else if res.ID, err = storeTransaction(ctx, q, nt, 0, &res.Created); err != nil {
			return err
		}
		res.Date, res.Amount = date.Format(ledger.DateLayout), amount
		return nil
	})
	if err != nil {
		return OpeningBalanceResult{}, err
	}
	return res, nil
}

// ---- maintenance -----------------------------------------------------------

// Check verifies the database invariants and returns a description of every
// problem found (an empty slice means the ledger is consistent).
func (s *Local) Check(ctx context.Context) ([]string, error) {
	q := s.repo.queries()
	var problems []string

	integrity, err := q.integrityCheck(ctx)
	if err != nil {
		return nil, err
	}
	if integrity != "ok" {
		problems = append(problems, "sqlite integrity check: "+integrity)
	}
	unbalanced, err := q.unbalanced(ctx)
	if err != nil {
		return nil, err
	}
	problems = append(problems, unbalanced...)
	mismatched, err := q.currencyMismatches(ctx)
	if err != nil {
		return nil, err
	}
	problems = append(problems, mismatched...)
	unnamed, err := q.missingIdentity(ctx)
	if err != nil {
		return nil, err
	}
	problems = append(problems, unnamed...)
	broken, err := q.brokenTransferLinks(ctx)
	if err != nil {
		return nil, err
	}
	return append(problems, broken...), nil
}

// Backup writes a consistent snapshot of the database to dest using
// VACUUM INTO, then opens the snapshot and checks it, so a backup that was
// reported as written can be relied on. dest must not exist yet. The snapshot
// is NOT encrypted.
func (s *Local) Backup(ctx context.Context, dest string) error {
	return snapshot(ctx, s.repo.queries(), dest)
}

// PreMigrationBackup is the copy of the database that Open made before it
// upgraded the schema, or "" if the database needed no upgrade. The copy is NOT
// encrypted.
func (s *Local) PreMigrationBackup() string { return s.repo.migrationBackup }

// snapshot writes a checked, private copy of the database q reads to dest, which
// must not exist yet.
func snapshot(ctx context.Context, q queries, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return coded.New("file_exists", map[string]any{"Path": dest})
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if err := q.vacuumInto(ctx, dest); err != nil {
		return fmt.Errorf("%w: %w", coded.New("backup_failed", map[string]any{"Path": dest}), err)
	}
	if err := os.Chmod(dest, 0o600); err != nil { // it holds financial data: just for you
		return err
	}
	if err := verifySnapshot(ctx, dest); err != nil {
		os.Remove(dest) // a snapshot that fails its check must not sit among the good ones
		return fmt.Errorf("%w: %w", coded.New("backup_check_failed", map[string]any{"Path": dest}), err)
	}
	return nil
}

var uriPath = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")

// verifySnapshot opens the database file at path read-only and runs SQLite's
// integrity check on it.
func verifySnapshot(ctx context.Context, path string) error {
	// In a file: URI, ?, # and % mean something, so a path that has them is escaped.
	// Immutable: this is a copy nobody else is using, and a database that is in
	// write-ahead-log mode would otherwise get -wal and -shm files that a read-only
	// connection cannot clean up again.
	db, err := sql.Open("sqlite", "file:"+uriPath.Replace(path)+"?mode=ro&immutable=1")
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := queries{db}.integrityCheck(ctx)
	if err != nil {
		return err
	}
	if result != "ok" {
		return coded.New("integrity_failed", map[string]any{"Result": result})
	}
	return nil
}

// ---- schedules -------------------------------------------------------------

// validate checks what can be checked without the database, and tidies the name.
func (ns *NewSchedule) validate() error {
	ns.Name = strings.TrimSpace(ns.Name)
	switch {
	case ns.Name == "":
		return coded.New("schedule_name_empty", nil)
	case ns.Every < 1:
		return coded.New("schedule_every_invalid", map[string]any{"Every": ns.Every})
	case ns.Unit != EveryWeek && ns.Unit != EveryMonth && ns.Unit != EveryYear:
		return coded.New("schedule_unit_invalid", map[string]any{"Unit": string(ns.Unit)})
	}
	if !ns.End.IsZero() && ns.End.Before(ns.Start) {
		return coded.New("schedule_end_before_start", map[string]any{"Start": ns.Start.Format(ledger.DateLayout), "End": ns.End.Format(ledger.DateLayout)})
	}
	if ns.Transfer {
		_, err := transferRequest(*ns)
		return err
	}
	// The template is a transaction that has not happened: the same rules apply.
	return NewTransaction{Date: ns.Start, Postings: ns.Postings}.validate()
}

// transferRequest is the transfer a schedule with Transfer set stands for, on
// ns.Start, or the error that says its postings are not the two of a transfer.
func transferRequest(ns NewSchedule) (TransferRequest, error) {
	p := ns.Postings
	if len(p) != 2 || p[0].Amount.Minor >= 0 || p[1].Amount.Minor <= 0 {
		return TransferRequest{}, coded.New("schedule_transfer_postings", nil)
	}
	req := TransferRequest{From: p[0].Account, To: p[1].Account, Amount: p[0].Amount.Neg(), Date: ns.Start, Memo: ns.Description}
	if p[0].Amount.Currency != p[1].Amount.Currency {
		req.ToAmount = p[1].Amount
	} else if p[1].Amount != req.Amount {
		return TransferRequest{}, coded.New("schedule_transfer_postings", nil)
	}
	return req, nil
}

// storeSchedule writes ns as schedule id, or as a new one when id is 0.
func storeSchedule(ctx context.Context, q queries, id int64, ns NewSchedule) (int64, error) {
	if err := ns.validate(); err != nil {
		return 0, err
	}
	if id != 0 {
		found, err := q.scheduleExists(ctx, id)
		if err != nil {
			return 0, err
		}
		if !found {
			return 0, coded.New("schedule_not_found", map[string]any{"ID": id})
		}
	}
	taken, err := q.scheduleNameTaken(ctx, ns.Name, id)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, coded.New("schedule_name_taken", map[string]any{"Name": ns.Name})
	}
	if ns.Transfer { // the accounts must exist, hold their currency and differ, as for a transfer
		req, err := transferRequest(ns)
		if err != nil {
			return 0, err
		}
		if _, err := planTransfer(ctx, q, req); err != nil {
			return 0, err
		}
	}
	var created []string
	accountIDs, err := resolvePostings(ctx, q, NewTransaction{Postings: ns.Postings}, &created)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		if id, err = q.insertSchedule(ctx, ns); err != nil {
			return 0, err
		}
	} else {
		if err := q.updateSchedule(ctx, id, ns); err != nil {
			return 0, err
		}
		if err := q.deleteSchedulePostings(ctx, id); err != nil {
			return 0, err
		}
	}
	return id, q.insertSchedulePostings(ctx, id, ns.Postings, accountIDs)
}

// CreateSchedule stores a new schedule.
func (s *Local) CreateSchedule(ctx context.Context, ns NewSchedule) (int64, error) {
	var id int64
	err := s.repo.inTx(ctx, func(q queries) (err error) {
		id, err = storeSchedule(ctx, q, 0, ns)
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ReplaceSchedule rewrites schedule id in place.
func (s *Local) ReplaceSchedule(ctx context.Context, id int64, ns NewSchedule) error {
	return s.repo.inTx(ctx, func(q queries) error {
		_, err := storeSchedule(ctx, q, id, ns)
		return err
	})
}

// DeleteSchedule removes schedule id.
func (s *Local) DeleteSchedule(ctx context.Context, id int64) error {
	return s.repo.inTx(ctx, func(q queries) error {
		found, err := q.deleteSchedule(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return coded.New("schedule_not_found", map[string]any{"ID": id})
		}
		return nil
	})
}

// Schedules lists every schedule, by name.
func (s *Local) Schedules(ctx context.Context) ([]Schedule, error) {
	return s.repo.queries().schedules(ctx)
}

// occurrenceOn is the k-th date (0 is the start) of a schedule that repeats every
// `every` units from start. It counts from start each time rather than from the
// previous date, so a schedule that starts on the 31st falls on the 28th in
// February and is back on the 31st in March: shortening a month once never moves
// the days after it.
func occurrenceOn(start time.Time, every int, unit ScheduleUnit, k int) time.Time {
	if unit == EveryWeek {
		return start.AddDate(0, 0, 7*every*k)
	}
	months := every * k
	if unit == EveryYear {
		months *= 12
	}
	// Day 1 of the right month first: time.Date would roll 31 February into March.
	first := time.Date(start.Year(), start.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(start.Day(), last), 0, 0, 0, 0, time.UTC)
}

// occurrencesUpTo lists the dates from start through upTo, both included.
func occurrencesUpTo(start time.Time, every int, unit ScheduleUnit, upTo time.Time) []time.Time {
	var out []time.Time
	for k := 0; ; k++ {
		d := occurrenceOn(start, every, unit, k)
		if d.After(upTo) {
			return out
		}
		out = append(out, d)
	}
}

// Due lists the occurrences up to and including upTo.
func (s *Local) Due(ctx context.Context, upTo string) ([]Occurrence, error) {
	end, err := ledger.ParseDate(upTo)
	if err != nil {
		return nil, err
	}
	schedules, err := s.repo.queries().schedules(ctx)
	if err != nil {
		return nil, err
	}
	done, err := s.repo.queries().handledOccurrences(ctx)
	if err != nil {
		return nil, err
	}
	var out []Occurrence
	for _, sch := range schedules {
		dates, err := scheduleDates(sch, end)
		if err != nil {
			return nil, err
		}
		for _, d := range dates {
			if !done[handled{sch.ID, d}] {
				out = append(out, Occurrence{Schedule: sch, Date: d})
			}
		}
	}
	// Schedules come by name, and the sort is stable: a tie on the date keeps that order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// scheduleDates is the dates of sch up to and including end, as stored: not past
// the schedule's own end, and not the ones that fell in a pause.
func scheduleDates(sch Schedule, end time.Time) ([]string, error) {
	start, err := ledger.ParseDate(sch.Start)
	if err != nil {
		return nil, err
	}
	var out []string
dates:
	for _, d := range occurrencesUpTo(start, sch.Every, sch.Unit, end) {
		day := d.Format(ledger.DateLayout)
		if sch.End != "" && day > sch.End {
			break
		}
		for _, z := range sch.Pauses {
			if day >= z.From && (z.To == "" || day < z.To) {
				continue dates
			}
		}
		out = append(out, day)
	}
	return out, nil
}

// PauseSchedule pauses schedule id from today.
func (s *Local) PauseSchedule(ctx context.Context, id int64) error {
	return s.repo.inTx(ctx, func(q queries) error {
		if err := scheduleMustExist(ctx, q, id); err != nil {
			return err
		}
		return q.pauseSchedule(ctx, id, s.today())
	})
}

// ResumeSchedule resumes schedule id from today.
func (s *Local) ResumeSchedule(ctx context.Context, id int64) error {
	return s.repo.inTx(ctx, func(q queries) error {
		if err := scheduleMustExist(ctx, q, id); err != nil {
			return err
		}
		return q.resumeSchedule(ctx, id, s.today())
	})
}

func scheduleMustExist(ctx context.Context, q queries, id int64) error {
	found, err := q.scheduleExists(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return coded.New("schedule_not_found", map[string]any{"ID": id})
	}
	return nil
}

// answerable finds the occurrence of schedule id on date and checks that it can
// be answered now: that it is one of the schedule's dates, is not in the future,
// and has not been answered. date is returned as stored.
func (s *Local) answerable(ctx context.Context, q queries, id int64, date string) (Schedule, string, error) {
	d, err := ledger.ParseDate(date)
	if err != nil {
		return Schedule{}, "", err
	}
	date = d.Format(ledger.DateLayout)
	all, err := q.schedules(ctx)
	if err != nil {
		return Schedule{}, "", err
	}
	for _, sch := range all {
		if sch.ID != id {
			continue
		}
		dates, err := scheduleDates(sch, d)
		if err != nil {
			return Schedule{}, "", err
		}
		args := map[string]any{"Name": sch.Name, "Date": date}
		switch {
		case len(dates) == 0 || dates[len(dates)-1] != date:
			return Schedule{}, "", coded.New("occurrence_not_on_schedule", args)
		case date > s.today():
			return Schedule{}, "", coded.New("occurrence_not_due", args)
		}
		done, err := q.handledOccurrences(ctx)
		if err != nil {
			return Schedule{}, "", err
		}
		if done[handled{id, date}] {
			return Schedule{}, "", coded.New("occurrence_done", args)
		}
		return sch, date, nil
	}
	return Schedule{}, "", coded.New("schedule_not_found", map[string]any{"ID": id})
}

// ConfirmOccurrence books one occurrence.
func (s *Local) ConfirmOccurrence(ctx context.Context, id int64, date string, postings []ledger.Posting) (ConfirmResult, error) {
	var res ConfirmResult
	err := s.repo.inTx(ctx, func(q queries) error {
		sch, date, err := s.answerable(ctx, q, id, date)
		if err != nil {
			return err
		}
		when, err := ledger.ParseDate(date)
		if err != nil {
			return err
		}
		if postings == nil {
			for _, p := range sch.Postings {
				postings = append(postings, ledger.Posting{Account: p.Account, Amount: p.Amount})
			}
		}
		if sch.Transfer {
			req, err := transferRequest(NewSchedule{Start: when, Description: sch.Description, Postings: postings})
			if err != nil {
				return err
			}
			plan, err := planTransfer(ctx, q, req)
			if err != nil {
				return err
			}
			stored, err := plan.store(ctx, q)
			if err != nil {
				return err
			}
			res.AddResult, res.InID = stored.AddResult, stored.InID
			return q.putOccurrence(ctx, handled{id, date}, res.ID)
		}
		nt := NewTransaction{Date: when, Payee: sch.Payee, Description: sch.Description, Postings: postings}
		if res.ID, err = storeTransaction(ctx, q, nt, 0, &res.Created); err != nil {
			return err
		}
		return q.putOccurrence(ctx, handled{id, date}, res.ID)
	})
	if err != nil {
		return ConfirmResult{}, err
	}
	return res, nil
}

// SkipOccurrence passes on one occurrence.
func (s *Local) SkipOccurrence(ctx context.Context, id int64, date string) error {
	return s.repo.inTx(ctx, func(q queries) error {
		_, date, err := s.answerable(ctx, q, id, date)
		if err != nil {
			return err
		}
		return q.putOccurrence(ctx, handled{id, date}, 0)
	})
}

// ---- schedules and imported rows -------------------------------------------

// occurrenceMatch is the occurrence of a schedule that a bank row stands for.
type occurrenceMatch struct {
	scheduleID int64
	name       string
	date       string
	category   string // the schedule's, for the row to be booked to
	transfer   bool   // a scheduled transfer: the row may still be paired with its other half
}

func (m occurrenceMatch) settled(txID int64) SettledOccurrence {
	return SettledOccurrence{ScheduleID: m.scheduleID, Schedule: m.name, Date: m.date, TransactionID: txID}
}

// occurrenceMatcher finds, for a bank row, the occurrence nobody answered yet. It
// reads the schedules once and remembers the occurrences it handed out, so that
// each is settled by one row only.
type occurrenceMatcher struct {
	schedules []Schedule
	done      map[handled]bool
}

func newOccurrenceMatcher(ctx context.Context, q queries) (*occurrenceMatcher, error) {
	schedules, err := q.schedules(ctx)
	if err != nil {
		return nil, err
	}
	done, err := q.handledOccurrences(ctx)
	if err != nil {
		return nil, err
	}
	return &occurrenceMatcher{schedules: schedules, done: done}, nil
}

// bookedTo is the account a bank row of amount on account is booked to when it is
// the booking of an occurrence of s: the category of an ordinary schedule, or the
// clearing account of a scheduled transfer, which the row may be either half of.
func (s Schedule) bookedTo(account string, amount ledger.Money) (string, bool) {
	if !s.Transfer {
		src, cat, ok := (TxView{Postings: s.Postings}).Legs()
		return cat.Account, ok && src.Account == account && src.Amount == amount
	}
	if len(s.Postings) != 2 {
		return "", false
	}
	clearing := ledger.TransferAccount
	if s.Postings[0].Amount.Currency != s.Postings[1].Amount.Currency {
		clearing = ledger.ConversionAccount
	}
	for _, p := range s.Postings {
		if p.Account == account && p.Amount == amount {
			return clearing, true
		}
	}
	return "", false
}

// find is the occurrence whose schedule books exactly amount on acc within
// matchWindowDays of date, the nearest first and then the earliest, or nil. A
// bill whose amount changes is no match: an estimate with a tolerance is REC-5.
// The occurrence need not be due yet: money that moved is news enough.
func (m *occurrenceMatcher) find(acc Account, amount ledger.Money, date time.Time, transfersOnly bool) *occurrenceMatch {
	var best *occurrenceMatch
	bestDistance := 0
	from := date.AddDate(0, 0, -matchWindowDays).Format(ledger.DateLayout)
	for _, sch := range m.schedules {
		category, ok := sch.bookedTo(acc.Name, amount)
		if !ok || (transfersOnly && !sch.Transfer) {
			continue
		}
		dates, err := scheduleDates(sch, date.AddDate(0, 0, matchWindowDays))
		if err != nil {
			continue
		}
		for _, d := range dates {
			if d < from || m.done[handled{sch.ID, d}] {
				continue
			}
			when, err := ledger.ParseDate(d)
			if err != nil {
				continue
			}
			distance := int(math.Abs(date.Sub(when).Hours() / 24))
			if best == nil || distance < bestDistance || (distance == bestDistance && d < best.date) {
				best, bestDistance = &occurrenceMatch{scheduleID: sch.ID, name: sch.Name, date: d, category: category, transfer: sch.Transfer}, distance
			}
		}
	}
	if best != nil {
		m.done[handled{best.scheduleID, best.date}] = true
	}
	return best
}
