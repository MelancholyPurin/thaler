package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

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
		return errors.New("base and quote currency must differ")
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
		return Account{}, fmt.Errorf("%s accounts need a currency, e.g. EUR", typ)
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
			return Account{}, fmt.Errorf("account %s does not exist; create it first as a %s account in %s", name, typ, cur)
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
		return Account{}, fmt.Errorf("account %s holds %s, but the posting is in %s", acc.Name, acc.Currency, cur)
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
		return errors.New("transaction has no date")
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
			return 0, ErrDuplicate
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
		return fmt.Errorf("transaction #%d does not exist", id)
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
		return fmt.Errorf("transaction #%d does not exist", id)
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
	return q.insertPostings(ctx, id, nt.Postings, accountIDs)
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
		return TxView{}, fmt.Errorf("transaction #%d does not exist", id)
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
				return fmt.Errorf("transaction #%d: %w", c.TransactionID, err)
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
		return fmt.Errorf("%s is not a category; use an Income, Expenses or Equity account", c.Category)
	}
	postings, err := q.categoryPostings(ctx, c.TransactionID)
	if err != nil {
		return err
	}
	if len(postings) != 1 {
		return fmt.Errorf("has %d category postings; only transactions with exactly one can be recategorized", len(postings))
	}
	acc, err := resolveAccount(ctx, q, c.Category, postings[0].currency, created)
	if err != nil {
		return err
	}
	return q.moveStoredPosting(ctx, postings[0].id, acc.ID)
}

