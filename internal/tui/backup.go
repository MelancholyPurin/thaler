package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/units"
)

// The backup popup writes a snapshot of the database where you say, by default
// into the backups folder next to it, and shows when the last one was made. With
// a passphrase the snapshot is encrypted.

// backupFile is a backup found in the backups folder.
type backupFile struct {
	core.BackupFile
	replaced bool // a database that a restore replaced, not a backup made on purpose
}

type backupField int

const (
	bfDest backupField = iota
	bfPass
	bfRepeat
	numBackupFields
)

type backupForm struct {
	field  backupField
	dest   textinput.Model
	pass   textinput.Model
	repeat textinput.Model
	files  []backupFile // in the backups folder, newest first
	dbSize int64        // the database file, 0 when it is not a file (in memory)
	err    string
}

func (f *backupForm) encrypting() bool { return f.pass.Value() != "" }

func (f *backupForm) input(field backupField) *textinput.Model {
	switch field {
	case bfPass:
		return &f.pass
	case bfRepeat:
		return &f.repeat
	}
	return &f.dest
}

const backupLabelWidth = 13

// refreshBackups looks up when the newest backup was made, how many days may pass
// before the reminder shows, and which backups are more than are kept. It is called when a backup is made or
// the config edited: not on every redraw, since it reads the disk.
func (m *Model) refreshBackups() {
	m.lastBackup, m.remindDays, m.prunable = time.Time{}, 0, core.PrunePlan{}
	m.backupChecked = m.now()
	if m.opts.BackupDir == "" {
		return
	}
	var cfg *config.Config // nil when it cannot be read: the defaults then apply
	if m.loadConfig != nil {
		if c, err := m.loadConfig(); err == nil {
			cfg = c
		}
	}
	m.remindDays = cfg.ReminderDays()
	if files := listBackups(m.opts.BackupDir); len(files) > 0 {
		m.lastBackup = files[0].When
	}
	m.prunable = m.pruneFor(cfg)
}

// backupCheckEvery is how often a key press looks at the backups folder again, so
// that a backup made by "thaler cli backup" while the interface is open (from
// cron, say) ends the reminder without a restart.
const backupCheckEvery = time.Minute

func (m *Model) refreshBackupsIfStale() {
	if m.now().Sub(m.backupChecked) >= backupCheckEvery {
		m.refreshBackups()
	}
}

// backupReminder is the text asking for a backup, or "" while none is due: the
// reminder is switched off, there is nothing worth backing up yet, or the newest
// backup is recent enough.
func (m *Model) backupReminder() string {
	if m.remindDays <= 0 || len(m.rows) == 0 {
		return ""
	}
	if m.lastBackup.IsZero() {
		return "No backup yet: S makes one"
	}
	if days := daysAgo(m.now(), m.lastBackup); days >= m.remindDays {
		return fmt.Sprintf("Last backup %d days ago: S makes a new one", days)
	}
	return ""
}

// backupNotes are the offers in the bottom bar about backups, most urgent first.
func (m *Model) backupNotes() []string {
	var notes []string
	if r := m.backupReminder(); r != "" {
		notes = append(notes, r)
	}
	if n := m.prunable.Count(); n > 0 {
		notes = append(notes, m.loc.T("old_backups_removable", map[string]any{"Count": n}))
	}
	return notes
}

func (m *Model) backupWidth() int {
	return min(max(m.width-6, 30), 76)
}

func (m *Model) backupInputWidth() int {
	return max(8, m.backupWidth()-2-backupLabelWidth-1-1) // minus the border, the label, a space and the cursor cell
}

// listBackups is every backup in dir, newest first. A folder that is missing
// or unreadable simply has none.
func listBackups(dir string) []backupFile {
	found, _ := core.ListBackups(dir)
	files := make([]backupFile, len(found))
	for i, f := range found {
		files[i] = backupFile{BackupFile: f}
	}
	return files
}

