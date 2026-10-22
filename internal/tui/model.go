// Package tui is the interactive terminal interface, laid out like lazygit: a
// column of panels on the left (home, accounts, transaction history) and a
// detail pane on the right that shows whatever is selected. Built on Bubble Tea.
//
// Database calls are made directly inside Update. The store is a local SQLite
// file and every query takes milliseconds, so the extra machinery of
// asynchronous commands would only add states that can go wrong.
package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/store"
)

type mode int

const (
	modeList    mode = iota // browsing the panels
	modeFilter              // typing into the search box
	modePick                // choosing a category for the selected transaction
	modeHelp                // key reference popup
	modeForm                // adding or editing a transaction
	modeConfirm             // asking before deleting a transaction
)

// screen is one top-level view, switched from the strip along the top.
type screen int

const (
	screenLedger screen = iota
	screenBudget
	screenForecast
	screenReports
	numScreens
)

var screenNames = [numScreens]string{"Ledger", "Budget", "Forecast", "Reports"}

type pane int

const (
	paneHome pane = iota
	paneAccounts
	paneTransactions
	numPanes
)

type pickItem struct {
	name   string
	hint   string
	create bool
}

// Model is the Bubble Tea model of the whole interface.
type Model struct {
	ctx context.Context
	st  *store.Store

	// data, see reload
	rows          []row // newest first
	accounts      []acctEntry
	categories    []string // category accounts that exist, for the picker
	counts        map[string]int
	netWorth      store.NetWorth
	multiCurrency bool

	// view state
	screen    screen
	period    string   // "2026-10", or "" for all time; scopes every screen
	months    []string // months that have transactions, plus the current one, newest first
	budget    budgetData
	budgetTop listState // scroll position of the months list on the Budget screen
	now       func() time.Time
	focus     pane
	accts     listState // index 0 is "All accounts", then m.accounts
	txs       listState // index into view
	view      []int     // indexes into rows that pass the filters
	onlyUncat bool

	mode   mode
	filter textinput.Model

	payees      []payeeStat // most recently used first
	form        *form
	lastAccount string // account of the last transaction added in this session
	lastEntered string // account of the most recently entered transaction in the database
	undo        []undoOp
	confirmTx   store.TxView // transaction waiting for a delete confirmation

	picker       textinput.Model
	pickItems    []pickItem
	pickCursor   int
	pickTarget   int64 // transaction being categorised
	pickPayee    string
	pickIsIncome bool

	width, height int
	status        string
	statusIsError bool
}

// New loads the data and returns a ready model.
func New(ctx context.Context, st *store.Store) (*Model, error) {
	return newWithClock(ctx, st, time.Now)
}

func newWithClock(ctx context.Context, st *store.Store, now func() time.Time) (*Model, error) {
	m := &Model{ctx: ctx, st: st, width: 100, height: 30, now: now}
	m.period = now().Format(monthLayout)
	m.filter = textinput.New()
	m.filter.Prompt = "/ "
	m.filter.Placeholder = "fuzzy search: payee, description, category, amount"
	m.picker = textinput.New()
	m.picker.Prompt = "> "
	m.picker.Placeholder = "category, or a new name like Expenses:Gifts"
	if err := m.reload(); err != nil {
		return nil, err
	}
	m.applyFilter(false)
	return m, nil
}

