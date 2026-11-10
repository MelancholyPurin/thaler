package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The due popup goes through the dates of the schedules that nobody has answered, the
// oldest first. Thaler only asks: each one is booked as it is, booked for another
// amount, or skipped, and nothing is booked without that.

type dueState struct {
	items []core.Occurrence
	cur   int
	edit  bool // typing the amount it really was
	field int  // in edit: 0 the amount, 1 what arrives
	err   string

	amount, received textinput.Model
}

func (m *Model) dueWidth() int { return min(max(m.width-6, 30), 64) }

func (m *Model) dueInputWidth() int { return max(8, m.dueWidth()-2-scheduleLabelW-1-5) }

// today is the day the dates are counted up to.
func (m *Model) today() string { return m.now().Format(ledger.DateLayout) }

// openDue starts going through what is due, or says that nothing is.
func (m *Model) openDue() tea.Cmd {
	items, err := m.svc.Due(m.ctx, m.today())
	if err != nil {
		m.setStatus(m.errText(err), true)
		return nil
	}
	if len(items) == 0 {
		m.setStatus(m.loc.T("due_nothing", nil), false)
		return nil
	}
	m.due = &dueState{items: items, amount: newInput(""), received: newInput("")}
	m.due.received.Placeholder = m.loc.T("transfer_received_placeholder", nil)
	m.mode = modeDue
	return nil
}

// current is the occurrence on show.
func (m *Model) dueCurrent() core.Occurrence { return m.due.items[m.due.cur] }

// dueLegs is the leg on your account of a schedule, and what it goes to:
// the category, or the account of a transfer.
func dueLegs(s core.Schedule) (source, other core.PostingView, ok bool) {
	if s.Transfer && len(s.Postings) == 2 {
		return s.Postings[0], s.Postings[1], true
	}
	return (core.TxView{Postings: s.Postings}).Legs()
}

func (m *Model) updateDue(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.due
	key := msg.String()
	if d.edit {
		return m.updateDueEdit(msg)
	}
	switch key {
	case "esc", "q":
		m.mode, m.due = modeList, nil
	case "enter", "y":
		m.answerDue(nil)
	case "s":
		m.skipDue()
	case "e":
		m.startDueEdit()
	case "up", "k":
		d.cur = max(0, d.cur-1)
	case "down", "j":
		d.cur = min(len(d.items)-1, d.cur+1)
	}
	return m, nil
}

// startDueEdit opens the amounts of the occurrence on show for typing, as they are.
func (m *Model) startDueEdit() {
	d := m.due
	s := m.dueCurrent().Schedule
	src, other, ok := dueLegs(s)
	if !ok {
		d.err = m.loc.T("schedule_cannot_edit", nil)
		return
	}
	text := m.loc.Amount(src.Amount)
	switch {
	case s.Transfer:
		text = strings.TrimPrefix(text, "-")
		d.received.SetValue(m.loc.Amount(other.Amount))
	case src.Amount.Minor > 0:
		text = "+" + text
	default:
		text = strings.TrimPrefix(text, "-")
	}
	d.amount.SetValue(text)
	d.amount.Width, d.received.Width = m.dueInputWidth(), m.dueInputWidth()
	d.edit, d.field, d.err = true, 0, ""
	d.received.Blur()
	d.amount.CursorEnd()
	d.amount.Focus()
}

// dueCross says the occurrence on show is a transfer that changes currency.
func (m *Model) dueCross() bool {
	s := m.dueCurrent().Schedule
	return s.Transfer && len(s.Postings) == 2 && s.Postings[0].Amount.Currency != s.Postings[1].Amount.Currency
}

func (m *Model) updateDueEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.due
	cross := m.dueCross()
	switch msg.String() {
	case "esc":
		d.edit, d.err = false, ""
		return m, nil
	case "tab", "shift+tab":
		if cross {
			d.field = 1 - d.field
			if d.field == 0 {
				d.received.Blur()
				return m, d.amount.Focus()
			}
			d.amount.Blur()
			return m, d.received.Focus()
		}
		return m, nil
	case "enter":
		if cross && d.field == 0 {
			d.field = 1
			d.amount.Blur()
			return m, d.received.Focus()
		}
		m.bookDueAsTyped()
		return m, nil
	case "ctrl+s":
		m.bookDueAsTyped()
		return m, nil
	}
	var cmd tea.Cmd
	if d.field == 0 {
		d.amount, cmd = d.amount.Update(msg)
	} else {
		d.received, cmd = d.received.Update(msg)
	}
	d.err = ""
	return m, cmd
}

