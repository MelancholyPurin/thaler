// Package tui is the interactive terminal interface, laid out like lazygit: a
// column of panels on the left (home, accounts, transaction history) and a
// detail pane on the right that shows whatever is selected. Built on Bubble Tea.
//
// Service calls are made directly inside Update. The local backend is a SQLite
// file and every call takes milliseconds, so the extra machinery of
// asynchronous commands would only add states that can go wrong. A network
// backend would need them moved into background commands.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// ConfigLoader reads the user's config file. The interface calls it each time an
// import starts, so rules edited while it is open take effect without a restart.
type ConfigLoader func() (*config.Config, error)

// Options is what the interface needs besides the service.
type Options struct {
	LoadConfig ConfigLoader
	// ConfigPath is the config file C opens in the user's editor, and
	// ConfigTemplate is what a file that does not exist yet starts from.
	ConfigPath     string
	ConfigTemplate string
	// DatabasePath is shown in the backup popup, and BackupDir is the folder
	// backups go to by default and the one it counts them in.
	DatabasePath string
	BackupDir    string
}

type mode int

const (
	modeList     mode = iota // browsing the panels
	modeFilter               // typing into the search box
	modePick                 // choosing a category for the selected transaction
	modeHelp                 // key reference popup
	modeForm                 // adding or editing a transaction
	modeConfirm              // asking before deleting a transaction
	modeImport               // importing a bank export
	modeAccount              // creating an account or setting its balance
	modeTransfer             // moving money between two accounts
	modeBackup               // writing a backup of the database
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
	svc core.Service

	// data, see reload
	rows          []row // newest first
	accounts      []acctEntry
	categories    []string // category accounts that exist, for the picker
	counts        map[string]int
	netWorth      core.NetWorth
	multiCurrency bool

	// view state
	screen    screen
	period    string   // "2026-10", or "" for all time; scopes every screen
	months    []string // months that have transactions, plus the current one, newest first
	budget    core.BudgetReport
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
	confirmTx   core.TxView // transaction waiting for a delete confirmation

	loadConfig  ConfigLoader
	opts        Options
	downloads   string // where bank exports usually land; offered when importing unless config.toml says otherwise
	imp         *importFlow
	acct        *accountForm
	xfer        *transferForm
	bkp         *backupForm
	lastProfile string // import profile used last in this session

	picker       textinput.Model
	pickItems    []pickItem
	pickCursor   int
	pickTarget   int64 // transaction being categorised
	pickPayee    string
	pickIsIncome bool

	// A rule can be made while categorizing: pickRule is the text it looks for
	// (empty = no rule), pickRuleOn says the field has the focus, pickCfg is the
	// config as it was when the field was first used, and pickNote explains
	// what the rule would do, or why it cannot be added.
	pickRule    textinput.Model
	pickRuleOn  bool
	pickCfg     *config.Config
	pickNote    string
	pickNoteBad bool

	width, height int
	status        string
	statusIsError bool
}

// New loads the data and returns a ready model.
func New(ctx context.Context, svc core.Service, opts Options) (*Model, error) {
	return newWithClock(ctx, svc, opts, time.Now)
}

func newWithClock(ctx context.Context, svc core.Service, opts Options, now func() time.Time) (*Model, error) {
	m := &Model{ctx: ctx, svc: svc, loadConfig: opts.LoadConfig, opts: opts, width: 100, height: 30, now: now}
	if home, err := os.UserHomeDir(); err == nil {
		m.downloads = filepath.Join(home, "Downloads")
	}
	m.period = now().Format(monthLayout)
	m.filter = textinput.New()
	m.filter.Prompt = "/ "
	m.filter.Placeholder = "fuzzy search: payee, description, category, amount"
	m.picker = textinput.New()
	m.picker.Prompt = "> "
	m.picker.Placeholder = "category, or a new name like Expenses:Gifts"
	m.pickRule = textinput.New()
	m.pickRule.Prompt = ""
	m.pickRule.CharLimit = 80
	if err := m.reload(); err != nil {
		return nil, err
	}
	m.applyFilter(false)
	return m, nil
}

