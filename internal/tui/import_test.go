package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
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
			want:  observed{Flow: Flow{Mode: "import", Err: "1 row(s) have problems and would be skipped; press y to import the rest"}},
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
			want: observed{Flow: Flow{Mode: "import", Err: "choose a CSV file: paste its path, or pick one from the list"}},
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
			want:      observed{Mode: "import", Err: "choose a CSV file: paste its path, or pick one from the list"},
			shows:     []string{"No CSV files in"},
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
			want: observed{Flow: Flow{Mode: "list", Status: "Undid the import of 1 transactions"}},
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
		play(m, "e enter text:_Sparplan ctrl+s")

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
			want: observed{Flow: Flow{Mode: "list", Status: "Nothing new: all 1 rows were already imported"}, Hashed: []bool{false, false, true, false, false}},
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
