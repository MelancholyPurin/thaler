package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the command tree with args against a temporary database.
func run(t *testing.T, db string, args ...string) (string, error) {
	t.Helper()
	a := &app{}
	t.Cleanup(a.close)
	root := newRoot(a)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--db", db, "--config", filepath.Join(t.TempDir(), "none.toml")}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestCLIUnderCliCommand(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	if _, err := run(t, db, "cli", "account", "add", "Assets:Bank", "--currency", "EUR"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, db, "cli", "account", "list")
	if err != nil || !strings.Contains(out, "Assets:Bank") {
		t.Errorf("account list = %q, %v", out, err)
	}
}

func TestOldTopLevelCommandsAreGone(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	if _, err := run(t, db, "balance"); err == nil {
		t.Error("`thaler balance` must not exist any more; it is `thaler cli balance`")
	}
	if _, err := run(t, db, "tui"); err == nil {
		t.Error("there is no `thaler tui` subcommand; plain `thaler` opens the interface")
	}
}

func TestPlainThalerNeedsATerminal(t *testing.T) {
	// Under `go test` stdin/stdout are not terminals, so the UI refuses to start
	// instead of printing escape codes into a pipe.
	_, err := run(t, filepath.Join(t.TempDir(), "t.db"))
	if err == nil || !strings.Contains(err.Error(), "thaler cli") {
		t.Errorf("err = %v, want a hint to use thaler cli", err)
	}
}

// Banks book zero-amount rows (card verification charges); importing a file
// that contains one must not fail, and re-importing must still dedupe it.
func TestImportZeroAmountRow(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	file := filepath.Join(t.TempDir(), "export.csv")
	csv := `"Buchungstag";"Buchungstext";"Verwendungszweck";"Beguenstigter/Zahlungspflichtiger";"Betrag";"Waehrung";"Info"` + "\n" +
		`"01.10.26";"KARTENZAHLUNG";"Verifizierung";"Some Shop";"0,00";"EUR";"Umsatz gebucht"` + "\n" +
		`"02.10.26";"KARTENZAHLUNG";"Einkauf";"Some Shop";"-3,20";"EUR";"Umsatz gebucht"` + "\n"
	if err := os.WriteFile(file, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "config.toml") // import needs a readable config; empty means no rules
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, db, "cli", "account", "add", "Assets:Bank", "-c", "EUR"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, db, "cli", "import", file, "-a", "Assets:Bank", "--config", cfg)
	if err != nil || !strings.Contains(out, "Imported 2 new") {
		t.Fatalf("import = %q, %v", out, err)
	}
	out, err = run(t, db, "cli", "import", file, "-a", "Assets:Bank", "--config", cfg)
	if err != nil || !strings.Contains(out, "Imported 0 new") || !strings.Contains(out, "2 already present") {
		t.Errorf("re-import = %q, %v", out, err)
	}
	if out, err = run(t, db, "cli", "list", "-n", "0"); err != nil || !strings.Contains(out, "Verifizierung") {
		t.Errorf("list should show the zero-amount row, got %q, %v", out, err)
	}
}
