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

func TestAccountBalanceCommand(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	for _, args := range [][]string{
		{"cli", "account", "add", "Assets:Bank", "-c", "EUR"},
		{"cli", "account", "add", "Liabilities:Card", "-c", "EUR"},
	} {
		if _, err := run(t, db, args...); err != nil {
			t.Fatal(err)
		}
	}
	out, err := run(t, db, "cli", "account", "balance", "Assets:Bank", "1234.56", "--as-of", "2026-10-22")
	if err != nil || !strings.Contains(out, "opening balance of 1234.56 EUR on 2026-10-22") {
		t.Fatalf("balance = %q, %v", out, err)
	}
	// A credit card is given as the amount owed.
	if out, err = run(t, db, "cli", "account", "balance", "Liabilities:Card", "500", "--as-of", "2026-10-22"); err != nil {
		t.Fatalf("balance = %q, %v", out, err)
	}
	out, err = run(t, db, "cli", "balance")
	if err != nil || !strings.Contains(out, "1234.56") || !strings.Contains(out, "-500.00") {
		t.Errorf("balance report = %q, %v", out, err)
	}
	// Saying the same again books nothing new.
	out, err = run(t, db, "cli", "account", "balance", "Assets:Bank", "1234.56", "--as-of", "2026-10-22")
	if err != nil || !strings.Contains(out, "Booked an opening balance") {
		t.Errorf("repeat = %q, %v", out, err)
	}
	if out, err = run(t, db, "cli", "list", "-n", "0"); err != nil || strings.Count(out, "Opening balance") != 2 {
		t.Errorf("want exactly two opening-balance transactions, got:\n%s (%v)", out, err)
	}
	if _, err = run(t, db, "cli", "account", "balance", "Assets:Nope", "1"); err == nil {
		t.Error("an unknown account must fail")
	}
}

func TestImportPairsTransfersAcrossAccounts(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	header := `"Buchungstag";"Buchungstext";"Verwendungszweck";"Beguenstigter/Zahlungspflichtiger";"Betrag";"Waehrung";"Info"` + "\n"
	bank := filepath.Join(dir, "bank.csv")
	savings := filepath.Join(dir, "savings.csv")
	if err := os.WriteFile(bank, []byte(header+`"04.10.26";"UMBUCHUNG";"Sparen";"Umbuchung Sparen";"-500,00";"EUR";"Umsatz gebucht"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savings, []byte(header+`"05.10.26";"UMBUCHUNG";"Sparen";"Umbuchung";"500,00";"EUR";"Umsatz gebucht"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"Assets:Bank", "Assets:Savings"} {
		if _, err := run(t, db, "cli", "account", "add", a, "-c", "EUR"); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := run(t, db, "cli", "import", bank, "-a", "Assets:Bank", "--config", cfg); err != nil || strings.Contains(out, "paired") {
		t.Fatalf("first import = %q, %v", out, err)
	}

	out, err := run(t, db, "cli", "import", savings, "-a", "Assets:Savings", "--dry-run", "--config", cfg)
	if err != nil || !strings.Contains(out, "⇄ #1 Assets:Bank") || !strings.Contains(out, "1 new row(s) would be paired") {
		t.Fatalf("dry run = %q, %v", out, err)
	}
	out, err = run(t, db, "cli", "import", savings, "-a", "Assets:Savings", "--config", cfg)
	if err != nil || !strings.Contains(out, "1 row(s) were paired") || strings.Contains(out, "matched no rule") {
		t.Fatalf("import = %q, %v", out, err)
	}
	if out, err = run(t, db, "cli", "list", "-n", "0"); err != nil || strings.Count(out, "Equity:Transfers") != 2 || strings.Contains(out, "Uncategorized") {
		t.Errorf("both halves belong in Equity:Transfers:\n%s (%v)", out, err)
	}
	if out, err = run(t, db, "cli", "check"); err != nil || !strings.Contains(out, "OK") {
		t.Errorf("check = %q, %v", out, err)
	}
}

func TestTransferCommandBooksALinkedPair(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	for _, args := range [][]string{
		{"cli", "account", "add", "Assets:Bank", "-c", "EUR"},
		{"cli", "account", "add", "Assets:Savings", "-c", "EUR"},
		{"cli", "account", "add", "Assets:Dollars", "-c", "USD"},
	} {
		if _, err := run(t, db, args...); err != nil {
			t.Fatal(err)
		}
	}
	out, err := run(t, db, "cli", "transfer", "--from", "Assets:Bank", "--to", "Assets:Savings", "--amount", "500", "-d", "2026-10-05")
	if err != nil || !strings.Contains(out, "#1 and #2") || !strings.Contains(out, "Transferred 500.00 EUR") {
		t.Fatalf("transfer = %q, %v", out, err)
	}
	out, err = run(t, db, "cli", "transfer", "--from", "Assets:Bank", "--to", "Assets:Dollars", "--amount", "100", "--to-amount", "108.50", "-d", "2026-10-06")
	if err != nil || !strings.Contains(out, "#3 and #4") || !strings.Contains(out, "effective rate 1 EUR = 1.0850 USD") {
		t.Fatalf("cross-currency transfer = %q, %v", out, err)
	}
	if out, err = run(t, db, "cli", "list", "-n", "0"); err != nil || strings.Count(out, "Transfer to ")+strings.Count(out, "Transfer from ") != 4 ||
		!strings.Contains(out, "Equity:Transfers") || !strings.Contains(out, "Equity:Conversions") {
		t.Errorf("list:\n%s (%v)", out, err)
	}
	if out, err = run(t, db, "cli", "balance"); err != nil || !strings.Contains(out, "-600.00 EUR") || !strings.Contains(out, "108.50 USD") {
		t.Errorf("balance:\n%s (%v)", out, err)
	}
	if out, err = run(t, db, "cli", "check"); err != nil || !strings.Contains(out, "OK") {
		t.Errorf("check = %q, %v", out, err)
	}
}

func TestImportTakesOverHandEnteredTransactions(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "bank.csv")
	csv := `"Buchungstag";"Buchungstext";"Verwendungszweck";"Beguenstigter/Zahlungspflichtiger";"Betrag";"Waehrung";"Info"` + "\n" +
		`"05.10.26";"KARTENZAHLUNG";"Einkauf";"REWE Markt";"-23,47";"EUR";"Umsatz gebucht"` + "\n"
	if err := os.WriteFile(file, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"cli", "account", "add", "Assets:Bank", "-c", "EUR"},
		{"cli", "add", "-a", "Assets:Bank", "--amount=-23.47", "-t", "Expenses:Groceries", "-p", "REWE", "-d", "2026-10-03"},
	} {
		if out, err := run(t, db, args...); err != nil {
			t.Fatalf("%v: %q, %v", args, out, err)
		}
	}
	out, err := run(t, db, "cli", "import", file, "-a", "Assets:Bank", "--dry-run", "--config", cfg)
	if err != nil || !strings.Contains(out, "↔ #1 REWE") || !strings.Contains(out, "take it over") {
		t.Fatalf("dry run = %q, %v", out, err)
	}
	out, err = run(t, db, "cli", "import", file, "-a", "Assets:Bank", "--config", cfg)
	if err != nil || !strings.Contains(out, "Imported 0 new") || !strings.Contains(out, "matched a transaction you entered by hand") {
		t.Fatalf("import = %q, %v", out, err)
	}
	if out, err = run(t, db, "cli", "list", "-n", "0"); err != nil || strings.Count(out, "REWE") != 1 {
		t.Errorf("one REWE transaction, not two:\n%s (%v)", out, err)
	}
}
