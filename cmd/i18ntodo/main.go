// Command i18ntodo lists the messages that a language still has to translate: on
// standard output as JSON with empty texts, to merge into its file in
// internal/i18n/locales, and on standard error the English to translate (see
// `make i18n-todo`).
//
//	i18ntodo es
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/melancholypurin/thaler/internal/i18n"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: i18ntodo LANGUAGE (one of %v)", i18n.Codes())
	}
	todo, err := i18n.Untranslated(args[0])
	if err != nil {
		return err
	}
	if len(todo) == 0 {
		fmt.Fprintf(stderr, "%s has every message.\n", args[0])
		return nil
	}
	fmt.Fprint(stderr, i18n.Reference(todo))
	_, err = io.WriteString(stdout, i18n.Stubs(todo))
	return err
}
