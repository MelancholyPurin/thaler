package tui

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The schedule popup creates or edits one schedule: a transaction that repeats, or,
// when it goes to another of your accounts, a transfer. Nothing it saves books
// anything: that is answering what is due (TFC-3).

type scheduleField int

const (
	sfName scheduleField = iota
	sfAmount
	sfReceived
	sfFrom
	sfTo
	sfEvery
	sfPeriod
	sfStart
	sfEnd
	sfTolerance
)

const (
	scheduleLabelW   = 13
	scheduleSuggests = 4 // suggestions shown under the To field
)

type scheduleForm struct {
	editing int64            // id of the schedule being edited, 0 when creating
	old     core.NewSchedule // as it was, for undo and for what the form does not show
	keep    string           // when editing: the category or account it goes to now
	field   scheduleField
	from    int // index into Model.accounts
	period  core.ScheduleUnit

	name, amount, received, every, start, end, tolerance, to textinput.Model

	toItems []pickItem
	toCur   int
	err     string
}

func (m *Model) scheduleWidth() int { return min(max(m.width-6, 30), 64) }

func (m *Model) scheduleInputWidth() int {
	return max(8, m.scheduleWidth()-2-scheduleLabelW-1-5) // minus the border, the label, the cursor cell and what stands beside the amount
}

// scheduleTarget is where the schedule goes: the category, or the account of a
// transfer, that is selected under the To field.
func (m *Model) scheduleTarget(f *scheduleForm) string {
	// Nothing typed is nothing chosen: the best match for an empty text is only the first on the list.
	if strings.TrimSpace(f.to.Value()) != "" && f.toCur < len(f.toItems) {
		return f.toItems[f.toCur].name
	}
	return ""
}

// scheduleAccount is the account of yours the schedule goes to, if it is a transfer.
func (m *Model) scheduleAccount(f *scheduleForm) (core.Account, bool) {
	target := m.scheduleTarget(f)
	for _, e := range m.accounts {
		if e.account.Name == target {
			return e.account, true
		}
	}
	return core.Account{}, false
}

// scheduleCross says the transfer changes currency, so that what arrives has to be said.
func (m *Model) scheduleCross(f *scheduleForm) bool {
	dst, ok := m.scheduleAccount(f)
	return ok && dst.Currency != m.accounts[f.from].account.Currency
}

// scheduleFields are the fields shown, in tab order: what arrives only for a transfer
// between currencies, the tolerance only for what is not a transfer.
func (m *Model) scheduleFields(f *scheduleForm) []scheduleField {
	fields := []scheduleField{sfName, sfAmount}
	if m.scheduleCross(f) {
		fields = append(fields, sfReceived)
	}
	fields = append(fields, sfFrom, sfTo, sfEvery, sfPeriod, sfStart, sfEnd)
	if _, transfer := m.scheduleAccount(f); !transfer {
		fields = append(fields, sfTolerance)
	}
	return fields
}

// openSchedule starts a new schedule, or edits the one selected on the Forecast.
func (m *Model) openSchedule(edit bool) tea.Cmd {
	if len(m.accounts) == 0 {
		m.setStatus(m.loc.T("form_no_accounts", nil), true)
		return nil
	}
	f := &scheduleForm{from: m.defaultAccount(), period: core.EveryMonth}
	f.name, f.amount, f.received = newInput(""), newInput(""), newInput("")
	f.every, f.start, f.end, f.tolerance, f.to = newInput("1"), newInput(m.loc.Date(m.now())), newInput(""), newInput(""), newInput("")
	f.received.Placeholder = m.loc.T("transfer_received_placeholder", nil)

	if edit {
		if m.sched.cursor >= len(m.schedules) {
			return nil
		}
		s := m.schedules[m.sched.cursor].Schedule
		if !s.Transfer {
			if _, _, ok := (core.TxView{Postings: s.Postings}).Legs(); !ok {
				m.setStatus(m.loc.T("schedule_cannot_edit", nil), true)
				return nil
			}
		}
		old, err := s.NewSchedule()
		if err != nil {
			m.setStatus(m.errText(err), true)
			return nil
		}
		f.editing, f.old = s.ID, old
		f.name.SetValue(s.Name)
		f.period = s.Unit
		f.every.SetValue(strconv.Itoa(s.Every))
		f.start.SetValue(m.loc.StoredDate(s.Start))
		if s.End != "" {
			f.end.SetValue(m.loc.StoredDate(s.End))
		}
		if s.Tolerance > 0 {
			f.tolerance.SetValue(strconv.Itoa(s.Tolerance))
		}
		if s.Transfer {
			f.from = m.accountIndex(s.Postings[0].Account)
			f.amount.SetValue(strings.TrimPrefix(m.loc.Amount(s.Postings[0].Amount), "-"))
			f.received.SetValue(m.loc.Amount(s.Postings[1].Amount))
			f.keep = s.Postings[1].Account
		} else {
			src, cat, _ := (core.TxView{Postings: s.Postings}).Legs()
			f.from = m.accountIndex(src.Account)
			text := m.loc.Amount(src.Amount)
			if src.Amount.Minor > 0 {
				text = "+" + text
			} else {
				text = strings.TrimPrefix(text, "-")
			}
			f.amount.SetValue(text)
			f.keep = cat.Account
		}
		f.to.SetValue(f.keep)
	}
	for _, in := range []*textinput.Model{&f.name, &f.amount, &f.received, &f.every, &f.start, &f.end, &f.tolerance, &f.to} {
		in.Width = m.scheduleInputWidth()
	}
	m.schedForm = f
	m.mode = modeSchedule
	m.refreshScheduleTargets(f)
	return m.focusScheduleField(sfName)
}

