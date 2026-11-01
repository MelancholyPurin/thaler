package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// appName and the banners are the only places the product name appears in the
// interface.
const appName = "thaler"

// bannerAntiqua is "Thaler" in a roman serif face, drawn with quadrant block
// characters (two pixels per cell in each direction). It was rasterised from
// Old Standard TT Bold, an open font (SIL Open Font License), at 56 pixels wide.
// The letters up to the e use a low cutoff so that the thin bar of the e
// survives; the r uses a higher one, which keeps its flag light. Every line has
// the same width so that centring them keeps the letters aligned.
var bannerAntiqua = []string{
	"█▛██▜█▝█▌        ▜█         ",
	"▛ ██ █ █▌        ▐█         ",
	"▌ ██ ▐ █▙▜▙ ▗▛▙▖ ▐█ ▗▛▙  █▄▙",
	"  ██   █▌▐█ ▜▌▐▌ ▐█ █▌▐▌ █▀█",
	"  ██   █▌▐█ ▗▞▜▌ ▐█ ███▌ █  ",
	"  ██   █▌▐█ █▌▐▌▖▐█ █▌ ▖ █  ",
	" ▄██▖ ▗█▙▐█▖▜▙██▘▟█▖▝▙▄▘▗█▖ ",
}

// bannerCompact is the three-line fallback for small terminals.
var bannerCompact = []string{
	"╺┳╸ ╻ ╻ ┏━┓ ╻   ┏━╸ ┏━┓",
	" ┃  ┣━┫ ┣━┫ ┃   ┣╸  ┣┳┛",
	" ╹  ╹ ╹ ╹ ╹ ┗━╸ ┗━╸ ╹┗╸",
}

// projectURL is where Thaler lives, shown on the home screen.
const projectURL = "https://github.com/MelancholyPurin"

// homeBanner picks the largest banner that fits w cells with room lines to
// spare, and whether there is a blank line to spare above it. When even the
// compact one does not fit, it is used anyway and the pane scrolls its end.
func homeBanner(w, room int) (art []string, blankAbove bool) {
	for _, b := range [][]string{bannerAntiqua, bannerCompact} {
		if lipgloss.Width(b[0]) > w {
			continue
		}
		if len(b)+1 <= room {
			return b, true
		}
		if len(b) <= room {
			return b, false
		}
	}
	return bannerCompact, true
}

// rightPane shows details for whatever is selected in the focused panel.
func (m *Model) rightPane(l layout) string {
	iw := l.rightW - 2
	var title string
	var lines []string
	switch {
	case m.screen == screenBudget:
		title, lines = m.loc.T("budget_title", map[string]any{"Period": m.periodLabel(m.period)}), m.budgetDetail(iw)
	case m.focus == paneAccounts:
		title, lines = m.accountDetail(iw)
	case m.focus == paneTransactions:
		title, lines = m.transactionDetail(iw)
	default:
		title, lines = appName, m.homeDetail(iw, l.avail-2)
	}
	if ih := l.avail - 2; ih > 1 && len(lines) > ih {
		lines = append(lines[:ih-1:ih-1], dim.Render(m.loc.T("more_lines", map[string]any{"Count": len(lines) - ih + 1})))
	}
	return box(title, lines, l.rightW, l.avail, false, "")
}

// kv renders a dim key and a value, with the value wrapped under itself.
func kv(key, value string, keyW, w int) []string {
	valueW := max(w-keyW-1, 8)
	var out []string
	for i, part := range strings.Split(ansi.Wrap(value, valueW, ""), "\n") {
		k := strings.Repeat(" ", keyW)
		if i == 0 {
			k = dim.Render(fit(key, keyW))
		}
		out = append(out, k+" "+part)
	}
	return out
}

// ---- home -------------------------------------------------------------------------