// Transfer moves money between two accounts that hold their own currency. A
// cross-currency transfer is booked through Equity:Conversions so that every
// currency balances on its own.
func (s *Local) Transfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	q := s.repo.queries()
	src, err := q.account(ctx, req.From)
	if err != nil {
		return TransferResult{}, err
	}
	dst, err := q.account(ctx, req.To)
	if err != nil {
		return TransferResult{}, err
	}
	if src.Currency == "" || dst.Currency == "" {
		return TransferResult{}, errors.New("transfers work between accounts with a fixed currency (Assets or Liabilities)")
	}
	if src.ID == dst.ID {
		return TransferResult{}, errors.New("source and destination are the same account")
	}
	if req.Amount.Currency != src.Currency {
		return TransferResult{}, fmt.Errorf("amount is in %s, but %s holds %s", req.Amount.Currency, src.Name, src.Currency)
	}
	if req.Amount.Minor <= 0 {
		return TransferResult{}, errors.New("amount must be greater than zero")
	}

	postings := []ledger.Posting{{Account: src.Name, Amount: req.Amount.Neg()}}
	res := TransferResult{In: req.Amount}
	if src.Currency == dst.Currency {
		if req.ToAmount != (ledger.Money{}) {
			return TransferResult{}, fmt.Errorf("both accounts use %s, so there is no separate amount received", src.Currency)
		}
		postings = append(postings, ledger.Posting{Account: dst.Name, Amount: req.Amount})
	} else {
		if req.ToAmount == (ledger.Money{}) {
			return TransferResult{}, fmt.Errorf("%s -> %s crosses currencies; say how much arrives in %s",
				src.Currency, dst.Currency, dst.Currency)
		}
		if req.ToAmount.Currency != dst.Currency {
			return TransferResult{}, fmt.Errorf("amount received is in %s, but %s holds %s", req.ToAmount.Currency, dst.Name, dst.Currency)
		}
		if req.ToAmount.Minor <= 0 {
			return TransferResult{}, errors.New("amount received must be greater than zero")
		}
		postings = append(postings,
			ledger.Posting{Account: ledger.ConversionAccount, Amount: req.Amount},
			ledger.Posting{Account: ledger.ConversionAccount, Amount: req.ToAmount.Neg()},
			ledger.Posting{Account: dst.Name, Amount: req.ToAmount},
		)
		res.In, res.CrossCurrency = req.ToAmount, true
	}
	add, err := s.AddTransaction(ctx, NewTransaction{Date: req.Date, Description: req.Memo, Postings: postings})
	if err != nil {
		return TransferResult{}, err
	}
	res.AddResult = add
	return res, nil
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
		return ImportResult{}, fmt.Errorf("%s has no fixed currency; import into an Assets or Liabilities account", acc.Name)
	}
	rules, err := importer.NewRuleset(req.Rules)
	if err != nil {
		return ImportResult{}, fmt.Errorf("invalid rules: %w", err)
	}

	var res ImportResult
	var rows []importer.Row
	for _, r := range req.Rows {
		if r.Amount.Currency != acc.Currency {
			res.Rejected = append(res.Rejected, RejectedRow{
				Line:   r.Line,
				Reason: fmt.Sprintf("currency %s does not match %s (%s)", r.Amount.Currency, acc.Name, acc.Currency),
			})
			continue
		}
		rows = append(rows, r)
	}
	if len(res.Rejected) > 0 && !req.IgnoreRejected {
		return res, fmt.Errorf("%w: %d row(s)", ErrRowsRejected, len(res.Rejected))
	}

	dedupe := importer.NewDeduper(strconv.FormatInt(acc.ID, 10))
	txs := make([]NewTransaction, len(rows))
	categories := make([]string, len(rows))
	for i, r := range rows {
		category, ok := rules.Match(r.Payee, r.Purpose, r.BookingText)
		if !ok {
			res.Uncategorized++
			category = ledger.UncategorizedExpense
			if r.Amount.Minor > 0 {
				category = ledger.UncategorizedIncome
			}
		}
		description := r.Purpose
		if description == "" {
			description = r.BookingText
		}
		categories[i] = category
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
		for i, tx := range txs {
			dup, err := q.importHashExists(ctx, tx.ImportHash)
			if err != nil {
				return ImportResult{}, err
			}
			if dup {
				res.Duplicates++
			} else {
				res.Inserted++
			}
			res.Rows = append(res.Rows, ImportedRow{Row: rows[i], Category: categories[i], Duplicate: dup})
		}
		return res, nil
	}

	err = s.repo.inTx(ctx, func(q queries) error {
		for i, nt := range txs {
			id, err := storeTransaction(ctx, q, nt, 0, &res.Created)
			if errors.Is(err, ErrDuplicate) {
				res.Duplicates++
				continue
			}
			if err != nil {
				return fmt.Errorf("transaction %d (%s %s): %w", i+1, nt.Date.Format(ledger.DateLayout), nt.Payee, err)
			}
			res.Inserted++
			res.IDs = append(res.IDs, id)
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return res, nil
}

// Recategorize runs rules over every transaction that sits in
// Expenses:Uncategorized or Income:Uncategorized and moves the ones that match.
// Transactions that were categorised by hand are never touched. Rules see the
// payee and the stored description, so a rule that only matched the booking
// text of a row with a purpose at import time may not match here.
func (s *Local) Recategorize(ctx context.Context, req RecategorizeRequest) (RecategorizeResult, error) {
	rules, err := importer.NewRuleset(req.Rules)
	if err != nil {
		return RecategorizeResult{}, fmt.Errorf("invalid rules: %w", err)
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
	rateDate := req.RateDate
	if rateDate == "" {
		rateDate = s.today()
	}
	if req.To != "" && req.To < rateDate {
		rateDate = req.To
	}

	b := BudgetReport{
		Base:         base,
		IncomeTotal:  ledger.Money{Currency: base},
		ExpenseTotal: ledger.Money{Currency: base},
	}
	byName := make(map[string]*BudgetLine)
	missing := make(map[ledger.Currency]bool)
	for _, t := range totals {
		amount, ok := book.Convert(t.Amount, base, rateDate)
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
			return fmt.Errorf("%s is a %s account; only asset and liability accounts have a balance to set", acc.Name, acc.Type)
		}
		if req.Balance.Currency != acc.Currency {
			return fmt.Errorf("%s holds %s, but the balance is in %s", acc.Name, acc.Currency, req.Balance.Currency)
		}
		if req.AsOf.IsZero() {
			return errors.New("the balance has no date")
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
				return fmt.Errorf("transaction #%d disappeared", existing)
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
	return append(problems, unnamed...), nil
}

// Backup writes a consistent snapshot of the database to dest using
// VACUUM INTO. dest must not exist yet. The snapshot is NOT encrypted.
func (s *Local) Backup(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if err := s.repo.queries().vacuumInto(ctx, dest); err != nil {
		return fmt.Errorf("backup to %s: %w", dest, err)
	}
	return nil
}