// openBackup shows the popup, ready to write a new backup into the backups folder.
func (m *Model) openBackup() tea.Cmd {
	if m.opts.BackupDir == "" {
		m.setStatus("no backup location is known", true)
		return nil
	}
	f := &backupForm{files: listBackups(m.opts.BackupDir)}
	if info, err := os.Stat(m.opts.DatabasePath); err == nil && info.Mode().IsRegular() {
		f.dbSize = info.Size()
	}
	f.dest = newInput("")
	f.dest.CharLimit = 0 // paths are long; set before the value, or it is cut at 80
	f.pass, f.repeat = newInput(""), newInput("")
	for _, in := range []*textinput.Model{&f.pass, &f.repeat} {
		in.CharLimit = 0
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
	}
	m.sizeBackup(f)
	f.dest.SetValue(filepath.Join(m.opts.BackupDir, config.BackupName(m.now())))
	m.bkp = f
	m.mode = modeBackup
	return m.focusBackup(bfDest)
}

func (m *Model) sizeBackup(f *backupForm) {
	for _, in := range []*textinput.Model{&f.dest, &f.pass, &f.repeat} {
		in.Width = m.backupInputWidth()
	}
}

func (m *Model) focusBackup(field backupField) tea.Cmd {
	f := m.bkp
	f.field = field
	f.dest.Blur()
	f.pass.Blur()
	f.repeat.Blur()
	return f.input(field).Focus()
}

func (m *Model) updateBackup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.bkp
	move := func(d int) (tea.Model, tea.Cmd) {
		return m, m.focusBackup((f.field + backupField(d) + numBackupFields) % numBackupFields)
	}
	switch msg.String() {
	case "esc":
		m.mode, m.bkp = modeList, nil
		return m, nil
	case "enter", "ctrl+s":
		return m, m.saveBackup()
	case "tab", "down":
		return move(1)
	case "shift+tab", "up":
		return move(-1)
	}
	var cmd tea.Cmd
	in := f.input(f.field)
	*in, cmd = in.Update(msg)
	f.err = ""
	return m, cmd
}

// backupTarget turns what was typed into the file to write: ~ is your home
// folder, a relative path is relative to where Thaler was started, a folder
// gets the usual timestamped file name inside it, and an encrypted backup ends
// in .age.
func (m *Model) backupTarget(typed string, encrypt bool) (string, error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return "", fmt.Errorf("say where to write the backup")
	}
	if typed == "~" || strings.HasPrefix(typed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		typed = filepath.Join(home, strings.TrimPrefix(typed, "~"))
	}
	dest, err := filepath.Abs(typed)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		dest = filepath.Join(dest, config.BackupName(m.now()))
	}
	if encrypt {
		dest = config.EncryptedName(dest)
	}
	return dest, nil
}

// backupDone is the end of the work that saveBackup starts.
type backupDone struct {
	dest      string
	encrypted bool
	err       error
}

// saveBackup checks what was typed and starts writing the backup.
func (m *Model) saveBackup() tea.Cmd {
	f := m.bkp
	if f.encrypting() && f.pass.Value() != f.repeat.Value() {
		f.err = "the two passphrases differ: type it again in Repeat"
		return m.focusBackup(bfRepeat)
	}
	if !f.encrypting() && f.repeat.Value() != "" {
		f.err = "Repeat is filled in but Passphrase is empty"
		return m.focusBackup(bfPass)
	}
	dest, err := m.backupTarget(f.dest.Value(), f.encrypting())
	if err != nil {
		f.err = m.errText(err)
		return nil
	}
	ctx, svc, pass, encrypting := m.ctx, m.svc, f.pass.Value(), f.encrypting()
	note := "Writing the backup, then checking it"
	if encrypting {
		note = "Encrypting the backup (slow on purpose: a moment)"
	}
	return m.start(note, func() tea.Msg {
		var err error
		if encrypting {
			err = svc.BackupEncrypted(ctx, dest, pass)
		} else {
			err = svc.Backup(ctx, dest)
		}
		return backupDone{dest: dest, encrypted: encrypting, err: err}
	})
}

