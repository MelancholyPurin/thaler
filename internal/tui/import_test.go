package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

const exportHeader = "Buchungstag;Verwendungszweck;Beguenstigter/Zahlungspflichtiger;Betrag;Waehrung;Info\n"

// exportCSV is a Sparkasse-style export: one groceries row the test rules
// know, two they do not, and one pending row that must be ignored.
const exportCSV = exportHeader +
	"02.10.26;Einkauf;REWE Markt;-23,47;EUR;Umsatz gebucht\n" +
	"03.10.26;;Mystery Shop;-5,00;EUR;Umsatz gebucht\n" +
	"04.10.26;Gehalt;Arbeitgeber;1.000,00;EUR;Umsatz gebucht\n" +
	"05.10.26;;Pending Shop;-9,99;EUR;Umsatz vorgemerkt\n"

// writeExport puts a CSV file in a fresh folder, points the model's downloads
// folder at it, and returns the file's path.
func writeExport(t *testing.T, m *Model, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	m.downloads = dir
	return filepath.Join(dir, name)
}

// statementOFX is an OFX statement of two transactions, made up for the tests.
const statementOFX = "OFXHEADER:100\nDATA:OFXSGML\n\n<OFX><BANKMSGSRSV1><STMTTRNRS><STMTRS><CURDEF>EUR<BANKTRANLIST>\n" +
	"<STMTTRN><TRNTYPE>POS<DTPOSTED>20261004<TRNAMT>-23.47<FITID>1<NAME>REWE Markt</STMTTRN>\n" +
	"<STMTTRN><TRNTYPE>CREDIT<DTPOSTED>20261001<TRNAMT>2450.00<FITID>2<NAME>Muster GmbH</STMTTRN>\n" +
	"</BANKTRANLIST></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>\n"