// bookDueAsTyped books the occurrence on show for the amounts typed.
func (m *Model) bookDueAsTyped() {
	d := m.due
	s := m.dueCurrent().Schedule
	src, other, ok := dueLegs(s)
	if !ok {
		return
	}
	text := strings.TrimSpace(d.amount.Value())
	income := strings.HasPrefix(text, "+")
	amount, err := ledger.ParseAmount(strings.TrimLeft(text, "+-−"), src.Amount.Currency, 0)
	if err != nil {
		d.err = m.loc.T("error_amount", map[string]any{"Error": m.errText(err)})
		return
	}
	if amount.Minor == 0 {
		d.err = m.loc.T("form_amount_positive", nil)
		return
	}
	if s.Transfer {
		in := amount
		if m.dueCross() {
			if in, err = ledger.ParseAmount(strings.TrimLeft(strings.TrimSpace(d.received.Value()), "+-−"), other.Amount.Currency, 0); err != nil {
				d.err = m.loc.T("error_received", map[string]any{"Error": m.errText(err)})
				return
			}
		}
		m.answerDue([]ledger.Posting{{Account: src.Account, Amount: amount.Neg()}, {Account: other.Account, Amount: in}})
		return
	}
	if !income {
		amount = amount.Neg()
	}
	m.answerDue([]ledger.Posting{{Account: src.Account, Amount: amount}, {Account: other.Account, Amount: amount.Neg()}})
}

// answerDue books the occurrence on show, as the schedule has it when postings is nil.
func (m *Model) answerDue(postings []ledger.Posting) {
	d := m.due
	occ := m.dueCurrent()
	res, err := m.svc.ConfirmOccurrence(m.ctx, occ.Schedule.ID, occ.Date, postings)
	if err != nil {
		d.err = m.errText(err)
		return
	}
	name, date := occ.Schedule.Name, occ.Date
	out, in := res.ID, res.InID
	m.undo = append(m.undo, undoOp{
		undone: func() string {
			return m.loc.T("undid_due_booked", map[string]any{"Name": name, "Date": m.loc.StoredDate(date)})
		},
		run: func() error { // deleting what it booked makes the date due again
			if err := m.svc.DeleteTransaction(m.ctx, out); err != nil {
				return err
			}
			if in != 0 {
				return m.svc.DeleteTransaction(m.ctx, in)
			}
			return nil
		},
	})
	tx, err := m.svc.GetTransaction(m.ctx, out)
	amount := ""
	if err == nil && len(tx.Postings) > 0 {
		amount = m.loc.Signed(tx.Postings[0].Amount)
	}
	m.afterDue(m.loc.T("due_booked", map[string]any{"Name": name, "Date": m.loc.StoredDate(date), "Amount": amount}))
}

// skipDue passes on the occurrence on show.
func (m *Model) skipDue() {
	occ := m.dueCurrent()
	if err := m.svc.SkipOccurrence(m.ctx, occ.Schedule.ID, occ.Date); err != nil {
		m.due.err = m.errText(err)
		return
	}
	id, name, date := occ.Schedule.ID, occ.Schedule.Name, occ.Date
	m.undo = append(m.undo, undoOp{
		undone: func() string {
			return m.loc.T("undid_due_skipped", map[string]any{"Name": name, "Date": m.loc.StoredDate(date)})
		},
		run: func() error { return m.svc.UnskipOccurrence(m.ctx, id, date) },
	})
	m.afterDue(m.loc.T("due_skipped", map[string]any{"Name": name, "Date": m.loc.StoredDate(date)}))
}

