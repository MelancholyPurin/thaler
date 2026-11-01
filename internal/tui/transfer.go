package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The transfer popup moves money between two of your own accounts. The service
// books it as two linked transactions, one on each account, like the two bank
// statements of a real transfer.

type transferField int

const (
	tfAmount transferField = iota
	tfReceived
	tfDate
	tfFrom
	tfTo
)

type transferForm struct {
	field    transferField
	from, to int // indexes into Model.accounts

	amount, received, date textinput.Model
	err                    string
}

func (m *Model) transferWidth() int {
	w := min(max(m.width-6, 30), 64)
	return max(8, w-2-12-1-5) // minus the border, the label, the cursor cell and the currency beside the amount
}

// cross reports whether the two accounts hold different currencies, in which
// case how much arrives has to be said.
func (m *Model) transferCross(f *transferForm) bool {
	return m.accounts[f.from].account.Currency != m.accounts[f.to].account.Currency
}

// fields are the fields shown, in tab order.
func (m *Model) transferFields(f *transferForm) []transferField {
	if m.transferCross(f) {
		return []transferField{tfAmount, tfReceived, tfDate, tfFrom, tfTo}
	}
	return []transferField{tfAmount, tfDate, tfFrom, tfTo}
}

// openTransferForm starts a transfer out of the account selected in the
// Accounts panel (else the one used last), into another that holds the same
// currency if there is one.
func (m *Model) openTransferForm() tea.Cmd {
	if len(m.accounts) < 2 {
		m.setStatus("a transfer needs two accounts: press n to create another", true)
		return nil
	}
	f := &transferForm{from: m.defaultAccount()}
	f.to = -1
	for i, e := range m.accounts {
		if i == f.from {
			continue
		}
		if f.to < 0 || (e.account.Currency == m.accounts[f.from].account.Currency && m.accounts[f.to].account.Currency != e.account.Currency) {
			f.to = i
		}
	}
	f.amount, f.received = newInput(""), newInput("")
	f.date = newInput(m.loc.Date(m.now()))
	f.received.Placeholder = "what arrives"
	for _, in := range []*textinput.Model{&f.amount, &f.received, &f.date} {
		in.Width = m.transferWidth()
	}
	m.xfer = f
	m.mode = modeTransfer
	return m.focusTransferField(tfAmount)
}

func (m *Model) focusTransferField(field transferField) tea.Cmd {
	f := m.xfer
	f.field = field
	f.amount.Blur()
	f.received.Blur()
	f.date.Blur()
	switch field {
	case tfAmount:
		return f.amount.Focus()
	case tfReceived:
		return f.received.Focus()
	case tfDate:
		return f.date.Focus()
	}
	return nil
}

func (m *Model) moveTransferField(d int) tea.Cmd {
	f := m.xfer
	fields := m.transferFields(f)
	cur := 0
	for i, x := range fields {
		if x == f.field {
			cur = i
		}
	}
	return m.focusTransferField(fields[(cur+d+len(fields))%len(fields)])
}

func (m *Model) updateTransfer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.xfer
	key := msg.String()
	switch key {
	case "esc":
		m.mode, m.xfer = modeList, nil
		return m, nil
	case "tab":
		return m, m.moveTransferField(1)
	case "shift+tab":
		return m, m.moveTransferField(-1)
	case "ctrl+s":
		m.saveTransfer()
		return m, nil
	case "enter":
		if f.field == tfAmount || f.field == tfReceived {
			return m, m.moveTransferField(1)
		}
		m.saveTransfer()
		return m, nil
	case "up", "down", "left", "right":
		if f.field == tfFrom || f.field == tfTo {
			d := 1
			if key == "up" || key == "left" {
				d = -1
			}
			m.stepTransferAccount(d)
			return m, nil
		}
	}
	var cmd tea.Cmd
	switch f.field {
	case tfAmount:
		f.amount, cmd = f.amount.Update(msg)
	case tfReceived:
		f.received, cmd = f.received.Update(msg)
	case tfDate:
		f.date, cmd = f.date.Update(msg)
	}
	f.err = ""
	return m, cmd
}

// stepTransferAccount picks the next account for the From or To field. Landing
// on the account the other field has swaps them, so one key flips the direction.
func (m *Model) stepTransferAccount(d int) {
	f := m.xfer
	n := len(m.accounts)
	if f.field == tfFrom {
		prev := f.from
		f.from = (f.from + d + n) % n
		if f.from == f.to {
			f.to = prev
		}
	} else {
		prev := f.to
		f.to = (f.to + d + n) % n
		if f.to == f.from {
			f.from = prev
		}
	}
	f.err = ""
}

