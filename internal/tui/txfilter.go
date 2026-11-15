package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// The filter popup (f on the Ledger) narrows the transactions list by the size of the
// transaction, by dates, and by category, and chooses the order it is in. They come on top of
// what the Ledger already narrows it by: the account selected in the Accounts panel, the
// month on show (a range of dates replaces the month, and the bar at the top says so), the
// uncategorized toggle (u) and the search (/). esc in the list clears them all, the order
// stays. The size is that of the transaction on your own account, whichever way the money
// went: a spending of 12.00 is 12.00.

// sortKind is the order of the transactions list.
type sortKind int

const (
	sortNewest sortKind = iota // date, the newest first: how the list always was
	sortOldest
	sortLargest
	sortSmallest
	sortPayee
	sortCategory
	numSorts
)

// txFilter is what narrows the list beyond the account, the month, the toggle and the search.
type txFilter struct {
	minAmount, maxAmount string // as typed, in the way the language writes a number; either may be empty
	from, to             string // YYYY-MM-DD, inclusive; either may be empty
	category             string // what the name of the category has to contain, as in the search
	sort                 sortKind
}

// active says whether anything narrows the list.
func (f txFilter) active() bool {
	return f.minAmount != "" || f.maxAmount != "" || f.dated() || f.category != ""
}

// dated says whether a range of dates is set, which takes the place of the month.
func (f txFilter) dated() bool { return f.from != "" || f.to != "" }

// size is how much money a transaction moved, whichever way, and in what currency: the amount
// on your own account, and for what has no single one (a split) what went in.
func (r row) size() (int64, ledger.Currency) {
	if r.simple {
		n := r.source.Amount.Minor
		if n < 0 {
			n = -n
		}
		return n, r.source.Amount.Currency
	}
	var total int64
	var cur ledger.Currency
	for _, p := range r.tx.Postings {
		if cur == "" {
			cur = p.Amount.Currency
		}
		if p.Amount.Minor > 0 {
			total += p.Amount.Minor
		}
	}
	return total, cur
}

// inCategory says whether a posting of the transaction is to an Income, Expenses or Equity
// account whose name matches what was typed.
func (r row) inCategory(q string) bool {
	for _, p := range r.tx.Postings {
		if typ, err := ledger.TypeOf(p.Account); err == nil && !typ.NeedsCurrency() && matchAll(q, p.Account) {
			return true
		}
	}
	return false
}

// amountBound is a size typed in the popup, in the minor units of a currency; ok is false for
// no bound, or one the currency cannot hold.
func amountBound(text string, cur ledger.Currency) (int64, bool) {
	text = strings.TrimLeft(strings.TrimSpace(text), "+-−")
	if text == "" {
		return 0, false
	}
	m, err := ledger.ParseAmount(text, cur, 0)
	if err != nil {
		return 0, false
	}
	return m.Minor, true
}

// matches says whether r passes the size, date and category the filter asks for.
func (f txFilter) matches(r row) bool {
	if f.dated() {
		if f.from != "" && r.tx.Date < f.from {
			return false
		}
		if f.to != "" && r.tx.Date > f.to {
			return false
		}
	}
	if f.category != "" && !r.inCategory(f.category) {
		return false
	}
	if f.minAmount != "" || f.maxAmount != "" {
		size, cur := r.size()
		if lo, ok := amountBound(f.minAmount, cur); ok && size < lo {
			return false
		}
		if hi, ok := amountBound(f.maxAmount, cur); ok && size > hi {
			return false
		}
	}
	return true
}

// sortRows puts the indexes of rows in the order asked for. The rows are held newest first, and
// a stable sort keeps that for what is equal.
func (m *Model) sortRows(view []int) {
	less := func(a, b row) bool { return false }
	switch m.tf.sort {
	case sortNewest:
		return
	case sortOldest:
		for i, j := 0, len(view)-1; i < j; i, j = i+1, j-1 {
			view[i], view[j] = view[j], view[i]
		}
		return
	case sortLargest, sortSmallest:
		less = func(a, b row) bool {
			x, _ := a.size()
			y, _ := b.size()
			if m.tf.sort == sortSmallest {
				return x < y
			}
			return x > y
		}
	case sortPayee:
		less = func(a, b row) bool { return strings.ToLower(clean(a.tx.Payee)) < strings.ToLower(clean(b.tx.Payee)) }
	case sortCategory:
		less = func(a, b row) bool { return a.category.Account < b.category.Account }
	}
	sort.SliceStable(view, func(i, j int) bool { return less(m.rows[view[i]], m.rows[view[j]]) })
}

