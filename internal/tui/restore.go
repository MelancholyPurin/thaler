package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/units"
)

// The restore popup puts one of the backups in the backups folder in place of
// the database. The database it replaces is not deleted: it is kept in the
// "replaced" folder, which the popup lists too, so a restore can be taken back.

// RestoreFunc replaces the database with the backup src (see core.RestoreBackup).
// The interface cannot do that itself because it does not own the open database:
// whoever does lets go of it, restores, and opens again.
//
// It returns the service for the database that is in place afterwards, also when
// the restore was refused: then that is the old database, opened again, and err
// says why. A nil service means that no database could be opened, and the
// interface ends.
type RestoreFunc func(ctx context.Context, src, passphrase string) (core.Service, core.RestoreResult, error)

type restoreField int

const (
	rfFile restoreField = iota
	rfPass
	numRestoreFields
)

// restoreRows is how many backups the popup shows at once.
const restoreRows = 5

type restoreForm struct {
	field   restoreField
	files   []backupFile // backups first, newest first, then the databases that restores replaced
	cursor  int
	pass    textinput.Model
	confirm bool // asking "really?" about files[cursor]
	dbSize  int64
	err     string
}

func (f *restoreForm) selected() backupFile { return f.files[f.cursor] }

// listRestorable is the backups in dir, then what restores have replaced,
// both newest first. Replaced databases are marked so as to be told apart.
func listRestorable(dir, dbPath string) []backupFile {
	files := listBackups(dir)
	for _, b := range listBackups(config.ReplacedDir(dbPath)) {
		b.replaced = true
		files = append(files, b)
	}
	return files
}

// openRestore shows the popup, on the newest backup.
func (m *Model) openRestore() tea.Cmd {
	if m.opts.Restore == nil || m.opts.BackupDir == "" || m.opts.DatabasePath == "" {
		m.setStatus("restoring is not possible here: this database is not a file", true)
		return nil
	}
	f := &restoreForm{files: listRestorable(m.opts.BackupDir, m.opts.DatabasePath)}
	if info, err := os.Stat(m.opts.DatabasePath); err == nil && info.Mode().IsRegular() {
		f.dbSize = info.Size()
	}
	f.pass = newInput("")
	f.pass.CharLimit = 0
	f.pass.EchoMode = textinput.EchoPassword
	f.pass.EchoCharacter = '•'
	f.pass.Width = m.backupInputWidth()
	m.rst = f
	m.mode = modeRestore
	return m.focusRestore(rfFile)
}

func (m *Model) focusRestore(field restoreField) tea.Cmd {
	f := m.rst
	f.field = field
	f.pass.Blur()
	if field == rfPass {
		return f.pass.Focus()
	}
	return nil
}

func (m *Model) updateRestore(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.rst
	key := msg.String()
	if f.confirm {
		switch key {
		case "enter", "y":
			return m.restore()
		case "esc", "n":
			f.confirm = false
		}
		return m, nil
	}
	switch key {
	case "esc":
		m.mode, m.rst = modeList, nil
		return m, nil
	case "enter", "ctrl+s":
		m.askRestore()
		return m, nil
	case "tab", "shift+tab":
		return m, m.focusRestore((f.field + 1) % numRestoreFields)
	}
	if f.field == rfFile {
		switch key {
		case "up", "k":
			f.cursor = max(f.cursor-1, 0)
		case "down", "j":
			f.cursor = min(f.cursor+1, max(len(f.files)-1, 0))
		case "home", "g":
			f.cursor = 0
		case "end", "G":
			f.cursor = max(len(f.files)-1, 0)
		}
		f.err = ""
		f.pass.SetValue("") // a passphrase is for the backup it was typed for
		return m, nil
	}
	if key == "up" || key == "down" {
		return m, m.focusRestore(rfFile)
	}
	var cmd tea.Cmd
	f.pass, cmd = f.pass.Update(msg)
	f.err = ""
	return m, cmd
}

// askRestore checks what can be checked without reading the backup, then asks
// for the go-ahead.
func (m *Model) askRestore() {
	f := m.rst
	if len(f.files) == 0 {
		f.err = "there is no backup to restore: S makes one"
		return
	}
	if b := f.selected(); b.Encrypted && f.pass.Value() == "" {
		f.err = filepath.Base(b.Path) + " is encrypted: type its passphrase"
		m.focusRestore(rfPass)
		return
	}
	f.confirm = true
}

// restoreDone is the end of the work that restore starts.
type restoreDone struct {
	from backupFile
	svc  core.Service
	res  core.RestoreResult
	err  error
}

// restore starts replacing the database with the chosen backup.
func (m *Model) restore() (tea.Model, tea.Cmd) {
	f := m.rst
	b, pass, hook, ctx := f.selected(), f.pass.Value(), m.opts.Restore, m.ctx
	note := "Checking the backup, then replacing the database"
	if b.Encrypted {
		note = "Decrypting and checking the backup (slow on purpose)"
	}
	return m, m.start(note, func() tea.Msg {
		svc, res, err := hook(ctx, b.Path, pass)
		return restoreDone{from: b, svc: svc, res: res, err: err}
	})
}

