package tui

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
)

// The prune popup removes the oldest backups, keeping the newest few in each
// backups folder. Nothing is removed by itself: the bottom bar says when there
// are some to remove, and this popup lists exactly which before asking.

type pruneLine struct {
	text    string
	heading bool
}

type pruneForm struct {
	plan  core.PrunePlan
	lines []pruneLine
	top   int // first line shown
	err   string
}

// pruneRows is how many lines of the list the popup shows at once.
const pruneRows = 9

// pruneFor is what would be pruned under cfg, or nothing: while the database is
// not a file, when keeping everything is asked for, or when nothing is over.
func (m *Model) pruneFor(cfg *config.Config) core.PrunePlan {
	keep := cfg.KeepCount()
	if m.opts.DatabasePath == "" || keep < 1 {
		return core.PrunePlan{}
	}
	plan, err := core.PlanPrune(m.opts.DatabasePath, keep)
	if err != nil {
		return core.PrunePlan{}
	}
	return plan
}

// openPrune lists what would be removed, looking at the folders afresh.
func (m *Model) openPrune() {
	m.refreshBackups()
	plan := m.prunable
	if plan.Count() == 0 {
		m.setStatus(m.nothingToPrune(), false)
		return
	}
	f := &pruneForm{plan: plan}
	for _, folder := range plan.Folders {
		f.lines = append(f.lines, pruneLine{text: m.loc.T("prune_folder_heading", map[string]any{"Folder": m.loc.BackupFolder(folder.Kind), "Count": len(folder.Remove), "Total": folder.Total}), heading: true})
		for _, file := range folder.Remove {
			f.lines = append(f.lines, pruneLine{text: fmt.Sprintf("  %s · %s · %s",
				m.loc.DateTime(file.When.Local()), m.loc.Size(file.Size), filepath.Base(file.Path))})
		}
	}
	m.prn = f
	m.mode = modePrune
}

func (m *Model) nothingToPrune() string {
	var cfg *config.Config
	if m.loadConfig != nil {
		if c, err := m.loadConfig(); err == nil {
			cfg = c
		}
	}
	switch {
	case m.opts.DatabasePath == "":
		return m.loc.T("prune_not_a_file", nil)
	case cfg.KeepCount() == 0:
		return m.loc.T("prune_keep_zero", nil)
	}
	return m.loc.T("prune_nothing_over", map[string]any{"Count": cfg.KeepCount()})
}

func (m *Model) updatePrune(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.prn
	maxTop := max(len(f.lines)-pruneRows, 0)
	switch msg.String() {
	case "esc", "n":
		m.mode, m.prn = modeList, nil
	case "enter", "y":
		m.prune()
	case "up", "k":
		f.top = max(f.top-1, 0)
	case "down", "j":
		f.top = min(f.top+1, maxTop)
	case "pgup":
		f.top = max(f.top-pruneRows, 0)
	case "pgdown":
		f.top = min(f.top+pruneRows, maxTop)
	}
	return m, nil
}

func (m *Model) prune() {
	f := m.prn
	removed, err := core.Prune(f.plan)
	m.refreshBackups()
	if err != nil {
		f.err = m.errText(err)
		return
	}
	m.mode, m.prn = modeList, nil
	m.setStatus(m.loc.T("old_backups_removed", map[string]any{"Count": removed, "Size": m.loc.Size(f.plan.Size())}), false)
}

func (m *Model) prunePopup() string {
	f := m.prn
	w := m.backupWidth()
	iw := w - 2
	lines := wrapped(m.loc.T("old_backups_listing", map[string]any{"Keep": f.plan.Keep, "Count": f.plan.Count()}), iw, lipgloss.NewStyle())
	lines = append(lines, wrapped(m.loc.T("old_backups_freed", map[string]any{"Size": m.loc.Size(f.plan.Size())}), iw, dim)...)
	lines = append(lines, "")
	header := len(lines)
	for i := f.top; i < min(f.top+pruneRows, len(f.lines)); i++ {
		l := f.lines[i]
		if l.heading {
			lines = append(lines, accent.Render(fit(l.text, iw)))
		} else {
			lines = append(lines, fit(l.text, iw))
		}
	}
	h := header + pruneRows + 3 // the header, the list, the message line and the border
	for len(lines) < h-3 {
		lines = append(lines, "")
	}
	lines = append(lines, errStyle.Render(fit(f.err, iw)))
	footer := m.loc.T("prune_footer", map[string]any{"Count": f.plan.Count()})
	if len(f.lines) > pruneRows {
		footer = m.loc.T("prune_footer_scroll", map[string]any{"Count": f.plan.Count()})
	}
	return box(m.loc.T("remove_old_backups", nil), lines, w, h, true, footer)
}