// Run starts the interface and blocks until the user quits.
func Run(ctx context.Context, st *store.Store) error {
	m, err := New(ctx, st)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

// ---- layout ------------------------------------------------------------------

// layout holds the sizes of the panels for the current terminal size.
type layout struct {
	two                 bool // false: only the left column fits
	leftW, rightW       int
	hHome, hAccts, hTxs int // outer heights; 0 = panel not shown
	avail               int // height above the bottom bar
	acctLines, txLines  int // list lines inside the panels
}

func (m *Model) layout() layout {
	l := layout{avail: max(m.height-2, 1), two: m.width >= 76} // minus the tab strip and the bottom bar
	if l.two {
		l.leftW = min(max(m.width*42/100, 44), 64)
		l.rightW = m.width - l.leftW
	} else {
		l.leftW = m.width
	}
	if l.avail < 14 {
		// Too short for three panels: show only the focused one.
		switch m.focus {
		case paneHome:
			l.hHome = l.avail
		case paneAccounts:
			l.hAccts = l.avail
		default:
			l.hTxs = l.avail
		}
	} else {
		l.hHome = 3
		l.hAccts = max(5, min(len(m.accounts)+3, l.avail*35/100))
		l.hTxs = l.avail - l.hHome - l.hAccts
	}
	l.acctLines = max(l.hAccts-2, 1)
	l.txLines = max(l.hTxs-2, 1)
	return l
}

func (m *Model) clampAll() {
	l := m.layout()
	m.accts.clamp(len(m.accounts)+1, l.acctLines)
	m.txs.clamp(len(m.view), l.txLines)
}

func (m *Model) setFocus(p pane) {
	m.focus = p
	m.clampAll() // the compact layout gives each panel a different height
}

// ---- Bubble Tea ------------------------------------------------------------

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.filter.Width = max(10, msg.Width-4)
		m.picker.Width = max(10, min(msg.Width-10, 60))
		if m.form != nil {
			for _, in := range []*textinput.Model{&m.form.amount, &m.form.payee, &m.form.category, &m.form.date} {
				in.Width = m.inputWidth()
			}
		}
		m.clampAll()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modePick:
			return m.updatePick(msg)
		case modeHelp:
			m.mode = modeList // any key closes the help
			return m, nil
		case modeForm:
			return m.updateForm(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m *Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	key := msg.String()
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		return m, nil
	case "f1", "L":
		m.setScreen(screenLedger)
		return m, nil
	case "f2", "B":
		m.setScreen(screenBudget)
		return m, nil
	case "f3", "F":
		m.setScreen(screenForecast)
		return m, nil
	case "f4", "R":
		m.setScreen(screenReports)
		return m, nil
	case "[":
		m.shiftPeriod(-1)
		return m, nil
	case "]":
		m.shiftPeriod(1)
		return m, nil
	case "t":
		m.setPeriod(m.now().Format(monthLayout))
		return m, nil
	case "T":
		m.setPeriod("")
		return m, nil
	}
	switch m.screen {
	case screenLedger:
		return m.updateLedger(key)
	case screenBudget:
		m.updateBudget(key)
	}
	return m, nil
}

func (m *Model) setScreen(s screen) {
	m.screen = s
	m.clampAll()
}

func (m *Model) updateLedger(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "tab":
		m.setFocus((m.focus + 1) % numPanes)
		return m, nil
	case "shift+tab":
		m.setFocus((m.focus + numPanes - 1) % numPanes)
		return m, nil
	case "1":
		m.setFocus(paneHome)
		return m, nil
	case "2":
		m.setFocus(paneAccounts)
		return m, nil
	case "3":
		m.setFocus(paneTransactions)
		return m, nil
	case "a":
		return m, m.openForm(false)
	case "z":
		m.undoLast()
		return m, nil
	case "u":
		m.onlyUncat = !m.onlyUncat
		m.applyFilter(true)
		return m, nil
	case "/":
		m.setFocus(paneTransactions)
		m.mode = modeFilter
		return m, m.filter.Focus()
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter(true)
		}
		return m, nil
	}

	l := m.layout()
	switch m.focus {
	case paneHome:
		if key == "enter" {
			m.setFocus(paneTransactions)
		}
	case paneAccounts:
		if m.accts.navigate(key, len(m.accounts)+1, l.acctLines) {
			m.applyFilter(false) // a different account is a different list
		} else if key == "enter" {
			m.setFocus(paneTransactions)
		}
	case paneTransactions:
		if m.txs.navigate(key, len(m.view), l.txLines) {
			return m, nil
		}
		switch key {
		case "c", "enter":
			return m, m.openPicker()
		case "e":
			return m, m.openForm(true)
		case "d":
			m.confirmDelete()
		}
	}
	return m, nil
}

func (m *Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := m.layout()
	switch msg.String() {
	case "enter":
		m.mode = modeList
		m.filter.Blur()
		return m, nil
	case "esc":
		m.mode = modeList
		m.filter.Blur()
		m.filter.SetValue("")
		m.applyFilter(true)
		return m, nil
	case "up", "down":
		m.txs.navigate(msg.String(), len(m.view), l.txLines)
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.applyFilter(true)
	return m, cmd
}

func (m *Model) applyCategory(category string) {
	id := m.pickTarget
	m.closePicker()
	previous := ""
	if r, ok := m.findRow(id); ok {
		previous = r.category.Account
	}
	if _, err := m.st.SetCategories(m.ctx, []store.CategoryChange{{TransactionID: id, Category: category}}); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	if previous != "" {
		m.undo = append(m.undo, undoOp{
			label: fmt.Sprintf("categorizing #%d", id),
			run: func() error {
				_, err := m.st.SetCategories(m.ctx, []store.CategoryChange{{TransactionID: id, Category: previous}})
				return err
			},
		})
	}
	if err := m.reload(); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.applyFilter(true)
	m.setStatus(fmt.Sprintf("#%d → %s", id, category), false)
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.statusIsError = s, isErr
}
