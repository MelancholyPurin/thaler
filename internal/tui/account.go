package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// One popup creates an account (n) and sets the balance of an existing one (b).
// The balance is what your bank shows on a day; the service books whatever the
// account's transactions do not explain as its opening balance.

type acctField int

const (
	acctName acctField = iota
	acctKind
	acctCurrency
	acctBalance
	acctAsOf
	numAcctFields
)

type accountForm struct {
	setting  bool         // only the balance of an existing account is being set
	existing core.Account // that account
	field    acctField
	liab     bool // creating a Liabilities account, else Assets

	name, currency, balance, asOf textinput.Model
	err                           string
}

// fields are the fields shown, in tab order.
func (f *accountForm) fields() []acctField {
	if f.setting {
		return []acctField{acctBalance, acctAsOf}
	}
	return []acctField{acctName, acctKind, acctCurrency, acctBalance, acctAsOf}
}

// accountName is the full name the account will have: "Sparkasse" typed on the
// Assets line is Assets:Sparkasse. A name that already starts with Assets: or
// Liabilities: is taken as it is, and so is one that starts with another type,
// which is then refused instead of silently becoming Assets:Expenses:Food.
func (f *accountForm) accountName() string {
	text := strings.TrimSpace(f.name.Value())
	if _, err := ledger.TypeOf(text); err == nil && strings.Contains(text, ":") {
		return text
	}
	prefix := "Assets"
	if f.liab {
		prefix = "Liabilities"
	}
	return prefix + ":" + text
}

func (m *Model) accountWidth() int {
	w, _ := m.accountSize()
	return max(8, w-2-12-1) // minus the border, the label and the cursor cell
}

func (m *Model) accountSize() (w, h int) { return min(max(m.width-6, 30), 64), 14 }

// openAccountForm starts creating an account, or, with setting, setting the
// balance of the account selected in the Accounts panel.
func (m *Model) openAccountForm(setting bool) tea.Cmd {
	f := &accountForm{setting: setting}
	f.name, f.currency = newInput(""), newInput("")
	f.balance, f.asOf = newInput(""), newInput(m.now().Format(ledger.DateLayout))
	f.name.Placeholder = "Sparkasse"
	f.currency.CharLimit = 3
	f.currency.SetValue(string(m.netWorth.Base))
	f.balance.Placeholder = "what your bank shows"
	if setting {
		i := m.accts.cursor - 1
		if i < 0 || i >= len(m.accounts) {
			m.setStatus("select an account first: press 2, then ↑/↓", true)
			return nil
		}
		f.existing = m.accounts[i].account
	}
	for _, in := range []*textinput.Model{&f.name, &f.currency, &f.balance, &f.asOf} {
		in.Width = m.accountWidth()
	}
	m.acct = f
	m.mode = modeAccount
	return m.focusAccountField(f.fields()[0])
}

func (m *Model) focusAccountField(field acctField) tea.Cmd {
	f := m.acct
	f.field = field
	for _, in := range []*textinput.Model{&f.name, &f.currency, &f.balance, &f.asOf} {
		in.Blur()
	}
	switch field {
	case acctName:
		return f.name.Focus()
	case acctCurrency:
		return f.currency.Focus()
	case acctBalance:
		return f.balance.Focus()
	case acctAsOf:
		return f.asOf.Focus()
	}
	return nil
}

func (m *Model) moveAccountField(d int) tea.Cmd {
	f := m.acct
	fields := f.fields()
	cur := 0
	for i, x := range fields {
		if x == f.field {
			cur = i
		}
	}
	return m.focusAccountField(fields[(cur+d+len(fields))%len(fields)])
}

func (m *Model) updateAccount(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.acct
	key := msg.String()
	switch key {
	case "esc":
		m.mode, m.acct = modeList, nil
		return m, nil
	case "tab":
		return m, m.moveAccountField(1)
	case "shift+tab":
		return m, m.moveAccountField(-1)
	case "ctrl+s":
		m.saveAccountForm()
		return m, nil
	case "enter":
		if f.field == acctAsOf {
			m.saveAccountForm()
			return m, nil
		}
		return m, m.moveAccountField(1)
	}
	if f.field == acctKind {
		switch key {
		case "left", "right", "up", "down", " ":
			f.liab = !f.liab
		}
		return m, nil
	}
	var cmd tea.Cmd
	switch f.field {
	case acctName:
		f.name, cmd = f.name.Update(msg)
	case acctCurrency:
		f.currency, cmd = f.currency.Update(msg)
	case acctBalance:
		f.balance, cmd = f.balance.Update(msg)
	case acctAsOf:
		f.asOf, cmd = f.asOf.Update(msg)
	}
	f.err = ""
	return m, cmd
}

