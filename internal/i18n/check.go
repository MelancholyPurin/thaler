package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The checks below hold the catalogs to account: Test_Catalogs and
// Test_Catalogs_usage run them (`make i18n-check`), so CI fails when a message is
// missing from a language that claims to be complete, when a translation is left
// over from a message that is gone, when a message is no longer used, or when the
// code asks for one that does not exist.

var argument = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

// arguments are the names of the arguments in the text of a message, sorted and
// without repeats, of all its plural forms.
func arguments(e Entry) []string {
	var names []string
	for _, f := range e.Forms {
		for _, m := range argument.FindAllStringSubmatch(f.Text, -1) {
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return slices.Compact(names)
}

// catalogProblems is what is wrong with the catalog of a language, measured against
// English. A message missing from the language is a problem only if complete.
func catalogProblems(code string, english, other catalog, complete bool) []string {
	var problems []string
	for _, id := range other.order {
		if _, ok := english.entries[id]; !ok {
			problems = append(problems, fmt.Sprintf("%s: %q is not a message of English: it was removed or renamed there", code, id))
		}
	}
	for _, id := range english.order {
		have, ok := other.entries[id]
		if !ok || !hasText(have) {
			if complete {
				problems = append(problems, fmt.Sprintf("%s: %q is not translated, but the language is marked Complete", code, id))
			}
			continue
		}
		if !slices.ContainsFunc(have.Forms, func(f Form) bool { return f.Name == "other" && strings.TrimSpace(f.Text) != "" }) {
			problems = append(problems, fmt.Sprintf("%s: %q has no \"other\" text, which every message needs", code, id))
		}
		for _, f := range have.Forms {
			if strings.TrimSpace(f.Text) == "" {
				problems = append(problems, fmt.Sprintf("%s: %q has an empty %q text", code, id, f.Name))
			}
		}
		if want, got := arguments(english.entries[id]), arguments(have); !slices.Equal(want, got) {
			problems = append(problems, fmt.Sprintf("%s: %q takes the arguments %v but English has %v: arguments are not translated or dropped", code, id, got, want))
		}
	}
	return problems
}

// englishProblems is what is wrong with the English catalog on its own.
func englishProblems(english catalog) []string {
	var problems []string
	idForm := regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	for _, id := range english.order {
		if !idForm.MatchString(id) {
			problems = append(problems, fmt.Sprintf("en: %q is not a lower case id with underscores", id))
		}
		e := english.entries[id]
		if !slices.ContainsFunc(e.Forms, func(f Form) bool { return f.Name == "other" && strings.TrimSpace(f.Text) != "" }) {
			problems = append(problems, fmt.Sprintf("en: %q has no \"other\" text, which every message needs", id))
		}
	}
	return problems
}

// use is a call of T in the code.
type use struct {
	ID    string // empty when the id is not a string literal
	Where string // file:line
}

// usesIn finds every call of a method named T, and of coded.New, in the non-test Go
// files below root, with its first argument: the id of a message.
func usesIn(root string) ([]use, error) {
	var uses []use
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		src, err := os.ReadFile(path) //nolint:gosec // walking the repository's own sources
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !isMessageCall(sel) {
				return true
			}
			u := use{Where: fmt.Sprintf("%s:%d", filepath.ToSlash(rel), fset.Position(call.Pos()).Line)}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				u.ID, _ = strconv.Unquote(lit.Value)
			} else if strings.HasPrefix(filepath.ToSlash(rel), "internal/i18n/") {
				return true // the lookup machinery itself passes ids on, such as the code of an error
			}
			uses = append(uses, u)
			return true
		})
		return nil
	})
	return uses, err
}

// isMessageCall is x.T, the lookup of a message, and coded.New, which names the
// message of an error by its code.
func isMessageCall(sel *ast.SelectorExpr) bool {
	if sel.Sel.Name == "T" {
		return true
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "coded" && sel.Sel.Name == "New"
}

// usageProblems compares the messages of English with the calls of T.
func usageProblems(english catalog, uses []use) []string {
	var problems []string
	used := map[string]bool{}
	for _, u := range uses {
		switch {
		case u.ID == "":
			problems = append(problems, fmt.Sprintf("%s: the id given to T or coded.New is not a string literal, so it cannot be checked", u.Where))
		case !contains(english, u.ID):
			problems = append(problems, fmt.Sprintf("%s: %q is not a message of English", u.Where, u.ID))
		default:
			used[u.ID] = true
		}
	}
	for _, id := range english.order {
		if !used[id] {
			problems = append(problems, fmt.Sprintf("en: %q is not used by any call of T or coded.New", id))
		}
	}
	return problems
}

func contains(c catalog, id string) bool {
	_, ok := c.entries[id]
	return ok
}