// finishBackup shows how the backup went.
func (m *Model) finishBackup(msg backupDone) tea.Cmd {
	quit := m.idle()
	if msg.err != nil {
		m.bkp.err = m.errText(msg.err)
		return quit
	}
	m.mode, m.bkp = modeList, nil
	m.refreshBackups()
	size := "?"
	if info, err := os.Stat(msg.dest); err == nil {
		size = units.HumanSize(info.Size())
	}
	how := "checked, NOT encrypted"
	if msg.encrypted {
		how = "checked, encrypted"
	}
	m.setStatus(fmt.Sprintf("Backed up (%s, %s) → %s", size, how, tailFit(msg.dest, max(24, m.width-56))), false)
	return quit
}

func (m *Model) backupPopup() string {
	f := m.bkp
	w := m.backupWidth()
	iw := w - 2
	label := func(s string) string { return dim.Render(fit("  "+s, backupLabelWidth)) }
	value := func(s string) string { return fit(s, iw-backupLabelWidth-1) }
	field := func(name string, which backupField, in textinput.Model) string {
		if f.field == which {
			return accent.Render(fit("▸ "+name, backupLabelWidth)) + " " + in.View()
		}
		return label(name) + " " + in.View()
	}

	db := m.opts.DatabasePath
	if f.dbSize > 0 {
		db += " · " + units.HumanSize(f.dbSize)
	}
	last := "never: enter makes the first one"
	if len(f.files) > 0 {
		b := f.files[0]
		last = fmt.Sprintf("%s · %s · %s", b.When.Local().Format("2006-01-02 15:04"), ago(m.now(), b.When), units.HumanSize(b.Size))
		if b.Encrypted {
			last += " · encrypted"
		} else {
			last += " · not encrypted"
		}
	}
	folder := m.opts.BackupDir
	switch len(f.files) {
	case 0:
	case 1:
		folder += " · 1 backup"
	default:
		folder += fmt.Sprintf(" · %d backups", len(f.files))
	}
	lines := []string{
		label("Database") + " " + value(tailFit(db, iw-backupLabelWidth-1)),
		label("Last backup") + " " + value(last),
		label("Folder") + " " + value(tailFit(folder, iw-backupLabelWidth-1)),
		dim.Render(strings.Repeat("─", iw)),
		field("Save to", bfDest, f.dest),
		field("Passphrase", bfPass, f.pass),
		field("Repeat", bfRepeat, f.repeat),
		"",
	}
	for _, h := range m.backupHints() {
		lines = append(lines, dim.Render(fit(h, iw)))
	}
	h := 15
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	last, footer := errStyle.Render(fit(f.err, iw)), "enter back up · tab next field · esc cancel"
	if m.busy != nil {
		last, footer = accent.Render(fit(m.busy.line(), iw)), m.footerBusy()
	}
	lines = append(lines, last)
	return box("Back up the database", lines, w, h, true, footer)
}

// backupHints say what the backup will be and what that means for you.
func (m *Model) backupHints() []string {
	f := m.bkp
	if f.encrypting() {
		return []string{
			"Encrypted with age; the file gets the ending .age.",
			"Lose the passphrase and the backup cannot be opened.",
			"Open it again with: thaler cli decrypt FILE",
		}
	}
	return []string{
		"A plain SQLite file, NOT encrypted. Type a passphrase to",
		"encrypt it, or keep it on an encrypted disk. A folder in",
		"Save to gets a timestamped name.",
	}
}

// tailFit shortens s to w cells by cutting from the left, which keeps the part
// of a path that tells files apart.
func tailFit(s string, w int) string {
	r := []rune(s)
	if w <= 1 || len(r) <= w {
		return s
	}
	return "…" + string(r[len(r)-(w-1):])
}

// daysAgo is how many calendar days before now t was.
func daysAgo(now, t time.Time) int {
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.In(now.Location()).Date()
	return int(time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC).Sub(time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

// ago says how long before now t was, in calendar days: today, yesterday, 3 days ago.
func ago(now, t time.Time) string {
	days := daysAgo(now, t)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}
