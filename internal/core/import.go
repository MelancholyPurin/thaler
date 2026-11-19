package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// Import books bank rows into one account. Each row becomes a transaction
// between the account and the category the first matching rule names; rows no
// rule matches go to Expenses:Uncategorized or Income:Uncategorized. Rows whose
// currency differs from the account's are rejected.
func (s *Local) Import(ctx context.Context, req ImportRequest) (ImportResult, error) {
	q := s.repo.queries()
	acc, err := importAccount(ctx, q, req.Account)
	if err != nil {
		return ImportResult{}, err
	}
	rules, err := importer.NewRuleset(req.Rules)
	if err != nil {
		return ImportResult{}, fmt.Errorf("%w: %w", coded.New("invalid_rules", nil), err)
	}
	var res ImportResult
	rows, rejected := rowsIn(acc, req.Rows)
	if res.Rejected = rejected; len(rejected) > 0 && !req.IgnoreRejected {
		return res, coded.New("rows_rejected", map[string]any{"Count": len(rejected)})
	}
	if err := checkChosenCategories(req.Categories); err != nil {
		return ImportResult{}, err
	}
	txs, ruled := importTransactions(acc, rows, rules, req.Categories)

	if req.DryRun {
		plan, err := planImport(ctx, q, acc, rows, txs, ruled)
		if err != nil {
			return ImportResult{}, err
		}
		res.preview(plan, rows, ruled)
		return res, nil
	}
	err = s.repo.inTx(ctx, func(q queries) error {
		// Planned inside the transaction, so the pairs it finds are still free
		// when the rows are written.
		plan, err := planImport(ctx, q, acc, rows, txs, ruled)
		if err != nil {
			return err
		}
		w := importWriter{ctx: ctx, q: q, res: &res, record: func() (int64, error) {
			return q.insertImport(ctx, acc.ID, req.Source, req.Profile, s.now())
		}}
		for i, p := range plan {
			if err := w.write(i, p, ruled[i]); err != nil {
				return err
			}
		}
		res.ImportID = w.importID
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return res, nil
}

// importAccount is the account an import goes into: one of yours that is open and holds
// one currency, the currency every row must be in.
func importAccount(ctx context.Context, q queries, name string) (Account, error) {
	acc, err := q.account(ctx, name)
	if err != nil {
		return Account{}, err
	}
	if err := guardOpen(acc); err != nil {
		return Account{}, err
	}
	if acc.Currency == "" {
		return Account{}, coded.New("import_no_fixed_currency", map[string]any{"Name": acc.Name})
	}
	return acc, nil
}

// rowsIn keeps the rows in the currency of the account and rejects the others.
func rowsIn(acc Account, all []importer.Row) (rows []importer.Row, rejected []RejectedRow) {
	for _, r := range all {
		if r.Amount.Currency != acc.Currency {
			rejected = append(rejected, RejectedRow{
				Line: r.Line,
				Err:  coded.New("row_currency_mismatch", map[string]any{"Currency": r.Amount.Currency, "Account": acc.Name, "Holds": acc.Currency}),
			})
			continue
		}
		rows = append(rows, r)
	}
	return rows, rejected
}

// checkChosenCategories refuses a category chosen for a row that is an account of yours.
func checkChosenCategories(chosen map[int]string) error {
	for _, c := range chosen {
		typ, err := ledger.TypeOf(c)
		if err != nil {
			return err
		}
		if typ.NeedsCurrency() {
			return coded.New("not_a_category", map[string]any{"Name": c})
		}
	}
	return nil
}

// importTransactions turns each row into a transaction between the account and its
// category: the one chosen for the row, else the first rule that matches, else
// Uncategorized. ruled says, for each, whether a rule or a person chose it.
func importTransactions(acc Account, rows []importer.Row, rules *importer.Ruleset, chosen map[int]string) (txs []NewTransaction, ruled []bool) {
	dedupe := importer.NewDeduper(strconv.FormatInt(acc.ID, 10))
	txs = make([]NewTransaction, len(rows))
	ruled = make([]bool, len(rows))
	for i, r := range rows {
		category, ok := rules.Match(r.Payee, r.Purpose, r.BookingText)
		if c, set := chosen[r.Line]; set {
			category, ok = c, c != ledger.UncategorizedExpense && c != ledger.UncategorizedIncome
		}
		ruled[i] = ok
		if !ok {
			category = uncategorizedFor(r.Amount)
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
	return txs, ruled
}

// preview counts what the plan would do and lists each row with it, for a dry run.
func (res *ImportResult) preview(plan []plannedRow, rows []importer.Row, ruled []bool) {
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
}

// importWriter writes the planned rows of an import, inside the caller's transaction,
// and counts what it did in res.
type importWriter struct {
	ctx      context.Context
	q        queries
	res      *ImportResult
	record   func() (int64, error) // makes the record of the import
	importID int64                 // the record, once a row needed it
}

// importRecord is the record of the import, made with the first row that is stored, so
// that an import that adds nothing leaves none.
func (w *importWriter) importRecord() (int64, error) {
	if w.importID != 0 {
		return w.importID, nil
	}
	id, err := w.record()
	w.importID = id
	return id, err
}

// write stores row i of the plan: nothing for a duplicate, the import hash on the entry
// it adopts, or a new transaction, with the occurrence it settles and the half it pairs.
func (w *importWriter) write(i int, p plannedRow, ruled bool) error {
	if p.uncategorized(ruled) {
		w.res.Uncategorized++
	}
	if p.duplicate {
		w.res.Duplicates++
		return nil
	}
	rowErr := func(err error) error {
		return fmt.Errorf("%w: %w", coded.New("import_row_error", map[string]any{"N": i + 1, "Date": p.tx.Date.Format(ledger.DateLayout), "Payee": p.tx.Payee}), err)
	}
	if p.adopts != nil {
		return w.adopt(p, rowErr)
	}
	var err error
	if p.tx.ImportID, err = w.importRecord(); err != nil {
		return err
	}
	id, err := storeTransaction(w.ctx, w.q, p.tx, 0, &w.res.Created)
	if errors.Is(err, ErrDuplicate) {
		w.res.Duplicates++
		return nil
	}
	if err != nil {
		return rowErr(err)
	}
	w.res.Inserted++
	w.res.IDs = append(w.res.IDs, id)
	if p.settles != nil {
		if err := w.q.putOccurrence(w.ctx, handled{p.settles.scheduleID, p.settles.date}, id); err != nil {
			return rowErr(err)
		}
		w.res.Settled = append(w.res.Settled, p.settles.settled(id))
	}
	if p.partner == nil {
		return nil
	}
	return w.pair(id, p, rowErr)
}

// adopt makes the row the bank's record of the entry you made by hand.
func (w *importWriter) adopt(p plannedRow, rowErr func(error) error) error {
	if err := w.q.setImportHash(w.ctx, p.adopts.id, p.tx.ImportHash); err != nil {
		return rowErr(err)
	}
	importID, err := w.importRecord()
	if err != nil {
		return err
	}
	if err := w.q.adoptedByImport(w.ctx, importID, p.adopts.id); err != nil {
		return rowErr(err)
	}
	w.res.Adopted++
	w.res.AdoptedIDs = append(w.res.AdoptedIDs, p.adopts.id)
	return nil
}

// pair links the stored row id with the half on another account it completes, moving
// that half to Equity:Transfers first.
func (w *importWriter) pair(id int64, p plannedRow, rowErr func(error) error) error {
	if p.partner.category != ledger.TransferAccount {
		change := CategoryChange{TransactionID: p.partner.id, Category: ledger.TransferAccount}
		if err := setCategory(w.ctx, w.q, change, &w.res.Created); err != nil {
			return fmt.Errorf("%w: %w", coded.New("transaction_n", map[string]any{"ID": p.partner.id}), err)
		}
	}
	if err := w.q.linkTransfer(w.ctx, id, p.partner.id); err != nil {
		return err
	}
	if err := w.q.pairedByImport(w.ctx, p.tx.ImportID, id, p.partner.id, p.partner.category); err != nil {
		return rowErr(err)
	}
	w.res.Transfers++
	w.res.Linked = append(w.res.Linked, TransferLink{ID: id, OtherID: p.partner.id, OtherWas: p.partner.category})
	return nil
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

// ListImports is the imports that were made and not taken back, newest first.
func (s *Local) ListImports(ctx context.Context) ([]ImportRecord, error) {
	rows, err := s.repo.queries().imports(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ImportRecord, len(rows))
	for i, r := range rows {
		out[i] = r.ImportRecord
	}
	return out, nil
}

// UndoImport takes an import back as a whole: everything or nothing.
func (s *Local) UndoImport(ctx context.Context, id int64) (ImportUndone, error) {
	var out ImportUndone
	err := s.repo.inTx(ctx, func(q queries) error {
		rec, found, err := q.importRecord(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return coded.New("import_not_found", map[string]any{"ID": id})
		}
		out = ImportUndone{Record: rec.ImportRecord}
		if name, err := q.closedAccountIn(ctx, []int64{rec.accountID}); err != nil {
			return err
		} else if name != "" {
			return coded.New("account_closed", map[string]any{"Name": name})
		}
		added, err := q.importTransactions(ctx, id)
		if err != nil {
			return err
		}
		adopted, err := q.importAdoptions(ctx, id)
		if err != nil {
			return err
		}
		pairings, err := q.importPairings(ctx, id)
		if err != nil {
			return err
		}

		// Your own entries let go of the bank rows they took over.
		for _, tx := range adopted {
			if err := q.clearImportHash(ctx, tx); err != nil {
				return err
			}
			out.Released++
		}
		// The halves it paired go back to where they were, unless they were
		// changed since: then they stay as they are now.
		var created []string
		restored := map[int64]bool{}
		for _, p := range pairings {
			restored[p.otherID] = true
			if ok, err := inTransfers(ctx, q, p.otherID); err != nil {
				return err
			} else if ok && p.otherWas != ledger.TransferAccount {
				if err := setCategory(ctx, q, CategoryChange{TransactionID: p.otherID, Category: p.otherWas}, &created); err != nil {
					return fmt.Errorf("%w: %w", coded.New("transaction_n", map[string]any{"ID": p.otherID}), err)
				}
				out.Unpaired++
			}
		}
		// A half that a later import paired with one of these, or that you paired by
		// hand, would wait for a half that is not coming: it is uncategorized again.
		for _, tx := range added {
			partner, err := q.transferPartner(ctx, tx)
			if err != nil {
				return err
			}
			if partner == 0 || restored[partner] || slices.Contains(added, partner) {
				continue
			}
			if err := unpairHalf(ctx, q, partner, &created); err != nil {
				return err
			}
			out.Unpaired++
		}
		for _, tx := range added {
			if err := guardTransactionOpen(ctx, q, tx); err != nil {
				return err
			}
			if _, err := q.deleteTransaction(ctx, tx); err != nil {
				return err
			}
		}
		return q.deleteImport(ctx, id)
	})
	if err != nil {
		return ImportUndone{}, err
	}
	return out, nil
}

// inTransfers says whether the transaction is booked to Equity:Transfers on its
// category side.
func inTransfers(ctx context.Context, q queries, id int64) (bool, error) {
	list, err := q.listTransactions(ctx, TxFilter{ID: id})
	if err != nil || len(list) != 1 {
		return false, err
	}
	_, category, ok := list[0].Legs()
	return ok && category.Account == ledger.TransferAccount, nil
}

// unpairHalf puts a half whose other half is going away back to Uncategorized,
// when it is still booked to Equity:Transfers.
func unpairHalf(ctx context.Context, q queries, id int64, created *[]string) error {
	list, err := q.listTransactions(ctx, TxFilter{ID: id})
	if err != nil || len(list) != 1 {
		return err
	}
	source, category, ok := list[0].Legs()
	if !ok || !isClearingAccount(category.Account) {
		return nil
	}
	return setCategory(ctx, q, CategoryChange{TransactionID: id, Category: uncategorizedFor(source.Amount)}, created)
}