// sortLabel is how an order is called in the popup.
func (m *Model) sortLabel(s sortKind) string {
	switch s {
	case sortOldest:
		return m.loc.T("sort_oldest", nil)
	case sortLargest:
		return m.loc.T("sort_largest", nil)
	case sortSmallest:
		return m.loc.T("sort_smallest", nil)
	case sortPayee:
		return m.loc.T("sort_payee", nil)
	case sortCategory:
		return m.loc.T("sort_category", nil)
	}
	return m.loc.T("sort_newest", nil)
}

// datesLabel is the range of dates the Ledger covers, for the top bar.
func (m *Model) datesLabel() string {
	f := m.tf
	switch {
	case f.from != "" && f.to != "":
		return m.loc.T("dates_between", map[string]any{"From": m.loc.StoredDate(f.from), "To": m.loc.StoredDate(f.to)})
	case f.from != "":
		return m.loc.T("dates_from", map[string]any{"From": m.loc.StoredDate(f.from)})
	}
	return m.loc.T("dates_until", map[string]any{"To": m.loc.StoredDate(f.to)})
}

// ---- the popup --------------------------------------------------------------------

type filterField int

const (
	filterMin filterField = iota
	filterMax
	filterFrom
	filterTo
	filterCategory
	filterSort
	numFilterFields
)

const filterLabelW = 14

type filterForm struct {
	field filterField
	in    [filterSort]textinput.Model // the fields that are typed
	sort  sortKind
	err   string
}

func (m *Model) filterSize() (w, h int) { return min(max(m.width-6, 30), 62), 15 }

func (m *Model) openFilter() tea.Cmd {
	f := &filterForm{sort: m.tf.sort}
	w, _ := m.filterSize()
	for i := range f.in {
		f.in[i] = newInput("")
		f.in[i].Width = max(8, w-2-filterLabelW-1-1) // minus the border, the label, a space and the cursor cell
	}
	f.in[filterMin].SetValue(m.tf.minAmount)
	f.in[filterMax].SetValue(m.tf.maxAmount)
	if m.tf.from != "" {
		f.in[filterFrom].SetValue(m.loc.StoredDate(m.tf.from))
	}
	if m.tf.to != "" {
		f.in[filterTo].SetValue(m.loc.StoredDate(m.tf.to))
	}
	f.in[filterCategory].SetValue(m.tf.category)
	f.in[filterMin].Placeholder = "0"
	f.in[filterFrom].Placeholder = m.loc.Date(m.now().AddDate(0, 0, -30))
	f.in[filterTo].Placeholder = m.loc.Date(m.now())
	f.in[filterCategory].Placeholder = "Groceries"
	m.filt, m.mode = f, modeFilterForm
	return m.focusFilter(filterMin)
}

func (m *Model) focusFilter(field filterField) tea.Cmd {
	f := m.filt
	f.field = field
	for i := range f.in {
		f.in[i].Blur()
	}
	if field == filterSort {
		return nil
	}
	return f.in[field].Focus()
}

