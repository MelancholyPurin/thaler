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
		name string
		keys string
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
		{name: "forecast", keys: "F"},
		{name: "reports", keys: "R"},
		{name: "language", keys: "ctrl+l"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR", "Assets:Dollars USD")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Uncategorized", -1000)
			book(t, st, "2026-10-02", "EDEKA", "Assets:Bank", "Expenses:Groceries", -2000)
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
		{name: "German, the budget", tag: language.German, keys: "B", shows: []string{"Budget · Okt 2026", "Einnahmen", "Ausgegeben"}, hides: []string{"Income", "Spent"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR", "Assets:Dollars USD")
			book(t, st, "2026-10-01", "REWE", "Assets:Bank", "Expenses:Uncategorized", -1000)
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
