package i18n

import (
	"strconv"
	"time"
)

// MonthYear is a month as the interface names it ("Oct 2026"): the short name of the
// month in the language, then the year.
func (l *Localizer) MonthYear(t time.Time) string {
	return l.monthName(t.Month()) + " " + strconv.Itoa(t.Year())
}

func (l *Localizer) monthName(m time.Month) string {
	switch m {
	case time.January:
		return l.T("month_1", nil)
	case time.February:
		return l.T("month_2", nil)
	case time.March:
		return l.T("month_3", nil)
	case time.April:
		return l.T("month_4", nil)
	case time.May:
		return l.T("month_5", nil)
	case time.June:
		return l.T("month_6", nil)
	case time.July:
		return l.T("month_7", nil)
	case time.August:
		return l.T("month_8", nil)
	case time.September:
		return l.T("month_9", nil)
	case time.October:
		return l.T("month_10", nil)
	case time.November:
		return l.T("month_11", nil)
	default:
		return l.T("month_12", nil)
	}
}
