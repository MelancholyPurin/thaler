package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func todayISO() string { return time.Now().Format(ledger.DateLayout) }

// parseDateFlag parses a --date value; an empty value means today. ISO and
// DD.MM.YYYY are read; a slash date is ambiguous and refused.
func (a *app) parseDateFlag(s string) (time.Time, error) {
	if s == "" {
		s = todayISO()
	}
	return a.cli().ParseDate(s)
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

// thisMonth is the current month, as YYYY-MM.
func (a *app) thisMonth() string {
	if a.now != nil {
		return a.now().Format("2006-01")
	}
	return time.Now().Format("2006-01")
}

// printTable writes a header and rows, the first column left-aligned and the others
// right-aligned, two spaces apart. A row may hold fewer cells than the header.
func printTable(w io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len([]rune(h))
	}
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], len([]rune(cell)))
		}
	}
	line := func(cells []string) {
		var b strings.Builder
		for i := range header {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			if i > 0 {
				b.WriteString("  ")
			}
			if i == 0 {
				fmt.Fprintf(&b, "%-*s", widths[i], cell)
			} else {
				fmt.Fprintf(&b, "%*s", widths[i], cell)
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	line(header)
	for _, row := range rows {
		line(row)
	}
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
