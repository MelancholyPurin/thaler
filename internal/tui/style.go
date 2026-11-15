package tui

import "github.com/charmbracelet/lipgloss"

// The colours of the interface are four, plus the grey of the borders of the panels that do
// not have the focus. A theme (theme.go) says which; the styles below are made from it by
// useTheme.
var (
	accentColor, warnColor, errColor, dimColor, borderDimColor lipgloss.TerminalColor

	dim      lipgloss.Style
	bold     = lipgloss.NewStyle().Bold(true)
	accent   lipgloss.Style
	warn     lipgloss.Style
	errStyle lipgloss.Style

	// selection: reverse video in the focused panel, bold in the others so the
	// position is still visible.
	selFocused   = lipgloss.NewStyle().Reverse(true)
	selUnfocused = lipgloss.NewStyle().Bold(true)

	borderFocus lipgloss.Style
	borderDim   lipgloss.Style
	titleFocus  lipgloss.Style
)

func init() { useTheme("auto") }
