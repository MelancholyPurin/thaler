package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A backup or a restore can take a while: sealing one under a passphrase is slow
// on purpose, and a big database takes time to copy. They therefore run as a
// command, off the update loop, so that the screen keeps being drawn and says
// what is going on. While one runs the popup that started it stays up and takes
// no keys, which also means nothing else touches the database meanwhile.

// busy is the work in progress.
type busy struct {
	note  string  // what is going on, shown in the popup
	run   tea.Cmd // the work; it ends with a message for Update
	frame int     // of the spinner
}

// spinMsg moves the spinner on.
type spinMsg struct{}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinEvery = 100 * time.Millisecond

func spinTick() tea.Cmd {
	return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} })
}

// start begins work and returns the commands that run it and animate the spinner.
func (m *Model) start(note string, run tea.Cmd) tea.Cmd {
	m.busy = &busy{note: note, run: run}
	return tea.Batch(run, spinTick())
}

// line is the spinner and what it is waiting for.
func (b *busy) line() string {
	return spinFrames[b.frame%len(spinFrames)] + " " + b.note
}

// keyWhileBusy handles a key pressed while work is in progress. Everything is
// ignored but ctrl+c, which quits once the work is done: ending the process in
// the middle could leave a half-written file behind.
func (m *Model) keyWhileBusy(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.quitWhenDone = true
	}
	return m, nil
}

// idle ends the work in progress; it returns tea.Quit if ctrl+c was pressed meanwhile.
func (m *Model) idle() tea.Cmd {
	m.busy = nil
	if m.quitWhenDone {
		return tea.Quit
	}
	return nil
}

// footerBusy is the footer of a popup while it waits.
func (m *Model) footerBusy() string {
	if m.quitWhenDone {
		return "quitting as soon as this is done"
	}
	return "please wait · ctrl+c quits when done"
}
