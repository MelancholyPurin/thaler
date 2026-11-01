// Package coded is the error that carries a stable code instead of a text.
//
// The code names a message in the catalog of internal/i18n (account_not_found), and
// the arguments fill it in. Whoever shows the error looks the message up in the
// language of the person: the interface and the command line with their
// Localizer, a server with the language the caller asked for. The code does not
// change once it is used, because clients, tests and the API depend on it.
//
// The package has no dependencies, so that the ledger and the service, which make
// these errors, can use it without knowing about languages.
package coded

import (
	"fmt"
	"sort"
	"strings"
)

// Error is a failure with a code and the arguments of its message.
type Error struct {
	Code string         `json:"code"`
	Args map[string]any `json:"args,omitempty"`
}

// New is the error with this code. The code is a string literal, so that the
// checks of internal/i18n can compare it with the catalog.
func New(code string, args map[string]any) *Error { return &Error{Code: code, Args: args} }

// Error is the code with its arguments, as in account_not_found(Name=Assets:X). It
// is for logs and for tests: a person is shown the message of the catalog.
func (e *Error) Error() string {
	if len(e.Args) == 0 {
		return e.Code
	}
	names := make([]string, 0, len(e.Args))
	for name := range e.Args {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%s=%v", name, e.Args[name])
	}
	return e.Code + "(" + strings.Join(parts, ", ") + ")"
}

// Is matches an error with the same code, whatever its arguments, so that a
// variable such as core.ErrNoAccount can be tested for with errors.Is.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Code is the code of the first coded error in err's chain, or "" if there is none.
func Code(err error) string {
	for _, c := range In(err) {
		return c.Code
	}
	return ""
}

// In lists the coded errors in err's tree, outermost first, however err wraps
// or joins them.
func In(err error) []*Error {
	var found []*Error
	var walk func(error)
	walk = func(err error) {
		if err == nil {
			return
		}
		if c, ok := err.(*Error); ok { //nolint:errorlint // looking at each error of the tree itself, not at what it wraps
			found = append(found, c)
		}
		switch u := err.(type) { //nolint:errorlint // same: the walk unwraps by hand to see every branch
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		case interface{ Unwrap() []error }:
			for _, e := range u.Unwrap() {
				walk(e)
			}
		}
	}
	walk(err)
	return found
}
