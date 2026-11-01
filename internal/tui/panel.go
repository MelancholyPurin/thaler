package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// listState is the cursor and scroll position of one scrollable list.
type listState struct {
	cursor, offset int
}

// clamp keeps the cursor inside a list of n items and the cursor visible in a
// window of h lines.
func (l *listState) clamp(n, h int) {
	if l.cursor >= n {
		l.cursor = n - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	if h < 1 {
		h = 1
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

// navigate handles the movement keys shared by every list and reports whether
// key was one of them.
func (l *listState) navigate(key string, n, h int) bool {
	page := max(1, h-1)
	switch key {
	case "up", "k":
		l.cursor--
	case "down", "j":
		l.cursor++
	case "pgup", "ctrl+u":
		l.cursor -= page
	case "pgdown", "ctrl+d":
		l.cursor += page
	case "home", "g":
		l.cursor = 0
	case "end", "G":
		l.cursor = n - 1
	default:
		return false
	}
	l.clamp(n, h)
	return true
}

// box draws a lazygit-style panel: a rounded border with the title set into the
// top edge and an optional footer set into the bottom edge. The result is
// exactly h lines of w cells; lines are truncated or padded to fit.
func box(title string, lines []string, w, h int, focused bool, footer string) string {
	if w < 4 || h < 3 {
		return strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", max(w, 0))+"\n", max(h, 0)), "\n")
	}
	border, titleStyle := borderDim, dim
	if focused {
		border, titleStyle = borderFocus, titleFocus
	}
	iw, ih := w-2, h-2

	title = ansi.Truncate(title, iw-3, "…")
	top := border.Render("╭─ ") + titleStyle.Render(title) + border.Render(" "+strings.Repeat("─", max(0, w-5-lipgloss.Width(title)))+"╮")

	bottom := border.Render("╰" + strings.Repeat("─", iw) + "╯")
	if footer != "" {
		footer = ansi.Truncate(footer, iw-4, "…")
		bottom = border.Render("╰"+strings.Repeat("─", max(0, w-5-lipgloss.Width(footer)))+" ") + dim.Render(footer) + border.Render(" ─╯")
	}

	var b strings.Builder
	b.WriteString(top)
	side := border.Render("│")
	for i := range ih {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteByte('\n')
		b.WriteString(side + fit(line, iw) + side)
	}
	b.WriteByte('\n')
	b.WriteString(bottom)
	return b.String()
}

// overlay draws popup over base with its top-left corner at cell (x, y).
func overlay(base, popup string, x, y int) string {
	const reset = "\x1b[0m"
	lines := strings.Split(base, "\n")
	pop := strings.Split(popup, "\n")
	pw := 0
	for _, p := range pop {
		pw = max(pw, lipgloss.Width(p))
	}
	for i, p := range pop {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		left := ansi.Truncate(lines[row], x, "")
		if w := lipgloss.Width(left); w < x {
			left += strings.Repeat(" ", x-w)
		}
		right := ansi.TruncateLeft(lines[row], x+pw, "")
		lines[row] = left + reset + fit(p, pw) + reset + right
	}
	return strings.Join(lines, "\n")
}

// fit truncates s to w cells with an ellipsis and pads it with spaces to w.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// fitRight is fit with right alignment.
func fitRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - lipgloss.Width(s); pad > 0 {
		s = strings.Repeat(" ", pad) + s
	}
	return s
}

// leftRight lays out a label on the left and a value on the right of w cells.
// The label gives way first when the line is too narrow.
func leftRight(label, value string, w int) string {
	vw := lipgloss.Width(value)
	if vw >= w {
		return fitRight(value, w)
	}
	return fit(label, w-vw-1) + " " + value
}

// clean collapses whitespace and control characters so a value stays on one line.
func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// wrapped is text broken into lines of at most w cells at the spaces, each drawn in
// style. A text that is too wide for one line goes on to the next instead of being
// cut short.
func wrapped(text string, w int, style lipgloss.Style) []string {
	if w <= 0 {
		return nil
	}
	parts := strings.Split(ansi.Wrap(text, w, ""), "\n")
	for i, p := range parts {
		parts[i] = style.Render(p)
	}
	return parts
}

// statusLine joins the parts of a status with " · " as far as they fit the line, the
// most important first: what does not fit is left out, not cut in half. The first
// part is always there.
func (m *Model) statusLine(parts []string) string {
	for n := len(parts); n > 1; n-- {
		if s := strings.Join(parts[:n], " · "); lipgloss.Width(s) <= m.width {
			return s
		}
	}
	return parts[0]
}

// packed puts the parts side by side, separated by a dim " · ", on as few lines of at
// most w cells as it takes: a part is never split, and a line that is full goes on to
// the next one.
func packed(parts []string, w int) []string {
	sep := dim.Render(" · ")
	var lines []string
	cur := ""
	for _, p := range parts {
		switch {
		case cur == "":
			cur = p
		case lipgloss.Width(cur)+lipgloss.Width(sep)+lipgloss.Width(p) <= w:
			cur += sep + p
		default:
			lines = append(lines, cur)
			cur = p
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// labelled is a label with a text after it on one line, or, when they do not fit
// together, the label with the text wrapped under it.
func labelled(label string, labelStyle lipgloss.Style, text string, textStyle lipgloss.Style, w int) []string {
	if lipgloss.Width(label)+1+lipgloss.Width(text) <= w {
		return []string{labelStyle.Render(label) + textStyle.Render(" "+text)}
	}
	return append([]string{labelStyle.Render(label)}, wrapped(text, w, textStyle)...)
}

// leftRightLines is leftRight, except that a label and a value that do not fit side by
// side go on two lines, the value under the label at the right, instead of the label
// being cut short.
func leftRightLines(label, value string, w int) []string {
	if lipgloss.Width(label)+1+lipgloss.Width(value) <= w {
		return []string{leftRight(label, value, w)}
	}
	return []string{fit(label, w), fitRight(value, w)}
}

// centered pads s on the left so it sits in the middle of w cells.
func centered(s string, w int) string {
	if pad := (w - lipgloss.Width(s)) / 2; pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}
