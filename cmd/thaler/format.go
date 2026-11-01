package main

import (
	"fmt"
	"io"
	"time"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func todayISO() string { return time.Now().Format(ledger.DateLayout) }

// parseDateFlag parses a --date value; an empty value means today. Besides ISO
// and DD.MM.YYYY it reads the date format of the language (01/10/2026 in Spanish).
func (a *app) parseDateFlag(s string) (time.Time, error) {
	if s == "" {
		s = todayISO()
	}
	return a.localizer().ParseDate(s)
}

// signed formats an amount with an explicit "+" for positive values.
func signed(m ledger.Money) string {
	if m.Minor > 0 {
		return "+" + m.String()
	}
	return m.String()
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit-1]) + "…"
}

// column is one "label ... amount" line of a report.
type column struct {
	label  string
	amount string
}

// printColumns writes lines with the labels left-aligned and the amounts
// right-aligned, each line prefixed by indent.
func printColumns(w io.Writer, indent string, lines []column) {
	labelW, amountW := 0, 0
	for _, l := range lines {
		if n := len([]rune(l.label)); n > labelW {
			labelW = n
		}
		if n := len([]rune(l.amount)); n > amountW {
			amountW = n
		}
	}
	for _, l := range lines {
		fmt.Fprintf(w, "%s%-*s  %*s\n", indent, labelW, l.label, amountW, l.amount)
	}
}