// afterDue goes on to the next date after one was answered, or closes the popup when
// there is none left, and says what was done.
func (m *Model) afterDue(done string) {
	d := m.due
	if err := m.reload(); err != nil {
		m.mode, m.due = modeList, nil
		m.setStatus(m.errText(err), true)
		return
	}
	items, err := m.svc.Due(m.ctx, m.today())
	if err != nil {
		m.mode, m.due = modeList, nil
		m.setStatus(m.errText(err), true)
		return
	}
	parts := []string{done}
	d.items, d.edit, d.err = items, false, ""
	d.cur = min(d.cur, max(len(items)-1, 0))
	if len(items) == 0 {
		m.mode, m.due = modeList, nil
		parts = append(parts, m.loc.T("due_all_done", nil))
	}
	m.applyFilter(false)
	m.setStatus(m.statusLine(append(parts, m.loc.T("hint_z_undoes", nil))), false)
}

func (m *Model) duePopup() string {
	d := m.due
	occ := m.dueCurrent()
	s := occ.Schedule
	w := m.dueWidth()
	iw := w - 2
	src, other, _ := dueLegs(s)

	when, _ := ledger.ParseDate(occ.Date)
	now := m.now()
	ago := int(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Sub(when).Hours() / 24)
	how := m.loc.T("due_today", nil)
	if ago > 0 {
		how = m.loc.T("due_days_ago", map[string]any{"Count": ago})
	}
	name := s.Name
	if s.Transfer {
		name = "⇄ " + name
	}
	lines := []string{"", accent.Render(fit(" "+name, iw)), " " + m.loc.StoredDate(occ.Date) + dim.Render(" · "+how)}

	if d.edit {
		label := func(text string, active bool) string {
			if active {
				return accent.Render(fit("▸ "+text, scheduleLabelW))
			}
			return dim.Render(fit("  "+text, scheduleLabelW))
		}
		lines = append(lines, "", leftRight(label(m.loc.T("label_amount", nil), d.field == 0)+" "+d.amount.View(), dim.Render(string(src.Amount.Currency)), iw))
		hint := m.loc.T("schedule_hint_amount", nil)
		if s.Transfer {
			hint = m.loc.T("transfer_hint_amount", map[string]any{"Account": m.loc.ShortName(src.Account)})
		}
		if m.dueCross() {
			lines = append(lines, leftRight(label(m.loc.T("label_received", nil), d.field == 1)+" "+d.received.View(), dim.Render(string(other.Amount.Currency)), iw))
			if d.field == 1 {
				hint = m.loc.T("transfer_hint_received", nil)
			}
		}
		lines = append(lines, dim.Render(strings.Repeat("─", iw)))
		lines = append(lines, wrapped(hint, iw, dim)...)
		return m.dueBox(lines, errLine(d.err, iw), m.loc.T("footer_due_edit", nil))
	}

	lines = append(lines, "", accent.Render(fit(" "+m.loc.Signed(src.Amount), iw)))
	target := m.loc.AccountName(other.Account)
	if s.Transfer {
		target = "⇄ " + m.loc.ShortName(other.Account)
		if m.dueCross() {
			target += " · " + m.loc.Signed(other.Amount)
		}
	}
	lines = append(lines, " "+m.loc.AccountName(src.Account)+dim.Render(" → ")+target)
	if s.Tolerance > 0 {
		lines = append(lines, wrapped(" "+m.loc.T("due_estimate", map[string]any{"Percent": s.Tolerance}), iw, dim)...)
	}
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	keys := []string{
		m.loc.T("due_key_book", nil), m.loc.T("due_key_change", nil), m.loc.T("due_key_skip", nil),
		m.loc.T("due_key_move", nil), m.loc.T("due_key_stop", nil),
	}
	for i := range keys {
		keys[i] = dim.Render(keys[i])
	}
	lines = append(lines, packed(keys, iw)...)
	return m.dueBox(lines, errLine(d.err, iw), m.loc.T("position_of", map[string]any{"N": d.cur + 1, "Total": len(d.items)}))
}

// errLine is an error message on two lines at most, wrapped rather than cut.
func errLine(err string, w int) []string {
	lines := wrapped(err, w, errStyle)
	for len(lines) < 2 {
		lines = append(lines, "")
	}
	return lines[:2]
}

// dueBox sets the lines, padded to a height that does not change from date to date, and
// the error under them.
func (m *Model) dueBox(lines, problem []string, footer string) string {
	for len(lines) < 12 {
		lines = append(lines, "")
	}
	lines = append(lines[:12], problem...)
	return box(m.loc.T("due_title", nil), lines, m.dueWidth(), len(lines)+2, true, footer)
}
