package i18n

import (
	"strings"

	"github.com/melancholypurin/thaler/internal/coded"
)

// Error is err written in the language: a coded error is looked up in the catalog
// by its code and filled in with its arguments, also when it is wrapped in more
// text (opening x: account_not_found(Name=y) reads opening x: account does not
// exist: y). An error without a code is shown as its own text.
func (l *Localizer) Error(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for _, c := range coded.In(err) {
		text = strings.Replace(text, c.Error(), l.T(c.Code, c.Args), 1)
	}
	return text
}
