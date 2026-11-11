package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The minimum popup (m on the Forecast) sets or clears the lowest an Assets account
// should fall. The accounts are listed with the minimum each has, the amount is typed
// under them, and an empty amount clears it. The Forecast warns about the day a
// balance would go under; an account with no minimum is never warned about. z takes
// each change back.

// lookAhead is how many days the Forecast looks ahead for a balance that falls under
// its minimum, as `thaler cli forecast` does.
const lookAhead = 90

type minimumRow struct {
	account core.Account
	minimum ledger.Money
	has     bool
}

type minimumForm struct {
	rows   []minimumRow
	list   listState
	amount textinput.Model
	err    string
}

// minimumRows is how many accounts the popup shows at once, and minimumLabelW the
// width of the name of its field.
const (
	minimumRows   = 8
	minimumLabelW = 12
)

func (m *Model) minimumSize() (w, h int) {
	return min(max(m.width-6, 30), 60), min(minimumRows, len(m.minForm.rows)) + 8 // the list, a rule, the amount, two lines of hint, two of message, the border
}

// refreshLows loads the minimums, and the accounts that would fall under theirs.
func (m *Model) refreshLows() (err error) {
	if m.minimums, err = m.svc.MinimumBalances(m.ctx); err != nil {
		return err
	}
	upTo := m.now().AddDate(0, 0, lookAhead).Format(ledger.DateLayout)
	m.lows, err = m.svc.LowBalances(m.ctx, upTo)
	return err
}

// openMinimum lists the Assets accounts with the minimum of each, the cursor on the
// account selected in the Accounts panel if it is one, else on the first.
func (m *Model) openMinimum() tea.Cmd {
	have := make(map[string]ledger.Money, len(m.minimums))
	for _, x := range m.minimums {
		have[x.Account] = x.Amount
	}
	f := &minimumForm{amount: newInput("")}
	for _, e := range m.accounts {
		if e.account.Type != ledger.Assets {
			continue
		}
		limit, ok := have[e.account.Name]
		f.rows = append(f.rows, minimumRow{account: e.account, minimum: limit, has: ok})
	}
	if len(f.rows) == 0 {
		m.setStatus(m.loc.T("minimum_no_accounts", nil), true)
		return nil
	}
	m.minForm, m.mode = f, modeMinimum
	if i := m.accts.cursor - 1; i >= 0 && i < len(m.accounts) {
		for j, r := range f.rows {
			if r.account.Name == m.accounts[i].account.Name {
				f.list.cursor = j
			}
		}
	}
	w, _ := m.minimumSize()
	f.amount.Width = max(8, w-2-minimumLabelW-1-1) // minus the border, the label, a space and the cursor cell
	f.amount.Placeholder = m.loc.T("minimum_placeholder", nil)
	m.showMinimumOfCursor()
	return f.amount.Focus()
}

// showMinimumOfCursor puts the minimum of the account under the cursor in the amount.
func (m *Model) showMinimumOfCursor() {
	f := m.minForm
	r := f.rows[f.list.cursor]
	f.amount.SetValue("")
	if r.has {
		f.amount.SetValue(m.loc.Amount(r.minimum))
	}
	f.err = ""
}

func (m *Model) updateMinimum(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.minForm
	key := msg.String()
	switch key {
	case "esc":
		m.mode, m.minForm = modeList, nil
		return m, nil
	case "enter":
		m.saveMinimum()
		return m, nil
	case "up", "down", "pgup", "pgdown":
		f.list.navigate(key, len(f.rows), minimumRows)
		f.list.clamp(len(f.rows), minimumRows)
		m.showMinimumOfCursor()
		return m, nil
	}
	var cmd tea.Cmd
	f.amount, cmd = f.amount.Update(msg)
	f.err = ""
	return m, cmd
}

