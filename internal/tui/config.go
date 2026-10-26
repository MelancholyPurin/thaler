package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/melancholypurin/thaler/internal/config"
)

// configEditedMsg arrives when the editor opened by C has closed.
type configEditedMsg struct{ err error }

// editConfig opens the config file in the user's editor, creating it from the
// example first if there is none. The interface is suspended while the editor
// runs and resumes when it exits.
func (m *Model) editConfig() tea.Cmd {
	if m.opts.ConfigPath == "" {
		m.setStatus("no config file location is known", true)
		return nil
	}
	// Find the editor first: with none, nothing should be created.
	cmd, err := editorCommand(os.Getenv, exec.LookPath, m.opts.ConfigPath)
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	if err := config.Ensure(m.opts.ConfigPath, m.opts.ConfigTemplate); err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return configEditedMsg{err} })
}

// editorCommand is the command that opens path: $VISUAL, else $EDITOR (either
// may carry arguments, like "code --wait"), else nano or vi when installed.
func editorCommand(getenv func(string) string, lookPath func(string) (string, error), path string) (*exec.Cmd, error) {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(getenv(name)); len(fields) > 0 {
			return exec.Command(fields[0], append(fields[1:], path)...), nil //nolint:gosec // running the user's own $VISUAL or $EDITOR is the point
		}
	}
	for _, name := range []string{"nano", "vi"} {
		if _, err := lookPath(name); err == nil {
			return exec.Command(name, path), nil
		}
	}
	return nil, errors.New("no editor found: set $VISUAL or $EDITOR, e.g. export EDITOR=nano")
}

// configEdited reports how the file looks after the editor closed, so a typo
// is noticed now and not at the next import.
func (m *Model) configEdited(editorErr error) {
	if editorErr != nil {
		m.setStatus("the editor failed: "+editorErr.Error(), true)
		return
	}
	if m.loadConfig == nil {
		m.setStatus("Config closed", false)
		return
	}
	cfg, err := m.loadConfig()
	if err != nil {
		m.setStatus("The config has a problem, imports will refuse it until it is fixed (press C): "+err.Error(), true)
		return
	}
	m.setStatus(fmt.Sprintf("Config saved: %d rule(s), %d bank profile(s); used by the next import", len(cfg.Rules), len(cfg.Profiles)), false)
}
