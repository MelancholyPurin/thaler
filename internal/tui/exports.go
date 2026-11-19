package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/config"
)

// While the interface runs it looks at the import folder every few seconds and says,
// in the bottom bar, when a bank export (CSV, OFX or QIF) has arrived that was not
// there before: the export that was just downloaded. Only names, sizes and times are read, never the
// content, and `watch_import_dir = false` in config.toml stops it. What was in the
// folder when the interface started, or when the folder changed, is not new.

// exportCheckEvery is how often the folder is looked at.
const exportCheckEvery = 3 * time.Second

// exportTick is the moment to look at the folder again.
type exportTick struct{}

// exportsSeen is what a look at the folder found. dir is empty when watching is off.
type exportsSeen struct {
	dir   string
	files []bankFile
}

// exportWatch is what has been seen in the import folder.
type exportWatch struct {
	dir   string
	known map[string]bool // the files seen so far, by exportKey
	fresh []bankFile      // the ones that arrived since, newest first, until the import is opened
}

// exportKey names a file as it is now: a file saved again under the same name is another.
func exportKey(f bankFile) string {
	return fmt.Sprintf("%s|%d|%d", f.path, f.size, f.modified.UnixNano())
}

// importDir is the folder exports are listed from.
func (m *Model) importDir(cfg *config.Config) string {
	if cfg != nil && cfg.ImportDir != "" {
		return expandHome(cfg.ImportDir)
	}
	return m.downloads
}

// scanExports looks at the import folder, away from the screen so that a slow
// folder cannot hold it up. The answer is an exportsSeen.
func (m *Model) scanExports() tea.Cmd {
	var cfg *config.Config
	if m.loadConfig != nil {
		cfg, _ = m.loadConfig() // a config that cannot be read is the defaults, until it is mended
	}
	dir := m.importDir(cfg)
	if !cfg.WatchesImportDir() || dir == "" {
		return func() tea.Msg { return exportsSeen{} }
	}
	return func() tea.Msg { return exportsSeen{dir: dir, files: listExports(dir)} }
}

// noticeExports takes in what a look at the folder found, and returns the command for
// the next look.
func (m *Model) noticeExports(seen exportsSeen) tea.Cmd {
	w := &m.watch
	next := tea.Tick(exportCheckEvery, func(time.Time) tea.Msg { return exportTick{} })
	if seen.dir == "" || seen.dir != w.dir {
		// Off, or a first look at this folder: what is there is not new.
		*w = exportWatch{dir: seen.dir, known: map[string]bool{}}
		for _, f := range seen.files {
			w.known[exportKey(f)] = true
		}
		return next
	}
	var arrived []bankFile
	present := map[string]bool{}
	for _, f := range seen.files {
		k := exportKey(f)
		present[k] = true
		if !w.known[k] {
			w.known[k] = true
			arrived = append(arrived, f)
		}
	}
	// A file that arrived and was taken away again, or saved again since, is offered as it is now, once.
	var fresh []bankFile
	for _, f := range append(arrived, w.fresh...) {
		if present[exportKey(f)] {
			fresh = append(fresh, f)
		}
	}
	w.fresh = fresh
	if len(arrived) > 0 {
		m.offerNewExports(seen)
	}
	return next
}

// offerNewExports puts the arrival in front of someone who already has the list of files
// open: it is listed, and highlighted unless they have moved down the list. The popup
// then is what tells them, so nothing is left to say in the bottom bar.
func (m *Model) offerNewExports(seen exportsSeen) {
	f := m.imp
	if f == nil || f.step != stepChoose || f.dir != seen.dir {
		return
	}
	f.files = seen.files
	if f.fileCur <= 0 {
		f.refreshFiles()
		f.detect()
	}
	m.watch.fresh = nil
}

// exportNotes is the offer in the bottom bar to import what arrived, or nothing.
func (m *Model) exportNotes() []string {
	fresh := m.watch.fresh
	if len(fresh) == 0 {
		return nil
	}
	return []string{m.loc.T("export_new", map[string]any{"Count": len(fresh), "Name": clean(fresh[0].name)})}
}
