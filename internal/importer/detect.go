package importer

import (
	"bytes"
	"io"
	"sort"
	"strings"
)

// detectBytes is how much of a file Detect reads: the header is at the start,
// and an export can be large.
const detectBytes = 64 << 10

// Detect says which profile reads a file, going by its header alone: a profile
// fits when the file has the columns it needs for the date and the amount, and
// the one that finds the most of the columns it names wins, so a profile that
// names every column of a bank's export beats one that names two. Of profiles
// that fit equally well the first wins: yours from config.toml in the order
// written (a profile of yours with the name of a built-in one replaces it, as
// in FindProfile), then the built-in ones by name. The name is the lower-case
// one ProfileNames lists. Nothing is parsed, so a file can be detected and
// still have rows that cannot be read.
func Detect(r io.Reader, custom []Profile) (string, bool) {
	raw, err := io.ReadAll(io.LimitReader(r, detectBytes))
	if err != nil {
		return "", false
	}
	if len(raw) == detectBytes {
		// Cut at a whole line, so that a character split in two cannot make the
		// text look like it is not UTF-8.
		if i := bytes.LastIndexByte(raw, '\n'); i >= 0 {
			raw = raw[:i]
		}
	}
	best, bestScore := "", 0
	for _, p := range candidates(custom) {
		if score := p.headerScore(raw); score > bestScore {
			best, bestScore = strings.ToLower(p.Name), score
		}
	}
	return best, best != ""
}

// candidates lists the profiles in the order Detect prefers them.
func candidates(custom []Profile) []Profile {
	out := append([]Profile(nil), custom...)
	taken := map[string]bool{}
	for _, p := range custom {
		taken[strings.ToLower(p.Name)] = true
	}
	var names []string
	for k := range builtin {
		if !taken[k] {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	for _, k := range names {
		out = append(out, builtin[k])
	}
	return out
}

// headerScore is how many of the profile's columns the header of the file has,
// or 0 when the file cannot be this profile's: no date or no amount column, a
// text the profile cannot decode, or no header at all.
func (p Profile) headerScore(raw []byte) int {
	cp, err := p.compile()
	if err != nil {
		return 0
	}
	tab, err := openTable(raw, p, cp)
	if err != nil { // including a file with no header
		return 0
	}
	header := tab.header
	index := headerIndex(header)
	if findColumn(index, p.Columns.Date) < 0 || findColumn(index, p.Columns.Amount) < 0 {
		return 0
	}
	score := 0
	for _, names := range [][]string{p.Columns.Date, p.Columns.Amount, p.Columns.Payee, p.Columns.Purpose, p.Columns.IBAN, p.Columns.BookingText, p.Columns.Currency} {
		if findColumn(index, names) >= 0 {
			score++
		}
	}
	if p.SkipColumn != "" && findColumn(index, []string{p.SkipColumn}) >= 0 {
		score++
	}
	return score
}
