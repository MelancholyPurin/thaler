package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/config"
)

// The backup popup writes a snapshot of the database where you say, by default
// into the backups folder next to it, and shows when the last one was made. With
// a passphrase the snapshot is encrypted.

// backupFile is a backup found in the backups folder.
type backupFile struct {
	path      string
	when      time.Time
	size      int64
	encrypted bool
	replaced  bool // a database that a restore replaced, not a backup made on purpose
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

func (m *Model) backupWidth() int {
	return min(max(m.width-6, 30), 76)
}

func (m *Model) backupInputWidth() int {
	return max(8, m.backupWidth()-2-backupLabelWidth-1-1) // minus the border, the label, a space and the cursor cell
}

// listBackups is every backup in dir, newest first. A folder that is missing
// or unreadable simply has none.
func listBackups(dir string) []backupFile {
	var files []backupFile
	for _, glob := range config.BackupGlobs {
		paths, _ := filepath.Glob(filepath.Join(dir, glob))
		for _, p := range paths {
			info, err := os.Stat(p)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			files = append(files, backupFile{
				path: p, when: info.ModTime(), size: info.Size(),
				encrypted: strings.HasSuffix(p, config.EncryptedSuffix),
			})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].when.After(files[j].when) })
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
		m.saveBackup()
		return m, nil
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

func (m *Model) saveBackup() {
	f := m.bkp
	if f.encrypting() && f.pass.Value() != f.repeat.Value() {
		f.err = "the two passphrases differ: type it again in Repeat"
		m.focusBackup(bfRepeat)
		return
	}
	if !f.encrypting() && f.repeat.Value() != "" {
		f.err = "Repeat is filled in but Passphrase is empty"
		m.focusBackup(bfPass)
		return
	}
	dest, err := m.backupTarget(f.dest.Value(), f.encrypting())
	if err != nil {
		f.err = err.Error()
		return
	}
	if f.encrypting() {
		err = m.svc.BackupEncrypted(m.ctx, dest, f.pass.Value())
	} else {
		err = m.svc.Backup(m.ctx, dest)
	}
	if err != nil {
		f.err = err.Error()
		return
	}
	encrypted := f.encrypting()
	m.mode, m.bkp = modeList, nil
	size := "?"
	if info, err := os.Stat(dest); err == nil {
		size = humanSize(info.Size())
	}
	how := "checked, NOT encrypted"
	if encrypted {
		how = "checked, encrypted"
	}
	m.setStatus(fmt.Sprintf("Backed up (%s, %s) → %s", size, how, tailFit(dest, max(24, m.width-56))), false)
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
		db += " · " + humanSize(f.dbSize)
	}
	last := "never: enter makes the first one"
	if len(f.files) > 0 {
		b := f.files[0]
		last = fmt.Sprintf("%s · %s · %s", b.when.Local().Format("2006-01-02 15:04"), ago(m.now(), b.when), humanSize(b.size))
		if b.encrypted {
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
	lines = append(lines, errStyle.Render(fit(f.err, iw)))
	return box("Back up the database", lines, w, h, true, "enter back up · tab next field · esc cancel")
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

// humanSize is a file size for reading: 512 B, 12.4 KB, 3.1 MB.
func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// ago says how long before now t was, in calendar days: today, yesterday, 3 days ago.
func ago(now, t time.Time) string {
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.In(now.Location()).Date()
	days := int(time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC).Sub(time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}
