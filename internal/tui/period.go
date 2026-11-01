package tui

import "time"

// monthLayout is how a month is written: "2026-10".
const monthLayout = "2006-01"

// periodLabel is the human name of a period.
func (m *Model) periodLabel(period string) string {
	if period == "" {
		return m.loc.T("all_time", nil)
	}
	t, err := time.Parse(monthLayout, period)
	if err != nil {
		return period
	}
	return m.loc.MonthYear(t)
}

// periodRange returns the first and last day (YYYY-MM-DD) of a period; both
// are empty for all time.
func periodRange(period string) (from, to string) {
	t, err := time.Parse(monthLayout, period)
	if err != nil {
		return "", ""
	}
	last := t.AddDate(0, 1, -1)
	return t.Format("2006-01-02"), last.Format("2006-01-02")
}

// setPeriod changes the period every screen is scoped to.
func (m *Model) setPeriod(period string) {
	m.period = period
	m.applyFilter(false)
	if err := m.refreshBudget(); err != nil {
		m.setStatus(m.errText(err), true)
	}
}

// shiftPeriod moves the period by d months. From all time it jumps to the
// current month.
func (m *Model) shiftPeriod(d int) {
	t, err := time.Parse(monthLayout, m.period)
	if err != nil {
		m.setPeriod(m.now().Format(monthLayout))
		return
	}
	m.setPeriod(t.AddDate(0, d, 0).Format(monthLayout))
}

// refreshMonths lists the months that have transactions, newest first. The
// current month is always included so there is somewhere to start.
func (m *Model) refreshMonths() {
	seen := map[string]bool{m.now().Format(monthLayout): true}
	m.months = []string{m.now().Format(monthLayout)}
	for _, r := range m.rows { // newest first
		if len(r.tx.Date) < len(monthLayout) {
			continue
		}
		month := r.tx.Date[:len(monthLayout)]
		if !seen[month] {
			seen[month] = true
			m.months = append(m.months, month)
		}
	}
	// The current month may be older than some row (future-dated entries):
	// keep the list sorted, newest first.
	for i := 1; i < len(m.months); i++ {
		for j := i; j > 0 && m.months[j] > m.months[j-1]; j-- {
			m.months[j], m.months[j-1] = m.months[j-1], m.months[j]
		}
	}
}
