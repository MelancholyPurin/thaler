package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/ledger"
	"github.com/melancholypurin/thaler/internal/store"
)

var ctx = context.Background()

func TestFuzzyScore(t *testing.T) {
	if _, ok := fuzzyScore("grc", "Expenses:Groceries"); !ok {
		t.Error("grc should match Expenses:Groceries")
	}
	if _, ok := fuzzyScore("xyz", "Expenses:Groceries"); ok {
		t.Error("xyz should not match")
	}
	if _, ok := fuzzyScore("GRO", "expenses:groceries"); !ok {
		t.Error("matching must ignore case")
	}
	prefix, _ := fuzzyScore("gro", "Expenses:Groceries")
	scattered, _ := fuzzyScore("gro", "Expenses:Gift-Rooms-Misc")
	if prefix <= scattered {
		t.Errorf("a word-start run should outrank scattered letters: %d vs %d", prefix, scattered)
	}
	if !matchAll("rewe 23", "2026-10-01 REWE Markt -23,47") || matchAll("rewe lidl", "REWE Markt") {
		t.Error("matchAll must require every term")
	}
}

// newModel builds a model on an in-memory store with a few transactions:
// two uncategorized spendings in October, categorized ones in September and
// October, and a transfer. The clock is fixed at 2026-10-22.
func newModel(t *testing.T) (*Model, *store.Store) {
	t.Helper()
	st, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, a := range []string{"Assets:Bank", "Assets:Savings"} {
		if _, err := st.AddAccount(ctx, a, "EUR"); err != nil {
			t.Fatal(err)
		}
	}
	add := func(date, payee, category string, cents int64) {
		t.Helper()
		d, _ := time.Parse(ledger.DateLayout, date)
		_, err := st.AddTransaction(ctx, store.NewTransaction{
			Date: d, Payee: payee,
			Postings: []ledger.Posting{
				{Account: "Assets:Bank", Amount: ledger.Money{Minor: -cents, Currency: "EUR"}},
				{Account: category, Amount: ledger.Money{Minor: cents, Currency: "EUR"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add("2026-09-15", "Edeka", "Expenses:Groceries", 3000)
	add("2026-10-01", "REWE", "Expenses:Groceries", 1000)
	add("2026-10-02", "REWE", ledger.UncategorizedExpense, 2000)
	add("2026-10-03", "Kino", ledger.UncategorizedExpense, 1200)
	d, _ := time.Parse(ledger.DateLayout, "2026-10-04")
	if _, err := st.AddTransaction(ctx, store.NewTransaction{
		Date: d, Payee: "to savings",
		Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: ledger.Money{Minor: -500, Currency: "EUR"}},
			{Account: "Assets:Savings", Amount: ledger.Money{Minor: 500, Currency: "EUR"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m, err := newWithClock(ctx, st, func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	return m, st
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(key(k))
	}
}

func typeText(m *Model, text string) {
	for _, r := range text {
		press(m, string(r))
	}
}

func TestHomeScreen(t *testing.T) {
	m, _ := newModel(t)
	if m.focus != paneHome {
		t.Fatalf("should start on the home tile, focus = %v", m.focus)
	}
	out := m.View()
	for _, want := range []string{banner[0], "Net worth", "-72.00 EUR", "Uncategorized", "[1] Home", "[2] Accounts", "[3] Transactions"} {
		if !strings.Contains(out, want) {
			t.Errorf("home view lacks %q", want)
		}
	}
}

func TestFocusKeys(t *testing.T) {
	m, _ := newModel(t)
	press(m, "3")
	if m.focus != paneTransactions {
		t.Errorf("3 should focus transactions, got %v", m.focus)
	}
	press(m, "tab")
	if m.focus != paneHome {
		t.Errorf("tab from the last panel should wrap to home, got %v", m.focus)
	}
	press(m, "shift+tab")
	if m.focus != paneTransactions {
		t.Errorf("shift+tab should go back, got %v", m.focus)
	}
	press(m, "2")
	press(m, "enter")
	if m.focus != paneTransactions {
		t.Errorf("enter on accounts should open the transactions, got %v", m.focus)
	}
}

func TestAccountScope(t *testing.T) {
	m, _ := newModel(t)
	if len(m.view) != 4 {
		t.Fatalf("all accounts: %d transactions, want 4", len(m.view))
	}
	press(m, "2", "down") // Assets:Bank touches everything
	if m.scopeAccount() != "Assets:Bank" || len(m.view) != 4 {
		t.Errorf("Bank: scope %q, %d transactions", m.scopeAccount(), len(m.view))
	}
	press(m, "down") // Assets:Savings only has the transfer
	if m.scopeAccount() != "Assets:Savings" || len(m.view) != 1 {
		t.Errorf("Savings: scope %q, %d transactions", m.scopeAccount(), len(m.view))
	}
	if out := m.View(); !strings.Contains(out, "Savings") || !strings.Contains(out, "Balance") {
		t.Error("the detail pane should describe the selected account")
	}
	press(m, "g")
	if m.scopeAccount() != "" || len(m.view) != 4 {
		t.Errorf("back on All accounts: scope %q, %d transactions", m.scopeAccount(), len(m.view))
	}
}

func TestUncategorizedToggle(t *testing.T) {
	m, _ := newModel(t)
	press(m, "3", "u")
	if len(m.view) != 2 {
		t.Fatalf("view has %d rows, want the 2 uncategorized ones", len(m.view))
	}
	if r, _ := m.selected(); r.tx.Payee != "Kino" {
		t.Errorf("newest uncategorized row should be selected first, got %q", r.tx.Payee)
	}
	press(m, "u")
	if len(m.view) != 4 {
		t.Errorf("after the second u: %d rows, want all 4", len(m.view))
	}
}

func TestSearch(t *testing.T) {
	m, _ := newModel(t)
	press(m, "/")
	if m.mode != modeFilter || m.focus != paneTransactions {
		t.Fatalf("/ should open the search on the transactions (mode %v, focus %v)", m.mode, m.focus)
	}
	typeText(m, "rew")
	if len(m.view) != 2 {
		t.Errorf("filter 'rew' shows %d rows, want 2", len(m.view))
	}
	press(m, "enter")
	if m.mode != modeList || len(m.view) != 2 {
		t.Errorf("enter keeps the filter: mode %v, %d rows", m.mode, len(m.view))
	}
	press(m, "esc")
	if len(m.view) != 4 {
		t.Errorf("esc should clear the search, %d rows", len(m.view))
	}
}

func TestCategorizeSuggestsSamePayee(t *testing.T) {
	m, st := newModel(t)
	press(m, "3", "u", "down") // Kino -> REWE (#2)
	if r, _ := m.selected(); r.tx.Payee != "REWE" {
		t.Fatalf("selected %q, want REWE", r.tx.Payee)
	}
	press(m, "c")
	if m.mode != modePick || len(m.pickItems) == 0 || m.pickItems[0].name != "Expenses:Groceries" {
		t.Fatalf("picker should suggest Expenses:Groceries first, got %+v", m.pickItems)
	}
	if !strings.Contains(m.View(), "same payee") {
		t.Error("the suggestion should be labelled in the view")
	}
	press(m, "enter")
	if m.mode != modeList || len(m.view) != 1 {
		t.Fatalf("after applying: mode %v, %d uncategorized left, want 1", m.mode, len(m.view))
	}
	txs, _ := st.ListTransactions(ctx, store.TxFilter{Account: "Expenses:Groceries"})
	if len(txs) != 3 { // Edeka in September, REWE in October, and the one just categorized
		t.Errorf("Expenses:Groceries has %d transactions, want 3", len(txs))
	}
}

func TestCategorizeNewCategory(t *testing.T) {
	m, st := newModel(t)
	press(m, "3", "u", "c") // Kino
	typeText(m, "Expenses:Fun")
	last := m.pickItems[len(m.pickItems)-1]
	if !last.create || last.name != "Expenses:Fun" {
		t.Fatalf("typing a new full name should offer to create it, got %+v", m.pickItems)
	}
	press(m, "down", "enter")
	if _, err := st.GetAccount(ctx, "Expenses:Fun"); err != nil {
		t.Errorf("category was not created: %v", err)
	}
	if m.statusIsError {
		t.Errorf("unexpected error: %s", m.status)
	}
}

func TestTransferCannotBeCategorized(t *testing.T) {
	m, _ := newModel(t)
	press(m, "3", "g", "c") // newest row is the transfer
	if m.mode != modeList || !m.statusIsError {
		t.Errorf("a transfer must not open the picker (mode %v, status %q)", m.mode, m.status)
	}
}

func TestScreenKeys(t *testing.T) {
	m, _ := newModel(t)
	for _, c := range []struct {
		key  tea.KeyMsg
		want screen
	}{
		{key("B"), screenBudget}, {key("F"), screenForecast}, {key("R"), screenReports}, {key("L"), screenLedger},
		{tea.KeyMsg{Type: tea.KeyF2}, screenBudget}, {tea.KeyMsg{Type: tea.KeyF4}, screenReports}, {tea.KeyMsg{Type: tea.KeyF1}, screenLedger},
	} {
		m.Update(c.key)
		if m.screen != c.want {
			t.Errorf("%q: screen = %v, want %v", c.key.String(), m.screen, c.want)
		}
	}
	press(m, "F")
	if out := m.View(); !strings.Contains(out, "Not built yet") || !strings.Contains(out, "Forecast") {
		t.Error("planned screens should say they are not built yet")
	}
}

func TestPeriodNavigation(t *testing.T) {
	m, _ := newModel(t)
	if m.period != "2026-10" || len(m.view) != 4 {
		t.Fatalf("starts on the current month: period %q, %d rows", m.period, len(m.view))
	}
	press(m, "[")
	if m.period != "2026-09" || len(m.view) != 1 {
		t.Errorf("[ : period %q, %d rows, want September with 1", m.period, len(m.view))
	}
	if !strings.Contains(m.View(), "Sep 2026") {
		t.Error("the period should be shown in the top strip")
	}
	press(m, "]", "]")
	if m.period != "2026-11" || len(m.view) != 0 {
		t.Errorf("] ] : period %q, %d rows", m.period, len(m.view))
	}
	if out := m.View(); !strings.Contains(out, "No transactions in Nov 2026") {
		t.Error("an empty month should say so")
	}
	press(m, "T")
	if m.period != "" || len(m.view) != 5 {
		t.Errorf("T : period %q, %d rows, want all 5", m.period, len(m.view))
	}
	press(m, "[")
	if m.period != "2026-10" {
		t.Errorf("[ from all time should land on the current month, got %q", m.period)
	}
	press(m, "[", "[", "t")
	if m.period != "2026-10" {
		t.Errorf("t should return to the current month, got %q", m.period)
	}
}

func TestBudgetScreen(t *testing.T) {
	m, _ := newModel(t)
	press(m, "B")
	out := m.View()
	for _, want := range []string{"[1] Months", "Oct 2026", "Spent", "42.00 EUR", "Groceries", "Uncategorized"} {
		if !strings.Contains(out, want) {
			t.Errorf("October budget view lacks %q", want)
		}
	}
	if m.budget.expenseTotal.Minor != 4200 || len(m.budget.expenses) != 2 {
		t.Errorf("October expenses = %+v", m.budget)
	}
	press(m, "down") // months list: All time, Oct, Sep
	if m.period != "2026-09" || m.budget.expenseTotal.Minor != 3000 {
		t.Errorf("down: period %q, spent %d, want September with 3000", m.period, m.budget.expenseTotal.Minor)
	}
	press(m, "g")
	if m.period != "" || m.budget.expenseTotal.Minor != 7200 {
		t.Errorf("g: period %q, spent %d, want all time with 7200", m.period, m.budget.expenseTotal.Minor)
	}
	press(m, "L")
	if len(m.view) != 5 {
		t.Errorf("the period is shared with the ledger: %d rows, want 5", len(m.view))
	}
}

func ctrlU() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlU} }

// txByPayee finds the newest transaction of a payee.
func txByPayee(t *testing.T, st *store.Store, payee string) store.TxView {
	t.Helper()
	txs, err := st.ListTransactions(ctx, store.TxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(txs) - 1; i >= 0; i-- {
		if txs[i].Payee == payee {
			return txs[i]
		}
	}
	t.Fatalf("no transaction from %q", payee)
	return store.TxView{}
}

func TestQuickAddFastPath(t *testing.T) {
	m, st := newModel(t)
	press(m, "a")
	if m.mode != modeForm || m.accounts[m.form.account].account.Name != "Assets:Bank" {
		t.Fatalf("a should open the form on the first account: mode %v", m.mode)
	}
	typeText(m, "8.40")
	press(m, "enter")
	typeText(m, "rew") // completes to the known payee REWE
	if m.form.payeeCur != 0 {
		t.Fatalf("a known payee prefix should be highlighted, items %+v", m.form.payeeItems)
	}
	press(m, "enter") // accept REWE, go to the category
	if m.form.catItems[0].name != "Expenses:Groceries" || m.form.catItems[0].hint != "same payee" {
		t.Fatalf("category should suggest the payee's last category first, got %+v", m.form.catItems)
	}
	press(m, "enter") // save
	if m.mode != modeList {
		t.Fatalf("form should have closed, err %q", m.form.err)
	}
	tx := txByPayee(t, st, "REWE")
	if tx.Date != "2026-10-22" || tx.Postings[0].Amount.Minor != -840 || tx.Postings[1].Account != "Expenses:Groceries" {
		t.Errorf("stored %+v", tx)
	}
	if r, _ := m.selected(); r.tx.ID != tx.ID {
		t.Errorf("the new transaction should be selected, got #%d", r.tx.ID)
	}
	if m.lastAccount != "Assets:Bank" || m.statusIsError || !strings.Contains(m.status, "Added") {
		t.Errorf("status %q, last account %q", m.status, m.lastAccount)
	}
}

func TestQuickAddAccountFollowsSelection(t *testing.T) {
	m, _ := newModel(t)
	press(m, "2", "down", "down") // Assets:Savings
	press(m, "a")
	if got := m.accounts[m.form.account].account.Name; got != "Assets:Savings" {
		t.Errorf("form account = %s, want the selected Assets:Savings", got)
	}
	press(m, "esc", "g", "a") // back on "All accounts": falls back to the last one used
	m.lastAccount = "Assets:Savings"
	press(m, "esc", "a")
	if got := m.accounts[m.form.account].account.Name; got != "Assets:Savings" {
		t.Errorf("form account = %s, want the last used Assets:Savings", got)
	}
}

func TestQuickAddDefaultsToMostRecentAccount(t *testing.T) {
	m, st := newModel(t)
	// The newest transaction in the database was booked on Savings, which is
	// not the first account alphabetically.
	_, err := st.AddTransaction(ctx, store.NewTransaction{
		Date: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), Payee: "Cash tip",
		Postings: []ledger.Posting{
			{Account: "Assets:Savings", Amount: ledger.Money{Minor: -200, Currency: "EUR"}},
			{Account: "Expenses:Tips", Amount: ledger.Money{Minor: 200, Currency: "EUR"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	m.applyFilter(true)
	press(m, "a")
	if got := m.accounts[m.form.account].account.Name; got != "Assets:Savings" {
		t.Errorf("default account = %s, want the most recently used Assets:Savings", got)
	}
}

func TestQuickAddIncomeAndNewCategory(t *testing.T) {
	m, st := newModel(t)
	press(m, "a")
	typeText(m, "+2450")
	press(m, "enter")
	typeText(m, "Muster GmbH")
	press(m, "enter")
	if !m.form.isIncome() {
		t.Fatal("a leading + means income")
	}
	typeText(m, "Income:Salary")
	if last := m.form.catItems[len(m.form.catItems)-1]; !last.create {
		t.Fatalf("a new full name should be offered as a new category: %+v", m.form.catItems)
	}
	press(m, "down", "enter")
	if m.mode != modeList {
		t.Fatalf("not saved: %q", m.form.err)
	}
	tx := txByPayee(t, st, "Muster GmbH")
	if tx.Postings[0].Amount.Minor != 245000 || tx.Postings[1].Account != "Income:Salary" || tx.Postings[1].Amount.Minor != -245000 {
		t.Errorf("stored %+v", tx)
	}
}

func TestQuickAddDefaultsToUncategorized(t *testing.T) {
	m, st := newModel(t)
	press(m, "a")
	typeText(m, "3")
	press(m, "enter")
	typeText(m, "Kiosk")
	press(m, "enter", "down") // category field: the last entry is "decide later"
	for m.form.catItems[m.form.catCur].hint != "decide later" {
		press(m, "down")
	}
	press(m, "enter")
	if tx := txByPayee(t, st, "Kiosk"); tx.Postings[1].Account != ledger.UncategorizedExpense {
		t.Errorf("stored %+v", tx)
	}
}

func TestQuickAddValidation(t *testing.T) {
	m, st := newModel(t)
	before, _ := st.ListTransactions(ctx, store.TxFilter{})
	press(m, "a")
	typeText(m, "abc")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != modeForm || !strings.Contains(m.form.err, "amount") {
		t.Errorf("a bad amount must keep the form open with a message: mode %v err %q", m.mode, m.form.err)
	}
	if out := m.View(); !strings.Contains(out, "amount") {
		t.Error("the error should be visible in the form")
	}
	m.Update(ctrlU())
	typeText(m, "5")
	press(m, "tab", "tab", "tab") // date
	m.Update(ctrlU())
	typeText(m, "31.02.2026")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != modeForm || !strings.Contains(m.form.err, "date") {
		t.Errorf("a bad date must be rejected: err %q", m.form.err)
	}
	press(m, "esc")
	after, _ := st.ListTransactions(ctx, store.TxFilter{})
	if len(after) != len(before) {
		t.Errorf("a rejected or cancelled form must not store anything: %d -> %d", len(before), len(after))
	}
}

func TestParseFormDate(t *testing.T) {
	now := time.Date(2026, 10, 22, 23, 59, 0, 0, time.Local)
	for in, want := range map[string]string{
		"": "2026-10-22", "t": "2026-10-22", "y": "2026-10-21", "-2": "2026-10-20",
		"2026-10-01": "2026-10-01", "01.10.2026": "2026-10-01",
	} {
		got, err := parseFormDate(in, now)
		if err != nil || got.Format(ledger.DateLayout) != want {
			t.Errorf("parseFormDate(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	if _, err := parseFormDate("someday", now); err == nil {
		t.Error("garbage must be rejected")
	}
}

func TestEditKeepsCategoryAndID(t *testing.T) {
	m, st := newModel(t)
	press(m, "3", "u", "e") // Kino, uncategorized
	if m.mode != modeForm || m.form.amount.Value() != "12.00" || m.form.payee.Value() != "Kino" {
		t.Fatalf("edit form should be prefilled: %q %q", m.form.amount.Value(), m.form.payee.Value())
	}
	id := m.form.editing
	m.Update(ctrlU())
	typeText(m, "15.50")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.mode != modeList {
		t.Fatalf("not saved: %q", m.form.err)
	}
	got, _ := st.GetTransaction(ctx, id)
	if got.Postings[0].Amount.Minor != -1550 || got.Postings[1].Account != ledger.UncategorizedExpense {
		t.Errorf("after edit: %+v (the category must stay as it was)", got)
	}

	press(m, "z")
	back, _ := st.GetTransaction(ctx, id)
	if back.Postings[0].Amount.Minor != -1200 {
		t.Errorf("undo should restore the old amount, got %+v", back)
	}
}

func TestDeleteAndUndo(t *testing.T) {
	m, st := newModel(t)
	press(m, "3", "u") // newest uncategorized: Kino
	r, _ := m.selected()
	press(m, "d")
	if m.mode != modeConfirm || !strings.Contains(m.View(), "Delete") {
		t.Fatalf("d should ask first, mode %v", m.mode)
	}
	press(m, "n")
	if _, err := st.GetTransaction(ctx, r.tx.ID); err != nil {
		t.Fatal("cancelling must keep the transaction")
	}
	press(m, "d", "y")
	if _, err := st.GetTransaction(ctx, r.tx.ID); err == nil {
		t.Fatal("y should delete")
	}
	if len(m.view) != 1 {
		t.Errorf("the list should update: %d rows", len(m.view))
	}
	press(m, "z")
	back, err := st.GetTransaction(ctx, r.tx.ID)
	if err != nil || back.Payee != "Kino" || len(m.view) != 2 {
		t.Errorf("undo should bring #%d back: %+v, %v, %d rows", r.tx.ID, back, err, len(m.view))
	}
	press(m, "z")
	if !m.statusIsError {
		t.Error("with nothing left to undo it should say so")
	}
}

func TestUndoStack(t *testing.T) {
	m, st := newModel(t)
	press(m, "3", "u", "c") // categorize Kino as Groceries, the first suggestion
	press(m, "enter")
	kino := txByPayee(t, st, "Kino")
	if kino.Postings[1].Account == ledger.UncategorizedExpense {
		t.Fatal("setup: Kino should be categorized")
	}
	press(m, "a")
	typeText(m, "2")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	before, _ := st.ListTransactions(ctx, store.TxFilter{})
	press(m, "z") // undo the add
	after, _ := st.ListTransactions(ctx, store.TxFilter{})
	if len(after) != len(before)-1 {
		t.Errorf("undo of an add: %d -> %d transactions", len(before), len(after))
	}
	press(m, "z") // undo the categorization
	if k := txByPayee(t, st, "Kino"); k.Postings[1].Account != ledger.UncategorizedExpense {
		t.Errorf("undo of a categorization: %+v", k)
	}
}

func TestViewFitsTerminal(t *testing.T) {
	m, _ := newModel(t)
	sizes := [][2]int{{120, 36}, {100, 20}, {80, 14}, {60, 24}, {40, 8}, {200, 60}}
	check := func(label string, w, h int) {
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) > h {
			t.Errorf("%s %dx%d: view has %d lines", label, w, h, len(lines))
		}
		for _, line := range lines {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("%s %dx%d: line is %d cells wide: %q", label, w, h, lw, line)
			}
		}
	}
	for _, size := range sizes {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, scr := range []string{"B", "F", "R", "L"} {
			press(m, scr)
			check("screen "+scr, size[0], size[1])
		}
		for _, focus := range []string{"1", "2", "3"} {
			press(m, focus)
			check("focus "+focus, size[0], size[1])
		}
		press(m, "c")
		check("picker", size[0], size[1])
		press(m, "esc", "?")
		check("help", size[0], size[1])
		press(m, "x", "a")
		check("form", size[0], size[1])
		press(m, "esc", "3", "d")
		check("confirm", size[0], size[1])
		press(m, "n")
		press(m, "x", "/", "r")
		check("search", size[0], size[1])
		press(m, "esc")
	}
}