// saveAccountForm checks everything before it writes anything, creates the
// account, and books its balance.
func (m *Model) saveAccountForm() {
	f := m.acct
	fail := func(format string, args ...any) { f.err = fmt.Sprintf(format, args...) }

	var name string
	cur := f.existing.Currency
	if !f.setting {
		if strings.TrimSpace(f.name.Value()) == "" {
			fail("give the account a name, e.g. Sparkasse")
			return
		}
		name = f.accountName()
		if err := ledger.ValidateAccountName(name); err != nil {
			fail("%v", err)
			return
		}
		if typ, _ := ledger.TypeOf(name); !typ.NeedsCurrency() {
			fail("only Assets and Liabilities accounts are created here")
			return
		}
		var err error
		if cur, err = ledger.ParseCurrency(f.currency.Value()); err != nil {
			fail("%v", err)
			return
		}
	}

	// The balance, read in the account's currency and in the ledger's sign.
	var req *core.OpeningBalanceRequest
	var shown ledger.Money
	text := strings.TrimSpace(f.balance.Value())
	if text != "" || f.setting {
		var err error
		if shown, err = ledger.ParseAmount(text, cur, 0); err != nil {
			fail("balance: %v", err)
			return
		}
		when, err := parseFormDate(f.asOf.Value(), m.now())
		if err != nil {
			fail("date: %v", err)
			return
		}
		typ := ledger.Assets
		if f.liab || f.existing.Type == ledger.Liabilities {
			typ = ledger.Liabilities
		}
		req = &core.OpeningBalanceRequest{Balance: core.Account{Type: typ}.LedgerBalance(shown), AsOf: when}
	}

	var parts []string
	if !f.setting {
		acc, err := m.svc.AddAccount(m.ctx, name, cur)
		if err != nil {
			if errors.Is(err, core.ErrAccountExists) {
				fail("%s already exists", name)
			} else {
				fail("%v", err)
			}
			return
		}
		f.existing = acc
		parts = append(parts, fmt.Sprintf("Created %s (%s)", acc.Name, acc.Currency))
	}
	if req != nil {
		req.Account = f.existing.Name
		res, err := m.svc.SetOpeningBalance(m.ctx, *req)
		if err != nil {
			m.closeAccountForm(f.existing.Name)
			m.setStatus(strings.Join(parts, " · ")+fmt.Sprintf(": the balance was not set: %v (b tries again)", err), true)
			return
		}
		m.pushBalanceUndo(res)
		if res.ID == 0 {
			parts = append(parts, fmt.Sprintf("nothing to book: the transactions already add up to %s on %s", shown, req.AsOf.Format(ledger.DateLayout)))
		} else {
			parts = append(parts, fmt.Sprintf("opening balance of %s booked on %s", res.Amount, res.Date))
		}
	}
	if f.setting && req != nil {
		parts = append(parts, "z undoes")
	} else {
		parts = append(parts, "i imports a bank export")
	}
	m.closeAccountForm(f.existing.Name)
	m.setStatus(strings.Join(parts, " · "), false)
}

// pushBalanceUndo lets z take back a change to an opening balance: it deletes
// a transaction the call created and puts back one it corrected.
func (m *Model) pushBalanceUndo(res core.OpeningBalanceResult) {
	if res.ID == 0 {
		return
	}
	id, before := res.ID, res.Before
	op := undoOp{label: fmt.Sprintf("the opening balance (#%d)", id)}
	if before == nil {
		op.run = func() error { return m.svc.DeleteTransaction(m.ctx, id) }
	} else {
		op.run = func() error {
			old, err := before.NewTransaction()
			if err != nil {
				return err
			}
			_, err = m.svc.ReplaceTransaction(m.ctx, id, old)
			return err
		}
	}
	m.undo = append(m.undo, op)
}

// closeAccountForm closes the popup, reloads, and selects the account.
func (m *Model) closeAccountForm(selectName string) {
	m.mode, m.acct = modeList, nil
	if err := m.reload(); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	for i, e := range m.accounts {
		if e.account.Name == selectName {
			m.accts.cursor = i + 1
		}
	}
	m.clampAll()
	m.applyFilter(false)
}

func (m *Model) accountPopup() string {
	f := m.acct
	w, _ := m.accountSize()
	iw := w - 2

	label := func(name string, field acctField) string {
		if f.field == field {
			return accent.Render(fit("▸ "+name, 11))
		}
		return dim.Render(fit("  "+name, 11))
	}
	var lines []string
	title := "New account"
	if f.setting {
		title = "Set balance · " + f.existing.Name
		held := m.accounts[m.accountIndex(f.existing.Name)].balance
		lines = append(lines, dim.Render(fit("  Thaler currently holds "+held.String(), iw)), "")
	} else {
		kind := "Assets"
		if f.liab {
			kind = "Liabilities"
		}
		if f.field == acctKind {
			kind += dim.Render("   ◂ ▸ change")
		}
		lines = append(lines,
			label("Name", acctName)+" "+f.name.View(),
			label("Type", acctKind)+" "+kind,
			label("Currency", acctCurrency)+" "+f.currency.View(),
		)
	}
	lines = append(lines,
		label("Balance", acctBalance)+" "+f.balance.View(),
		label("As of", acctAsOf)+" "+f.asOf.View(),
		dim.Render(strings.Repeat("─", iw)),
	)
	lines = append(lines, m.accountHints(iw)...)
	_, h := m.accountSize()
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	lines = append(lines, errStyle.Render(fit(f.err, iw)))
	footer := "enter next · ctrl+s save · esc cancel"
	return box(title, lines, w, h, true, footer)
}

// accountHints explains the field being edited.
func (m *Model) accountHints(w int) []string {
	f := m.acct
	owes := f.liab || f.existing.Type == ledger.Liabilities
	switch f.field {
	case acctName:
		if strings.TrimSpace(f.name.Value()) == "" {
			return []string{dim.Render("e.g. Sparkasse, Cash, Savings")}
		}
		return []string{dim.Render("will be called " + f.accountName())}
	case acctKind:
		return []string{dim.Render("Assets hold money (bank, cash); Liabilities owe it (credit card, loan)")}
	case acctCurrency:
		return []string{dim.Render("the one currency this account holds, e.g. EUR")}
	case acctBalance:
		hint := "what the bank shows on that day; empty = start from nothing"
		if f.setting {
			hint = "what the bank shows on that day"
		}
		if owes {
			hint = "the amount you owe, e.g. 500"
		}
		return []string{
			dim.Render(fit(hint, w)),
			dim.Render(fit("The difference is booked as an opening balance before the first transaction.", w)),
		}
	default:
		return []string{dim.Render("empty = today · y yesterday · -2 two days ago · 2026-10-20")}
	}
}
