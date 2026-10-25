package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// The import popup has two steps. First you choose the file, the account and
// the bank profile; then you review what would happen (a dry run through the
// service, so nothing is written) and confirm.

type importStep int

const (
	stepChoose importStep = iota
	stepReview
)

type importField int

const (
	importFile importField = iota
	importAccount
	importProfile
	numImportFields
)

// csvFile is a candidate export found in a folder.
type csvFile struct {
	path, name string
	modified   time.Time
}

type importFlow struct {
	step  importStep
	field importField
	cfg   *config.Config

	file     textinput.Model
	account  int // index into Model.accounts
	profiles []string
	profile  int
	err      string

	dir      string    // the folder listed while nothing is typed
	files    []csvFile // its CSV files, newest first
	browsing string    // a folder typed into the field, whose files are listed instead
	matches  []csvFile // the files that fit what has been typed
	fileCur  int       // highlighted match, -1 for none

	// the review step
	path     string
	req      core.ImportRequest // the dry run; sent again without DryRun to import
	res      core.ImportResult
	skipped  int      // pending rows the profile ignores
	problems []string // rows that could not be read or do not fit the account
	newUncat int      // new rows no rule matched
	list     listState
}

// importSize is the outer size of the import popup.
func (m *Model) importSize() (w, h int) {
	return min(max(m.width-6, 30), 84), min(max(m.height-2, 12), 22)
}

// importWidth is the width of the file input.
func (m *Model) importWidth() int {
	w, _ := m.importSize()
	return max(8, w-2-12-1) // minus the border, the label and the cursor cell
}

// openImport starts importing a bank export. The account defaults like the
// add form's: the one selected in the Accounts panel, else the last one used.
func (m *Model) openImport() tea.Cmd {
	if len(m.accounts) == 0 {
		m.setStatus("no accounts yet: press n to create one", true)
		return nil
	}
	cfg := &config.Config{}
	if m.loadConfig != nil {
		var err error
		if cfg, err = m.loadConfig(); err != nil {
			m.setStatus(err.Error(), true)
			return nil
		}
	}
	f := &importFlow{cfg: cfg, account: m.defaultAccount(), file: newInput(""), fileCur: -1}
	f.file.CharLimit = 512
	f.file.Placeholder = "paste or drop a file path, or pick one below"
	f.file.Width = m.importWidth()
	f.profiles = importer.ProfileNames(cfg.Profiles)
	for i, name := range f.profiles {
		if name == m.lastProfile || (m.lastProfile == "" && name == "sparkasse") {
			f.profile = i
		}
	}
	f.dir = m.downloads
	if cfg.ImportDir != "" {
		f.dir = expandHome(cfg.ImportDir)
	}
	f.files = listCSVs(f.dir)
	m.imp = f
	m.mode = modeImport
	f.refreshFiles()
	return f.file.Focus()
}

// listCSVs lists the .csv files directly in dir, newest first.
func listCSVs(dir string) []csvFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []csvFile
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".csv") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, csvFile{path: filepath.Join(dir, e.Name()), name: e.Name(), modified: info.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].modified.After(out[j].modified) })
	return out
}

// typedPath is what was typed in the file field, tidied: whitespace, the quotes
// or backslashes a terminal adds when a file is dropped onto it, and ~.
func (f *importFlow) typedPath() string { return cleanPath(f.file.Value()) }