// refreshScheduleTargets ranks what the schedule can go to for what has been typed:
// the categories and your other accounts, the best match first. A full category name
// that does not exist yet is offered as a new category.
func (m *Model) refreshScheduleTargets(f *scheduleForm) {
	q := strings.TrimSpace(f.to.Value())
	type scored struct {
		pickItem
		score, count int
		account      bool
	}
	var items []scored
	exact := false
	add := func(name, hint string, account bool) {
		s, ok := fuzzyScore(q, name)
		if !ok {
			return
		}
		exact = exact || name == q
		items = append(items, scored{pickItem: pickItem{name: name, hint: hint}, score: s, count: m.counts[name], account: account})
	}
	for _, name := range m.categories {
		hint := ""
		if m.counts[name] > 0 {
			hint = m.loc.T("pick_times", map[string]any{"Count": m.counts[name]})
		}
		add(name, hint, false)
	}
	for i, e := range m.accounts {
		if i != f.from {
			add(e.account.Name, m.loc.T("schedule_target_account", nil), true)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch {
		case f.keep != "" && q == strings.TrimSpace(f.keep) && (a.name == f.keep) != (b.name == f.keep):
			return a.name == f.keep
		case q != "" && a.score != b.score:
			return a.score > b.score
		case a.account != b.account:
			return !a.account
		case a.count != b.count:
			return a.count > b.count
		}
		return a.name < b.name
	})
	f.toItems = f.toItems[:0]
	for _, it := range items {
		f.toItems = append(f.toItems, it.pickItem)
	}
	if q != "" && !exact {
		if err := ledger.ValidateAccountName(q); err == nil {
			if typ, _ := ledger.TypeOf(q); !typ.NeedsCurrency() {
				f.toItems = append(f.toItems, pickItem{name: q, hint: m.loc.T("pick_new_category", nil), create: true})
			}
		}
	}
	f.toCur = 0
	f.err = ""
}

func (m *Model) focusScheduleField(field scheduleField) tea.Cmd {
	f := m.schedForm
	if f.field == sfTo && field != sfTo { // leaving the To field: it shows what it chose
		if target := m.scheduleTarget(f); target != "" {
			f.to.SetValue(target)
			f.keep = target
			m.refreshScheduleTargets(f)
		}
	}
	f.field = field
	for _, in := range []*textinput.Model{&f.name, &f.amount, &f.received, &f.every, &f.start, &f.end, &f.tolerance, &f.to} {
		in.Blur()
	}
	switch field {
	case sfName:
		return f.name.Focus()
	case sfAmount:
		return f.amount.Focus()
	case sfReceived:
		return f.received.Focus()
	case sfTo:
		return f.to.Focus()
	case sfEvery:
		return f.every.Focus()
	case sfStart:
		return f.start.Focus()
	case sfEnd:
		return f.end.Focus()
	case sfTolerance:
		return f.tolerance.Focus()
	}
	return nil
}

func (m *Model) moveScheduleField(d int) tea.Cmd {
	f := m.schedForm
	fields := m.scheduleFields(f)
	cur := 0
	for i, x := range fields {
		if x == f.field {
			cur = i
		}
	}
	return m.focusScheduleField(fields[(cur+d+len(fields))%len(fields)])
}