// saveMinimum sets the minimum to the amount typed, or clears it when the amount is
// empty. The change leaves a way to take it back.
func (m *Model) saveMinimum() {
	f := m.minForm
	r := f.rows[f.list.cursor]
	name := r.account.Name
	text := strings.TrimSpace(f.amount.Value())

	var set ledger.Money
	clearing := text == ""
	switch {
	case clearing && !r.has:
		f.err = m.loc.T("minimum_type_amount", nil)
		return
	case !clearing:
		var err error
		if set, err = ledger.ParseAmount(text, r.account.Currency, 0); err != nil {
			f.err = m.loc.T("error_amount", map[string]any{"Error": m.errText(err)})
			return
		}
	}
	changed := clearing || !r.has || set != r.minimum
	if changed {
		var err error
		if clearing {
			err = m.svc.ClearMinimumBalance(m.ctx, name)
		} else {
			err = m.svc.SetMinimumBalance(m.ctx, name, set)
		}
		if err != nil {
			f.err = m.errText(err)
			return
		}
		before := r
		m.undo = append(m.undo, undoOp{
			undone: func() string { return m.loc.T("undid_minimum", map[string]any{"Name": m.loc.ShortName(name)}) },
			run: func() error {
				if before.has {
					return m.svc.SetMinimumBalance(m.ctx, name, before.minimum)
				}
				return m.svc.ClearMinimumBalance(m.ctx, name)
			},
		})
	}

	m.mode, m.minForm = modeList, nil
	if !changed {
		return
	}
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	args := map[string]any{"Name": m.loc.ShortName(name), "Amount": m.loc.Money(set)}
	said := m.loc.T("minimum_set", args)
	if clearing {
		said = m.loc.T("minimum_cleared", args)
	}
	m.setStatus(m.statusLine([]string{said, m.loc.T("hint_z_undoes", nil)}), false)
}

func (m *Model) minimumPopup() string {
	f := m.minForm
	w, h := m.minimumSize()
	iw := w - 2

	var lines []string
	f.list.clamp(len(f.rows), minimumRows)
	for i := f.list.offset; i < min(f.list.offset+minimumRows, len(f.rows)); i++ {
		r := f.rows[i]
		amount := ""
		if r.has {
			amount = m.loc.Money(r.minimum)
		}
		line := " " + leftRight(m.loc.ShortName(r.account.Name), amount, iw-2) + " "
		if i == f.list.cursor {
			line = selFocused.Render(fit(line, iw))
		} else {
			line = fit(line, iw)
		}
		lines = append(lines, line)
	}
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	lines = append(lines, accent.Render(fit("▸ "+m.loc.T("label_minimum", nil), minimumLabelW))+" "+f.amount.View())
	lines = append(lines, wrapped(m.loc.T("minimum_hint", map[string]any{"Currency": f.rows[f.list.cursor].account.Currency}), iw, dim)...)
	for len(lines) < h-4 {
		lines = append(lines, "")
	}
	msg := wrapped(f.err, iw, errStyle)
	for len(msg) < 2 {
		msg = append(msg, "")
	}
	lines = append(lines, msg...)
	return box(m.loc.T("minimum_title", nil), lines, w, h, true, m.loc.T("minimum_footer", nil))
}

// lowLines say, for each account that would fall under its minimum, on which day.
func (m *Model) lowLines(iw int) []string {
	var out []string
	for _, l := range m.lows {
		if m.fcAccount != "" && l.Account != m.fcAccount {
			continue
		}
		args := map[string]any{
			"Account": m.loc.ShortName(l.Account), "Balance": m.loc.Money(l.Balance), "Minimum": m.loc.Money(l.Minimum),
			"Date": m.loc.StoredDate(l.Date), "Schedule": l.Schedule,
		}
		var text string
		switch {
		case l.Now:
			text = m.loc.T("low_now", args)
		case l.Schedule != "":
			text = m.loc.T("low_on_by", args)
		default:
			text = m.loc.T("low_on", args)
		}
		out = append(out, wrapped(text, iw, warn)...)
	}
	return out
}