// cleanPath turns text pasted or dropped into the terminal into a file path.
func cleanPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	} else if filepath.Separator == '/' && strings.Contains(s, `\`) {
		// "/Users/me/My\ Export.csv": undo the shell-style escapes.
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
			}
			b.WriteByte(s[i])
		}
		s = b.String()
	}
	return expandHome(s)
}

// expandHome turns a leading ~ into the home directory.
func expandHome(s string) string {
	if s == "~" || strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, s[1:])
		}
	}
	return s
}

// looksLikePath tells a path from a few letters of a file name.
func looksLikePath(s string) bool {
	return strings.ContainsRune(s, filepath.Separator) || strings.HasPrefix(s, "~") || strings.HasPrefix(s, ".")
}

// refreshFiles recomputes which files fit what has been typed. A typed path
// that is a folder lists that folder's CSV files; any other path is taken as it
// is, and plain letters search the files of the default folder.
func (f *importFlow) refreshFiles() {
	typed := f.typedPath()
	f.matches = f.matches[:0]
	f.browsing = ""
	switch {
	case typed == "":
		f.matches = append(f.matches, f.files...)
	case looksLikePath(typed):
		if info, err := os.Stat(typed); err == nil && info.IsDir() {
			f.browsing = typed
			f.matches = append(f.matches, listCSVs(typed)...)
		}
	default:
		type scored struct {
			csvFile
			score int
		}
		var hits []scored
		for _, d := range f.files {
			if s, ok := fuzzyScore(typed, d.name); ok {
				hits = append(hits, scored{d, s})
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		for _, h := range hits {
			f.matches = append(f.matches, h.csvFile)
		}
	}
	f.fileCur = -1
	if len(f.matches) > 0 {
		f.fileCur = 0
	}
}

// chosenPath is the file to import: a typed path, else the highlighted file.
func (f *importFlow) chosenPath() string {
	typed := f.typedPath()
	if typed != "" && looksLikePath(typed) && f.browsing == "" {
		return typed
	}
	if f.fileCur >= 0 && f.fileCur < len(f.matches) {
		return f.matches[f.fileCur].path
	}
	if f.browsing != "" {
		return "" // a folder with nothing to pick
	}
	return typed
}

func (m *Model) closeImport() {
	m.mode, m.imp = modeList, nil
}

func (m *Model) focusImportField(d int) tea.Cmd {
	f := m.imp
	f.field = importField((int(f.field) + d + int(numImportFields)) % int(numImportFields))
	f.file.Blur()
	if f.field == importFile {
		return f.file.Focus()
	}
	return nil
}

func (m *Model) updateImport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.imp
	if f.step == stepReview {
		m.updateImportReview(msg.String())
		return m, nil
	}
	key := msg.String()
	switch key {
	case "esc":
		m.closeImport()
		return m, nil
	case "tab":
		return m, m.focusImportField(1)
	case "shift+tab":
		return m, m.focusImportField(-1)
	case "enter":
		m.reviewImport()
		return m, nil
	case "up", "down", "left", "right":
		d := 1
		if key == "up" || key == "left" {
			d = -1
		}
		switch f.field {
		case importFile:
			if key == "up" || key == "down" {
				f.fileCur = max(-1, min(len(f.matches)-1, f.fileCur+d))
				return m, nil
			}
		case importAccount:
			f.account = (f.account + d + len(m.accounts)) % len(m.accounts)
			return m, nil
		case importProfile:
			f.profile = (f.profile + d + len(f.profiles)) % len(f.profiles)
			return m, nil
		}
	}
	if f.field != importFile {
		return m, nil
	}
	var cmd tea.Cmd
	f.file, cmd = f.file.Update(msg)
	f.err = ""
	f.refreshFiles()
	return m, cmd
}

// reviewImport reads the file and asks the service what importing it would do.
// Problems are shown in the form and nothing is written.
func (m *Model) reviewImport() {
	f := m.imp
	fail := func(format string, args ...any) { f.err = fmt.Sprintf(format, args...) }

	path := f.chosenPath()
	if path == "" {
		fail("choose a CSV file: paste its path, or pick one from the list")
		return
	}
	profile, err := importer.FindProfile(f.profiles[f.profile], f.cfg.Profiles)
	if err != nil {
		fail("%v", err)
		return
	}
	acc := m.accounts[f.account].account

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail("no such file: %s", path)
		} else {
			fail("%v", err)
		}
		return
	}
	defer file.Close()
	parsed, err := importer.Parse(file, profile, acc.Currency)
	if err != nil {
		fail("%v", err)
		return
	}
	if len(parsed.Rows) == 0 && len(parsed.Errors) == 0 {
		fail("no booked transactions found (%d pending ignored); is the profile right?", parsed.Skipped)
		return
	}

	req := core.ImportRequest{Account: acc.Name, Rows: parsed.Rows, Rules: f.cfg.Rules, DryRun: true, IgnoreRejected: true}
	res, err := m.svc.Import(m.ctx, req)
	if err != nil {
		fail("%v", err)
		return
	}

	f.problems = f.problems[:0]
	for _, e := range parsed.Errors {
		f.problems = append(f.problems, e.Error())
	}
	for _, r := range res.Rejected {
		f.problems = append(f.problems, fmt.Sprintf("line %d: %s", r.Line, r.Reason))
	}
	f.newUncat = 0
	for _, r := range res.Rows {
		if !r.Duplicate && (r.Category == ledger.UncategorizedExpense || r.Category == ledger.UncategorizedIncome) {
			f.newUncat++
		}
	}
	f.path, f.req, f.res, f.skipped = path, req, res, parsed.Skipped
	f.list = listState{}
	f.err = ""
	f.step = stepReview
	m.lastProfile = f.profiles[f.profile]
}

func (m *Model) updateImportReview(key string) {
	f := m.imp
	_, h := m.importSize()
	switch key {
	case "esc":
		f.step, f.err = stepChoose, ""
	case "enter":
		switch {
		case f.res.Inserted == 0:
			m.closeImport()
			m.setStatus(fmt.Sprintf("Nothing new: all %d rows were already imported", f.res.Duplicates), false)
		case len(f.problems) > 0:
			f.err = fmt.Sprintf("%d row(s) have problems and would be skipped; press y to import the rest", len(f.problems))
		default:
			m.finishImport()
		}
	case "y":
		if f.res.Inserted > 0 {
			m.finishImport()
		}
	default:
		f.list.navigate(key, len(f.problems)+len(f.res.Rows), h-5)
	}
}

// finishImport stores the rows for real, makes the import undoable, and shows
// the account's transactions.
func (m *Model) finishImport() {
	f := m.imp
	req := f.req
	req.DryRun = false
	res, err := m.svc.Import(m.ctx, req)
	if err != nil {
		f.err = err.Error()
		return
	}
	acc := m.accounts[f.account].account
	outside := false
	for _, r := range f.res.Rows {
		if !r.Duplicate && m.period != "" && !strings.HasPrefix(r.Row.Date.Format(ledger.DateLayout), m.period) {
			outside = true
		}
	}
	newUncat := f.newUncat
	m.closeImport()

	if len(res.IDs) > 0 {
		ids := res.IDs
		m.undo = append(m.undo, undoOp{
			label: fmt.Sprintf("the import of %d transactions", len(ids)),
			run: func() error {
				var errs []error
				for _, id := range ids {
					errs = append(errs, m.svc.DeleteTransaction(m.ctx, id))
				}
				return errors.Join(errs...)
			},
		})
	}
	m.lastAccount = acc.Name
	if err := m.reload(); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	// Show what arrived: this account, newest first, and the rows that still
	// need a category. An export of older months needs the period widened.
	m.accts.cursor = m.accountIndex(acc.Name) + 1
	if newUncat > 0 {
		m.onlyUncat = true
	}
	if outside {
		m.setPeriod("")
	}
	m.setFocus(paneTransactions)
	m.applyFilter(false)

	msg := fmt.Sprintf("Imported %d into %s", res.Inserted, shortName(acc.Name))
	if res.Duplicates > 0 {
		msg += fmt.Sprintf(" · %d already there", res.Duplicates)
	}
	if newUncat > 0 {
		msg += fmt.Sprintf(" · %d need a category (u shows all)", newUncat)
	}
	m.setStatus(msg+" · z undoes", false)
}

func (m *Model) accountIndex(name string) int {
	for i, e := range m.accounts {
		if e.account.Name == name {
			return i
		}
	}
	return 0
}

// ---- drawing -------------------------------------------------------------------

func (m *Model) importPopup() string {
	f := m.imp
	w, h := m.importSize()
	iw := w - 2
	if f.step == stepReview {
		return m.importReviewPopup(w, h)
	}

	label := func(name string, field importField) string {
		if f.field == field {
			return accent.Render(fit("▸ "+name, 11))
		}
		return dim.Render(fit("  "+name, 11))
	}
	acc := m.accounts[f.account].account
	accText := fmt.Sprintf("%s · %s", acc.Name, acc.Currency)
	profText := f.profiles[f.profile]
	if f.field == importAccount {
		accText += dim.Render("   ◂ ▸ change")
	}
	if f.field == importProfile {
		profText += dim.Render("   ◂ ▸ change")
	}
	lines := []string{
		label("File", importFile) + " " + f.file.View(),
		label("Account", importAccount) + " " + accText,
		label("Profile", importProfile) + " " + profText,
		dim.Render(strings.Repeat("─", iw)),
	}
	lines = append(lines, m.importFileList(iw, h-7)...)
	for len(lines) < h-2-1 {
		lines = append(lines, "")
	}
	lines = append(lines, errStyle.Render(f.err))
	return box("Import a bank export", lines, w, h, true, "enter review · tab next field · esc cancel")
}

// importFileList is the list of files under the fields, or a hint.
func (m *Model) importFileList(w, h int) []string {
	f := m.imp
	typed := f.typedPath()
	switch {
	case f.browsing != "" && len(f.matches) == 0:
		return []string{dim.Render("No CSV files in " + tildePath(f.browsing) + ".")}
	case f.browsing == "" && typed != "" && looksLikePath(typed):
		return []string{dim.Render("enter reads this file")}
	case f.browsing == "" && len(f.files) == 0:
		return []string{
			dim.Render("No CSV files in " + tildePath(f.dir) + "."),
			dim.Render("Paste or drag a file's path (or a folder's) into the File field."),
		}
	case len(f.matches) == 0:
		return []string{dim.Render("No file matches. Paste or drag the file's path instead.")}
	}
	header := "CSV files in " + tildePath(f.dir) + ", newest first"
	switch {
	case f.browsing != "":
		header = "CSV files in " + tildePath(f.browsing) + ", newest first"
	case typed != "":
		header = "Matching files in " + tildePath(f.dir)
	}
	out := []string{dim.Render(fit(header, w))}
	start := 0
	if f.fileCur >= h-1 {
		start = f.fileCur - (h - 1) + 1
	}
	for i := start; i < len(f.matches) && len(out) < h; i++ {
		d := f.matches[i]
		when := d.modified.Format("2006-01-02 15:04")
		line := leftRight(d.name, dim.Render(when), w)
		if i == f.fileCur {
			line = selFocused.Render(fit(d.name+"  "+when, w))
		}
		out = append(out, line)
	}
	return out
}

// tildePath shortens a path under the home directory to ~/...
func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func (m *Model) importReviewPopup(w, h int) string {
	f := m.imp
	iw := w - 2
	listH := h - 5 // minus the border, the summary, its rule and the message line

	parts := []string{fmt.Sprintf("%d new", f.res.Inserted), fmt.Sprintf("%d already imported", f.res.Duplicates)}
	if f.newUncat > 0 {
		parts = append(parts, warn.Render(fmt.Sprintf("%d uncategorized", f.newUncat)))
	}
	if f.skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d pending ignored", f.skipped))
	}
	if len(f.problems) > 0 {
		parts = append(parts, errStyle.Render(fmt.Sprintf("%d with problems", len(f.problems))))
	}
	lines := []string{
		fit(strings.Join(parts, dim.Render(" · ")), iw),
		dim.Render(strings.Repeat("─", iw)),
	}

	amtW := 0
	for _, r := range f.res.Rows {
		amtW = max(amtW, lipgloss.Width(importAmount(r.Row.Amount)))
	}
	total := len(f.problems) + len(f.res.Rows)
	ls := f.list
	ls.clamp(total, listH)
	f.list = ls
	for i := ls.offset; i < total && len(lines)-2 < listH; i++ {
		if i < len(f.problems) {
			lines = append(lines, warn.Render(fit("⚠ "+f.problems[i], iw)))
			continue
		}
		lines = append(lines, importRowLine(f.res.Rows[i-len(f.problems)], iw, amtW, i == ls.cursor))
	}
	for len(lines) < h-2-1 {
		lines = append(lines, "")
	}
	switch {
	case f.err != "":
		lines = append(lines, errStyle.Render(fit(f.err, iw)))
	case len(f.problems) > 0:
		lines = append(lines, warn.Render(fit("Rows with problems are skipped.", iw)))
	default:
		lines = append(lines, "")
	}

	footer := "enter import · esc back"
	switch {
	case f.res.Inserted == 0:
		footer = "enter close · esc back"
	case len(f.problems) > 0:
		footer = "y import the rest · esc back"
	}
	title := fmt.Sprintf("Import %s → %s", filepath.Base(f.path), shortName(f.req.Account))
	return box(title, lines, w, h, true, footer)
}

// importAmount is an amount without its currency: every row of an import is in
// the currency of the account, which the title names.
func importAmount(a ledger.Money) string {
	if a.Minor > 0 {
		return "+" + a.Decimal()
	}
	return a.Decimal()
}

// importRowLine is one row of the review list: whether it is new, its date,
// the category the rules chose, the payee and the amount.
func importRowLine(r core.ImportedRow, w, amtW int, selected bool) string {
	status := "new"
	if r.Duplicate {
		status = "dup"
	}
	catW := 16
	payeeW := max(4, w-17-catW-amtW)
	category := shortName(r.Category)
	date := r.Row.Date.Format(ledger.DateLayout)
	payee := fit(clean(r.Row.Payee), payeeW)
	amount := fitRight(importAmount(r.Row.Amount), amtW)

	if selected {
		return selFocused.Render(fit(status+" "+date+" "+fit(category, catW)+" "+payee+" "+amount, w))
	}
	if r.Duplicate {
		return dim.Render(fit(status+" "+date+" "+fit(category, catW)+" "+payee+" "+amount, w))
	}
	catText := fit(category, catW)
	if r.Category == ledger.UncategorizedExpense || r.Category == ledger.UncategorizedIncome {
		catText = warn.Render(catText)
	}
	if r.Row.Amount.Minor > 0 {
		amount = accent.Render(amount)
	}
	return accent.Render(status) + " " + date + " " + catText + " " + payee + " " + amount
}