// onlyFile makes name, with content, the only export in the import folder.
func onlyFile(t *testing.T, m *Model, name, content string) {
	t.Helper()
	entries, err := os.ReadDir(m.downloads)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(m.downloads, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(m.downloads, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func Test_isExport(t *testing.T) {
	tcs := []struct {
		name string
		want bool
	}{
		{name: "umsaetze.csv", want: true},
		{name: "statement.ofx", want: true},
		{name: "statement.QFX", want: true},
		{name: "konto.qif", want: true},
		{name: "statement.pdf"},
		{name: "notes.txt"},
		{name: "csv"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.name

			// when
			got := isExport(tc.name)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_cleanPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	tcs := []struct{ name, in, want string }{
		{name: "a plain path", in: "/tmp/a.csv", want: "/tmp/a.csv"},
		{name: "surrounding whitespace", in: "  /tmp/a.csv \n", want: "/tmp/a.csv"},
		{name: "a dropped file has its spaces escaped", in: `/tmp/my\ export.csv`, want: "/tmp/my export.csv"},
		{name: "double quotes", in: `"/tmp/my export.csv"`, want: "/tmp/my export.csv"},
		{name: "single quotes", in: `'/tmp/my export.csv'`, want: "/tmp/my export.csv"},
		{name: "a home directory", in: "~/Downloads/a.csv", want: filepath.Join(home, "Downloads/a.csv")},
		{name: "just a name", in: "umsaetze", want: "umsaetze"},
		{name: "empty", in: "", want: ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.in

			// when
			got := cleanPath(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Update_import(t *testing.T) {
	type observed struct {
		Flow
		Scope     string
		OnlyUncat bool
		Undoable  int // how many changes z could take back
	}
	rewe := "2026-10-02 REWE Markt | Assets:Bank -23.47 | Expenses:Groceries 23.47"
	shop := "2026-10-03 Mystery Shop | Assets:Bank -5.00 | Expenses:Uncategorized 5.00"
	pay := "2026-10-04 Arbeitgeber | Assets:Bank 1000.00 | Income:Uncategorized -1000.00"
	imported := Flow{
		Mode:   "list",
		Status: "Imported 3 into Bank · 2 need a category (u shows all) · z undoes",
		Added:  []string{rewe, shop, pay},
	}
	tcs := []struct {
		name   string
		file   string // the export's content; "" for a folder with nothing in it
		script string
		want   observed
		shows  []string
	}{
		{
			name: "i lists the downloads", file: exportCSV, script: "i",
			want:  observed{Flow: Flow{Mode: "import"}},
			shows: []string{"Import a bank export", "umsaetze.csv", "Assets:Bank", "sparkasse"},
		},
		{
			name: "enter reviews it and writes nothing", file: exportCSV, script: "i enter",
			want:  observed{Flow: Flow{Mode: "import"}},
			shows: []string{"3 new", "0 already imported", "2 uncategorized", "1 pending ignored", "Groceries", "REWE Markt"},
		},
		{
			name: "a second enter imports it, and shows what needs a category", file: exportCSV, script: "i enter enter",
			want: observed{Flow: imported, Scope: "Assets:Bank", OnlyUncat: true, Undoable: 1},
		},
		{
			name: "z takes the import back", file: exportCSV, script: "i enter enter z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid the import of 3 transactions"}, Scope: "Assets:Bank", OnlyUncat: true},
		},
		{
			name: "the same file again finds nothing new and leaves nothing to undo", file: exportCSV, script: "i enter enter i enter enter",
			want: observed{
				Flow:  Flow{Mode: "list", Status: "Nothing new: all 3 rows were already imported", Added: []string{rewe, shop, pay}},
				Scope: "Assets:Bank", OnlyUncat: true, Undoable: 1,
			},
		},
		{
			name: "the second review says all three are stored", file: exportCSV, script: "i enter enter i enter",
			want:  observed{Flow: Flow{Mode: "import", Added: []string{rewe, shop, pay}}, Scope: "Assets:Bank", OnlyUncat: true, Undoable: 1},
			shows: []string{"0 new", "3 already imported"},
		},
		{
			name: "a row that cannot be read is listed, and enter does not import past it", file: exportCSV + "07.10.26;;Broken;abc;EUR;Umsatz gebucht\n", script: "i enter enter",
			want:  observed{Flow: Flow{Mode: "import", Err: "1 row has problems and would be skipped; press y to import the rest"}},
			shows: []string{"1 with problems", "line 6"},
		},
		{
			name: "y imports the rest", file: exportCSV + "07.10.26;;Broken;abc;EUR;Umsatz gebucht\n", script: "i enter y",
			want: observed{Flow: imported, Scope: "Assets:Bank", OnlyUncat: true, Undoable: 1},
		},
		{
			name: "esc in the review goes back to the file", file: exportCSV, script: "i enter esc",
			want: observed{Flow: Flow{Mode: "import"}},
		},
		{
			name: "esc again writes nothing", file: exportCSV, script: "i enter esc esc",
			want: observed{Flow: Flow{Mode: "list"}},
		},
		{
			name: "with no file to pick, enter asks for one", file: "", script: "i enter",
			want: observed{Flow: Flow{Mode: "import", Err: "choose an export (CSV, OFX or QIF): paste its path, or pick one from the list"}},
		},
		{
			name: "a path that does not exist", file: "", script: "i raw:/no/such/file.csv enter",
			want: observed{Flow: Flow{Mode: "import", Err: "no such file: /no/such/file.csv"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			if tc.file != "" {
				writeExport(t, m, "umsaetze.csv", tc.file)
			} else {
				m.downloads = t.TempDir()
			}
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), Scope: m.scopeAccount(), OnlyUncat: m.onlyUncat, Undoable: len(m.undo)}
			if m.mode == modeImport || m.mode == modeList && tc.script == "i enter esc esc" {
				got.Scope, got.OnlyUncat, got.Undoable = tc.want.Scope, tc.want.OnlyUncat, tc.want.Undoable
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			view := ""
			if m.mode == modeImport {
				view = m.View()
			}
			checkView(t, view, tc.shows, nil)
		})
	}
}

// Changing a row's category in the review, before anything is written.
func Test_Update_import_category(t *testing.T) {
	type observed struct {
		Step       string
		Chosen     map[int]string
		Categories []string // of the rows of the review, in order
		NewUncat   int
		Err        string
		Added      []string // what the import wrote
	}
	const (
		groceries = "Expenses:Groceries"
		unc       = ledger.UncategorizedExpense
		incUnc    = ledger.UncategorizedIncome
	)
	broken := exportCSV + "07.10.26;;Broken;abc;EUR;Umsatz gebucht\n"
	tcs := []struct {
		name   string
		file   string
		setup  func(t *testing.T, m *Model, st *core.Local)
		script string
		want   observed
		shows  []string
	}{
		{
			name: "c on a row lists the categories", file: exportCSV, script: "i enter c",
			want:  observed{Step: "category", Categories: []string{groceries, unc, incUnc}, NewUncat: 2},
			shows: []string{"Categorize · REWE Markt -23.47", "enter choose"},
		},
		{
			name: "esc leaves it as it was", file: exportCSV, script: "i enter down c esc",
			want: observed{Step: "review", Categories: []string{groceries, unc, incUnc}, NewUncat: 2},
		},
		{
			name: "a new category is typed in full", file: exportCSV, script: "i enter down c raw:Expenses:Gifts enter",
			want:  observed{Step: "review", Chosen: map[int]string{3: "Expenses:Gifts"}, Categories: []string{groceries, "Expenses:Gifts", incUnc}, NewUncat: 1},
			shows: []string{"✎ Gifts", "1 uncategorized", "Your choice"},
		},
		{
			name: "it beats the rule that matched", file: exportCSV, script: "i enter c raw:Expenses:Dining enter",
			want: observed{Step: "review", Chosen: map[int]string{2: "Expenses:Dining"}, Categories: []string{"Expenses:Dining", unc, incUnc}, NewUncat: 2},
		},
		{
			name: "choosing Uncategorized gives a row back to later", file: exportCSV, script: "i enter c" + strings.Repeat(" down", 20) + " enter",
			want: observed{Step: "review", Chosen: map[int]string{2: unc}, Categories: []string{unc, unc, incUnc}, NewUncat: 3},
		},
		{
			name: "r goes back to what the rules chose", file: exportCSV, script: "i enter c raw:Expenses:Dining enter r",
			want: observed{Step: "review", Categories: []string{groceries, unc, incUnc}, NewUncat: 2},
		},
		{
			name: "r on a row nobody changed does nothing", file: exportCSV, script: "i enter down r",
			want: observed{Step: "review", Categories: []string{groceries, unc, incUnc}, NewUncat: 2},
		},
		{
			name: "a list with nothing to choose stays open", file: exportCSV, script: "i enter c raw:zzzz enter",
			want:  observed{Step: "category", Categories: []string{groceries, unc, incUnc}, NewUncat: 2},
			shows: []string{"No match"},
		},
		{
			name: "enter then imports with the choice", file: exportCSV, script: "i enter down c raw:Expenses:Gifts enter enter",
			want: observed{
				Added: []string{
					"2026-10-02 REWE Markt | Assets:Bank -23.47 | Expenses:Groceries 23.47",
					"2026-10-03 Mystery Shop | Assets:Bank -5.00 | Expenses:Gifts 5.00",
					"2026-10-04 Arbeitgeber | Assets:Bank 1000.00 | Income:Uncategorized -1000.00",
				},
			},
		},
		{
			name: "a row that was imported before cannot be changed", file: exportCSV, script: "i enter enter i enter c",
			want: observed{
				Step: "review", Categories: []string{groceries, unc, incUnc}, NewUncat: 0,
				Err:   "Already imported: its category is set.",
				Added: []string{},
			},
		},
		{
			name: "the choice is dropped when the file is read again", file: exportCSV, script: "i enter down c raw:Expenses:Gifts enter esc enter",
			want: observed{Step: "review", Categories: []string{groceries, unc, incUnc}, NewUncat: 2},
		},
		{
			name: "what was said about a row goes away with the next key", file: exportCSV, script: "i enter enter i enter c down",
			want: observed{Step: "review", Categories: []string{groceries, unc, incUnc}, Added: []string{}},
		},
		{
			name: "a line that could not be read has no category", file: broken, script: "i enter c",
			want: observed{Step: "review", Categories: []string{groceries, unc, incUnc}, NewUncat: 2, Err: "This line could not be read, so it has no category."},
		},
		{
			name: "a row that takes over your entry keeps the entry's category", file: exportCSV,
			setup: func(t *testing.T, m *Model, st *core.Local) {
				book(t, st, "2026-10-03", "Shop", "Assets:Bank", "Expenses:Dining", -500)
				if err := m.reload(); err != nil {
					t.Fatal(err)
				}
			},
			script: "i enter down c",
			want:   observed{Step: "review", Categories: []string{groceries, "Expenses:Dining", incUnc}, NewUncat: 1, Err: "This row takes over your entry: the entry keeps its category."},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			writeExport(t, m, "umsaetze.csv", tc.file)
			if tc.setup != nil {
				tc.setup(t, m, st)
			}
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			steps := map[importStep]string{stepChoose: "choose", stepReview: "review", stepCategory: "category"}
			var got observed
			if f := m.imp; f != nil {
				got = observed{Step: steps[f.step], Chosen: f.chosen, NewUncat: f.newUncat, Err: f.err}
				for _, r := range f.res.Rows {
					got.Categories = append(got.Categories, r.Category)
				}
			} else { // it was imported, and the review has gone: what it wrote is what is left to see
				got.Added, _ = delta(before, storedLines(t, st))
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

// Which profile reads the file: found from its columns, or chosen.
func Test_Update_import_profile(t *testing.T) {
	type observed struct {
		Choice   string // what the profile line is set to: "auto" or a name
		Detected string
		Used     string // the profile the review was made with
		Step     string
		Err      string
	}
	ing := importer.Profile{
		Name: "Ing", Delimiter: ",", Columns: importer.Columns{Date: []string{"Date"}, Payee: []string{"Name"}, Amount: []string{"Amount"}},
	}
	withIng := func(m *Model) {
		m.loadConfig = func() (*config.Config, error) { return &config.Config{Profiles: []importer.Profile{ing}}, nil }
	}
	const ingFile = "Date,Name,Amount\n2026-10-02,REWE,-23.47\n"
	// A second file, older than the first, for the highlight to move to.
	older := func(t *testing.T, m *Model, name, content string) {
		t.Helper()
		path := filepath.Join(m.downloads, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatal(err)
		}
	}
	tcs := []struct {
		name   string
		file   string
		setup  func(t *testing.T, m *Model)
		script string
		want   observed
		shows  []string
	}{
		{
			name: "auto is the default, and finds Sparkasse", file: exportCSV, script: "i",
			want:  observed{Choice: "auto", Detected: "sparkasse", Step: "choose"},
			shows: []string{"auto: sparkasse"},
		},
		{
			name: "the review says which profile read the file", file: exportCSV, script: "i enter",
			want:  observed{Choice: "auto", Detected: "sparkasse", Used: "sparkasse", Step: "review"},
			shows: []string{"profile sparkasse", "3 new"},
		},
		{
			name: "a profile of yours fits a file of another bank", file: ingFile, setup: func(t *testing.T, m *Model) { withIng(m) }, script: "i enter",
			want:  observed{Choice: "auto", Detected: "ing", Used: "ing", Step: "review"},
			shows: []string{"profile ing", "1 new"},
		},
		{
			name: "a file no profile fits says so, and nothing is read", file: ingFile, script: "i enter",
			want:  observed{Choice: "auto", Step: "choose", Err: "No profile fits this file. Choose one with ← →."},
			shows: []string{"auto: none fits", "No profile fits this file"},
		},
		{
			name: "choosing one by hand is not second-guessed", file: exportCSV, setup: func(t *testing.T, m *Model) { withIng(m) }, script: "i tab tab right enter",
			want: observed{Choice: "ing", Detected: "sparkasse", Step: "choose", Err: `profile "Ing" does not match this file: need a date column (Date) and an amount column (Amount), but the header is: Buchungstag;Verwendungszweck;Beguenstigter/Zahlungspflichtiger;Betrag;Waehrung;Info`},
		},
		{
			name: "← from auto goes to the last profile", file: exportCSV, setup: func(t *testing.T, m *Model) { withIng(m) }, script: "i tab tab left",
			want:  observed{Choice: "sparkasse", Detected: "sparkasse", Step: "choose"},
			shows: []string{"sparkasse"},
		},
		{
			name: "a profile chosen is the one offered next time", file: exportCSV, script: "i tab tab left enter esc esc i",
			want:  observed{Choice: "sparkasse", Detected: "sparkasse", Step: "choose"},
			shows: []string{"sparkasse"},
		},
		{
			name: "an OFX statement in the folder is listed, and auto finds it as one", file: exportCSV,
			setup:  func(t *testing.T, m *Model) { onlyFile(t, m, "statement.ofx", statementOFX) },
			script: "i enter",
			want:   observed{Choice: "auto", Detected: "ofx", Used: "ofx", Step: "review"},
			shows:  []string{"profile ofx", "2 new"},
		},
		{
			name: "so is a QIF file", file: exportCSV,
			setup:  func(t *testing.T, m *Model) { onlyFile(t, m, "konto.QIF", "!Type:Bank\nD10/4'26\nT-23.47\nPREWE\n^\n") },
			script: "i enter",
			want:   observed{Choice: "auto", Detected: "qif", Used: "qif", Step: "review"},
			shows:  []string{"profile qif", "1 new"},
		},
		{
			name: "auto, once used, is still what is offered next time", file: exportCSV, script: "i enter esc esc i",
			want: observed{Choice: "auto", Detected: "sparkasse", Step: "choose"},
		},
		{
			name: "highlighting another file looks again", file: exportCSV,
			setup:  func(t *testing.T, m *Model) { withIng(m); older(t, m, "ing.csv", ingFile) },
			script: "i down",
			want:   observed{Choice: "auto", Detected: "ing", Step: "choose"},
			shows:  []string{"auto: ing"},
		},
		{
			name: "so does typing letters of a name", file: exportCSV,
			setup:  func(t *testing.T, m *Model) { withIng(m); older(t, m, "ing.csv", ingFile) },
			script: "i raw:ing.c",
			want:   observed{Choice: "auto", Detected: "ing", Step: "choose"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			writeExport(t, m, "umsaetze.csv", tc.file)
			if tc.setup != nil {
				tc.setup(t, m)
			}

			// when
			play(m, tc.script)

			// then
			steps := map[importStep]string{stepChoose: "choose", stepReview: "review", stepCategory: "category"}
			f := m.imp
			got := observed{Choice: "auto", Detected: f.detected, Used: f.used, Step: steps[f.step], Err: f.err}
			if f.profile > 0 {
				got.Choice = f.profiles[f.profile-1]
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), tc.shows, nil)
		})
	}
}

// A file that is replaced between choosing it and reviewing it is read as it is
// then, not as it was when the profile was found.
func Test_Update_import_fileChanged(t *testing.T) {
	tcs := []struct {
		name    string
		changed string
		want    string // the profile the review used
	}{
		{name: "another bank's export", changed: "Date,Name,Amount\n2026-10-02,REWE,-23.47\n", want: "ing"},
		{name: "the same bank's", changed: exportCSV, want: "sparkasse"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			path := writeExport(t, m, "umsaetze.csv", exportCSV)
			m.loadConfig = func() (*config.Config, error) {
				ing := importer.Profile{Name: "ing", Delimiter: ",", Columns: importer.Columns{Date: []string{"Date"}, Payee: []string{"Name"}, Amount: []string{"Amount"}}}
				return &config.Config{Profiles: []importer.Profile{ing}}, nil
			}
			press(m, "i")
			if err := os.WriteFile(path, []byte(tc.changed), 0o600); err != nil {
				t.Fatal(err)
			}

			// when
			press(m, "enter")

			// then
			if diff := cmp.Diff(tc.want, m.imp.used); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// A path typed or dropped into the file field, the way a terminal writes it.
func Test_Update_import_typedPath(t *testing.T) {
	type observed struct {
		Step   string
		ToAdd  int // rows the review would import
		Err    string
		Stored []string // lines written; reviewing writes nothing
	}
	escaped := func(p string) string { return strings.ReplaceAll(p, " ", `\ `) }
	tcs := []struct {
		name string
		typ  func(path string) string
		want observed
	}{
		{name: "spaces escaped, as when a file is dropped", typ: escaped, want: observed{Step: "review", ToAdd: 3}},
		{name: "in double quotes", typ: func(p string) string { return `"` + p + `"` }, want: observed{Step: "review", ToAdd: 3}},
		{name: "in single quotes", typ: func(p string) string { return `'` + p + `'` }, want: observed{Step: "review", ToAdd: 3}},
		{name: "with whitespace around it", typ: func(p string) string { return "  " + escaped(p) + "  " }, want: observed{Step: "review", ToAdd: 3}},
		{
			name: "a file that is not there", typ: func(p string) string { return escaped(p) + ".missing" },
			want: observed{Step: "choose", Err: "no such file: {path}.missing"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			path := writeExport(t, m, "my export.csv", exportCSV)
			m.downloads = t.TempDir() // nothing to pick from: the path has to be typed
			before := storedLines(t, st)
			typed := tc.typ(path)

			// when
			press(m, "i")
			typeText(m, typed)
			press(m, "enter")

			// then
			steps := map[importStep]string{stepChoose: "choose", stepReview: "review"}
			got := observed{Step: steps[m.imp.step], Err: m.imp.err}
			if m.imp.step == stepReview {
				got.ToAdd = m.imp.res.Inserted
			}
			got.Stored, _ = delta(before, storedLines(t, st))
			want := tc.want
			want.Err = strings.ReplaceAll(want.Err, "{path}", path)
			if diff := cmp.Diff(want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Where the list of files comes from, and which account and months the import
// lands in.
func Test_Update_import_destination(t *testing.T) {
	type observed struct {
		Mode   string
		Scope  string
		Period string
		Added  int
	}
	oldRow := "Buchungstag;Beguenstigter/Zahlungspflichtiger;Betrag;Waehrung;Info\n15.09.26;Bakery;-3,00;EUR;Umsatz gebucht\n"
	tcs := []struct {
		name  string
		setup func(t *testing.T, m *Model) (script string) // returns what to press
		want  observed
		shows []string
	}{
		{
			name: "the account selected in the Accounts panel is the default",
			setup: func(t *testing.T, m *Model) string {
				writeExport(t, m, "older.csv", oldRow)
				return "2 down down i"
			},
			want:  observed{Mode: "import", Scope: "Assets:Savings", Period: "2026-10"},
			shows: []string{"Assets:Savings"},
		},
		{
			name: "a row outside the month widens the period to all time",
			setup: func(t *testing.T, m *Model) string {
				writeExport(t, m, "older.csv", oldRow)
				return "2 down down i enter enter"
			},
			want: observed{Mode: "list", Scope: "Assets:Savings", Period: "", Added: 1},
		},
		{
			name: "the folder named in config.toml is listed, with ~ expanded",
			setup: func(t *testing.T, m *Model) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				if err := os.MkdirAll(filepath.Join(home, "bank"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, "bank", "konto.csv"), []byte(exportCSV), 0o600); err != nil {
					t.Fatal(err)
				}
				m.downloads = t.TempDir() // empty: only the configured folder has a file
				m.loadConfig = func() (*config.Config, error) { return &config.Config{ImportDir: "~/bank"}, nil }
				return "i"
			},
			want:  observed{Mode: "import", Period: "2026-10"},
			shows: []string{"konto.csv", "~/bank"},
		},
		{
			name: "and its file can be imported",
			setup: func(t *testing.T, m *Model) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				if err := os.MkdirAll(filepath.Join(home, "bank"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, "bank", "konto.csv"), []byte(exportCSV), 0o600); err != nil {
					t.Fatal(err)
				}
				m.downloads = t.TempDir()
				m.loadConfig = func() (*config.Config, error) { return &config.Config{ImportDir: "~/bank"}, nil }
				return "i enter enter"
			},
			want: observed{Mode: "list", Scope: "Assets:Bank", Period: "2026-10", Added: 3},
		},
		{
			name: "a config that cannot be read refuses to start an import",
			setup: func(t *testing.T, m *Model) string {
				m.loadConfig = func() (*config.Config, error) { return nil, errors.New("config.toml: unknown key(s): [rule.typo]") }
				return "i"
			},
			want: observed{Mode: "list", Period: "2026-10"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			script := tc.setup(t, m)
			before := storedLines(t, st)

			// when
			play(m, script)

			// then
			added, _ := delta(before, storedLines(t, st))
			got := observed{Mode: modeNames[m.mode], Scope: m.scopeAccount(), Period: m.period, Added: len(added)}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			view := ""
			if m.mode == modeImport {
				view = m.View()
			}
			checkView(t, view, tc.shows, nil)
		})
	}

	t.Run("a config that cannot be read says why", func(t *testing.T) {
		// given
		m, _ := newModel(t)
		m.loadConfig = func() (*config.Config, error) { return nil, errors.New("config.toml: unknown key(s): [rule.typo]") }

		// when
		play(m, "i")

		// then
		want := Flow{Mode: "list", Status: "config.toml: unknown key(s): [rule.typo]", StatusIsError: true}
		got := Flow{Mode: modeNames[m.mode], Status: m.status, StatusIsError: m.statusIsError}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
}

// Typing the path of a folder lists its CSV files.
func Test_Update_import_browsingAFolder(t *testing.T) {
	type observed struct {
		Mode  string
		Err   string
		Rows  int // rows the review would import; 0 outside the review
		Added int
	}
	tcs := []struct {
		name       string
		scriptFor  func(folders map[string]string) string
		want       observed
		shows      []string
		hides      []string
		wantAdded  int
		showsAfter func(map[string]string) []string
	}{
		{
			name:      "an empty folder says so, and enter does not try to read the folder",
			scriptFor: func(f map[string]string) string { return "i raw:" + f["empty"] + " enter" },
			want:      observed{Mode: "import", Err: "choose an export (CSV, OFX or QIF): paste its path, or pick one from the list"},
			shows:     []string{"No CSV, OFX or QIF files in"},
		},
		{
			name:      "a folder lists only its CSV files",
			scriptFor: func(f map[string]string) string { return "i raw:" + f["other"] + "/" },
			want:      observed{Mode: "import"},
			shows:     []string{"alt.csv"},
			hides:     []string{"notes.txt"},
		},
		{
			name:      "enter takes the highlighted file, not the folder",
			scriptFor: func(f map[string]string) string { return "i raw:" + f["other"] + "/ enter" },
			want:      observed{Mode: "import", Rows: 3},
		},
		{
			name:      "and a second enter imports it",
			scriptFor: func(f map[string]string) string { return "i raw:" + f["other"] + "/ enter enter" },
			want:      observed{Mode: "list", Added: 3},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			folders := map[string]string{"other": t.TempDir(), "empty": t.TempDir()}
			for name, content := range map[string]string{"alt.csv": exportCSV, "notes.txt": "not a bank export"} {
				if err := os.WriteFile(filepath.Join(folders["other"], name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			m.downloads = t.TempDir()
			before := storedLines(t, st)

			// when
			play(m, tc.scriptFor(folders))

			// then
			added, _ := delta(before, storedLines(t, st))
			got := observed{Mode: modeNames[m.mode], Added: len(added)}
			if m.imp != nil {
				got.Err = m.imp.err
				if m.imp.step == stepReview {
					got.Rows = m.imp.res.Inserted
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			view := ""
			if m.mode == modeImport {
				view = m.View()
			}
			checkView(t, view, tc.shows, tc.hides)
		})
	}
}

// pairedModel has Assets:Bank and Assets:Savings, the bank's export already
// imported through the interface, and the savings export ready to be imported.
func pairedModel(t *testing.T) (*Model, *core.Local) {
	t.Helper()
	m, st := noAccountsModel(t)
	openAccounts(t, st, "Assets:Bank EUR", "Assets:Savings EUR")
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	writeExport(t, m, "bank.csv", exportHeader+
		"04.10.26;;Umbuchung Sparen;-500,00;EUR;Umsatz gebucht\n"+
		"05.10.26;;REWE;-20,00;EUR;Umsatz gebucht\n")
	play(m, "i enter enter")
	if txCount(t, st) != 2 || m.scopeAccount() != "Assets:Bank" {
		t.Fatalf("setup: %d transactions, scope %q, status %q", txCount(t, st), m.scopeAccount(), m.status)
	}
	writeExport(t, m, "savings.csv", exportHeader+"05.10.26;;Umbuchung;500,00;EUR;Umsatz gebucht\n")
	press(m, "2", "down") // Savings
	return m, st
}

func Test_Update_import_pairingTransfers(t *testing.T) {
	type observed struct {
		Flow
		OnlyUncat bool
	}
	const (
		outBefore = "2026-10-04 Umbuchung Sparen | Assets:Bank -500.00 | Expenses:Uncategorized 500.00"
		outAfter  = "2026-10-04 Umbuchung Sparen | Assets:Bank -500.00 | Equity:Transfers 500.00"
		inAfter   = "2026-10-05 Umbuchung | Assets:Savings 500.00 | Equity:Transfers -500.00"
	)
	tcs := []struct {
		name   string
		script string
		want   observed
		shows  []string
		hides  []string
	}{
		{
			name: "the review shows the pairing and counts nothing as uncategorized", script: "i enter",
			want:  observed{Flow: Flow{Mode: "import"}, OnlyUncat: true}, // left on by the first import
			shows: []string{"Savings", "1 transfer paired", "⇄ Bank"},
			hides: []string{"uncategorized"},
		},
		{
			name: "importing pairs it; nothing is left to categorize so the list shows everything", script: "i enter enter",
			want: observed{Flow: Flow{
				Mode: "list", Status: "Imported 1 into Savings · 1 paired as transfers · z undoes",
				Added: []string{outAfter, inAfter}, Removed: []string{outBefore},
			}},
		},
		{
			name: "z takes the import back, pairing included", script: "i enter enter z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid the import of 1 transaction"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := pairedModel(t)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before), OnlyUncat: m.onlyUncat}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			view := ""
			if m.mode == modeImport {
				view = m.View()
			}
			checkView(t, view, tc.shows, tc.hides)
			checkConsistent(t, st)
		})
	}

	t.Run("the list marks both halves and the detail pane names the other one", func(t *testing.T) {
		// given
		m, st := pairedModel(t)
		play(m, "i enter enter")
		m.accts.cursor = 0
		m.applyFilter(false)
		out := txByPayee(t, st, "Umbuchung Sparen")

		// when
		selectPayee(t, m, "Umbuchung")

		// then
		checkView(t, m.View(), []string{"⇄ Umbuchung", "Transfer with #" + itoa(out.ID), "Assets:Bank"}, nil)
	})

	t.Run("renaming a paired half keeps the pair", func(t *testing.T) {
		// given
		m, st := pairedModel(t)
		play(m, "i enter enter")
		m.accts.cursor = 0
		m.applyFilter(false)
		selectPayee(t, m, "Umbuchung")

		// when
		play(m, "r text:_Sparplan ctrl+s")

		// then
		half := txByPayee(t, st, "Umbuchung Sparplan")
		got := struct {
			Mode     string
			Category string
			Paired   bool
		}{modeNames[m.mode], categoryOf(t, st, "Umbuchung Sparplan"), half.TransferWith != 0}
		want := struct {
			Mode     string
			Category string
			Paired   bool
		}{"list", "Equity:Transfers", true}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
		checkConsistent(t, st)
	})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

// An export row that is the bank's record of something you typed in by hand takes
// that entry over instead of being added.
func Test_Update_import_adoptingEntries(t *testing.T) {
	type observed struct {
		Flow
		Hashed []bool // does each of the 5 transactions stand for a bank row now
	}
	const (
		reweEntry = "2026-10-02 REWE | Assets:Bank -20.00 | Expenses:Uncategorized 20.00"
		newShop   = "2026-10-05 Neuer Laden | Assets:Bank -7.00 | Expenses:Uncategorized 7.00"
	)
	both := exportHeader +
		"03.10.26;Einkauf;REWE Markt;-20,00;EUR;Umsatz gebucht\n" + // the bank's record of the entry
		"05.10.26;;Neuer Laden;-7,00;EUR;Umsatz gebucht\n"
	onlyMatch := exportHeader + "03.10.26;;REWE Markt;-20,00;EUR;Umsatz gebucht\n"
	tcs := []struct {
		name   string
		file   string
		script string
		want   observed
		shows  []string
	}{
		{
			name: "the review shows which entry a row would take over", file: both, script: "i enter",
			want:  observed{Flow: Flow{Mode: "import"}, Hashed: []bool{false, false, false, false, false}},
			shows: []string{"1 new", "1 matched your entries", "↔ #3 REWE"},
		},
		{
			name: "importing adds one row and the entry remembers its bank row", file: both, script: "i enter enter",
			want: observed{
				Flow:   Flow{Mode: "list", Status: "Imported 1 into Bank · 1 matched your entries · 2 need a category (u shows all) · z undoes", Added: []string{newShop}},
				Hashed: []bool{false, false, true, false, false, true},
			},
		},
		{
			name: "z lets go of the entry and removes the new row", file: both, script: "i enter enter z",
			want: observed{Flow: Flow{Mode: "list", Status: "Undid the import of 2 transactions"}, Hashed: []bool{false, false, false, false, false}},
		},
		{
			name: "after the undo the same file takes the entry over again", file: both, script: "i enter enter z i enter",
			want:  observed{Flow: Flow{Mode: "import"}, Hashed: []bool{false, false, false, false, false}},
			shows: []string{"1 matched your entries"},
		},
		{
			name: "an import that only matches is still an import", file: onlyMatch, script: "i enter enter",
			want: observed{Flow: Flow{Mode: "list", Status: "Matched 1 of your entries on Bank with the bank's rows · 1 need a category (u shows all) · z undoes"}, Hashed: []bool{false, false, true, false, false}},
		},
		{
			name: "once matched, the file is a plain duplicate", file: onlyMatch, script: "i enter enter i enter",
			want:  observed{Flow: Flow{Mode: "import"}, Hashed: []bool{false, false, true, false, false}},
			shows: []string{"1 already imported"},
		},
		{
			name: "and importing it again says there is nothing new", file: onlyMatch, script: "i enter enter i enter enter",
			want: observed{Flow: Flow{Mode: "list", Status: "Nothing new: the 1 row was already imported"}, Hashed: []bool{false, false, true, false, false}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t) // REWE 20.00 on 2 October is a hand-entered, uncategorized transaction
			writeExport(t, m, "umsaetze.csv", tc.file)
			before := storedLines(t, st)

			// when
			play(m, tc.script)

			// then
			got := observed{Flow: observe(t, m, st, before)}
			txs, err := st.ListTransactions(t.Context(), core.TxFilter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, tx := range txs {
				got.Hashed = append(got.Hashed, tx.ImportHash != "")
			}
			if diff := cmp.Diff(tc.want, got, equalEmpty); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
			view := ""
			if m.mode == modeImport {
				view = m.View()
			}
			checkView(t, view, tc.shows, nil)
		})
	}
}