func (m *Model) updateScheduleForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.schedForm
	key := msg.String()
	switch key {
	case "esc":
		m.mode, m.schedForm = modeList, nil
		return m, nil
	case "tab":
		return m, m.moveScheduleField(1)
	case "shift+tab":
		return m, m.moveScheduleField(-1)
	case "ctrl+s":
		m.saveSchedule()
		return m, nil
	case "enter":
		if fields := m.scheduleFields(f); f.field == fields[len(fields)-1] {
			m.saveSchedule()
			return m, nil
		}
		return m, m.moveScheduleField(1)
	case "up", "down", "left", "right":
		d := 1
		if key == "up" || key == "left" {
			d = -1
		}
		switch f.field {
		case sfFrom:
			n := len(m.accounts)
			f.from = (f.from + d + n) % n
			m.refreshScheduleTargets(f) // it cannot go to the account it comes from
			f.err = ""
			return m, nil
		case sfPeriod:
			f.period = stepPeriod(f.period, d)
			f.err = ""
			return m, nil
		case sfTo:
			if key == "up" || key == "down" {
				if n := len(f.toItems); n > 0 {
					f.toCur = (f.toCur + d + n) % n
				}
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	switch f.field {
	case sfName:
		f.name, cmd = f.name.Update(msg)
	case sfAmount:
		f.amount, cmd = f.amount.Update(msg)
	case sfReceived:
		f.received, cmd = f.received.Update(msg)
	case sfTo:
		f.to, cmd = f.to.Update(msg)
		m.refreshScheduleTargets(f)
	case sfEvery:
		f.every, cmd = f.every.Update(msg)
	case sfStart:
		f.start, cmd = f.start.Update(msg)
	case sfEnd:
		f.end, cmd = f.end.Update(msg)
	case sfTolerance:
		f.tolerance, cmd = f.tolerance.Update(msg)
	}
	f.err = ""
	return m, cmd
}

// stepPeriod is the period after or before p, going round.
func stepPeriod(p core.ScheduleUnit, d int) core.ScheduleUnit {
	periods := []core.ScheduleUnit{core.EveryWeek, core.EveryMonth, core.EveryYear}
	for i, x := range periods {
		if x == p {
			return periods[(i+d+len(periods))%len(periods)]
		}
	}
	return core.EveryMonth
}

func (m *Model) periodName(p core.ScheduleUnit) string {
	switch p {
	case core.EveryWeek:
		return m.loc.T("period_week", nil)
	case core.EveryYear:
		return m.loc.T("period_year", nil)
	}
	return m.loc.T("period_month", nil)
}

// saveSchedule checks the form and stores the schedule, or says what is wrong.
func (m *Model) saveSchedule() {
	f := m.schedForm
	fail := func(text string) { f.err = text }
	src := m.accounts[f.from].account

	target := m.scheduleTarget(f)
	if target == "" {
		fail(m.loc.T("form_no_category", nil))
		return
	}
	dst, transfer := m.scheduleAccount(f)

	text := strings.TrimSpace(f.amount.Value())
	income := strings.HasPrefix(text, "+")
	amount, err := ledger.ParseAmount(strings.TrimLeft(text, "+-−"), src.Currency, 0)
	if err != nil {
		fail(m.loc.T("error_amount", map[string]any{"Error": m.errText(err)}))
		return
	}
	if amount.Minor == 0 {
		fail(m.loc.T("form_amount_positive", nil))
		return
	}
	every, err := strconv.Atoi(strings.TrimSpace(f.every.Value()))
	if err != nil {
		fail(m.loc.T("error_every", nil))
		return
	}
	start, err := parseFormDate(m.loc, f.start.Value(), m.now())
	if err != nil {
		fail(m.loc.T("error_date", map[string]any{"Error": m.errText(err)}))
		return
	}
	var end time.Time
	if strings.TrimSpace(f.end.Value()) != "" {
		if end, err = parseFormDate(m.loc, f.end.Value(), m.now()); err != nil {
			fail(m.loc.T("error_end", map[string]any{"Error": m.errText(err)}))
			return
		}
	}

	ns := core.NewSchedule{Name: clean(f.name.Value()), Every: every, Unit: f.period, Start: start, End: end, Description: f.old.Description}
	if transfer {
		in := amount
		if m.scheduleCross(f) {
			if in, err = ledger.ParseAmount(strings.TrimLeft(strings.TrimSpace(f.received.Value()), "+-−"), dst.Currency, 0); err != nil {
				fail(m.loc.T("error_received", map[string]any{"Error": m.errText(err)}))
				return
			}
		}
		ns.Transfer = true
		ns.Postings = []ledger.Posting{{Account: src.Name, Amount: amount.Neg()}, {Account: dst.Name, Amount: in}}
	} else {
		if t := strings.TrimSpace(f.tolerance.Value()); t != "" {
			if ns.Tolerance, err = strconv.Atoi(t); err != nil {
				fail(m.loc.T("error_tolerance", nil))
				return
			}
		}
		if !income {
			amount = amount.Neg()
		}
		ns.Postings = []ledger.Posting{{Account: src.Name, Amount: amount}, {Account: target, Amount: amount.Neg()}}
		// What the booked transactions are called: the schedule's name, until it was given another payee.
		ns.Payee = ns.Name
		if f.editing != 0 && f.old.Payee != "" && f.old.Payee != f.old.Name {
			ns.Payee = f.old.Payee
		}
	}

	id := f.editing
	var undo undoOp
	if id != 0 {
		if err := m.svc.ReplaceSchedule(m.ctx, id, ns); err != nil {
			fail(m.errText(err))
			return
		}
		old := f.old
		undo = undoOp{
			undone: func() string { return m.loc.T("undid_schedule_edit", map[string]any{"Name": old.Name}) },
			run:    func() error { return m.svc.ReplaceSchedule(m.ctx, id, old) },
		}
	} else {
		if id, err = m.svc.CreateSchedule(m.ctx, ns); err != nil {
			fail(m.errText(err))
			return
		}
		name := ns.Name
		undo = undoOp{
			undone: func() string { return m.loc.T("undid_schedule_new", map[string]any{"Name": name}) },
			run:    func() error { return m.svc.DeleteSchedule(m.ctx, id) },
		}
	}
	m.undo = append(m.undo, undo)
	m.mode, m.schedForm = modeList, nil
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.selectSchedule(id)
	m.setStatus(m.statusLine([]string{m.loc.T("schedule_saved", map[string]any{"Name": ns.Name}), m.loc.T("hint_z_undoes", nil)}), false)
}

// selectSchedule puts the cursor on schedule id, if it is listed.
func (m *Model) selectSchedule(id int64) {
	for i, s := range m.schedules {
		if s.ID == id {
			m.sched.cursor = i
		}
	}
	m.clampAll()
}

// toggleSchedulePause pauses the selected schedule, or resumes it if it is paused.
func (m *Model) toggleSchedulePause() {
	if m.sched.cursor >= len(m.schedules) {
		return
	}
	s := m.schedules[m.sched.cursor]
	name := s.Name
	if s.Paused() {
		m.changePause(s.ID, m.svc.ResumeSchedule, m.svc.PauseSchedule,
			func() string { return m.loc.T("schedule_now_resumed", map[string]any{"Name": name}) },
			func() string { return m.loc.T("undid_schedule_resume", map[string]any{"Name": name}) })
		return
	}
	m.changePause(s.ID, m.svc.PauseSchedule, m.svc.ResumeSchedule,
		func() string { return m.loc.T("schedule_now_paused", map[string]any{"Name": name}) },
		func() string { return m.loc.T("undid_schedule_pause", map[string]any{"Name": name}) })
}

// changePause does one of pausing and resuming, and keeps the other to undo it. The
// two texts are written when shown, so that they follow the language.
func (m *Model) changePause(id int64, do, undo func(context.Context, int64) error, done, undone func() string) {
	if err := do(m.ctx, id); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.undo = append(m.undo, undoOp{undone: undone, run: func() error { return undo(m.ctx, id) }})
	if err := m.reload(); err != nil {
		m.setStatus(m.errText(err), true)
		return
	}
	m.selectSchedule(id)
	m.setStatus(m.statusLine([]string{done(), m.loc.T("hint_z_undoes", nil)}), false)
}

func (m *Model) schedulePopup() string {
	f := m.schedForm
	w := m.scheduleWidth()
	iw := w - 2
	src := m.accounts[f.from].account
	dst, transfer := m.scheduleAccount(f)

	label := func(name string, field scheduleField) string {
		if f.field == field {
			return accent.Render(fit("▸ "+name, scheduleLabelW))
		}
		return dim.Render(fit("  "+name, scheduleLabelW))
	}
	account := func(a core.Account) string {
		text := a.Name + " · " + string(a.Currency)
		if f.field == sfFrom {
			text += dim.Render("   " + m.loc.T("change_hint", nil))
		}
		return text
	}
	var lines []string
	for _, field := range m.scheduleFields(f) {
		switch field {
		case sfName:
			lines = append(lines, label(m.loc.T("label_name", nil), field)+" "+f.name.View())
		case sfAmount:
			lines = append(lines, leftRight(label(m.loc.T("label_amount", nil), field)+" "+f.amount.View(), dim.Render(string(src.Currency)), iw))
		case sfReceived:
			lines = append(lines, leftRight(label(m.loc.T("label_received", nil), field)+" "+f.received.View(), dim.Render(string(dst.Currency)), iw))
		case sfFrom:
			lines = append(lines, label(m.loc.T("label_from", nil), field)+" "+account(src))
		case sfTo:
			value := f.to.View()
			if f.field != sfTo {
				value = dim.Render("—")
				if target := m.scheduleTarget(f); target != "" {
					value = target
					if transfer {
						value += dim.Render("  ⇄")
					}
				}
			}
			lines = append(lines, label(m.loc.T("label_to", nil), field)+" "+value)
		case sfEvery:
			lines = append(lines, label(m.loc.T("label_every", nil), field)+" "+f.every.View())
		case sfPeriod:
			text := m.periodName(f.period)
			if f.field == sfPeriod {
				text += dim.Render("   " + m.loc.T("change_hint", nil))
			}
			lines = append(lines, label(m.loc.T("label_period", nil), field)+" "+text)
		case sfStart:
			lines = append(lines, label(m.loc.T("label_start", nil), field)+" "+f.start.View())
		case sfEnd:
			lines = append(lines, label(m.loc.T("label_end", nil), field)+" "+f.end.View())
		case sfTolerance:
			lines = append(lines, leftRight(label(m.loc.T("label_tolerance", nil), field)+" "+f.tolerance.View(), dim.Render("%"), iw))
		}
	}
	for len(lines) < 9 {
		lines = append(lines, "")
	}
	lines = append(lines, dim.Render(strings.Repeat("─", iw)))
	below := m.scheduleBelow(iw)
	for len(below) < 4 {
		below = append(below, "")
	}
	lines = append(lines, below[:4]...)
	problem := wrapped(f.err, iw, errStyle) // two lines: the longest message wraps once
	for len(problem) < 2 {
		problem = append(problem, "")
	}
	lines = append(lines, problem[:2]...)
	title := m.loc.T("schedule_title_new", nil)
	if f.editing != 0 {
		title = m.loc.T("schedule_title_edit", nil)
	}
	return box(title, lines, w, len(lines)+2, true, m.loc.T("footer_form_next", nil))
}

// scheduleBelow is what stands under the fields: what the To field can go to while it is
// being typed in, else a line about the field being edited.
func (m *Model) scheduleBelow(iw int) []string {
	f := m.schedForm
	switch f.field {
	case sfTo:
		if len(f.toItems) == 0 {
			return wrapped(m.loc.T("form_no_category", nil), iw, dim)
		}
		start := max(0, min(f.toCur-scheduleSuggests/2, len(f.toItems)-scheduleSuggests))
		var lines []string
		for i := start; i < len(f.toItems) && len(lines) < scheduleSuggests; i++ {
			it := f.toItems[i]
			line := leftRight(it.name, dim.Render(it.hint), iw)
			if it.hint == "" {
				line = fit(it.name, iw)
			}
			if i == f.toCur {
				line = selFocused.Render(fit(it.name+"  "+it.hint, iw))
			}
			lines = append(lines, line)
		}
		return lines
	case sfName:
		return wrapped(m.loc.T("schedule_hint_name", nil), iw, dim)
	case sfAmount:
		if _, transfer := m.scheduleAccount(f); transfer {
			return wrapped(m.loc.T("transfer_hint_amount", map[string]any{"Account": m.loc.ShortName(m.accounts[f.from].account.Name)}), iw, dim)
		}
		return wrapped(m.loc.T("schedule_hint_amount", nil), iw, dim)
	case sfReceived:
		return wrapped(m.loc.T("transfer_hint_received", nil), iw, dim)
	case sfFrom:
		return wrapped(m.loc.T("schedule_hint_from", nil), iw, dim)
	case sfEvery:
		return wrapped(m.loc.T("schedule_hint_every", nil), iw, dim)
	case sfPeriod:
		return wrapped(m.loc.T("schedule_hint_period", nil), iw, dim)
	case sfStart:
		return wrapped(m.loc.T("schedule_hint_start", nil), iw, dim)
	case sfEnd:
		return wrapped(m.loc.T("schedule_hint_end", nil), iw, dim)
	}
	return wrapped(m.loc.T("schedule_hint_tolerance", nil), iw, dim)
}