func (m *Model) updateFilterForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.filt
	switch key := msg.String(); key {
	case "esc":
		m.mode, m.filt = modeList, nil
		return m, nil
	case "enter", "ctrl+s":
		m.saveFilter()
		return m, nil
	case "ctrl+x":
		m.mode, m.filt = modeList, nil
		m.tf = txFilter{sort: m.tf.sort}
		m.afterFilter(m.loc.T("filter_cleared", nil))
		return m, nil
	case "tab", "down":
		return m, m.focusFilter((f.field + 1) % numFilterFields)
	case "shift+tab", "up":
		return m, m.focusFilter((f.field + numFilterFields - 1) % numFilterFields)
	default:
		if f.field == filterSort {
			switch key {
			case "left", "h":
				f.sort = (f.sort + numSorts - 1) % numSorts
			case "right", "l", " ":
				f.sort = (f.sort + 1) % numSorts
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	f.in[f.field], cmd = f.in[f.field].Update(msg)
	f.err = ""
	return m, cmd
}

// saveFilter checks the fields and applies them. A problem is said in the popup and nothing
// changes.
func (m *Model) saveFilter() {
	f := m.filt
	next := txFilter{
		minAmount: strings.TrimSpace(f.in[filterMin].Value()), maxAmount: strings.TrimSpace(f.in[filterMax].Value()),
		category: clean(f.in[filterCategory].Value()), sort: f.sort,
	}
	var lo, hi int64
	for _, a := range []struct {
		text string
		out  *int64
	}{{next.minAmount, &lo}, {next.maxAmount, &hi}} {
		if a.text == "" {
			continue
		}
		v, err := ledger.ParseAmount(strings.TrimLeft(a.text, "+-−"), "EUR", 0) // the number is read as it is; each transaction applies it in its own currency
		if err != nil {
			f.err = m.loc.T("error_amount", map[string]any{"Error": m.errText(err)})
			return
		}
		*a.out = v.Minor
	}
	if next.minAmount != "" && next.maxAmount != "" && lo > hi {
		f.err = m.loc.T("filter_order_amount", nil)
		return
	}
	for _, d := range []struct {
		text string
		out  *string
	}{{f.in[filterFrom].Value(), &next.from}, {f.in[filterTo].Value(), &next.to}} {
		if strings.TrimSpace(d.text) == "" {
			continue
		}
		t, err := m.loc.ParseDate(d.text)
		if err != nil {
			f.err = m.loc.T("error_date", map[string]any{"Error": m.errText(err)})
			return
		}
		*d.out = t.Format(ledger.DateLayout)
	}
	if next.from != "" && next.to != "" && next.from > next.to {
		f.err = m.loc.T("filter_order_date", nil)
		return
	}
	m.mode, m.filt = modeList, nil
	if next == m.tf {
		return
	}
	sorted, was := next.sort != m.tf.sort, m.tf.active()
	m.tf = next
	if was && !next.active() {
		m.afterFilter(m.loc.T("filter_cleared", nil))
		return
	}
	if next.active() || !sorted {
		m.afterFilter("")
		return
	}
	m.afterFilter(m.loc.T("filter_sorted", map[string]any{"Sort": m.sortLabel(next.sort)}))
}

// afterFilter shows the list as the filter now makes it, and says how many it has.
func (m *Model) afterFilter(note string) {
	m.applyFilter(false)
	if note == "" {
		note = m.loc.T("filter_matches", map[string]any{"Count": len(m.view)})
	}
	m.setStatus(note, false)
}

func (m *Model) filterPopup() string {
	f := m.filt
	w, h := m.filterSize()
	iw := w - 2
	label := func(name string, field filterField) string {
		if f.field == field {
			return accent.Render(fit("▸ "+name, filterLabelW))
		}
		return dim.Render(fit("  "+name, filterLabelW))
	}
	names := [numFilterFields]string{
		m.loc.T("label_min_amount", nil), m.loc.T("label_max_amount", nil), m.loc.T("label_from_date", nil),
		m.loc.T("label_to_date", nil), m.loc.T("label_category", nil), m.loc.T("label_sort", nil),
	}
	var lines []string
	for field := filterMin; field < filterSort; field++ {
		lines = append(lines, label(names[field], field)+" "+f.in[field].View())
	}
	sortText := m.sortLabel(f.sort)
	if f.field == filterSort {
		sortText = "‹ " + sortText + " ›"
	}
	lines = append(lines, label(names[filterSort], filterSort)+" "+sortText, "")
	lines = append(lines, wrapped(m.loc.T("filter_hint", nil), iw, dim)...)
	for len(lines) < h-4 {
		lines = append(lines, "")
	}
	msg := wrapped(f.err, iw, errStyle)
	for len(msg) < 2 {
		msg = append(msg, "")
	}
	lines = append(lines, msg...)
	return box(m.loc.T("filter_title", nil), lines, w, h, true, m.loc.T("filter_footer", nil))
}
