// Package releasenotes writes what a release changed from the titles of the
// commits it contains. A commit title is "[ID] domain/service: Name", where ID is
// a roadmap story (PLT-1) or MAINTENANCE; the pull request number Github adds in
// brackets stays where it is. Stories are grouped by the epic their letters name.
package releasenotes

import (
	"fmt"
	"regexp"
	"strings"
)

// Epic is a group of stories, from the "Epic keys" table of the roadmap.
type Epic struct {
	Key  string // three letters, for example PLT
	Name string // for example "Installing and running on macOS and Linux"
}

var epicRow = regexp.MustCompile("^\\| `([A-Z]{3})` \\| ([^|]+?) \\|")

// ParseEpics reads the epics from the roadmap text, in the order of its table.
func ParseEpics(roadmap string) []Epic {
	var epics []Epic
	for _, line := range strings.Split(roadmap, "\n") {
		if m := epicRow.FindStringSubmatch(line); m != nil {
			epics = append(epics, Epic{Key: m[1], Name: m[2]})
		}
	}
	return epics
}

// Change is one commit title, taken apart.
type Change struct {
	Story       string // the story ID, empty for maintenance and for a title that does not follow the form
	Maintenance bool
	Text        string // the title without its [ID]; the whole title when it does not follow the form
}

var (
	titleForm = regexp.MustCompile(`^\[([^\]]+)\]:?\s*(.*)$`)
	storyID   = regexp.MustCompile(`^[A-Z]{3}-[0-9]+$`)
)

// Parse takes a commit title apart. A title that does not start with a known
// [ID] is returned whole, to be listed as it is under "Other changes", where a
// mistake is easy to see.
func Parse(title string) Change {
	title = strings.TrimSpace(title)
	m := titleForm.FindStringSubmatch(title)
	if m == nil {
		return Change{Text: title}
	}
	switch {
	case storyID.MatchString(m[1]):
		return Change{Story: m[1], Text: m[2]}
	case strings.EqualFold(m[1], "maintenance"):
		return Change{Maintenance: true, Text: m[2]}
	}
	return Change{Text: title}
}

// group is a heading and what is listed under it.
type group struct {
	heading string
	lines   []string
}

// groups sorts the changes under the epics, in the order of the roadmap, then
// maintenance, then whatever does not fit. A story whose letters are no epic
// goes under "Other changes" with its ID in front, so that the mistake shows.
func groups(titles []string, epics []Epic) []group {
	byEpic := make([]group, len(epics))
	for i, e := range epics {
		byEpic[i].heading = fmt.Sprintf("%s (%s)", e.Name, e.Key)
	}
	maintenance := group{heading: "Maintenance"}
	other := group{heading: "Other changes"}
	for _, title := range titles {
		c := Parse(title)
		switch {
		case c.Maintenance:
			maintenance.lines = append(maintenance.lines, "- "+c.Text)
		case c.Story != "":
			line := fmt.Sprintf("- `%s` %s", c.Story, c.Text)
			if i := epicIndex(epics, c.Story); i >= 0 {
				byEpic[i].lines = append(byEpic[i].lines, line)
			} else {
				other.lines = append(other.lines, line)
			}
		default:
			other.lines = append(other.lines, "- "+c.Text)
		}
	}
	var out []group
	for _, g := range append(byEpic, maintenance, other) {
		if len(g.lines) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func epicIndex(epics []Epic, story string) int {
	for i, e := range epics {
		if strings.HasPrefix(story, e.Key+"-") {
			return i
		}
	}
	return -1
}

// Notes is the release notes of version, as Markdown: the titles grouped by epic.
func Notes(version, date string, titles []string, epics []Epic) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s (%s)\n", version, date)
	gs := groups(titles, epics)
	if len(gs) == 0 {
		b.WriteString("\nNothing changed.\n")
	}
	for _, g := range gs {
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", g.heading, strings.Join(g.lines, "\n"))
	}
	return b.String()
}

// Row is the line for the "Releases" table of the roadmap: the version, the date
// and the stories it shipped, each once, in the order of the notes.
func Row(version, date string, titles []string, epics []Epic) string {
	var stories []string
	seen := map[string]bool{}
	for _, id := range epicOrder(titles, epics) {
		if !seen[id] {
			seen[id] = true
			stories = append(stories, "`"+id+"`")
		}
	}
	list := "none"
	if len(stories) > 0 {
		list = strings.Join(stories, ", ")
	}
	return fmt.Sprintf("| `%s` | %s | %s |", version, date, list)
}

// epicOrder is the story IDs of the titles that name a roadmap epic, grouped by
// epic in the order of the roadmap and by commit within it.
func epicOrder(titles []string, epics []Epic) []string {
	byEpic := make([][]string, len(epics))
	for _, title := range titles {
		if c := Parse(title); c.Story != "" {
			if i := epicIndex(epics, c.Story); i >= 0 {
				byEpic[i] = append(byEpic[i], c.Story)
			}
		}
	}
	var out []string
	for _, ids := range byEpic {
		out = append(out, ids...)
	}
	return out
}