// Run starts the interface and blocks until the user quits.
func Run(ctx context.Context, svc core.Service, opts Options) error {
	m, err := New(ctx, svc, opts)
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
		m.pickRule.Width = max(10, min(msg.Width-6, 66)-2-10)
		if m.form != nil {
			m.sizeForm(m.form)
		}
		if m.imp != nil {
			m.imp.file.Width = m.importWidth()
		}
		if m.xfer != nil {
			for _, in := range []*textinput.Model{&m.xfer.amount, &m.xfer.received, &m.xfer.date} {
				in.Width = m.transferWidth()
			}
		}
		if m.bkp != nil {
			m.sizeBackup(m.bkp)
		}
		if m.acct != nil {
			for _, in := range []*textinput.Model{&m.acct.name, &m.acct.currency, &m.acct.balance, &m.acct.asOf} {
				in.Width = m.accountWidth()
			}
		}
		m.clampAll()
		return m, nil
	case configEditedMsg:
		m.configEdited(msg.err)
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
		case modeImport:
			return m.updateImport(msg)
		case modeAccount:
			return m.updateAccount(msg)
		case modeTransfer:
			return m.updateTransfer(msg)
		case modeBackup:
			return m.updateBackup(msg)
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
	case "C":
		return m, m.editConfig()
	case "S":
		return m, m.openBackup()
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
		return m, m.openForm(false, fieldAmount)
	case "i":
		return m, m.openImport()
	case "x":
		return m, m.openTransferForm()
	case "n":
		return m, m.openAccountForm(false)
	case "b":
		return m, m.openAccountForm(true)
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
			return m, m.openForm(true, fieldAmount)
		case "r":
			return m, m.openForm(true, fieldPayee)
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
	match := clean(m.pickRule.Value())
	var check ruleCheck
	if match != "" {
		if check = m.checkRule(match, category); check.problem != "" {
			m.refreshRuleNote() // the popup stays open and says why
			return
		}
	}
	m.closePicker()
	previous := ""
	if r, ok := m.findRow(id); ok {
		previous = r.category.Account
	}
	rule := importer.Rule{Contains: match, Category: category}
	// The rule goes into the config before any data changes, so that if the
	// file cannot be written nothing has happened.
	if match != "" && check.covered == 0 {
		if err := config.AppendRule(m.opts.ConfigPath, m.opts.ConfigTemplate, rule); err != nil {
			m.setStatus("could not add the rule: "+err.Error(), true)
			return
		}
	}
	if _, err := m.svc.SetCategories(m.ctx, []core.CategoryChange{{TransactionID: id, Category: category}}); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	var back []core.CategoryChange // what z puts back
	if previous != "" {
		back = append(back, core.CategoryChange{TransactionID: id, Category: previous})
	}
	moved := 0
	if match != "" {
		res, err := m.svc.Recategorize(m.ctx, core.RecategorizeRequest{Rules: []importer.Rule{rule}})
		if err != nil {
			m.setStatus("the rule was added, but categorizing the others failed: "+err.Error(), true)
		}
		for _, mv := range res.Moves {
			was := ledger.UncategorizedExpense
			if mv.Amount.Minor > 0 {
				was = ledger.UncategorizedIncome
			}
			back = append(back, core.CategoryChange{TransactionID: mv.TransactionID, Category: was})
		}
		moved = len(res.Moves)
	}
	if len(back) > 0 {
		label := fmt.Sprintf("categorizing #%d", id)
		if moved > 0 {
			label = fmt.Sprintf("categorizing #%d and %d more", id, moved)
		}
		m.undo = append(m.undo, undoOp{label: label, run: func() error {
			_, err := m.svc.SetCategories(m.ctx, back)
			return err
		}})
	}
	if err := m.reload(); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.applyFilter(true)
	if match == "" {
		m.setStatus(fmt.Sprintf("#%d → %s", id, category), false)
		return
	}
	msg := fmt.Sprintf("#%d → %s · rule added to config.toml", id, category)
	if check.covered > 0 {
		msg = fmt.Sprintf("#%d → %s · rule %d already covers this", id, category, check.covered)
	}
	if moved > 0 {
		msg += fmt.Sprintf(" · %d more categorized", moved)
	}
	m.setStatus(msg+" · z undoes the categories, C edits the rules", false)
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.statusIsError = s, isErr
}
