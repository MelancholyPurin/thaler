package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/melancholypurin/thaler/internal/coded"
)

// A Feature is a story that is merged before it is finished. It stays off until
// config.toml switches it on under [features], so an unfinished story never
// holds up a release train. Finishing the story deletes its flag (and the check
// in the code), and the release notes say so.
type Feature struct {
	Name  string // the key under [features] in config.toml: lower case, with underscores
	Story string // the roadmap story it belongs to, for example "TBU-2"
	About string // one line saying what it switches on
}

// knownFeatures is every flag there is, and the only names config.toml may use
// under [features]. To add one: declare a constant for its name next to this
// list, add it here, and ask c.Enabled(thatConstant) where the story begins.
// None of the stories on the roadmap is merged unfinished yet.
var knownFeatures []Feature

// Enabled reports whether the feature called name is switched on. Everything is
// off in a nil Config and in one that does not mention it.
func (c *Config) Enabled(name string) bool {
	return c != nil && c.Features[name]
}

// checkFeatures refuses the first (in alphabetical order) name in set that is
// not in known, so that a typo does not leave a feature off without a word.
func checkFeatures(set map[string]bool, known []Feature) error {
	var unknown []string
	for name := range set {
		if !slices.ContainsFunc(known, func(f Feature) bool { return f.Name == name }) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	slices.Sort(unknown)
	if len(known) == 0 {
		return coded.New("unknown_feature_none", map[string]any{"Name": unknown[0]})
	}
	names := make([]string, len(known))
	for i, f := range known {
		names[i] = fmt.Sprintf("%s (%s: %s)", f.Name, f.Story, f.About)
	}
	return coded.New("unknown_feature_list", map[string]any{"Name": unknown[0], "Features": strings.Join(names, ", ")})
}

var (
	featureName  = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	featureStory = regexp.MustCompile(`^[A-Z]{3}-[0-9]+$`)
)

// registryProblems lists what is wrong with a list of features, for a test to
// hold the list to: the names must be usable as TOML keys and different from
// each other, and each flag must say which story it belongs to and what it does.
func registryProblems(known []Feature) []string {
	var problems []string
	seen := map[string]bool{}
	for _, f := range known {
		if !featureName.MatchString(f.Name) {
			problems = append(problems, fmt.Sprintf("%q is not a lower case name with underscores", f.Name))
		}
		if seen[f.Name] {
			problems = append(problems, fmt.Sprintf("%q is there twice", f.Name))
		}
		seen[f.Name] = true
		if !featureStory.MatchString(f.Story) {
			problems = append(problems, fmt.Sprintf("%q: %q is not a story ID like TBU-2", f.Name, f.Story))
		}
		if strings.TrimSpace(f.About) == "" {
			problems = append(problems, fmt.Sprintf("%q has no description", f.Name))
		}
	}
	return problems
}