// finishRestore shows how the restore went.
func (m *Model) finishRestore(msg restoreDone) (tea.Model, tea.Cmd) {
	quit := m.idle()
	f, b, res, err := m.rst, msg.from, msg.res, msg.err
	if msg.svc == nil {
		m.fatal = fmt.Errorf("restoring %s: %w", b.Path, err)
		return m, tea.Quit
	}
	m.svc = msg.svc
	if err != nil {
		f.confirm, f.err = false, m.errText(err)
		if b.Encrypted {
			m.focusRestore(rfPass)
		}
		return m, quit
	}

	m.mode, m.rst = modeList, nil
	m.undo, m.lastAccount = nil, "" // they are about the database that was replaced
	m.accts = listState{}
	m.txs = listState{}
	m.filter.SetValue("")
	m.onlyUncat = false
	if err := m.reload(); err != nil {
		m.fatal = fmt.Errorf("reading the restored database: %w", err)
		return m, tea.Quit
	}
	m.applyFilter(false)
	m.clampAll()

	text := "Restored " + filepath.Base(b.Path)
	if res.Upgrade {
		text += " (upgraded to this version)"
	}
	if res.Replaced != "" {
		text += " · the database before is kept: ctrl+r lists it"
	}
	m.setStatus(text, false)
	return m, quit
}

func (m *Model) restorePopup() string {
	f := m.rst
	w := m.backupWidth()
	iw := w - 2
	label := func(s string) string { return dim.Render(fit("  "+s, backupLabelWidth)) }
	value := func(s string) string { return fit(s, iw-backupLabelWidth-1) }

	db := m.opts.DatabasePath
	if f.dbSize > 0 {
		db += " · " + units.HumanSize(f.dbSize)
	}
	lines := []string{
		label("Database") + " " + value(tailFit(db, iw-backupLabelWidth-1)),
		label("Folder") + " " + value(tailFit(m.opts.BackupDir, iw-backupLabelWidth-1)),
		dim.Render(strings.Repeat("─", iw)),
	}
	footer := "enter restore · ↑/↓ choose · tab passphrase · esc cancel"
	if f.confirm {
		lines = append(lines, m.restoreConfirmLines(iw)...)
		footer = "enter yes, restore · esc back"
	} else {
		lines = append(lines, m.restoreListLines(iw)...)
		pass := label("Passphrase") + " " + f.pass.View()
		if f.field == rfPass {
			pass = accent.Render(fit("▸ Passphrase", backupLabelWidth)) + " " + f.pass.View()
		}
		lines = append(lines, "", pass, "")
		for _, h := range []string{
			"The backup is checked before anything changes. Your current",
			"database is kept, not deleted: it is listed here as replaced.",
			"Close any other Thaler first. A file elsewhere: thaler cli restore FILE",
		} {
			lines = append(lines, dim.Render(fit(h, iw)))
		}
	}
	h := 17
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	last := errStyle.Render(fit(f.err, iw))
	if m.busy != nil {
		last, footer = accent.Render(fit(m.busy.line(), iw)), m.footerBusy()
	}
	lines = append(lines, last)
	return box("Restore a backup", lines, w, h, true, footer)
}

// restoreListLines is the backups, a window of them around the cursor.
func (m *Model) restoreListLines(iw int) []string {
	f := m.rst
	if len(f.files) == 0 {
		return []string{dim.Render(fit("  No backups yet: S makes one.", iw))}
	}
	top := min(max(f.cursor-restoreRows+1, 0), max(len(f.files)-restoreRows, 0))
	var lines []string
	for i := top; i < min(top+restoreRows, len(f.files)); i++ {
		b := f.files[i]
		row := fmt.Sprintf("%s · %s · %s", b.When.Local().Format("2006-01-02 15:04"), ago(m.now(), b.When), units.HumanSize(b.Size))
		switch {
		case b.replaced:
			row += " · replaced by a restore"
		case b.Encrypted:
			row += " · encrypted"
		}
		if i == f.cursor {
			lines = append(lines, accent.Render(fit("▸ "+row, iw)))
		} else {
			lines = append(lines, dim.Render(fit("  "+row, iw)))
		}
	}
	for len(lines) < restoreRows {
		lines = append(lines, "")
	}
	return lines
}

func (m *Model) restoreConfirmLines(iw int) []string {
	f := m.rst
	b := f.selected()
	return []string{
		fit("Replace the database with", iw),
		accent.Render(fit("  "+filepath.Base(b.Path), iw)),
		dim.Render(fit(fmt.Sprintf("  %s · %s · %s", b.When.Local().Format("2006-01-02 15:04"), ago(m.now(), b.When), units.HumanSize(b.Size)), iw)),
		"",
		fit("What you have now is kept in", iw),
		dim.Render(fit("  "+tailFit(config.ReplacedDir(m.opts.DatabasePath), iw-2), iw)),
		fit("and can be restored from here the same way.", iw),
	}
}
