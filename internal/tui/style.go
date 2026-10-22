package tui

import "github.com/charmbracelet/lipgloss"

var (
	accentColor = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	warnColor   = lipgloss.AdaptiveColor{Light: "166", Dark: "214"}
	errColor    = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	dimColor    = lipgloss.AdaptiveColor{Light: "242", Dark: "245"}

	dim      = lipgloss.NewStyle().Foreground(dimColor)
	bold     = lipgloss.NewStyle().Bold(true)
	accent   = lipgloss.NewStyle().Foreground(accentColor)
	warn     = lipgloss.NewStyle().Foreground(warnColor)
	errStyle = lipgloss.NewStyle().Foreground(errColor)

	// selection: reverse video in the focused panel, bold in the others so the
	// position is still visible.
	selFocused   = lipgloss.NewStyle().Reverse(true)
	selUnfocused = lipgloss.NewStyle().Bold(true)

	borderFocus = lipgloss.NewStyle().Foreground(accentColor)
	borderDim   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "240"})
	titleFocus  = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
)
