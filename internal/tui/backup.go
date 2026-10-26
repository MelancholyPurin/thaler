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
// into the backups folder next to it, and shows when the last one was made.

// backupFile is a backup found in the backups folder.
type backupFile struct {
	path string
	when time.Time
	size int64
}

type backupForm struct {
	dest   textinput.Model
	files  []backupFile // in the backups folder, newest first
	dbSize int64        // the database file, 0 when it is not a file (in memory)
	err    string
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
	paths, _ := filepath.Glob(filepath.Join(dir, config.BackupGlob))
	var files []backupFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, backupFile{path: p, when: info.ModTime(), size: info.Size()})
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
	f.dest.Width = m.backupInputWidth()
	f.dest.SetValue(filepath.Join(m.opts.BackupDir, config.BackupName(m.now())))
	m.bkp = f
	m.mode = modeBackup
	return f.dest.Focus()
}

func (m *Model) updateBackup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.bkp = modeList, nil
		return m, nil
	case "enter", "ctrl+s":
		m.saveBackup()
		return m, nil
	}
	var cmd tea.Cmd
	m.bkp.dest, cmd = m.bkp.dest.Update(msg)
	m.bkp.err = ""
	return m, cmd
}

// backupTarget turns what was typed into the file to write: ~ is your home
// folder, a relative path is relative to where Thaler was started, and a folder
// gets the usual timestamped file name inside it.
func (m *Model) backupTarget(typed string) (string, error) {
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
	return dest, nil
}

func (m *Model) saveBackup() {
	f := m.bkp
	dest, err := m.backupTarget(f.dest.Value())
	if err != nil {
		f.err = err.Error()
		return
	}
	if err := m.svc.Backup(m.ctx, dest); err != nil {
		f.err = err.Error()
		return
	}
	m.mode, m.bkp = modeList, nil
	size := "?"
	if info, err := os.Stat(dest); err == nil {
		size = humanSize(info.Size())
	}
	m.setStatus(fmt.Sprintf("Backed up (%s, checked, not encrypted) → %s", size, tailFit(dest, max(24, m.width-56))), false)
}

func (m *Model) backupPopup() string {
	f := m.bkp
	w := m.backupWidth()
	iw := w - 2
	label := func(s string) string { return dim.Render(fit("  "+s, backupLabelWidth)) }
	value := func(s string) string { return fit(s, iw-backupLabelWidth-1) }

	db := m.opts.DatabasePath
	if f.dbSize > 0 {
		db += " · " + humanSize(f.dbSize)
	}
	last := "never: enter makes the first one"
	if len(f.files) > 0 {
		b := f.files[0]
		last = fmt.Sprintf("%s · %s · %s", b.when.Local().Format("2006-01-02 15:04"), ago(m.now(), b.when), humanSize(b.size))
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
		accent.Render(fit("▸ Save to", backupLabelWidth)) + " " + f.dest.View(),
		"",
		dim.Render(fit("A plain SQLite file, NOT encrypted: keep it on an", iw)),
		dim.Render(fit("encrypted disk, or encrypt it before copying it away.", iw)),
		dim.Render(fit("A folder is fine too: it gets a timestamped name.", iw)),
	}
	h := 14
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	lines = append(lines, errStyle.Render(fit(f.err, iw)))
	return box("Back up the database", lines, w, h, true, "enter back up · esc cancel")
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
