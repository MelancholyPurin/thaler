package config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/importer"
)

// Changing the rules of the file: deleting one, moving one up or down, rewriting one.
// Only the lines of the rules that change are touched: the comments, the settings and the
// other rules stay as the person wrote them. A comment that sits directly above a rule's
// [[rule]] line (no blank line between) belongs to the rule and moves with it; the lines
// between two rules belong to neither.
//
// The new text is read back as TOML before anything is written, and has to give exactly
// the rules that were meant, so that a file written some other way (rules in a list, a
// string over several lines) is refused and not damaged. The file is replaced in one step.

var (
	ruleHeader  = regexp.MustCompile(`^\s*\[\[\s*rule\s*\]\]\s*(#.*)?$`)
	tableHeader = regexp.MustCompile(`^\s*\[`)
	commentLine = regexp.MustCompile(`^\s*#`)
)

// Change is what a change to the rules did to the file: the text before and after, which
// Revert needs to put it back.
type Change struct {
	Before, After []byte
}

// ruleSpan is where a rule is in the lines of the file: its comments, its [[rule]] line and
// its keys. start is the first of its comments, end is one past its last key.
type ruleSpan struct{ start, header, end int }

// ruleSpans finds the [[rule]] tables.
func ruleSpans(lines []string) []ruleSpan {
	var spans []ruleSpan
	for i, l := range lines {
		if !ruleHeader.MatchString(l) {
			continue
		}
		start := i
		for start > 0 && commentLine.MatchString(lines[start-1]) {
			start--
		}
		end := i + 1
		for j := i + 1; j < len(lines) && !tableHeader.MatchString(lines[j]); j++ {
			if t := strings.TrimSpace(lines[j]); t != "" && !commentLine.MatchString(t) {
				end = j + 1
			}
		}
		spans = append(spans, ruleSpan{start: start, header: i, end: end})
	}
	return spans
}

// decodeRules reads the rules out of the text of a config file.
func decodeRules(body []byte) ([]importer.Rule, error) {
	var c struct {
		Rules []importer.Rule `toml:"rule"`
	}
	if _, err := toml.Decode(string(body), &c); err != nil {
		return nil, err
	}
	return c.Rules, nil
}

// rewriteRules reads the file, lets edit make the new lines from the old ones and the
// rules the result has to hold, and writes it, if the text read back is those rules.
func rewriteRules(path string, edit func(lines []string, spans []ruleSpan) (out []string, want []importer.Rule, err error)) (Change, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Change{}, fmt.Errorf("%w: %w", coded.New("reading_file", map[string]any{"Path": path}), err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return Change{}, fmt.Errorf("%w: %w", coded.New("reading_file", map[string]any{"Path": path}), err)
	}
	rules, err := decodeRules(before)
	if err != nil {
		return Change{}, fmt.Errorf("%w: %w", coded.New("reading_file", map[string]any{"Path": path}), err)
	}
	lines := strings.Split(string(before), "\n")
	spans := ruleSpans(lines)
	if len(spans) != len(rules) {
		return Change{}, coded.New("rules_layout", map[string]any{"Path": path})
	}
	out, want, err := edit(lines, spans)
	if err != nil {
		return Change{}, err
	}
	if _, err := importer.NewRuleset(want); err != nil {
		return Change{}, err
	}
	after := []byte(strings.Join(out, "\n"))
	if got, err := decodeRules(after); err != nil || !slices.Equal(got, want) {
		return Change{}, coded.New("rules_layout", map[string]any{"Path": path})
	}
	if err := replaceFile(path, after, info.Mode().Perm()); err != nil {
		return Change{}, err
	}
	return Change{Before: before, After: after}, nil
}

func ruleNotFound(index int) error {
	return coded.New("rule_not_found", map[string]any{"Rule": index + 1})
}

// DeleteRule removes the rule at index (from 0) and the blank lines after it.
func DeleteRule(path string, index int) (Change, error) {
	return rewriteRules(path, func(lines []string, spans []ruleSpan) ([]string, []importer.Rule, error) {
		if index < 0 || index >= len(spans) {
			return nil, nil, ruleNotFound(index)
		}
		rules, _ := decodeRules([]byte(strings.Join(lines, "\n")))
		s := spans[index]
		stop := s.end
		for stop < len(lines)-1 && strings.TrimSpace(lines[stop]) == "" {
			stop++
		}
		out := slices.Concat(lines[:s.start], lines[stop:])
		if stop == len(lines)-1 { // it was the last thing in the file: keep one line end, not a trail of blank lines
			for len(out) > 1 && out[len(out)-1] == "" && out[len(out)-2] == "" {
				out = out[:len(out)-1]
			}
		}
		return out, slices.Delete(rules, index, index+1), nil
	})
}

// MoveRule swaps the rule at index with the one before it (delta -1) or after it (delta 1),
// each with its comments; whatever lies between the two stays where it is.
func MoveRule(path string, index, delta int) (Change, error) {
	return rewriteRules(path, func(lines []string, spans []ruleSpan) ([]string, []importer.Rule, error) {
		other := index + delta
		if (delta != -1 && delta != 1) || index < 0 || other < 0 || index >= len(spans) || other >= len(spans) {
			return nil, nil, ruleNotFound(index)
		}
		rules, _ := decodeRules([]byte(strings.Join(lines, "\n")))
		a, b := spans[min(index, other)], spans[max(index, other)]
		out := slices.Concat(lines[:a.start], lines[b.start:b.end], lines[a.end:b.start], lines[a.start:a.end], lines[b.end:])
		rules[index], rules[other] = rules[other], rules[index]
		return out, rules, nil
	})
}

// EditRule gives the rule at index the text to look for and the category of r, in the way it
// looks (contains or regex) that r has. The rule keeps its comments and its place; a comment
// on one of its own lines goes.
func EditRule(path string, index int, r importer.Rule) (Change, error) {
	return rewriteRules(path, func(lines []string, spans []ruleSpan) ([]string, []importer.Rule, error) {
		if index < 0 || index >= len(spans) {
			return nil, nil, ruleNotFound(index)
		}
		rules, _ := decodeRules([]byte(strings.Join(lines, "\n")))
		r.Contains, r.Regex = strings.TrimSpace(r.Contains), strings.TrimSpace(r.Regex)
		body := []string{}
		if r.Regex != "" {
			body = append(body, "regex = "+tomlString(r.Regex))
		}
		if r.Contains != "" {
			body = append(body, "contains = "+tomlString(r.Contains))
		}
		body = append(body, "category = "+tomlString(r.Category))
		s := spans[index]
		out := slices.Concat(lines[:s.header+1], body, lines[s.end:])
		rules[index] = r
		return out, rules, nil
	})
}

// Revert puts the file back as it was before c, as long as nothing else has changed it
// since: it never overwrites something it has not written.
func Revert(path string, c Change) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %w", coded.New("reading_file", map[string]any{"Path": path}), err)
	}
	now, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%w: %w", coded.New("reading_file", map[string]any{"Path": path}), err)
	}
	if !bytes.Equal(now, c.After) {
		return coded.New("config_changed", map[string]any{"Path": path})
	}
	return replaceFile(path, c.Before, info.Mode().Perm())
}
