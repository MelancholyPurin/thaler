package tui

import (
	"strings"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// row is one transaction prepared for display.
type row struct {
	tx            core.TxView
	source        core.PostingView // leg on your own account
	category      core.PostingView // leg on the category
	simple        bool             // false for transfers and splits, which cannot be recategorized here
	uncategorized bool
	text          string // everything the search looks at
}

// acctEntry is one asset or liability account with figures for its panel.
type acctEntry struct {
	account core.Account
	balance ledger.Money
	txCount int
	uncat   int
	last    string // date of the newest transaction, empty if none
}

// shortName drops the type segment: "Assets:Sparkasse" becomes "Sparkasse".
func shortName(name string) string {
	if _, rest, ok := strings.Cut(name, ":"); ok {
		return rest
	}
	return name
}

func newRow(tx core.TxView) row {
	r := row{tx: tx}
	if src, cat, ok := tx.Legs(); ok {
		r.source, r.category, r.simple = src, cat, true
		r.uncategorized = cat.Account == ledger.UncategorizedExpense || cat.Account == ledger.UncategorizedIncome
	} else {
		names := make([]string, len(tx.Postings))
		for i, p := range tx.Postings {
			names[i] = p.Account
		}
		r.category = core.PostingView{Account: strings.Join(names, " → ")}
		if len(tx.Postings) > 0 {
			r.source = tx.Postings[0]
		}
	}
	r.text = strings.Join([]string{tx.Date, tx.Payee, tx.Description, r.category.Account, r.source.Account, signed(r.source.Amount)}, " ")
	return r
}

func signed(m ledger.Money) string {
	if m.Minor > 0 {
		return "+" + m.String()
	}
	return m.String()
}

// reload reads everything the interface shows from the database.
func (m *Model) reload() error {
	txs, err := m.svc.ListTransactions(m.ctx, core.TxFilter{})
	if err != nil {
		return err
	}
	accs, err := m.svc.ListAccounts(m.ctx)
	if err != nil {
		return err
	}
	report, err := m.svc.BalanceReport(m.ctx, core.BalanceRequest{})
	if err != nil {
		return err
	}
	balances := report.Lines
	m.netWorth = report.NetWorth

	m.rows = m.rows[:0]
	m.counts = make(map[string]int)
	currencies := make(map[ledger.Currency]bool)
	for i := len(txs) - 1; i >= 0; i-- { // newest first
		r := newRow(txs[i])
		if r.simple && !r.uncategorized {
			m.counts[r.category.Account]++
		}
		for _, p := range r.tx.Postings {
			currencies[p.Amount.Currency] = true
		}
		m.rows = append(m.rows, r)
	}
	m.multiCurrency = len(currencies) > 1
	m.refreshMonths()
	m.refreshPayees()
	m.lastEntered = ""
	var newest int64
	for _, r := range m.rows {
		if r.simple && r.tx.ID > newest {
			newest, m.lastEntered = r.tx.ID, r.source.Account
		}
	}

	m.categories = m.categories[:0]
	byName := make(map[string]*acctEntry)
	m.accounts = m.accounts[:0]
	for _, a := range accs {
		switch {
		case a.Type.NeedsCurrency():
			m.accounts = append(m.accounts, acctEntry{account: a, balance: ledger.Money{Currency: a.Currency}})
		case (a.Type == ledger.Income || a.Type == ledger.Expenses) && a.Name != ledger.UncategorizedExpense && a.Name != ledger.UncategorizedIncome:
			m.categories = append(m.categories, a.Name)
		}
	}
	for i := range m.accounts {
		byName[m.accounts[i].account.Name] = &m.accounts[i]
	}
	for _, b := range balances {
		if e := byName[b.Account]; e != nil {
			e.balance = b.Amount
		}
	}
	for _, r := range m.rows {
		for _, p := range r.tx.Postings {
			if e := byName[p.Account]; e != nil {
				e.txCount++
				if r.uncategorized {
					e.uncat++
				}
				if e.last == "" {
					e.last = r.tx.Date // rows are newest first
				}
			}
		}
	}
	return m.refreshBudget()
}

func (m *Model) uncategorizedCount() int {
	n := 0
	for _, r := range m.rows {
		if r.uncategorized {
			n++
		}
	}
	return n
}

// scopeAccount is the account the transaction list is limited to, or "" for all.
func (m *Model) scopeAccount() string {
	if i := m.accts.cursor - 1; i >= 0 && i < len(m.accounts) {
		return m.accounts[i].account.Name
	}
	return ""
}

func (r row) touches(account string) bool {
	for _, p := range r.tx.Postings {
		if p.Account == account {
			return true
		}
	}
	return false
}

// applyFilter recomputes the visible transactions from the account scope, the
// uncategorized toggle and the search text. With keep set, the cursor stays on
// the same transaction when it is still visible.
func (m *Model) applyFilter(keep bool) {
	var keepID int64 = -1
	if r, ok := m.selected(); ok && keep {
		keepID = r.tx.ID
	}
	scope := m.scopeAccount()
	q := strings.TrimSpace(m.filter.Value())
	m.view = m.view[:0]
	for i, r := range m.rows {
		if scope != "" && !r.touches(scope) {
			continue
		}
		if m.period != "" && !strings.HasPrefix(r.tx.Date, m.period) {
			continue
		}
		if m.onlyUncat && !r.uncategorized {
			continue
		}
		if q != "" && !matchAll(q, r.text) {
			continue
		}
		m.view = append(m.view, i)
	}
	if keepID >= 0 {
		for vi, ri := range m.view {
			if m.rows[ri].tx.ID == keepID {
				m.txs.cursor = vi
				break
			}
		}
	} else if !keep {
		m.txs = listState{}
	}
	m.clampAll()
}

// selected is the transaction under the cursor. After a reload the view can
// still hold indexes into the old, longer list of rows (until applyFilter
// rebuilds it), so an index past the end means there is no selection.
func (m *Model) selected() (row, bool) {
	if m.txs.cursor < 0 || m.txs.cursor >= len(m.view) || m.view[m.txs.cursor] >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.view[m.txs.cursor]], true
}

func (m *Model) findRow(id int64) (row, bool) {
	for _, r := range m.rows {
		if r.tx.ID == id {
			return r, true
		}
	}
	return row{}, false
}

// payeeStat is a payee seen before, with the category it most recently went to.
type payeeStat struct {
	name     string
	category string // empty if never categorised
	count    int
}

// refreshPayees lists every payee used so far, most recently used first.
func (m *Model) refreshPayees() {
	m.payees = m.payees[:0]
	index := make(map[string]int)
	for _, r := range m.rows { // newest first
		name := clean(r.tx.Payee)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		i, ok := index[key]
		if !ok {
			i = len(m.payees)
			index[key] = i
			m.payees = append(m.payees, payeeStat{name: name})
		}
		m.payees[i].count++
		if m.payees[i].category == "" && r.simple && !r.uncategorized {
			m.payees[i].category = r.category.Account
		}
	}
}