// homeDetail is the pane shown beside the Home tile; h is the number of lines
// it has, which decides how large a banner there is room for.
func (m *Model) homeDetail(w, h int) []string {
	var facts []string
	netText := m.loc.Money(m.netWorth.Total)
	if len(m.netWorth.Missing) > 0 {
		netText = "≈ " + netText
	}
	uncat := m.uncategorizedCount()
	uncatText := fmt.Sprint(uncat)
	if uncat > 0 {
		uncatText = warn.Render(m.loc.T("uncat_review", map[string]any{"Count": uncat}))
	}
	span := "—"
	if n := len(m.rows); n > 0 {
		span = m.loc.StoredDate(m.rows[n-1].tx.Date) + " → " + m.loc.StoredDate(m.rows[0].tx.Date)
	}
	const keyW = 15
	pad := strings.Repeat(" ", max(0, (w-44)/2))
	add := func(key, value string) {
		for _, line := range kv(key, value, keyW, w-len(pad)) {
			facts = append(facts, pad+line)
		}
	}
	add(m.loc.T("label_net_worth", nil), bold.Render(netText))
	if len(m.netWorth.Missing) > 0 {
		codes := make([]string, len(m.netWorth.Missing))
		for i, c := range m.netWorth.Missing {
			codes[i] = string(c)
		}
		add("", warn.Render(m.loc.T("home_no_rate", map[string]any{"Codes": strings.Join(codes, ", "), "First": codes[0], "Base": string(m.netWorth.Base)})))
	}
	add(m.loc.T("label_accounts", nil), fmt.Sprint(len(m.accounts)))
	add(m.loc.T("label_transactions", nil), fmt.Sprint(len(m.rows)))
	add(m.loc.T("label_period", nil), span)
	add(m.loc.T("label_uncategorized", nil), uncatText)
	add(m.loc.T("label_base_currency", nil), string(m.netWorth.Base))
	facts = append(facts, "")
	for _, l := range wrapped(m.loc.T("home_keys_hint", nil), w, dim) {
		facts = append(facts, centered(l, w))
	}

	credit := []string{"", centered(dim.Render(m.loc.T("home_credit", nil)), w), centered(dim.Render(projectURL), w), ""}
	art, blankAbove := homeBanner(w, h-len(credit)-len(facts))
	if len(art)+len(credit)+len(facts) > h {
		// Even the small banner does not fit with the facts: the facts matter most,
		// then the banner, and the credit goes first.
		credit = nil
		art, blankAbove = homeBanner(w, h-len(facts))
		if len(art)+len(facts) > h {
			art, blankAbove = nil, false
		}
	}
	var lines []string
	if blankAbove {
		lines = append(lines, "")
	}
	for _, b := range art {
		lines = append(lines, centered(accent.Render(b), w))
	}
	lines = append(lines, credit...)
	return append(lines, facts...)
}

// ---- accounts ----------------------------------------------------------------------

func (m *Model) accountDetail(w int) (string, []string) {
	const keyW = 15
	var lines []string
	if m.scopeAccount() == "" {
		lines = append(lines, bold.Render(m.loc.T("all_accounts", nil)), "")
		for _, e := range m.accounts {
			lines = append(lines, leftRight(m.loc.AccountName(e.account.Name), m.loc.Money(e.balance), w))
		}
		lines = append(lines, "")
		netText := m.loc.Money(m.netWorth.Total)
		if len(m.netWorth.Missing) > 0 {
			netText = "≈ " + netText
		}
		lines = append(lines, kv(m.loc.T("label_net_worth", nil), bold.Render(netText), keyW, w)...)
		for _, c := range m.netWorth.Missing {
			lines = append(lines, wrapped(m.loc.T("account_no_rate", map[string]any{"From": c, "To": m.netWorth.Base}), w, warn)...)
		}
		return m.loc.T("all_accounts", nil), lines
	}

	e := m.accounts[m.accts.cursor-1]
	lines = append(lines, bold.Render(m.loc.AccountName(e.account.Name)), "")
	lines = append(lines, kv(m.loc.T("label_type", nil), m.loc.AccountName(string(e.account.Type)), keyW, w)...)
	lines = append(lines, kv(m.loc.T("label_currency", nil), string(e.account.Currency), keyW, w)...)
	lines = append(lines, kv(m.loc.T("label_balance", nil), bold.Render(m.loc.Money(e.balance)), keyW, w)...)
	lines = append(lines, kv(m.loc.T("label_transactions", nil), fmt.Sprint(e.txCount), keyW, w)...)
	if e.uncat > 0 {
		lines = append(lines, kv(m.loc.T("label_uncategorized", nil), warn.Render(fmt.Sprint(e.uncat)), keyW, w)...)
	}
	if e.last != "" {
		lines = append(lines, kv(m.loc.T("label_last_activity", nil), m.loc.StoredDate(e.last), keyW, w)...)
	}

	in, out := m.periodFlow(e.account.Name)
	lines = append(lines, "", dim.Render(m.loc.T("money_moved", map[string]any{"Period": m.periodLabel(m.period)})))
	lines = append(lines, kv(m.loc.T("label_in", nil), accent.Render(m.loc.Money(in)), keyW, w)...)
	lines = append(lines, kv(m.loc.T("label_out", nil), m.loc.Money(out), keyW, w)...)
	return m.loc.AccountName(e.account.Name), lines
}

