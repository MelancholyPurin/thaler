package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
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

func testConfig() (*config.Config, error) {
	return &config.Config{Rules: []importer.Rule{{Contains: "rewe", Category: "Expenses:Groceries"}}}, nil
}

// newModel builds a model on an in-memory store with a few transactions:
// two uncategorized spendings in October, categorized ones in September and
// October, and a transfer. The clock is fixed at 2026-10-22.
func newModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	st, err := core.Open(ctx, ":memory:")
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
		_, err := st.AddTransaction(ctx, core.NewTransaction{
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
	if _, err := st.AddTransaction(ctx, core.NewTransaction{
		Date: d, Payee: "to savings",
		Postings: []ledger.Posting{
			{Account: "Assets:Bank", Amount: ledger.Money{Minor: -500, Currency: "EUR"}},
			{Account: "Assets:Savings", Amount: ledger.Money{Minor: 500, Currency: "EUR"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m, err := newWithClock(ctx, st, Options{LoadConfig: testConfig}, func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) })
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
	txs, _ := st.ListTransactions(ctx, core.TxFilter{Account: "Expenses:Groceries"})
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
	if m.budget.ExpenseTotal.Minor != 4200 || len(m.budget.Expenses) != 2 {
		t.Errorf("October expenses = %+v", m.budget)
	}
	press(m, "down") // months list: All time, Oct, Sep
	if m.period != "2026-09" || m.budget.ExpenseTotal.Minor != 3000 {
		t.Errorf("down: period %q, spent %d, want September with 3000", m.period, m.budget.ExpenseTotal.Minor)
	}
	press(m, "g")
	if m.period != "" || m.budget.ExpenseTotal.Minor != 7200 {
		t.Errorf("g: period %q, spent %d, want all time with 7200", m.period, m.budget.ExpenseTotal.Minor)
	}
	press(m, "L")
	if len(m.view) != 5 {
		t.Errorf("the period is shared with the ledger: %d rows, want 5", len(m.view))
	}
}

func ctrlU() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlU} }

// txByPayee finds the newest transaction of a payee.
func txByPayee(t *testing.T, st core.Service, payee string) core.TxView {
	t.Helper()
	txs, err := st.ListTransactions(ctx, core.TxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(txs) - 1; i >= 0; i-- {
		if txs[i].Payee == payee {
			return txs[i]
		}
	}
	t.Fatalf("no transaction from %q", payee)
	return core.TxView{}
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
	_, err := st.AddTransaction(ctx, core.NewTransaction{
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
	before, _ := st.ListTransactions(ctx, core.TxFilter{})
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
	after, _ := st.ListTransactions(ctx, core.TxFilter{})
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
	before, _ := st.ListTransactions(ctx, core.TxFilter{})
	press(m, "z") // undo the add
	after, _ := st.ListTransactions(ctx, core.TxFilter{})
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

// ---- import ------------------------------------------------------------------

// exportCSV is a Sparkasse-style export: one groceries row the test rules
// know, two they do not, and one pending row that must be ignored.
const exportCSV = `Buchungstag;Verwendungszweck;Beguenstigter/Zahlungspflichtiger;Betrag;Waehrung;Info
02.10.26;Einkauf;REWE Markt;-23,47;EUR;Umsatz gebucht
03.10.26;;Mystery Shop;-5,00;EUR;Umsatz gebucht
04.10.26;Gehalt;Arbeitgeber;1.000,00;EUR;Umsatz gebucht
05.10.26;;Pending Shop;-9,99;EUR;Umsatz vorgemerkt
`

// writeExport puts a CSV file in a fresh folder and points the model's
// downloads folder at it.
func writeExport(t *testing.T, m *Model, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	m.downloads = dir
	return filepath.Join(dir, name)
}

func txCount(t *testing.T, st core.Service) int {
	t.Helper()
	txs, err := st.ListTransactions(ctx, core.TxFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return len(txs)
}

func TestImportFlow(t *testing.T) {
	m, st := newModel(t)
	writeExport(t, m, "umsaetze.csv", exportCSV)
	before := txCount(t, st)

	press(m, "i")
	if m.mode != modeImport {
		t.Fatalf("i should open the import popup, mode = %v", m.mode)
	}
	view := m.View()
	for _, want := range []string{"Import a bank export", "umsaetze.csv", "Assets:Bank", "sparkasse"} {
		if !strings.Contains(view, want) {
			t.Errorf("choose step is missing %q:\n%s", want, view)
		}
	}

	press(m, "enter") // the newest download is already highlighted
	view = m.View()
	for _, want := range []string{"3 new", "0 already imported", "2 uncategorized", "1 pending ignored", "Groceries", "REWE Markt"} {
		if !strings.Contains(view, want) {
			t.Errorf("review step is missing %q:\n%s", want, view)
		}
	}
	if got := txCount(t, st); got != before {
		t.Fatalf("reviewing wrote %d transactions", got-before)
	}

	press(m, "enter")
	if m.mode != modeList || m.imp != nil {
		t.Fatalf("confirming should close the popup, mode = %v", m.mode)
	}
	if got := txCount(t, st); got != before+3 {
		t.Fatalf("transactions = %d, want %d", got, before+3)
	}
	if !strings.HasPrefix(m.status, "Imported 3 into Bank") || !strings.Contains(m.status, "2 need a category") {
		t.Errorf("status = %q", m.status)
	}
	if m.focus != paneTransactions || m.scopeAccount() != "Assets:Bank" || !m.onlyUncat {
		t.Errorf("should show the uncategorized rows of the account: focus %v, scope %q, onlyUncat %v", m.focus, m.scopeAccount(), m.onlyUncat)
	}
	if _, cat, _ := txByPayee(t, st, "REWE Markt").Legs(); cat.Account != "Expenses:Groceries" {
		t.Errorf("the rule should have categorised REWE, got %s", cat.Account)
	}

	press(m, "z")
	if got := txCount(t, st); got != before {
		t.Errorf("undo left %d imported transactions", got-before)
	}
	if !strings.Contains(m.status, "the import of 3 transactions") {
		t.Errorf("status after undo = %q", m.status)
	}
}

func TestImportAgainFindsNothingNew(t *testing.T) {
	m, st := newModel(t)
	writeExport(t, m, "umsaetze.csv", exportCSV)
	press(m, "i", "enter", "enter")
	imported := txCount(t, st)
	undos := len(m.undo)

	press(m, "i", "enter")
	if view := m.View(); !strings.Contains(view, "0 new") || !strings.Contains(view, "3 already imported") {
		t.Errorf("second review:\n%s", view)
	}
	press(m, "enter")
	if m.mode != modeList || !strings.Contains(m.status, "Nothing new") {
		t.Errorf("mode %v, status %q", m.mode, m.status)
	}
	if txCount(t, st) != imported || len(m.undo) != undos {
		t.Error("importing a file twice must change nothing and leave nothing to undo")
	}
}

func TestImportWithProblems(t *testing.T) {
	m, st := newModel(t)
	writeExport(t, m, "umsaetze.csv", exportCSV+"07.10.26;;Broken;abc;EUR;Umsatz gebucht\n")
	before := txCount(t, st)

	press(m, "i", "enter")
	if view := m.View(); !strings.Contains(view, "1 with problems") || !strings.Contains(view, "line 6") {
		t.Errorf("review should list the broken row:\n%s", view)
	}
	press(m, "enter")
	if m.mode != modeImport || txCount(t, st) != before || !strings.Contains(m.imp.err, "press y") {
		t.Fatalf("enter must not import past problems: mode %v, err %q", m.mode, m.imp.err)
	}
	press(m, "y")
	if m.mode != modeList || txCount(t, st) != before+3 {
		t.Errorf("y should import the readable rows: mode %v, imported %d", m.mode, txCount(t, st)-before)
	}
}

func TestImportTypedPathAndCancel(t *testing.T) {
	m, st := newModel(t)
	path := writeExport(t, m, "my export.csv", exportCSV)
	m.downloads = t.TempDir() // nothing to pick from: the path has to be typed
	before := txCount(t, st)

	press(m, "i", "enter")
	if m.mode != modeImport || m.imp.step != stepChoose || !strings.Contains(m.imp.err, "choose a CSV file") {
		t.Fatalf("without a file enter must complain: step %v, err %q", m.imp.step, m.imp.err)
	}
	// A dropped file arrives with its spaces escaped.
	typeText(m, strings.ReplaceAll(path, " ", `\ `))
	press(m, "enter")
	if m.imp.step != stepReview || m.imp.res.Inserted != 3 {
		t.Fatalf("typed path: step %v, err %q", m.imp.step, m.imp.err)
	}
	press(m, "esc")
	if m.imp.step != stepChoose {
		t.Error("esc in the review should go back to the file")
	}
	press(m, "esc")
	if m.mode != modeList || txCount(t, st) != before {
		t.Errorf("cancelling must write nothing: mode %v", m.mode)
	}

	press(m, "i")
	typeText(m, "/no/such/file.csv")
	press(m, "enter")
	if !strings.Contains(m.imp.err, "no such file") {
		t.Errorf("err = %q", m.imp.err)
	}
}

func TestImportPicksAccountAndWidensPeriod(t *testing.T) {
	m, st := newModel(t)
	writeExport(t, m, "older.csv", "Buchungstag;Beguenstigter/Zahlungspflichtiger;Betrag;Waehrung;Info\n15.09.26;Bakery;-3,00;EUR;Umsatz gebucht\n")

	press(m, "2", "down", "down") // Assets:Savings in the Accounts panel
	press(m, "i")
	if !strings.Contains(m.View(), "Assets:Savings") {
		t.Fatalf("the selected account should be the default:\n%s", m.View())
	}
	press(m, "enter", "enter")
	if m.period != "" {
		t.Errorf("a September row is outside October; period = %q, want all time", m.period)
	}
	if m.scopeAccount() != "Assets:Savings" {
		t.Errorf("scope = %q", m.scopeAccount())
	}
	tx := txByPayee(t, st, "Bakery")
	if src, _, _ := tx.Legs(); src.Account != "Assets:Savings" {
		t.Errorf("imported into %s", src.Account)
	}
}

func TestImportNeedsAGoodConfig(t *testing.T) {
	m, _ := newModel(t)
	m.loadConfig = func() (*config.Config, error) { return nil, errors.New("config.toml: unknown key(s): [rule.typo]") }
	press(m, "i")
	if m.mode != modeList || !m.statusIsError || !strings.Contains(m.status, "unknown key") {
		t.Errorf("a broken config must be reported, not ignored: mode %v, status %q", m.mode, m.status)
	}
}

func TestCleanPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	for in, want := range map[string]string{
		"/tmp/a.csv":           "/tmp/a.csv",
		"  /tmp/a.csv \n":      "/tmp/a.csv",
		`/tmp/my\ export.csv`:  "/tmp/my export.csv",
		`"/tmp/my export.csv"`: "/tmp/my export.csv",
		`'/tmp/my export.csv'`: "/tmp/my export.csv",
		"~/Downloads/a.csv":    filepath.Join(home, "Downloads/a.csv"),
		"umsaetze":             "umsaetze",
	} {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// emptyModel is a model on a ledger that has one account and nothing else.
func emptyModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	st, err := core.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.AddAccount(ctx, "Assets:Bank", "EUR"); err != nil {
		t.Fatal(err)
	}
	m, err := newWithClock(ctx, st, Options{LoadConfig: testConfig}, func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	return m, st
}

// Undoing an import, or deleting the last transaction, leaves the list empty
// while the view still points into the old rows. That must not crash.
func TestEmptyingTheLedgerKeepsTheListUsable(t *testing.T) {
	m, st := emptyModel(t)
	writeExport(t, m, "umsaetze.csv", exportCSV)
	press(m, "i", "enter", "enter", "z")
	if txCount(t, st) != 0 || len(m.rows) != 0 {
		t.Fatalf("undo should empty the ledger: %d transactions", txCount(t, st))
	}
	if !strings.Contains(m.status, "Undid the import") {
		t.Errorf("status = %q", m.status)
	}

	press(m, "u") // the import left "only uncategorized" on
	press(m, "a")
	typeText(m, "5")
	press(m, "enter")
	typeText(m, "Bakery")
	press(m, "enter", "enter")
	if txCount(t, st) != 1 {
		t.Fatalf("adding a transaction: %d stored (mode %v, status %q)", txCount(t, st), m.mode, m.status)
	}
	press(m, "3", "d", "y")
	if txCount(t, st) != 0 || len(m.view) != 0 {
		t.Errorf("deleting the only transaction: %d stored, %d listed", txCount(t, st), len(m.view))
	}
	_ = m.View()
}

func TestImportFolderFromConfig(t *testing.T) {
	m, st := newModel(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "bank"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "bank", "konto.csv"), []byte(exportCSV), 0o600); err != nil {
		t.Fatal(err)
	}
	m.downloads = t.TempDir() // empty: only the configured folder has a file
	m.loadConfig = func() (*config.Config, error) { return &config.Config{ImportDir: "~/bank"}, nil }

	press(m, "i")
	if view := m.View(); !strings.Contains(view, "konto.csv") || !strings.Contains(view, "~/bank") {
		t.Fatalf("the configured folder should be listed:\n%s", view)
	}
	before := txCount(t, st)
	press(m, "enter", "enter")
	if txCount(t, st) != before+3 {
		t.Errorf("imported %d, want 3", txCount(t, st)-before)
	}
}

func TestImportBrowsesATypedFolder(t *testing.T) {
	m, st := newModel(t)
	other := t.TempDir()
	for name, content := range map[string]string{"alt.csv": exportCSV, "notes.txt": "not a bank export"} {
		if err := os.WriteFile(filepath.Join(other, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	empty := t.TempDir()
	m.downloads = t.TempDir()

	press(m, "i")
	typeText(m, empty)
	if view := m.View(); !strings.Contains(view, "No CSV files in") {
		t.Errorf("an empty folder should say so:\n%s", view)
	}
	press(m, "enter")
	if !strings.Contains(m.imp.err, "choose a CSV file") {
		t.Errorf("enter on an empty folder must not try to read the folder: err %q", m.imp.err)
	}

	m.imp.file.SetValue(other + "/")
	m.imp.refreshFiles()
	view := m.View()
	if !strings.Contains(view, "alt.csv") || strings.Contains(view, "notes.txt") {
		t.Fatalf("the typed folder's CSV files should be listed:\n%s", view)
	}
	before := txCount(t, st)
	press(m, "enter") // the highlighted file, not the folder
	if m.imp.step != stepReview || m.imp.res.Inserted != 3 {
		t.Fatalf("step %v, err %q", m.imp.step, m.imp.err)
	}
	press(m, "enter")
	if txCount(t, st) != before+3 {
		t.Errorf("imported %d, want 3", txCount(t, st)-before)
	}
}

// ---- accounts ----------------------------------------------------------------

var (
	keyCtrlS = tea.KeyMsg{Type: tea.KeyCtrlS}
	keyRight = tea.KeyMsg{Type: tea.KeyRight}
)

// bankBalance is what the ledger holds on an account, in minor units.
func bankBalance(t *testing.T, st core.Service, account string) int64 {
	t.Helper()
	report, err := st.BalanceReport(ctx, core.BalanceRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range report.Lines {
		if l.Account == account {
			return l.Amount.Minor
		}
	}
	return 0
}

func noAccountsModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	st, err := core.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m, err := newWithClock(ctx, st, Options{LoadConfig: testConfig}, func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return m, st
}

func TestCreateAccountWithBalance(t *testing.T) {
	m, st := noAccountsModel(t)
	press(m, "a")
	if !strings.Contains(m.status, "press n") {
		t.Errorf("with no accounts the add form should point at n, status %q", m.status)
	}

	press(m, "n")
	if m.mode != modeAccount {
		t.Fatalf("n should open the account popup, mode = %v", m.mode)
	}
	typeText(m, "Sparkasse")
	if view := m.View(); !strings.Contains(view, "will be called Assets:Sparkasse") {
		t.Errorf("the popup should show the full name:\n%s", view)
	}
	press(m, "enter", "enter", "enter") // type, currency (EUR is the base), balance
	typeText(m, "1234.56")
	press(m, "enter", "enter") // date (today), save
	if m.mode != modeList {
		t.Fatalf("popup still open: mode %v, err %q", m.mode, m.acct.err)
	}

	acc, err := st.GetAccount(ctx, "Assets:Sparkasse")
	if err != nil || acc.Currency != "EUR" {
		t.Fatalf("account = %+v, %v", acc, err)
	}
	if got := bankBalance(t, st, "Assets:Sparkasse"); got != 123456 {
		t.Errorf("balance = %d, want 123456", got)
	}
	if m.scopeAccount() != "Assets:Sparkasse" {
		t.Errorf("the new account should be selected, scope %q", m.scopeAccount())
	}
	for _, want := range []string{"Created Assets:Sparkasse (EUR)", "opening balance of 1234.56", "2026-10-22", "i imports"} {
		if !strings.Contains(m.status, want) {
			t.Errorf("status %q is missing %q", m.status, want)
		}
	}

	// z takes back the balance; the account stays.
	press(m, "z")
	if bankBalance(t, st, "Assets:Sparkasse") != 0 {
		t.Error("undo should remove the opening balance")
	}
	if _, err := st.GetAccount(ctx, "Assets:Sparkasse"); err != nil {
		t.Error("undo must not delete the account")
	}
}

func TestCreateLiabilityAccount(t *testing.T) {
	m, st := noAccountsModel(t)
	press(m, "n")
	typeText(m, "Card")
	press(m, "enter")
	m.Update(keyRight) // Assets → Liabilities
	press(m, "enter", "enter")
	if view := m.View(); !strings.Contains(view, "the amount you owe") {
		t.Errorf("a liability should ask for the amount owed:\n%s", view)
	}
	typeText(m, "500")
	press(m, "enter", "enter")
	if bankBalance(t, st, "Liabilities:Card") != -50000 {
		t.Errorf("500 owed should be -50000 in the ledger, got %d (status %q)", bankBalance(t, st, "Liabilities:Card"), m.status)
	}
}

func TestCreateAccountWithoutBalance(t *testing.T) {
	m, st := noAccountsModel(t)
	press(m, "n")
	typeText(m, "Cash")
	m.Update(keyCtrlS)
	if m.mode != modeList {
		t.Fatalf("ctrl+s should save: err %q", m.acct.err)
	}
	if _, err := st.GetAccount(ctx, "Assets:Cash"); err != nil {
		t.Error(err)
	}
	if txs, _ := st.ListTransactions(ctx, core.TxFilter{}); len(txs) != 0 {
		t.Errorf("no balance given, yet %d transactions were booked", len(txs))
	}
}

func TestCreateAccountRejectsBadInput(t *testing.T) {
	m, st := noAccountsModel(t)
	if _, err := st.AddAccount(ctx, "Assets:Cash", "EUR"); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]struct{ name, currency, balance, want string }{
		"no name":         {"", "EUR", "", "name"},
		"duplicate":       {"Cash", "EUR", "", "already exists"},
		"bad currency":    {"Bank", "E1", "", "currency"},
		"bad balance":     {"Bank", "EUR", "lots", "balance"},
		"a category here": {"Expenses:Food", "EUR", "", "Assets and Liabilities"},
	} {
		press(m, "n")
		m.acct.name.SetValue(input.name)
		m.acct.currency.SetValue(input.currency)
		m.acct.balance.SetValue(input.balance)
		m.Update(keyCtrlS)
		if m.mode != modeAccount {
			t.Errorf("%s: the popup closed (status %q); want an error mentioning %q", name, m.status, input.want)
			continue
		}
		if !strings.Contains(m.acct.err, input.want) {
			t.Errorf("%s: err %q; want it to mention %q", name, m.acct.err, input.want)
		}
		press(m, "esc")
	}
	accs, _ := st.ListAccounts(ctx)
	if len(accs) != 1 {
		t.Errorf("%d accounts, want only the one that existed", len(accs))
	}
}

func TestSetBalanceOfAnAccount(t *testing.T) {
	m, st := newModel(t)
	press(m, "b")
	if m.mode == modeAccount || !m.statusIsError {
		t.Fatalf("with no account selected b must refuse: mode %v, status %q", m.mode, m.status)
	}

	press(m, "2", "down") // Assets:Bank
	press(m, "b")
	if view := m.View(); m.mode != modeAccount || !strings.Contains(view, "Set balance · Assets:Bank") || !strings.Contains(view, "Thaler currently holds") {
		t.Fatalf("balance popup:\n%s", view)
	}
	typeText(m, "500")
	press(m, "enter", "enter")
	if m.mode != modeList {
		t.Fatalf("err %q", m.acct.err)
	}
	if got := bankBalance(t, st, "Assets:Bank"); got != 50000 {
		t.Errorf("balance = %d, want 50000", got)
	}
	opening, _ := st.ListTransactions(ctx, core.TxFilter{Account: ledger.OpeningBalanceAccount})
	if len(opening) != 1 || opening[0].Date >= "2026-09-15" {
		t.Fatalf("opening balance should come before the first transaction (2026-09-15): %+v", opening)
	}

	// A second reading corrects the same transaction, and z restores the first.
	press(m, "b")
	typeText(m, "700")
	press(m, "enter", "enter")
	if got := bankBalance(t, st, "Assets:Bank"); got != 70000 {
		t.Errorf("balance = %d, want 70000", got)
	}
	if again, _ := st.ListTransactions(ctx, core.TxFilter{Account: ledger.OpeningBalanceAccount}); len(again) != 1 {
		t.Errorf("%d opening balances, want one", len(again))
	}
	press(m, "z")
	if got := bankBalance(t, st, "Assets:Bank"); got != 50000 {
		t.Errorf("after undo balance = %d, want 50000", got)
	}
	press(m, "z")
	if got := bankBalance(t, st, "Assets:Bank"); got == 50000 {
		t.Errorf("the second undo should remove the opening balance, balance %d", got)
	}
}

func TestAccountPopupCancels(t *testing.T) {
	m, st := noAccountsModel(t)
	press(m, "n")
	typeText(m, "Cash")
	press(m, "esc")
	if accs, _ := st.ListAccounts(ctx); m.mode != modeList || len(accs) != 0 {
		t.Errorf("esc must write nothing: mode %v, %d accounts", m.mode, len(accs))
	}
}

// ---- config ------------------------------------------------------------------

func TestEditorCommand(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	none := func(string) (string, error) { return "", errors.New("not found") }
	only := func(name string) func(string) (string, error) {
		return func(n string) (string, error) {
			if n == name {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("not found")
		}
	}
	for name, tc := range map[string]struct {
		vars map[string]string
		look func(string) (string, error)
		want []string // nil: an error
	}{
		"visual wins":    {map[string]string{"VISUAL": "hx", "EDITOR": "nano"}, none, []string{"hx", "/c.toml"}},
		"editor":         {map[string]string{"EDITOR": "nano"}, none, []string{"nano", "/c.toml"}},
		"with arguments": {map[string]string{"EDITOR": "code --wait"}, none, []string{"code", "--wait", "/c.toml"}},
		"blank is unset": {map[string]string{"VISUAL": "  ", "EDITOR": "vim"}, none, []string{"vim", "/c.toml"}},
		"falls back":     {nil, only("vi"), []string{"vi", "/c.toml"}},
		"prefers nano":   {nil, func(string) (string, error) { return "x", nil }, []string{"nano", "/c.toml"}},
		"nothing at all": {nil, none, nil},
	} {
		cmd, err := editorCommand(env(tc.vars), tc.look, "/c.toml")
		switch {
		case tc.want == nil && err == nil:
			t.Errorf("%s: want an error, got %v", name, cmd.Args)
		case tc.want == nil && !strings.Contains(err.Error(), "$EDITOR"):
			t.Errorf("%s: the error should say what to set, got %v", name, err)
		case tc.want != nil && (err != nil || strings.Join(cmd.Args, " ") != strings.Join(tc.want, " ")):
			t.Errorf("%s: got %v, %v; want %v", name, cmd, err, tc.want)
		}
	}
}

func TestConfigKeyOpensTheEditor(t *testing.T) {
	m, _ := newModel(t)
	path := filepath.Join(t.TempDir(), "thaler", "config.toml")
	m.opts = Options{ConfigPath: path, ConfigTemplate: "[[rule]]\ncontains = \"x\"\ncategory = \"Expenses:X\"\n"}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")

	for _, screenKey := range []string{"L", "B", "F", "R"} { // every screen
		press(m, screenKey)
		_ = os.Remove(path)
		_, cmd := m.Update(key("C"))
		if cmd == nil {
			t.Fatalf("screen %v: C should start the editor, status %q", m.screen, m.status)
		}
		body, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(body), "# contains") || strings.Contains(string(body), "\ncontains") {
			t.Errorf("the file should exist with the sample switched off: %q, %v", body, err)
		}
	}

	// An existing file is left alone.
	if err := os.WriteFile(path, []byte("# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.Update(key("C"))
	if body, _ := os.ReadFile(path); string(body) != "# mine\n" {
		t.Errorf("C overwrote the config: %q", body)
	}
}

func TestConfigKeyWithoutAnEditor(t *testing.T) {
	m, _ := newModel(t)
	m.opts = Options{ConfigPath: filepath.Join(t.TempDir(), "config.toml")}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("PATH", "")
	if _, cmd := m.Update(key("C")); cmd != nil || !m.statusIsError || !strings.Contains(m.status, "$EDITOR") {
		t.Errorf("cmd %v, status %q", cmd, m.status)
	}
}

func TestAfterEditingTheConfig(t *testing.T) {
	m, _ := newModel(t)
	m.loadConfig = func() (*config.Config, error) {
		return &config.Config{Rules: make([]importer.Rule, 3), Profiles: make([]importer.Profile, 1)}, nil
	}
	m.Update(configEditedMsg{})
	if m.statusIsError || !strings.Contains(m.status, "3 rule(s), 1 bank profile(s)") {
		t.Errorf("status = %q", m.status)
	}
	m.loadConfig = func() (*config.Config, error) { return nil, errors.New("config.toml: unknown key(s): [rule.typo]") }
	m.Update(configEditedMsg{})
	if !m.statusIsError || !strings.Contains(m.status, "unknown key") {
		t.Errorf("a mistake must be reported: %q", m.status)
	}
	m.Update(configEditedMsg{err: errors.New("exit status 1")})
	if !m.statusIsError || !strings.Contains(m.status, "editor failed") {
		t.Errorf("status = %q", m.status)
	}
}

func TestConfigKeyIsPlainTextInAForm(t *testing.T) {
	m, _ := newModel(t)
	m.opts = Options{ConfigPath: filepath.Join(t.TempDir(), "config.toml")}
	press(m, "a", "C")
	if m.mode != modeForm || m.form.amount.Value() != "C" {
		t.Errorf("the letter belongs to the form: mode %v, amount %q", m.mode, m.form.amount.Value())
	}
	if _, err := os.Stat(m.opts.ConfigPath); err == nil {
		t.Error("typing in a form must not create the config")
	}
}
