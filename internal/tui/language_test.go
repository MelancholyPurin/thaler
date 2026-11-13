package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
)

// inLanguage is a model on the store, writing amounts and dates in that language.
func inLanguage(t *testing.T, st *core.Local, tag language.Tag) *Model {
	t.Helper()
	m, err := newWithClock(t.Context(), st, Options{LoadConfig: testConfig, Loc: i18n.New(tag)}, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	return m
}

// A language writes the dates and amounts it shows its own way; English keeps ISO
// dates and gains thousands separators.
func Test_View_language(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		shows []string
		hides []string
	}{
		{name: "English", tag: language.English, shows: []string{"2026-10-01", "-1,234.56"}, hides: []string{"01.10.2026", "01/10/2026"}},
		{name: "German", tag: language.German, shows: []string{"01.10.2026", "-1.234,56"}, hides: []string{"2026-10-01", "-1,234.56"}},
		{name: "Spanish", tag: language.Spanish, shows: []string{"01/10/2026", "-1.234,56"}, hides: []string{"2026-10-01", "01.10.2026"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -123456)
			m := inLanguage(t, st, tc.tag)

			// when
			view := m.View()

			// then
			checkView(t, view, tc.shows, tc.hides)
		})
	}
}

// The form opens on today in the language's format and reads what is typed in it.
func Test_Update_addForm_language(t *testing.T) {
	tcs := []struct {
		name      string
		tag       language.Tag
		script    string
		shows     string // on the form, before it is saved
		wantAdded string
	}{
		{name: "German date and amount", tag: language.German, script: "a text:5,50 tab tab tab ctrl+u text:03.10.2026 ctrl+s", shows: "22.10.2026", wantAdded: "2026-10-03  | Assets:Bank -5.50 | Expenses:Groceries 5.50"},
		{name: "Spanish date", tag: language.Spanish, script: "a text:5,50 tab tab tab ctrl+u text:3/10/2026 ctrl+s", shows: "22/10/2026", wantAdded: "2026-10-03  | Assets:Bank -5.50 | Expenses:Groceries 5.50"},
		{name: "English date", tag: language.English, script: "a text:5.50 tab tab tab ctrl+u text:2026-10-03 ctrl+s", shows: "2026-10-22", wantAdded: "2026-10-03  | Assets:Bank -5.50 | Expenses:Groceries 5.50"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
			m := inLanguage(t, st, tc.tag)
			before := storedLines(t, st)

			// when
			play(m, "a")
			opened := m.View()
			play(m, "esc "+tc.script)

			// then
			checkView(t, opened, []string{tc.shows}, nil)
			added, _ := delta(before, storedLines(t, st))
			if diff := cmp.Diff([]string{tc.wantAdded}, added); diff != "" {
				t.Errorf("stored transactions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The search finds a date as it is written on screen, and as it is stored.
func Test_Update_filter_language(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		typed string
		shows []string
		hides []string
	}{
		{name: "German date", tag: language.German, typed: "01.10", shows: []string{"REWE"}, hides: []string{"Edeka"}},
		{name: "the stored form still works", tag: language.German, typed: "2026-10-01", shows: []string{"REWE"}, hides: []string{"Edeka"}},
		{name: "Spanish date", tag: language.Spanish, typed: "15/09", shows: []string{"Edeka"}, hides: []string{"REWE"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-09-15", "Edeka", "Assets:Bank", "Expenses:Groceries", -3000)
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, "T /") // all time, then the search box
			typeText(m, tc.typed)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// A failure is told in the language: here a date that does not exist (the form
// cuts the end of the message to fit).
func Test_Update_addForm_language_error(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		date  string
		shows string
	}{
		{name: "English", tag: language.English, date: "2026-02-31", shows: `invalid date "2026-02-31"`},
		{name: "Spanish", tag: language.Spanish, date: "31/02/2026", shows: `fecha inválida "31/02/2026"`},
		{name: "German", tag: language.German, date: "31.02.2026", shows: `ungültiges Datum "31.02.2026"`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, "a text:5 tab tab tab ctrl+u")
			typeText(m, tc.date)
			play(m, "ctrl+s")

			// then
			checkView(t, m.View(), []string{tc.shows}, nil)
		})
	}
}

// The accounts Thaler makes, and the types of accounts, are written in the language
// where a person reads them; the names a person chose are not touched.
func Test_View_builtinNames(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		keys  string
		shows []string
		hides []string
	}{
		{name: "the postings of a transaction, German", tag: language.German, keys: "tab tab down", shows: []string{"Ausgaben:Ohne Kategorie", "Vermögen:Bank"}, hides: []string{"Expenses:Uncategorized", "Assets:Bank"}},
		{name: "the postings of a transaction, Spanish", tag: language.Spanish, keys: "tab tab down", shows: []string{"Gastos:Sin categoría", "Activos:Bank"}, hides: []string{"Expenses:Uncategorized"}},
		{name: "the postings of a transaction, English", tag: language.English, keys: "tab tab down", shows: []string{"Expenses:Uncategorized", "Assets:Bank"}, hides: []string{"Ausgaben", "Activos"}},
		{name: "the budget, German", tag: language.German, keys: "B", shows: []string{"Ohne Kategorie", "Groceries"}, hides: []string{"Uncategorized:", " Uncategorized "}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Uncategorized", -1000)
			book(t, st, "2026-10-02", "EDEKA", "Assets:Bank", "Expenses:Groceries", -2000)
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// Every text on every screen and popup comes from the catalog: in the pseudo-language
// each message is accented and in brackets, so a plain English word left on screen
// is text that was written into the code.
func Test_View_pseudoLanguage(t *testing.T) {
	english := regexp.MustCompile(`(?i)\b(press|cancel|save|next|field|create|review|choose|passphrase|encrypted|restore|undo|undoes|search|quit|categorize|transfer|account|accounts|balance|import|backup|backups|transaction|transactions|payee|description|amount|never|nothing|please|wait|current|folder|database|profile|income|spending|targets|reports|forecast|ledger|budget|home|keys|delete|rule|rules|match|new|today|yesterday|language)\b`)
	tcs := []struct {
		name   string
		keys   string
		salary bool // books Income:Salary first
	}{
		{name: "home", keys: ""},
		{name: "the transactions", keys: "tab tab down"},
		{name: "one transaction", keys: "tab tab down enter"},
		{name: "add form", keys: "a"},
		{name: "add form, payee", keys: "a tab"},
		{name: "add form, category", keys: "a tab tab"},
		{name: "add form, date", keys: "a tab tab tab"},
		{name: "add form, description", keys: "a tab tab tab tab"},
		{name: "transfer", keys: "x"},
		{name: "transfer, date", keys: "x tab"},
		{name: "import", keys: "i"},
		{name: "new account", keys: "n"},
		{name: "new account, type", keys: "n tab"},
		{name: "new account, balance", keys: "n tab tab tab"},
		{name: "set balance", keys: "tab down b"},
		{name: "backup", keys: "S"},
		{name: "restore", keys: "ctrl+r"},
		{name: "remove old backups", keys: "ctrl+p"},
		{name: "delete", keys: "tab tab down d"},
		{name: "categorize", keys: "tab tab down c"},
		{name: "categorize, rule", keys: "tab tab down c tab"},
		{name: "budget", keys: "B"},
		{name: "budget with a target", keys: "B e text:30 enter"},
		{name: "budget with a target passed", keys: "B e text:5 enter"},
		{name: "budget target", keys: "B e"},
		{name: "budget target, income", keys: "B e down", salary: true},
		{name: "budget, planned income", keys: "B e down text:80 enter", salary: true},
		{name: "budget, planned income passed", keys: "B e down text:20 enter", salary: true},
		{name: "budget, targets over the planned income", keys: "B e down text:10 enter e text:30 enter", salary: true},
		{name: "budget target, error", keys: "B e enter"},
		{name: "budget, categories", keys: "B tab"},
		{name: "budget, a category's transactions", keys: "B tab enter"},
		{name: "budget, targets copied", keys: "B c"},
		{name: "budget, carry-over on", keys: "B e tab enter"},
		{name: "budget, carry-over off", keys: "B e tab enter e tab enter"},
		{name: "budget, carry-over popup", keys: "B e tab"},
		{name: "budget, targets copied and undone", keys: "B c z"},
		{name: "budget, nothing to copy", keys: "B c c"},
		{name: "budget, no targets to copy", keys: "B [ c"},
		{name: "forecast", keys: "F"},
		{name: "forecast, the last schedule", keys: "F G"},
		{name: "forecast, one account", keys: "F a"},
		{name: "forecast, a second account", keys: "F a a"},
		{name: "forecast, one account, the balances", keys: "F a tab"},
		{name: "forecast, one account, nothing due", keys: "F a a enter"},
		{name: "balances", keys: "F tab"},
		{name: "balances, the end", keys: "F tab G"},
		{name: "balances, under a minimum", keys: "F m text:5000 enter tab"},
		{name: "schedule, new", keys: "F n"},
		{name: "schedule, what it goes to", keys: "F n tab tab tab"},
		{name: "schedule, a transfer between currencies", keys: "F n tab tab tab text:Dollars tab"},
		{name: "schedule, an error", keys: "F n ctrl+s"},
		{name: "schedule, edited", keys: "F e"},
		{name: "schedule, paused", keys: "F p"},
		{name: "due", keys: "F enter"},
		{name: "due, the amount", keys: "F enter e"},
		{name: "due, an error", keys: "F enter e ctrl+u text:0 enter"},
		{name: "due, booked", keys: "F enter enter"},
		{name: "due, skipped", keys: "F enter s"},
		{name: "minimum", keys: "F m"},
		{name: "minimum, set", keys: "F m text:500"},
		{name: "minimum, an error", keys: "F m enter"},
		{name: "minimum, would fall", keys: "F m text:-50 enter"},
		{name: "minimum, already under", keys: "F m text:500 enter"},
		{name: "minimum, set and undone", keys: "F m text:500 enter z"},
		{name: "reports", keys: "R"},
		{name: "reports, a category", keys: "R down"},
		{name: "reports, an earlier month", keys: "R [ [ ["},
		{name: "reports, income against spending", keys: "R tab"},
		{name: "reports, income against spending, the oldest", keys: "R tab g"},
		{name: "reports, income against spending, an earlier month", keys: "R tab [ [ ["},
		{name: "reports, payees", keys: "R tab tab"},
		{name: "reports, payees, the last", keys: "R tab tab G"},
		{name: "reports, payees, an earlier month", keys: "R tab tab [ [ ["},
		{name: "reports, net worth", keys: "R tab tab tab"},
		{name: "reports, net worth, the oldest", keys: "R tab tab tab g"},
		{name: "reports, net worth, an earlier month", keys: "R tab tab tab [ [ ["},
		{name: "reports, range", keys: "R r"},
		{name: "reports, range, typed", keys: "R r text:2026-09 enter text:2026-10"},
		{name: "reports, range, not a month", keys: "R r text:soon enter enter"},
		{name: "reports, range, backwards", keys: "R r text:2026-10 enter text:2026-09 enter"},
		{name: "reports, range, too long", keys: "R r text:2000-01 enter enter"},
		{name: "reports, a chosen range", keys: "R r text:2025-01 enter enter"},
		{name: "reports, a chosen range, the second month", keys: "R r text:2026-09 enter text:2026-10 enter"},
		{name: "reports, a long range", keys: "R r text:2020-01 enter enter"},
		{name: "reports, a long range, income against spending", keys: "R tab r text:2020-01 enter enter"},
		{name: "reports, a chosen range, payees", keys: "R tab tab r text:2025-01 enter enter"},
		{name: "reports, a long range, net worth", keys: "R tab tab tab r text:2020-01 enter enter"},
		{name: "reports, save as CSV", keys: "R e"},
		{name: "reports, save as CSV, nothing typed", keys: "R e ctrl+u enter"},
		{name: "reports, save as CSV, a folder that is not there", keys: "R e ctrl+u raw:/zz/qq/out.csv enter"},
		{name: "reports, save as CSV, income against spending", keys: "R tab e"},
		{name: "reports, save as CSV, payees, a chosen range", keys: "R tab tab r text:2020-01 enter enter e"},
		{name: "reports, save as CSV, net worth", keys: "R tab tab tab e"},
		{name: "reports, back to the categories", keys: "R tab tab tab tab"},
		{name: "language", keys: "ctrl+l"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR", "Assets:Dollars USD")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Uncategorized", -1000)
			book(t, st, "2026-10-02", "EDEKA", "Assets:Bank", "Expenses:Groceries", -2000)
			if tc.salary { // not in the others: the picker shows the stored name, Income:Salary, in English
				book(t, st, "2026-10-03", "ACME", "Assets:Bank", "Income:Salary", 5000)
			}
			testSchedules(t, st, "Assets:Dollars")
			if err := st.SetBudgetTarget(t.Context(), "2026-09", "Expenses:Groceries", eur(10000)); err != nil {
				t.Fatal(err)
			}
			if err := st.SetBudgetRollover(t.Context(), "Expenses:Groceries", "2026-09"); err != nil {
				t.Fatal(err)
			}
			m := inLanguage(t, st, i18n.PseudoTag)

			// when
			play(m, tc.keys)
			view := m.View()

			// then
			if found := english.FindAllString(view, -1); len(found) > 0 {
				t.Errorf("plain English left on the screen: %q\n%s", found, view)
			}
		})
	}
}

// Spanish and German are translated: the screens and popups show their words, and no
// English message is left among them.
func Test_View_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		keys  string
		shows []string
		hides []string
	}{
		{name: "Spanish, the schedule popup", tag: language.Spanish, keys: "F n", shows: []string{"Nueva programación", "Nombre", "Importe", "Desde", "Cada", "Periodo", "Inicio", "Fin", "Tolerancia", "cómo llamarla", "enter siguiente"}, hides: []string{"New schedule", "Tolerance"}},
		{name: "German, the schedule popup", tag: language.German, keys: "F n", shows: []string{"Neuer Zeitplan", "Name", "Betrag", "Von", "Alle", "Beginn", "Ende", "Toleranz", "wie er heißen soll", "enter weiter"}, hides: []string{"New schedule", "Tolerance", "Start"}},
		{name: "Spanish, the minimum", tag: language.Spanish, keys: "F m", shows: []string{"Saldo mínimo", "Mínimo", "Lo más bajo que debe llegar", "enter guardar"}, hides: []string{"Minimum balance", "The lowest", "enter save"}},
		{name: "German, the minimum", tag: language.German, keys: "F m", shows: []string{"Mindestkontostand", "Minimum", "Der niedrigste Stand", "enter speichern"}, hides: []string{"Minimum balance", "The lowest", "enter save"}},
		{name: "Spanish, a balance that would fall", tag: language.Spanish, keys: "F m text:-50 enter", shows: []string{"bajaría a", "por debajo de su mínimo de", "-50,00 EUR"}, hides: []string{"would fall", "below its minimum"}},
		{name: "German, a balance that would fall", tag: language.German, keys: "F m text:-50 enter", shows: []string{"fiele am", "unter den Mindestbetrag von", "-50,00 EUR"}, hides: []string{"would fall", "below its minimum"}},
		{name: "Spanish, the balances", tag: language.Spanish, keys: "F tab", shows: []string{"Saldos", "Hoy", "Cambio", "al final de cada semana", "tab programaciones"}, hides: []string{"Balances", "Today", "Change", "end of each week"}},
		{name: "German, the balances", tag: language.German, keys: "F tab", shows: []string{"Salden", "Heute", "Änderung", "am Ende jeder Woche", "tab Zeitpläne"}, hides: []string{"Balances", "Today", "Change", "end of each week"}},
		{name: "Spanish, one account", tag: language.Spanish, keys: "F a", shows: []string{"Programaciones · Bank"}, hides: []string{"Schedules"}},
		{name: "Spanish, one account, the balances", tag: language.Spanish, keys: "F a tab", shows: []string{"Saldos · Bank", "a cuenta"}, hides: []string{"Balances", "a account"}},
		{name: "German, one account", tag: language.German, keys: "F a", shows: []string{"Zeitpläne · Bank"}, hides: []string{"Schedules"}},
		{name: "German, one account, the balances", tag: language.German, keys: "F a tab", shows: []string{"Salden · Bank", "a Konto"}, hides: []string{"Balances", "a account"}},
		{name: "Spanish, the reports", tag: language.Spanish, keys: "R down", shows: []string{"Gasto por categoría", "nov 2025 – oct 2026 · EUR", "Todo el gasto", "Total", "Al mes", "sep 2026"}, hides: []string{"Spending by category", "All spending", "Per month"}},
		{name: "German, the reports", tag: language.German, keys: "R down", shows: []string{"Ausgaben nach Kategorie", "Nov 2025 – Okt 2026 · EUR", "Alle Ausgaben", "Gesamt", "Pro Monat", "Sep 2026"}, hides: []string{"Spending by category", "All spending", "Per month"}},
		{name: "Spanish, income against spending", tag: language.Spanish, keys: "R tab", shows: []string{"Ingresos frente a gastos", "Ingresos", "Gastos", "Sobra", "Ahorro", "Total", "Barra: la parte de los ingresos que se gastó", "tab informe"}, hides: []string{"Income against spending", "Spent", "Kept", "Saved"}},
		{name: "German, income against spending", tag: language.German, keys: "R tab", shows: []string{"Einnahmen gegen Ausgaben", "Einnahmen", "Ausgaben", "Übrig", "Sparquote", "Gesamt", "Balken: der ausgegebene Teil der Einnahmen", "tab Bericht"}, hides: []string{"Income against spending", "Spent", "Kept", "Saved"}},
		{name: "Spanish, the reports keys", tag: language.Spanish, keys: "R", shows: []string{"↑/↓ categoría", "[ ] mes"}, hides: []string{"↑/↓ category"}},
		{name: "German, the reports keys", tag: language.German, keys: "R", shows: []string{"↑/↓ Kategorie", "[ ] Monat"}, hides: []string{"↑/↓ category"}},
		{name: "Spanish, the balances key", tag: language.Spanish, keys: "F", shows: []string{"tab saldos"}, hides: []string{"tab balances"}},
		{name: "German, the balances key", tag: language.German, keys: "F", shows: []string{"tab Salden"}, hides: []string{"tab balances"}},
		{name: "Spanish, the minimum key", tag: language.Spanish, keys: "F tab", shows: []string{"m mínimo"}, hides: []string{"m minimum"}},
		{name: "German, the minimum key", tag: language.German, keys: "F tab", shows: []string{"m Minimum"}, hides: []string{"m minimum"}},
		{name: "Spanish, what is due", tag: language.Spanish, keys: "F enter", shows: []string{"Qué vence", "Miete", "28/09/2026", "hace 24 días", "enter apuntarlo", "e cambiar el importe", "s omitir", "esc dejarlo"}, hides: []string{"What is due", "days ago", "book it"}},
		{name: "German, what is due", tag: language.German, keys: "F enter", shows: []string{"Was fällig ist", "Miete", "28.09.2026", "vor 24 Tagen", "enter buchen", "e Betrag ändern", "s überspringen", "esc beenden"}, hides: []string{"What is due", "days ago", "book it"}},
		{name: "Spanish, a date booked", tag: language.Spanish, keys: "F enter enter", shows: []string{"Miete apuntada el 28/09/2026: -900,00 EUR", "todo respondido", "z deshace"}, hides: []string{"booked"}},
		{name: "German, a date skipped", tag: language.German, keys: "F enter s", shows: []string{"Miete 28.09.2026 übersprungen", "alles beantwortet", "z macht rückgängig"}, hides: []string{"skipped"}},
		{name: "Spanish, a schedule paused", tag: language.Spanish, keys: "F p", shows: []string{"Miete está en pausa · z deshace"}, hides: []string{"is paused"}},
		{name: "German, a schedule paused", tag: language.German, keys: "F p", shows: []string{"Miete ist pausiert · z macht rückgängig"}, hides: []string{"is paused"}},
		{name: "Spanish, the tabs and panels", tag: language.Spanish, keys: "", shows: []string{"Libro", "Presupuesto", "Previsión", "Informes", "[2] Cuentas", "Todas las cuentas"}, hides: []string{"Ledger", "Accounts"}},
		{name: "German, the tabs and panels", tag: language.German, keys: "", shows: []string{"Transaktionen", "Prognose", "Berichte", "[2] Konten", "Alle Konten"}, hides: []string{"Forecast", "Accounts"}},
		{name: "Spanish, the add form", tag: language.Spanish, keys: "a", shows: []string{"Añadir transacción", "Importe", "Destinatario", "Categoría", "enter sig."}, hides: []string{"Add transaction", "Payee"}},
		{name: "German, the add form", tag: language.German, keys: "a", shows: []string{"Transaktion hinzufügen", "Betrag", "Empfänger", "Kategorie", "enter weiter"}, hides: []string{"Add transaction", "Payee"}},
		{name: "Spanish, the transfer", tag: language.Spanish, keys: "x", shows: []string{"Transferencia entre tus cuentas", "Recibido", "Desde", "Hacia"}, hides: []string{"Transfer between"}},
		{name: "German, the transfer", tag: language.German, keys: "x", shows: []string{"Überweisungen zwischen deinen Konten", "Erhalten", "Von", "Nach"}, hides: []string{"Transfer between"}},
		{name: "Spanish, the help", tag: language.Spanish, keys: "?", shows: []string{"Teclas", "pantallas: Libro, Presupuesto"}, hides: []string{"screens:"}},
		{name: "Spanish, the help, further down", tag: language.Spanish, keys: "? pgdown", shows: []string{"c, luego tab", "deshacer el último cambio"}, hides: []string{"then tab", "undo the last change"}},
		{name: "German, the help", tag: language.German, keys: "?", shows: []string{"Tasten", "Ansichten: Transaktionen, Budget"}, hides: []string{"screens:"}},
		{name: "German, the help, further down", tag: language.German, keys: "? pgdown", shows: []string{"c, dann tab", "die letzte Änderung rückgängig machen"}, hides: []string{"then tab", "undo the last change"}},
		{name: "Spanish, the language popup", tag: language.Spanish, keys: "ctrl+l", shows: []string{"Idioma", "✓ Español", "Se guarda en config.toml"}, hides: []string{"Saved in"}},
		{name: "German, the language popup", tag: language.German, keys: "ctrl+l", shows: []string{"Sprache", "✓ Deutsch", "In config.toml gespeichert"}, hides: []string{"Saved in"}},
		{name: "Spanish, the budget", tag: language.Spanish, keys: "B", shows: []string{"Presupuesto · oct 2026", "Ingresos", "Gastado"}, hides: []string{"Income", "Spent"}},
		{name: "Spanish, the budget with a target", tag: language.Spanish, keys: "B e text:30 enter", shows: []string{"Objetivo", "Queda", "Objetivos", "Sin objetivo", "de 30,00 · +100,00 de arrastre · quedan 120,00"}, hides: []string{"Target", "left", "No target"}},
		{name: "Spanish, the target popup", tag: language.Spanish, keys: "B e", shows: []string{"Objetivos de oct 2026", "En EUR, la moneda base", "enter guardar"}, hides: []string{"Targets for", "base currency"}},
		{name: "German, the budget with a target", tag: language.German, keys: "B e text:30 enter", shows: []string{"Ziel", "Übrig", "Ziele", "Ohne Ziel", "von 30,00 · +100,00 Übertrag · 120,00 übrig"}, hides: []string{"Target", "left", "No target"}},
		{name: "Spanish, something carried in", tag: language.Spanish, keys: "B", shows: []string{"Arrastre", "+100,00", "+100,00 de arrastre · quedan 90,00"}, hides: []string{"carried", "left"}},
		{name: "Spanish, the carry-over", tag: language.Spanish, keys: "B e", shows: []string{"Arrastre", "[x]", "↻", "tab cambia", "tab arrastre"}, hides: []string{"Carry over", "switches"}},
		{name: "German, something carried in", tag: language.German, keys: "B", shows: []string{"Übertrag", "+100,00", "+100,00 Übertrag · 90,00 übrig"}, hides: []string{"carried", "left"}},
		{name: "German, the carry-over", tag: language.German, keys: "B e", shows: []string{"Übertrag", "[x]", "↻", "tab wechselt", "tab Übertrag"}, hides: []string{"Carry over", "switches"}},
		{name: "Spanish, a target and the carry-over together", tag: language.Spanish, keys: "B e text:30 tab enter", shows: []string{"Groceries: objetivo 30,00 EUR · arrastre desactivado · z deshace"}, hides: []string{"target", "carry-over"}},
		{name: "German, a target and the carry-over together", tag: language.German, keys: "B e text:30 tab enter", shows: []string{"Groceries: Ziel 30,00 EUR · Übertrag ausgeschaltet · z macht rückgängig"}, hides: []string{"target", "carry-over"}},
		{name: "Spanish, a category's transactions", tag: language.Spanish, keys: "B tab enter", shows: []string{"1 transacción en Groceries · esc las muestra todas", "Transacciones"}, hides: []string{"transaction in"}},
		{name: "Spanish, the bar on the categories", tag: language.Spanish, keys: "B tab", shows: []string{"↑/↓ categoría", "enter transacciones"}, hides: []string{"category"}},
		{name: "German, a category's transactions", tag: language.German, keys: "B tab enter", shows: []string{"1 Transaktion in Groceries · esc zeigt alle"}, hides: []string{"transaction in"}},
		{name: "German, the bar on the categories", tag: language.German, keys: "B tab", shows: []string{"↑/↓ Kategorie", "enter Transaktionen"}, hides: []string{"category"}},
		{name: "Spanish, left this month", tag: language.Spanish, keys: "", shows: []string{"Queda este mes", "90,00 EUR"}, hides: []string{"Left this month"}},
		{name: "German, left this month", tag: language.German, keys: "", shows: []string{"Übrig im Monat", "90,00 EUR"}, hides: []string{"Left this month"}},
		{name: "Spanish, targets copied", tag: language.Spanish, keys: "B c", shows: []string{"Se copió 1 objetivo de sep 2026 a oct 2026", "z deshace"}, hides: []string{"copied"}},
		{name: "Spanish, nothing to copy", tag: language.Spanish, keys: "B c c", shows: []string{"nada que copiar: cada objetivo de sep 2026 ya está fijado en oct 2026"}, hides: []string{"nothing to copy"}},
		{name: "German, targets copied", tag: language.German, keys: "B c", shows: []string{"1 Ziel von Sep 2026 nach Okt 2026 kopiert"}, hides: []string{"copied"}},
		{name: "German, nothing to copy", tag: language.German, keys: "B c c", shows: []string{"nichts zu kopieren: jedes Ziel von Sep 2026 ist in Okt 2026 schon gesetzt"}, hides: []string{"nothing to copy"}},
		{name: "German, the target popup", tag: language.German, keys: "B e", shows: []string{"Ziele für Okt 2026", "In EUR, der Basiswährung", "enter speichern"}, hides: []string{"Targets for", "base currency"}},
		{name: "German, the budget", tag: language.German, keys: "B", shows: []string{"Budget · Okt 2026", "Einnahmen", "Ausgegeben"}, hides: []string{"Income", "Spent"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR", "Assets:Dollars USD")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Uncategorized", -1000)
			book(t, st, "2026-10-02", "EDEKA", "Assets:Bank", "Expenses:Groceries", -1000)
			testSchedules(t, st, "Assets:Dollars")
			if err := st.SetBudgetTarget(t.Context(), "2026-09", "Expenses:Groceries", eur(10000)); err != nil {
				t.Fatal(err)
			}
			if err := st.SetBudgetRollover(t.Context(), "Expenses:Groceries", "2026-09"); err != nil {
				t.Fatal(err)
			}
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// ctrl+l chooses the language: the screen changes at once, and the choice is written
// to config.toml so that the next start opens in it.
func Test_Update_language(t *testing.T) {
	tcs := []struct {
		name       string
		start      language.Tag
		config     *string // nil: no file yet
		noPath     bool    // no config file location is known
		pathIsDir  bool    // what is at the config file location is a folder, which cannot be read as a file
		keys       string
		wantCode   string
		wantStatus string
		wantErr    bool
		wantFile   string // "" when there should be no file
		shows      []string
	}{
		{
			name: "German, written to a new file", start: language.English, keys: "ctrl+l down down enter",
			wantCode: "de", wantStatus: "Sprache: Deutsch (in config.toml gespeichert)",
			wantFile: "# Created by Thaler. The sample below is switched off: remove the leading # from a line to use it.\n#\n# sample\nlanguage = \"de\"\n",
			shows:    []string{"Transaktionen", "Prognose"},
		},
		{
			name: "Spanish, replacing the setting that was there", start: language.German, config: ptr("# mine\nlanguage = \"de\"\nkeep_backups = 3\n"), keys: "ctrl+l up enter",
			wantCode: "es", wantStatus: "Idioma: Español (guardado en config.toml)",
			wantFile: "# mine\nlanguage = \"es\"\nkeep_backups = 3\n",
			shows:    []string{"Libro", "Previsión"},
		},
		{
			name: "back to English", start: language.German, config: ptr("language = \"de\"\n"), keys: "ctrl+l up up enter",
			wantCode: "en", wantStatus: "Language: English (saved in config.toml)",
			wantFile: "language = \"en\"\n",
			shows:    []string{"Ledger", "Forecast"},
		},
		{
			name: "the popup opens on the language in use, so enter alone keeps it", start: language.German, config: ptr(""), keys: "ctrl+l enter",
			wantCode: "de", wantStatus: "Sprache: Deutsch (in config.toml gespeichert)",
			wantFile: "language = \"de\"\n",
			shows:    []string{"Transaktionen"},
		},
		{
			name: "esc leaves everything as it was", start: language.English, config: ptr("keep_backups = 3\n"), keys: "ctrl+l down esc",
			wantCode: "en", wantStatus: "",
			wantFile: "keep_backups = 3\n",
			shows:    []string{"Ledger"},
		},
		{
			name: "the cursor stops at the ends of the list", start: language.English, config: ptr(""), keys: "ctrl+l up up down down down down enter",
			wantCode: "de", wantStatus: "Sprache: Deutsch (in config.toml gespeichert)",
			wantFile: "language = \"de\"\n",
			shows:    []string{"Transaktionen"},
		},
		{
			name: "no config file location: the switch stands for the session", start: language.English, noPath: true, keys: "ctrl+l down enter",
			wantCode: "es", wantStatus: "Idioma: Español (hasta que salgas: no hay archivo de configuración donde guardarlo)",
			shows: []string{"Libro"},
		},
		{
			name: "a config file that cannot be written: the switch stands, and the status says why", start: language.English, pathIsDir: true, keys: "ctrl+l down enter",
			wantCode: "es", wantErr: true,
			wantStatus: "Idioma: Español (hasta que salgas: no se pudo guardar: ",
			shows:      []string{"Libro"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
			m := inLanguage(t, st, tc.start)
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			if !tc.noPath {
				m.opts.ConfigPath, m.opts.ConfigTemplate = path, "# sample\n"
			}
			if tc.config != nil {
				if err := os.WriteFile(path, []byte(*tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.pathIsDir {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}

			// when
			play(m, tc.keys)

			// then
			gotFile := ""
			if b, err := os.ReadFile(path); err == nil {
				gotFile = string(b)
			}
			status := m.status
			if tc.wantErr && strings.HasPrefix(status, tc.wantStatus) {
				status = tc.wantStatus // what follows is the system's words, which differ from machine to machine
			}
			got := []string{m.loc.Code(), status, gotFile}
			want := []string{tc.wantCode, tc.wantStatus, tc.wantFile}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantErr, m.statusIsError); diff != "" {
				t.Errorf("status error mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

// What was made before the switch speaks the new language afterwards: the undo of a
// change, the text the search looks through and the hint in the search box.
func Test_Update_language_followsOn(t *testing.T) {
	type observed struct {
		Status      string
		Placeholder string
		Found       int // transactions the search for the German date finds
	}
	tcs := []struct {
		name   string
		script string
		want   observed
	}{
		{
			name:   "undo after the switch",
			script: "a text:5.50 tab tab tab ctrl+u text:2026-10-03 ctrl+s ctrl+l down down enter z",
			want:   observed{Status: "Anlegen von #2 rückgängig gemacht", Placeholder: "Fuzzy-Suche: Empfänger, Notiz, Kategorie, Betrag", Found: 0},
		},
		{
			name:   "the search finds the date as the new language writes it",
			script: "ctrl+l down down enter / text:01.10.2026",
			want:   observed{Status: "", Placeholder: "Fuzzy-Suche: Empfänger, Notiz, Kategorie, Betrag", Found: 1},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Groceries", -1000)
			m := inLanguage(t, st, language.English)
			m.opts.ConfigPath = filepath.Join(t.TempDir(), "config.toml")

			// when
			play(m, tc.script)

			// then
			got := observed{Status: m.status, Placeholder: m.filter.Placeholder}
			if m.mode == modeFilter {
				got.Found = len(m.view)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