func (m *Model) saveTransfer() {
	f := m.xfer
	fail := func(format string, args ...any) { f.err = fmt.Sprintf(format, args...) }
	src, dst := m.accounts[f.from].account, m.accounts[f.to].account

	out, err := ledger.ParseAmount(f.amount.Value(), src.Currency, 0)
	if err != nil {
		fail("amount: %v", err)
		return
	}
	var in ledger.Money
	if m.transferCross(f) {
		if in, err = ledger.ParseAmount(f.received.Value(), dst.Currency, 0); err != nil {
			fail("received: %v", err)
			return
		}
	}
	when, err := parseFormDate(m.loc, f.date.Value(), m.now())
	if err != nil {
		fail("date: %v", err)
		return
	}
	res, err := m.svc.Transfer(m.ctx, core.TransferRequest{From: src.Name, To: dst.Name, Amount: out, ToAmount: in, Date: when})
	if err != nil {
		fail("%v", err)
		return
	}

	m.mode, m.xfer = modeList, nil
	outID, inID := res.ID, res.InID
	m.undo = append(m.undo, undoOp{
		label: fmt.Sprintf("the transfer (#%d and #%d)", outID, inID),
		run: func() error {
			if err := m.svc.DeleteTransaction(m.ctx, outID); err != nil {
				return err
			}
			return m.svc.DeleteTransaction(m.ctx, inID)
		},
	})
	m.lastAccount = src.Name
	if err := m.reload(); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.accts.cursor = m.accountIndex(src.Name) + 1
	m.clampAll()
	m.applyFilter(false)

	msg := fmt.Sprintf("Transferred %s from %s to %s", out, shortName(src.Name), shortName(dst.Name))
	if res.CrossCurrency {
		msg += fmt.Sprintf(" (%s arrived, %s)", res.In, impliedRate(out, res.In))
	}
	m.setStatus(msg+" · z undoes", false)
}

// impliedRate describes the exchange rate a cross-currency transfer got. It is
// for display only; the stored amounts are exact integers.
func impliedRate(out, in ledger.Money) string {
	if out.Minor <= 0 || in.Minor <= 0 {
		return ""
	}
	rate := (float64(in.Minor) / math.Pow10(in.Currency.Exponent())) / (float64(out.Minor) / math.Pow10(out.Currency.Exponent()))
	return fmt.Sprintf("1 %s = %.4f %s", out.Currency, rate, in.Currency)
}

func (m *Model) transferPopup() string {
	f := m.xfer
	w := min(max(m.width-6, 30), 64)
	iw := w - 2
	src, dst := m.accounts[f.from].account, m.accounts[f.to].account
	cross := m.transferCross(f)

	label := func(name string, field transferField) string {
		if f.field == field {
			return accent.Render(fit("▸ "+name, 11))
		}
		return dim.Render(fit("  "+name, 11))
	}
	account := func(a core.Account, field transferField) string {
		text := fmt.Sprintf("%s · %s", a.Name, a.Currency)
		if f.field == field {
			text += dim.Render("   ◂ ▸ change")
		}
		return text
	}
	lines := []string{
		leftRight(label("Amount", tfAmount)+" "+f.amount.View(), dim.Render(string(src.Currency)), iw),
	}
	if cross {
		lines = append(lines, leftRight(label("Received", tfReceived)+" "+f.received.View(), dim.Render(string(dst.Currency)), iw))
	}
	lines = append(lines,
		label("Date", tfDate)+" "+f.date.View(),
		label("From", tfFrom)+" "+account(src, tfFrom),
		label("To", tfTo)+" "+account(dst, tfTo),
		dim.Render(strings.Repeat("─", iw)),
	)
	lines = append(lines, m.transferHints(iw)...)
	h := 14
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	lines = append(lines, errStyle.Render(fit(f.err, iw)))
	return box("Transfer between your accounts", lines, w, h, true, "enter next · ctrl+s save · esc cancel")
}

// transferHints explains the field being edited and, across currencies, shows
// the rate the typed amounts imply.
func (m *Model) transferHints(w int) []string {
	f := m.xfer
	switch f.field {
	case tfAmount:
		return []string{dim.Render(fit("how much leaves "+shortName(m.accounts[f.from].account.Name), w))}
	case tfReceived:
		hint := []string{dim.Render(fit("how much arrives; this fixes the exchange rate", w))}
		out, err1 := ledger.ParseAmount(f.amount.Value(), m.accounts[f.from].account.Currency, 0)
		in, err2 := ledger.ParseAmount(f.received.Value(), m.accounts[f.to].account.Currency, 0)
		if err1 == nil && err2 == nil {
			if rate := impliedRate(out, in); rate != "" {
				hint = append(hint, accent.Render(rate))
			}
		}
		return hint
	case tfDate:
		return []string{dim.Render("empty = today · y yesterday · -2 two days ago · 2026-10-20")}
	default:
		return []string{dim.Render("↑ ↓ or ◂ ▸ choose; picking the other field's account swaps them")}
	}
}
