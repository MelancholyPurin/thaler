package tui

import (
	"strings"
	"unicode"
)

// fuzzyScore reports whether every rune of pattern occurs in text in order,
// ignoring case, and how good the match is (higher is better). Consecutive runs
// and matches at the start of a word score extra, so "gro" ranks
// "Expenses:Groceries" above "Expenses:Gift-Rooms".
func fuzzyScore(pattern, text string) (int, bool) {
	p := []rune(strings.ToLower(pattern))
	if len(p) == 0 {
		return 0, true
	}
	t := []rune(strings.ToLower(text))
	score, pi, prev := 0, 0, -2
	for ti, r := range t {
		if pi == len(p) || r != p[pi] {
			continue
		}
		score++
		if ti == prev+1 {
			score += 3
		}
		if ti == 0 || isSeparator(t[ti-1]) {
			score += 2
		}
		prev = ti
		pi++
	}
	if pi < len(p) {
		return 0, false
	}
	return score*4 - len(t)/8, true
}

// matchAll requires every whitespace-separated term of query to match text.
func matchAll(query, text string) bool {
	for _, term := range strings.Fields(query) {
		if _, ok := fuzzyScore(term, text); !ok {
			return false
		}
	}
	return true
}

func isSeparator(r rune) bool {
	return unicode.IsSpace(r) || r == ':' || r == '-' || r == '_' || r == '.' || r == '/'
}
