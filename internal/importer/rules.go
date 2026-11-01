package importer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/ledger"
)

// Rule assigns a category to matching transactions. Exactly one of Contains
// and Regex must be set. Both match case-insensitively against the payee,
// purpose and booking text joined by spaces.
type Rule struct {
	Contains string `toml:"contains"`
	Regex    string `toml:"regex"`
	Category string `toml:"category"`
}

// Ruleset is an ordered list of compiled rules; the first match wins.
type Ruleset struct {
	rules []compiledRule
}

type compiledRule struct {
	contains string
	re       *regexp.Regexp
	category string
}

// NewRuleset validates and compiles rules.
func NewRuleset(rules []Rule) (*Ruleset, error) {
	s := &Ruleset{}
	for i, r := range rules {
		n := i + 1
		if (strings.TrimSpace(r.Contains) == "") == (strings.TrimSpace(r.Regex) == "") {
			return nil, coded.New("rule_set_one", map[string]any{"Rule": n})
		}
		if err := ledger.ValidateAccountName(r.Category); err != nil {
			return nil, fmt.Errorf("%w: %w", coded.New("rule_category", map[string]any{"Rule": n}), err)
		}
		cr := compiledRule{category: r.Category}
		if r.Regex != "" {
			re, err := regexp.Compile("(?i)" + r.Regex)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", coded.New("rule_regex", map[string]any{"Rule": n}), err)
			}
			cr.re = re
		} else {
			cr.contains = strings.ToLower(strings.TrimSpace(r.Contains))
		}
		s.rules = append(s.rules, cr)
	}
	return s, nil
}

// Match returns the category of the first rule that matches any of fields.
func (s *Ruleset) Match(fields ...string) (string, bool) {
	_, category, ok := s.Find(fields...)
	return category, ok
}

// Find is Match that also says which rule it was: the index into the rules the
// set was built from.
func (s *Ruleset) Find(fields ...string) (index int, category string, ok bool) {
	text := strings.Join(fields, " ")
	lower := strings.ToLower(text)
	for i, r := range s.rules {
		if r.re != nil {
			if r.re.MatchString(text) {
				return i, r.category, true
			}
			continue
		}
		if strings.Contains(lower, r.contains) {
			return i, r.category, true
		}
	}
	return 0, "", false
}