// periodFlow sums the money that moved into and out of an account in the
// selected period.
func (m *Model) periodFlow(account string) (in, out ledger.Money) {
	cur := ledger.Money{}
	for _, e := range m.accounts {
		if e.account.Name == account {
			cur = ledger.Money{Currency: e.account.Currency}
		}
	}
	in, out = cur, cur
	for _, r := range m.rows {
		if !strings.HasPrefix(r.tx.Date, m.period) {
			continue
		}
		for _, p := range r.tx.Postings {
			if p.Account != account {
				continue
			}
			if p.Amount.Minor > 0 {
				in.Minor += p.Amount.Minor
			} else {
				out.Minor += p.Amount.Minor
			}
		}
	}
	return in, out
}

// ---- transactions ----------------------------------------------------------------------

func (m *Model) transactionDetail(w int) (string, []string) {
	r, ok := m.selected()
	if !ok {
		return m.loc.T("transaction_title", nil), []string{dim.Render(m.loc.T("nothing_selected", nil))}
	}
	const keyW = 10
	var lines []string
	lines = append(lines, bold.Render(m.loc.StoredDate(r.tx.Date))+dim.Render(fmt.Sprintf("   #%d", r.tx.ID)), "")
	if p := clean(r.tx.Payee); p != "" {
		lines = append(lines, strings.Split(ansi.Wrap(bold.Render(p), w, ""), "\n")...)
	}
	if d := clean(r.tx.Description); d != "" {
		lines = append(lines, strings.Split(ansi.Wrap(d, w, ""), "\n")...)
	}

	lines = append(lines, "", dim.Render(m.loc.T("postings_heading", nil)))
	for _, p := range r.tx.Postings {
		name := m.loc.AccountName(p.Account)
		amount := m.loc.Signed(p.Amount)
		if p.Account == ledger.UncategorizedExpense || p.Account == ledger.UncategorizedIncome {
			name, amount = warn.Render(name), warn.Render(amount)
		}
		for _, l := range leftRightLines(name, amount, w-2) {
			lines = append(lines, "  "+l)
		}
	}

	lines = append(lines, "")
	switch {
	case r.simple:
		lines = append(lines, kv(m.loc.T("label_account", nil), m.loc.AccountName(r.source.Account), keyW, w)...)
		lines = append(lines, kv(m.loc.T("label_category", nil), m.loc.AccountName(r.category.Account), keyW, w)...)
		if r.uncategorized {
			lines = append(lines, "")
			lines = append(lines, labelled(m.loc.T("uncat_label", nil), warn, m.loc.T("uncat_notice", nil), dim, w)...)
		}
		if r.transfer {
			lines = append(lines, "")
			if partner, ok := m.findRow(r.tx.TransferWith); ok && r.tx.TransferWith != 0 {
				lines = append(lines, labelled(m.loc.T("transfer_label", nil), accent, m.loc.T("transfer_with", map[string]any{"ID": partner.tx.ID, "Account": m.loc.AccountName(partner.source.Account), "Date": m.loc.StoredDate(partner.tx.Date)}), dim, w)...)
			} else {
				lines = append(lines, labelled(m.loc.T("transfer_label", nil), warn, m.loc.T("transfer_waiting", nil), dim, w)...)
			}
		}
	default:
		lines = append(lines, wrapped(m.loc.T("transfer_or_split", nil), w, dim)...)
	}
	return m.loc.T("transaction_n_title", map[string]any{"ID": r.tx.ID}), lines
}
