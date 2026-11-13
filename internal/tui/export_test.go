package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_writeNew(t *testing.T) {
	tcs := []struct {
		name    string
		prior   *string // what the file holds already, if it is there
		dir     string  // a folder inside the temporary one, left missing
		want    string
		wantErr string
	}{
		{name: "a new file", want: "a,b\n"},
		{name: "a file that is there is kept", prior: new("keep"), want: "keep", wantErr: "FILE is already there: give another name"},
		{name: "a folder that is not there", dir: "missing", want: "", wantErr: "no such file or directory"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), tc.dir, "out.csv")
			if tc.prior != nil {
				if err := os.WriteFile(path, []byte(*tc.prior), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			err := writeNew(path, []byte("a,b\n"))

			// then
			got := ""
			if err != nil {
				got = i18n.New(language.English).Error(err)
			}
			got = strings.ReplaceAll(got, path, "FILE")
			if tc.wantErr == "" {
				if diff := cmp.Diff(tc.wantErr, got); diff != "" {
					t.Errorf("error mismatch (-want +got):\n%s", diff)
				}
			} else if !strings.Contains(got, tc.wantErr) {
				t.Errorf("error %q lacks %q", got, tc.wantErr)
			}
			body, readErr := os.ReadFile(path)
			if tc.dir == "" {
				if readErr != nil {
					t.Fatal(readErr)
				}
				if diff := cmp.Diff(tc.want, string(body)); diff != "" {
					t.Errorf("file mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func Test_writeNew_private(t *testing.T) {
	tcs := []struct {
		name string
		want os.FileMode
	}{
		{name: "readable by you alone", want: 0o600},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			path := filepath.Join(t.TempDir(), "out.csv")

			// when
			if err := writeNew(path, []byte("x")); err != nil {
				t.Fatal(err)
			}

			// then
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, info.Mode().Perm()); diff != "" {
				t.Errorf("mode mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_exportScreen(t *testing.T) {
	tcs := []struct {
		name  string
		keys  string
		shows []string
		hides []string
	}{
		{
			name:  "e asks where to save the report on show",
			keys:  "R e",
			shows: []string{"Save as CSV", "Report", "Spending by category · Nov 2025 – Oct 2026", "Save to", "thaler-categories-2025-11-2026-10.csv", "A plain CSV for a spreadsheet: amounts in EUR", "enter save · esc cancel"},
		},
		{
			name:  "the income report",
			keys:  "R tab e",
			shows: []string{"Income against spending · Nov 2025 – Oct 2026", "thaler-cashflow-2025-11-2026-10.csv"},
		},
		{
			name:  "the payees",
			keys:  "R tab tab e",
			shows: []string{"Payees · Nov 2025 – Oct 2026", "thaler-payees-2025-11-2026-10.csv"},
		},
		{
			name:  "the net worth",
			keys:  "R tab tab tab e",
			shows: []string{"Net worth · Nov 2025 – Oct 2026", "thaler-networth-2025-11-2026-10.csv"},
		},
		{
			name:  "the months of a chosen range",
			keys:  "R r text:2026-09 enter text:2026-10 enter e",
			shows: []string{"Spending by category · Sep 2026 – Oct 2026", "thaler-categories-2026-09-2026-10.csv"},
		},
		{
			name:  "esc closes it",
			keys:  "R e esc",
			shows: []string{"Spending by category"},
			hides: []string{"Save as CSV"},
		},
		{
			name:  "nothing to say where",
			keys:  "R e ctrl+u enter",
			shows: []string{"Save as CSV", "say where to save the report"},
		},
		{
			name:  "typing takes the message away",
			keys:  "R e ctrl+u enter raw:x",
			shows: []string{"Save as CSV"},
			hides: []string{"say where to save the report"},
		},
		{
			name:  "the hint in the bottom bar",
			keys:  "R",
			shows: []string{"e CSV"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}

// The reports of newModel's ledger for September and October 2026, as files.
func Test_exportScreen_saves(t *testing.T) {
	tcs := []struct {
		name  string
		setup func(t *testing.T, st *core.Local)
		keys  string // before the path is typed
		file  string // the name of the file inside the folder
		want  string
		shows []string
	}{
		{
			name: "spending by category",
			keys: "R r text:2026-09 enter text:2026-10 enter e",
			file: "spending.csv",
			want: "category,currency,2026-09,2026-10,total\n" +
				"Expenses:Groceries,EUR,30.00,10.00,40.00\n" +
				"Expenses:Uncategorized,EUR,0.00,32.00,32.00\n" +
				"Total,EUR,30.00,42.00,72.00\n",
			shows: []string{"Saved Spending by category as CSV: "},
		},
		{
			name: "income against spending",
			keys: "R tab r text:2026-09 enter text:2026-10 enter e",
			file: "cashflow.csv",
			want: "month,currency,income,spending,net,saved_percent\n" +
				"2026-09,EUR,0.00,30.00,-30.00,\n" +
				"2026-10,EUR,0.00,42.00,-42.00,\n" +
				"Total,EUR,0.00,72.00,-72.00,\n",
			shows: []string{"Saved Income against spending as CSV: "},
		},
		{
			name: "the payees",
			keys: "R tab tab r text:2026-09 enter text:2026-10 enter e",
			file: "payees.csv",
			want: "payee,currency,amount,share_percent,transactions\n" +
				"Edeka,EUR,30.00,41.67,1\n" +
				"REWE,EUR,30.00,41.67,2\n" +
				"Kino,EUR,12.00,16.67,1\n",
			shows: []string{"Saved Payees as CSV: "},
		},
		{
			name: "net worth at the end of each month",
			keys: "R tab tab tab r text:2026-08 enter text:2026-10 enter e",
			file: "networth.csv",
			want: "month,currency,net_worth\n" +
				"2026-08,EUR,0.00\n" +
				"2026-09,EUR,-30.00\n" +
				"2026-10,EUR,-72.00\n",
			shows: []string{"Saved Net worth as CSV: "},
		},
		{
			name: "the months on show, when no range was chosen",
			keys: "R tab e",
			file: "months.csv",
			want: "month,currency,income,spending,net,saved_percent\n" +
				"2025-11,EUR,0.00,0.00,0.00,\n2025-12,EUR,0.00,0.00,0.00,\n2026-01,EUR,0.00,0.00,0.00,\n2026-02,EUR,0.00,0.00,0.00,\n" +
				"2026-03,EUR,0.00,0.00,0.00,\n2026-04,EUR,0.00,0.00,0.00,\n2026-05,EUR,0.00,0.00,0.00,\n2026-06,EUR,0.00,0.00,0.00,\n" +
				"2026-07,EUR,0.00,0.00,0.00,\n2026-08,EUR,0.00,0.00,0.00,\n" +
				"2026-09,EUR,0.00,30.00,-30.00,\n2026-10,EUR,0.00,42.00,-42.00,\nTotal,EUR,0.00,72.00,-72.00,\n",
		},
		{
			name:  "every payee, not the first hundred the screen lists",
			setup: manyPayees(120),
			keys:  "R tab tab e",
			file:  "payees.csv",
			want: func() string {
				var b strings.Builder
				b.WriteString("payee,currency,amount,share_percent,transactions\nEdeka,EUR,30.00,15.63,1\nREWE,EUR,30.00,15.63,2\nKino,EUR,12.00,6.25,1\n")
				for i := range 120 {
					b.WriteString("P" + pad3(i) + ",EUR,1.00,0.52,1\n")
				}
				return b.String()
			}(),
		},
		{
			name: "a currency without a rate is left out, and said",
			setup: func(t *testing.T, st *core.Local) {
				openAccounts(t, st, "Assets:Dollars USD")
				if _, err := st.AddTransaction(t.Context(), core.NewTransaction{Date: startOn("2026-10-12"), Payee: "Diner", Postings: []ledger.Posting{
					{Account: "Assets:Dollars", Amount: ledger.Money{Minor: -900, Currency: "USD"}}, {Account: "Expenses:Fun", Amount: ledger.Money{Minor: 900, Currency: "USD"}}}}); err != nil {
					t.Fatal(err)
				}
			},
			keys: "R tab tab r text:2026-10 enter text:2026-10 enter e",
			file: "payees.csv",
			want: "payee,currency,amount,share_percent,transactions\n" +
				"REWE,EUR,30.00,71.43,2\nKino,EUR,12.00,28.57,1\n",
			shows: []string{"no exchange rate USD → EUR"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, st := newModel(t)
			if tc.setup != nil {
				tc.setup(t, st)
			}
			if err := m.reload(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), tc.file)

			// when
			play(m, tc.keys+" ctrl+u raw:"+path+" enter")

			// then
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v\n%s", err, m.View())
			}
			if diff := cmp.Diff(tc.want, string(got)); diff != "" {
				t.Errorf("file mismatch (-want +got):\n%s", diff)
			}
			checkView(t, m.View(), append([]string{"Reports"}, tc.shows...), []string{"Save as CSV"})
		})
	}
}

// The status that says where the file went fits the line, in every language, whatever
// the report, and keeps the end of the path.
func Test_exportScreen_status(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		keys  string
		width int
		shows []string
	}{
		{name: "English, 80 wide", tag: language.English, keys: "R tab e", width: 80, shows: []string{"Saved Income against spending as CSV: "}},
		{name: "Spanish, 80 wide", tag: language.Spanish, keys: "R tab e", width: 80, shows: []string{"Ingresos frente a gastos guardado como CSV: "}},
		{name: "German, 80 wide", tag: language.German, keys: "R tab e", width: 80, shows: []string{"Einnahmen gegen Ausgaben als CSV gespeichert: "}},
		{name: "wider: more of the path", tag: language.English, keys: "R tab e", width: 140, shows: []string{"Saved Income against spending as CSV: ", "/thaler-cashflow-2025-11-2026-10.csv"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-10", "x", "Assets:Bank", "Expenses:Groceries", -4200)
			m, err := newWithClock(t.Context(), st, Options{LoadConfig: testConfig, Loc: i18n.New(tc.tag)}, fixedNow)
			if err != nil {
				t.Fatal(err)
			}
			m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
			path := filepath.Join(t.TempDir(), "thaler-cashflow-2025-11-2026-10.csv")

			// when
			play(m, tc.keys+" ctrl+u raw:"+path+" enter")

			// then
			view := m.View()
			checkView(t, view, tc.shows, []string{"Save as CSV"})
			for _, l := range strings.Split(view, "\n") {
				if strings.Contains(l, "…") && strings.Contains(l, "csv") && !strings.HasSuffix(strings.TrimRight(l, " "), "csv") {
					t.Errorf("the path was cut at the wrong end: %q", l)
				}
				if w := lipgloss.Width(l); w > tc.width {
					t.Errorf("a line is wider than the terminal (%d > %d): %q", w, tc.width, l)
				}
			}
		})
	}
}

func pad3(i int) string {
	return string([]byte{'0' + byte(i/100), '0' + byte(i/10%10), '0' + byte(i%10)})
}

func Test_exportScreen_refuses(t *testing.T) {
	tcs := []struct {
		name  string
		prior string // written to the file first, if not empty
		path  func(dir string) string
		keys  string
		shows []string
		kept  string // what the file holds after
	}{
		{
			name:  "a file that is there is not overwritten",
			prior: "keep me",
			path:  func(dir string) string { return filepath.Join(dir, "out.csv") },
			shows: []string{"Save as CSV", "there: give another name"},
			kept:  "keep me",
		},
		{
			name:  "a folder that is not there",
			path:  func(dir string) string { return filepath.Join(dir, "no", "out.csv") },
			shows: []string{"Save as CSV", "no such file or"},
		},
		{
			name: "a folder gets the usual name inside it",
			path: func(dir string) string { return dir },
			kept: "category,currency,2025-11,",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			m, _ := newModel(t)
			dir := t.TempDir()
			file := filepath.Join(dir, "out.csv")
			if tc.prior != "" {
				if err := os.WriteFile(file, []byte(tc.prior), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			play(m, "R e ctrl+u raw:"+tc.path(dir)+" enter")

			// then
			checkView(t, m.View(), tc.shows, nil)
			if tc.kept != "" {
				name := "out.csv"
				if tc.prior == "" {
					name = "thaler-categories-2025-11-2026-10.csv"
				}
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if tc.prior != "" {
					if diff := cmp.Diff(tc.kept, string(got)); diff != "" {
						t.Errorf("file mismatch (-want +got):\n%s", diff)
					}
				} else if !strings.HasPrefix(string(got), tc.kept) {
					t.Errorf("file starts %q, want %q", got[:min(len(got), 40)], tc.kept)
				}
			}
		})
	}
}

func Test_exportScreen_translated(t *testing.T) {
	tcs := []struct {
		name  string
		tag   language.Tag
		keys  string
		shows []string
		hides []string
	}{
		{name: "Spanish: the popup", tag: language.Spanish, keys: "R e", shows: []string{"Guardar como CSV", "Informe", "Gasto por categoría · nov 2025 – oct 2026", "Un CSV simple para una hoja de cálculo", "enter guardar · esc cancelar"}, hides: []string{"Save as CSV", "Report"}},
		{name: "German: the popup", tag: language.German, keys: "R e", shows: []string{"Als CSV speichern", "Bericht", "Ausgaben nach Kategorie · Nov 2025 – Okt 2026", "Eine einfache CSV-Datei", "enter speichern · esc abbrechen"}, hides: []string{"Save as CSV", "Report"}},
		{name: "Spanish: nothing to say where", tag: language.Spanish, keys: "R e ctrl+u enter", shows: []string{"indica dónde guardar el informe"}, hides: []string{"say where"}},
		{name: "German: nothing to say where", tag: language.German, keys: "R e ctrl+u enter", shows: []string{"gib an, wo der Bericht gespeichert werden soll"}, hides: []string{"say where"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			st := openLedger(t)
			openAccounts(t, st, "Assets:Bank EUR")
			book(t, st, "2026-10-10", "x", "Assets:Bank", "Expenses:Groceries", -4200)
			m := inLanguage(t, st, tc.tag)

			// when
			play(m, tc.keys)

			// then
			checkView(t, m.View(), tc.shows, tc.hides)
		})
	}
}
