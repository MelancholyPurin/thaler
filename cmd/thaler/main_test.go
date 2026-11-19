package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/coded"
	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
)

// cheapScrypt makes encrypting fast for the rest of the test: it matters here
// that a backup can be opened again, not how slow guessing a passphrase is.
func cheapScrypt(t *testing.T) {
	t.Helper()
	old := core.ScryptWorkFactor
	core.ScryptWorkFactor = 8
	t.Cleanup(func() { core.ScryptWorkFactor = old })
}

// fixedNow is the clock of every test: 22 October 2026.
func fixedNow() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }

// runApp executes the command tree with args against a temporary database.
func runApp(t *testing.T, a *app, db string, args ...string) (string, error) {
	t.Helper()
	t.Cleanup(a.close)
	root := newRoot(a)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--db", db, "--config", filepath.Join(t.TempDir(), "none.toml")}, args...))
	err := root.Execute()
	return out.String(), err
}

// cliEnv is a fresh database, a folder holding the files a test names and an
// empty config file (so there are no rules), with the setup commands already
// run. {dir} in an argument stands for the folder.
type cliEnv struct {
	t   *testing.T
	db  string
	dir string
	cfg string
	// answers are what the passphrase prompts get, in order; none means there is
	// no terminal to ask on.
	answers []string
	// last is the app of the latest command, whose language renders its errors.
	last *app
	// env is the environment variables the program sees; none are set by default,
	// so a test does not depend on the language of the machine it runs on.
	env map[string]string
}

func newCLIEnv(t *testing.T, files map[string]string, setup ...[]string) *cliEnv {
	t.Helper()
	cheapScrypt(t)
	e := &cliEnv{t: t, db: filepath.Join(t.TempDir(), "t.db"), dir: t.TempDir()}
	e.cfg = filepath.Join(e.dir, "config.toml")
	files = mergeFiles(map[string]string{"config.toml": ""}, files) // an empty config unless the test brings one
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(e.dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range setup {
		if out, err := e.run(args...); err != nil {
			t.Fatalf("setup %v: %q, %v", args, out, err)
		}
	}
	return e
}

func mergeFiles(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// run executes one command. The output and the error text show the folder as
// {dir}.
func (e *cliEnv) run(args ...string) (string, error) {
	e.t.Helper()
	full := make([]string, 0, len(args)+2)
	for _, a := range args {
		full = append(full, strings.ReplaceAll(a, "{dir}", e.dir))
	}
	full = append(full, "--config", e.cfg)
	a := &app{version: "v1.2.3", getenv: func(name string) string { return e.env[name] }, now: fixedNow}
	if e.answers != nil {
		answers := e.answers
		a.ask = func(prompt string) (string, error) {
			if len(answers) == 0 {
				return "", fmt.Errorf("no answer prepared for %q", prompt)
			}
			answer := answers[0]
			answers = answers[1:]
			return answer, nil
		}
	}
	e.last = a
	out, err := runApp(e.t, a, e.db, full...)
	return strings.ReplaceAll(out, e.dir, "{dir}"), err
}

func (e *cliEnv) errText(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(e.last.cli().Error(err), e.dir, "{dir}")
}

// cliCase is a command with the state it runs against and what it should say.
type cliCase struct {
	name    string
	files   map[string]string
	setup   [][]string
	args    []string
	want    string // the output
	wantErr string // the error text; {dir} stands for the folder
	env     map[string]string
}

func (tc cliCase) check(t *testing.T, e *cliEnv) {
	t.Helper()
	e.env = tc.env
	// when
	got, err := e.run(tc.args...)

	// then
	if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
		t.Errorf("error mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(tc.want, got); diff != "" {
		t.Errorf("output mismatch (-want +got):\n%s", diff)
	}
}

var (
	addBank    = []string{"cli", "account", "add", "Assets:Bank", "-c", "EUR"}
	addSavings = []string{"cli", "account", "add", "Assets:Savings", "-c", "EUR"}
	addDollars = []string{"cli", "account", "add", "Assets:Dollars", "-c", "USD"}
	addCard    = []string{"cli", "account", "add", "Liabilities:Card", "-c", "EUR"}
)

const csvHeader = `"Buchungstag";"Buchungstext";"Verwendungszweck";"Beguenstigter/Zahlungspflichtiger";"Betrag";"Waehrung";"Info"` + "\n"

// zeroRowCSV has a zero-amount row, as banks book for card verification.
const zeroRowCSV = csvHeader +
	`"01.10.26";"KARTENZAHLUNG";"Verifizierung";"Some Shop";"0,00";"EUR";"Umsatz gebucht"` + "\n" +
	`"02.10.26";"KARTENZAHLUNG";"Einkauf";"Some Shop";"-3,20";"EUR";"Umsatz gebucht"` + "\n"

const (
	bankTransferCSV    = csvHeader + `"04.10.26";"UMBUCHUNG";"Sparen";"Umbuchung Sparen";"-500,00";"EUR";"Umsatz gebucht"` + "\n"
	savingsTransferCSV = csvHeader + `"05.10.26";"UMBUCHUNG";"Sparen";"Umbuchung";"500,00";"EUR";"Umsatz gebucht"` + "\n"
	reweCSV            = csvHeader + `"05.10.26";"KARTENZAHLUNG";"Einkauf";"REWE Markt";"-23,47";"EUR";"Umsatz gebucht"` + "\n"
)

var (
	addRewe = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-23.47", "-t", "Expenses:Groceries", "-p", "REWE", "-d", "2026-10-03"}
)

func Test_account_add(t *testing.T) {
	tcs := []cliCase{
		{name: "an asset", args: addBank, want: "Created Assets:Bank (Assets, EUR)\n"},
		{name: "a category needs no currency", args: []string{"cli", "account", "add", "Expenses:Food"}, want: "Created Expenses:Food (Expenses, any currency)\n"},
		{name: "twice", setup: [][]string{addBank}, args: addBank, wantErr: "account already exists: Assets:Bank"},
		{name: "an asset without a currency", args: []string{"cli", "account", "add", "Assets:Bank"}, wantErr: "Assets accounts need a currency, e.g. EUR"},
		{name: "a name without a type", args: []string{"cli", "account", "add", "bank", "-c", "EUR"}, wantErr: `account "bank" must start with one of Assets, Liabilities, Income, Expenses, Equity`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_account_list(t *testing.T) {
	tcs := []cliCase{
		{name: "none yet", args: []string{"cli", "account", "list"}, want: "No accounts yet. Create one with: thaler cli account add Assets:Sparkasse --currency EUR\n"},
		{
			name:  "with their types and currencies",
			setup: [][]string{addBank, {"cli", "account", "add", "Expenses:Food"}},
			args:  []string{"cli", "account", "list"},
			want:  "Assets:Bank               Assets, EUR\nExpenses:Food  Expenses, any currency\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_account_rename_close_reopen(t *testing.T) {
	list := []string{"cli", "account", "list"}
	food := []string{"cli", "add", "-a", "Assets:Bank", "--amount=-10.00", "-p", "shop", "-t", "Expenses:Food", "-d", "2026-10-01"}
	tcs := []cliCase{
		{name: "rename", setup: [][]string{addBank}, args: []string{"cli", "account", "rename", "Assets:Bank", "Assets:Sparkasse"}, want: "Renamed Assets:Bank to Assets:Sparkasse\n"},
		{name: "the transactions go with the name", setup: [][]string{addBank, food, {"cli", "account", "rename", "Assets:Bank", "Assets:Sparkasse"}}, args: []string{"cli", "list"}, want: "2026-10-01  #1     shop\n    Assets:Sparkasse  -10.00 EUR\n    Expenses:Food     +10.00 EUR\n"},
		{name: "rename: no such account", args: []string{"cli", "account", "rename", "Assets:Nope", "Assets:New"}, wantErr: "account does not exist: Assets:Nope"},
		{name: "rename: a category is not an account of yours", setup: [][]string{{"cli", "account", "add", "Expenses:Food"}}, args: []string{"cli", "account", "rename", "Expenses:Food", "Expenses:Dining"}, wantErr: "Expenses:Food is not one of your accounts: only Assets and Liabilities accounts are renamed or closed"},
		{name: "rename: the name is taken", setup: [][]string{addBank, {"cli", "account", "add", "Assets:Savings", "--currency", "EUR"}}, args: []string{"cli", "account", "rename", "Assets:Bank", "Assets:Savings"}, wantErr: "account already exists: Assets:Savings"},
		{name: "close", setup: [][]string{addBank}, args: []string{"cli", "account", "close", "Assets:Bank"}, want: "Closed Assets:Bank\n"},
		{name: "a closed account is marked in the list", setup: [][]string{addBank, {"cli", "account", "close", "Assets:Bank"}}, args: list, want: "Assets:Bank  Assets, EUR, closed\n"},
		{name: "close: it must be empty", setup: [][]string{addBank, {"cli", "account", "balance", "Assets:Bank", "10", "--as-of", "2026-10-01"}}, args: []string{"cli", "account", "close", "Assets:Bank"}, wantErr: "Assets:Bank still holds 10.00 EUR: move it out before closing the account"},
		{name: "close: already closed", setup: [][]string{addBank, {"cli", "account", "close", "Assets:Bank"}}, args: []string{"cli", "account", "close", "Assets:Bank"}, wantErr: "Assets:Bank is already closed"},
		{name: "a closed account takes nothing new", setup: [][]string{addBank, {"cli", "account", "close", "Assets:Bank"}}, args: food, wantErr: "Assets:Bank is closed: reopen it first"},
		{name: "reopen", setup: [][]string{addBank, {"cli", "account", "close", "Assets:Bank"}}, args: []string{"cli", "account", "reopen", "Assets:Bank"}, want: "Reopened Assets:Bank\n"},
		{name: "reopen: it is not closed", setup: [][]string{addBank}, args: []string{"cli", "account", "reopen", "Assets:Bank"}, wantErr: "Assets:Bank is not closed"},
		{name: "reopened, it is in use again", setup: [][]string{addBank, {"cli", "account", "close", "Assets:Bank"}, {"cli", "account", "reopen", "Assets:Bank"}}, args: list, want: "Assets:Bank  Assets, EUR\n"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_account_balance(t *testing.T) {
	asOf := []string{"--as-of", "2026-10-22"}
	with := func(args ...string) []string {
		return append(append([]string{"cli", "account", "balance"}, args...), asOf...)
	}
	tcs := []cliCase{
		{
			name: "an asset", setup: [][]string{addBank}, args: with("Assets:Bank", "1234.56"),
			want: "Booked an opening balance of 1234.56 EUR on 2026-10-22 (#1) so that Assets:Bank holds 1234.56 EUR on 2026-10-22.\n",
		},
		{
			name: "a credit card is given as the amount owed", setup: [][]string{addCard}, args: with("Liabilities:Card", "500"),
			want: "Booked an opening balance of -500.00 EUR on 2026-10-22 (#1) so that Liabilities:Card holds 500.00 EUR on 2026-10-22.\n",
		},
		{
			name: "saying it again corrects the same opening balance", setup: [][]string{addBank, with("Assets:Bank", "1234.56")}, args: with("Assets:Bank", "1234.56"),
			want: "Booked an opening balance of 1234.56 EUR on 2026-10-22 (#1) so that Assets:Bank holds 1234.56 EUR on 2026-10-22.\n",
		},
		{
			name: "a balance of nothing needs no transaction", setup: [][]string{addBank}, args: with("Assets:Bank", "0"),
			want: "Nothing to book: the transactions already add up to 0.00 EUR on 2026-10-22.\n",
		},
		{name: "an account that does not exist", args: with("Assets:Nope", "1"), wantErr: "account does not exist: Assets:Nope"},
		{
			name: "a category has no balance", setup: [][]string{{"cli", "account", "add", "Expenses:Food"}}, args: with("Expenses:Food", "1"),
			wantErr: "Expenses:Food has no fixed currency; only Assets and Liabilities accounts have a balance",
		},
		{name: "an amount that is not a number", setup: [][]string{addBank}, args: with("Assets:Bank", "lots"), wantErr: `invalid amount "lots"`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_balance(t *testing.T) {
	balance := func(account, amount string) []string {
		return []string{"cli", "account", "balance", account, amount, "--as-of", "2026-10-22"}
	}
	tcs := []cliCase{
		{
			name:  "with an asset and a card",
			setup: [][]string{addBank, addCard, balance("Assets:Bank", "1234.56"), balance("Liabilities:Card", "500")},
			args:  []string{"cli", "balance", "--as-of", "2026-12-31"},
			want:  "Balances as of 2026-12-31\n\n  Assets:Bank       1234.56 EUR\n  Liabilities:Card  -500.00 EUR\n\nNet worth: 734.56 EUR\n",
		},
		{
			name:  "across currencies without a rate",
			setup: [][]string{addBank, addDollars, balance("Assets:Bank", "100"), balance("Assets:Dollars", "50")},
			args:  []string{"cli", "balance", "--as-of", "2026-12-31"},
			want: "Balances as of 2026-12-31\n\n  Assets:Bank     100.00 EUR\n  Assets:Dollars   50.00 USD\n\nNet worth: 100.00 EUR\n" +
				"warning: no exchange rate USD -> EUR, so USD accounts are not in the net worth; add one with: thaler cli rate set USD EUR <rate>\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

// utf16LE is s as little-endian UTF-16 with a byte order mark.
func utf16LE(s string) string {
	out := []byte{0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return string(out)
}

func Test_import(t *testing.T) {
	importFile := func(file, account string, extra ...string) []string {
		return append([]string{"cli", "import", "{dir}/" + file, "-a", account}, extra...)
	}
	zeroFiles := map[string]string{"export.csv": zeroRowCSV}
	transferFiles := map[string]string{"bank.csv": bankTransferCSV, "savings.csv": savingsTransferCSV}
	tcs := []cliCase{
		{
			name: "a file with a zero-amount row", files: zeroFiles, setup: [][]string{addBank},
			args: importFile("export.csv", "Assets:Bank"),
			want: "Imported 2 new transaction(s) into Assets:Bank; 0 already present (skipped); 0 pending row(s) ignored.\n" +
				"Created new account Expenses:Uncategorized\n" +
				"2 row(s) in this file matched no rule and are uncategorized; review them with: thaler cli list -a Expenses:Uncategorized\n",
		},
		{
			name: "the same file again is recognised", files: zeroFiles, setup: [][]string{addBank, importFile("export.csv", "Assets:Bank")},
			args: importFile("export.csv", "Assets:Bank"),
			want: "Imported 0 new transaction(s) into Assets:Bank; 2 already present (skipped); 0 pending row(s) ignored.\n" +
				"2 row(s) in this file matched no rule and are uncategorized; review them with: thaler cli list -a Expenses:Uncategorized\n",
		},
		{
			name: "a dry run lists the rows and writes nothing", files: zeroFiles, setup: [][]string{addBank},
			args: importFile("export.csv", "Assets:Bank", "--dry-run"),
			want: "new  2026-10-01  Expenses:Uncategorized        Some Shop   0.00 EUR\n" +
				"new  2026-10-02  Expenses:Uncategorized        Some Shop  -3.20 EUR\n\n" +
				"Dry run: 2 new, 0 already imported, 0 pending row(s) ignored. Nothing was written.\n" +
				"The file was read as utf-8, its columns separated by \";\".\n",
		},
		{
			name: "a copy a spreadsheet saved as Unicode text: UTF-16 with tabs", files: map[string]string{"export.csv": utf16LE(strings.ReplaceAll(zeroRowCSV, ";", "\t"))},
			setup: [][]string{addBank},
			args:  importFile("export.csv", "Assets:Bank", "--dry-run"),
			want: "new  2026-10-01  Expenses:Uncategorized        Some Shop   0.00 EUR\n" +
				"new  2026-10-02  Expenses:Uncategorized        Some Shop  -3.20 EUR\n\n" +
				"Dry run: 2 new, 0 already imported, 0 pending row(s) ignored. Nothing was written.\n" +
				"The file was read as utf-16le, its columns separated by tabs.\n",
		},
		{
			name: "the other half of a transfer: previewed", files: transferFiles,
			setup: [][]string{addBank, addSavings, importFile("bank.csv", "Assets:Bank")},
			args:  importFile("savings.csv", "Assets:Savings", "--dry-run"),
			want: "new  2026-10-05  ⇄ #1 Assets:Bank              Umbuchung  +500.00 EUR\n\n" +
				"Dry run: 1 new, 0 already imported, 0 pending row(s) ignored. Nothing was written.\n" +
				"The file was read as utf-8, its columns separated by \";\".\n" +
				"1 new row(s) would be paired as the other half of a transfer with a transaction on another account.\n",
		},
		{
			name: "the other half of a transfer: imported", files: transferFiles,
			setup: [][]string{addBank, addSavings, importFile("bank.csv", "Assets:Bank")},
			args:  importFile("savings.csv", "Assets:Savings"),
			want: "Imported 1 new transaction(s) into Assets:Savings; 0 already present (skipped); 0 pending row(s) ignored.\n" +
				"Created new account Equity:Transfers\n" +
				"1 row(s) were paired as the other half of a transfer with a transaction on another account (Equity:Transfers).\n",
		},
		{
			name: "a row that is the bank's record of an entry made by hand: previewed", files: map[string]string{"bank.csv": reweCSV},
			setup: [][]string{addBank, addRewe},
			args:  importFile("bank.csv", "Assets:Bank", "--dry-run"),
			want: "new  2026-10-05  ↔ #1 REWE                     REWE Markt  -23.47 EUR\n\n" +
				"Dry run: 0 new, 0 already imported, 0 pending row(s) ignored. Nothing was written.\n" +
				"The file was read as utf-8, its columns separated by \";\".\n" +
				"1 row(s) match a transaction you entered by hand and would take it over instead of being added (marked ↔).\n",
		},
		{
			name: "a row that is the bank's record of an entry made by hand: imported", files: map[string]string{"bank.csv": reweCSV},
			setup: [][]string{addBank, addRewe},
			args:  importFile("bank.csv", "Assets:Bank"),
			want: "Imported 0 new transaction(s) into Assets:Bank; 0 already present (skipped); 0 pending row(s) ignored.\n" +
				"1 row(s) matched a transaction you entered by hand; it now stands for the bank row and nothing was added twice.\n",
		},
		{name: "an account that does not exist", files: zeroFiles, args: importFile("export.csv", "Assets:Nope"), wantErr: "account does not exist: Assets:Nope"},
		{name: "a file that does not exist", setup: [][]string{addBank}, args: importFile("nope.csv", "Assets:Bank"), wantErr: "open {dir}/nope.csv: no such file or directory"},
		{name: "a profile that does not exist", files: zeroFiles, setup: [][]string{addBank}, args: importFile("export.csv", "Assets:Bank", "--profile", "nope"), wantErr: `unknown import profile "nope" (available: sparkasse)`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_list(t *testing.T) {
	importFile := func(file, account string) []string {
		return []string{"cli", "import", "{dir}/" + file, "-a", account}
	}
	list := []string{"cli", "list", "-n", "0"}
	tcs := []cliCase{
		{name: "nothing yet", args: []string{"cli", "list"}, want: "No transactions found.\n"},
		{
			name: "a zero-amount row is listed", files: map[string]string{"export.csv": zeroRowCSV},
			setup: [][]string{addBank, importFile("export.csv", "Assets:Bank")}, args: list,
			want: "2026-10-01  #1     Some Shop - Verifizierung\n" +
				"    Assets:Bank              0.00 EUR\n" +
				"    Expenses:Uncategorized   0.00 EUR\n" +
				"2026-10-02  #2     Some Shop - Einkauf\n" +
				"    Assets:Bank             -3.20 EUR\n" +
				"    Expenses:Uncategorized  +3.20 EUR\n",
		},
		{
			name:  "an opening balance said twice is one transaction",
			setup: [][]string{addBank, {"cli", "account", "balance", "Assets:Bank", "10", "--as-of", "2026-10-22"}, {"cli", "account", "balance", "Assets:Bank", "10", "--as-of", "2026-10-22"}},
			args:  list,
			want:  "2026-10-22  #1     Opening balance\n    Assets:Bank     +10.00 EUR\n    Equity:Opening  -10.00 EUR\n",
		},
		{
			name: "the two halves of an imported transfer", files: map[string]string{"bank.csv": bankTransferCSV, "savings.csv": savingsTransferCSV},
			setup: [][]string{addBank, addSavings, importFile("bank.csv", "Assets:Bank"), importFile("savings.csv", "Assets:Savings")}, args: list,
			want: "2026-10-04  #1     Umbuchung Sparen - Sparen\n" +
				"    Assets:Bank       -500.00 EUR\n" +
				"    Equity:Transfers  +500.00 EUR\n" +
				"2026-10-05  #2     Umbuchung - Sparen\n" +
				"    Assets:Savings    +500.00 EUR\n" +
				"    Equity:Transfers  -500.00 EUR\n",
		},
		{
			name: "an entry made by hand is not listed twice once imported", files: map[string]string{"bank.csv": reweCSV},
			setup: [][]string{addBank, addRewe, importFile("bank.csv", "Assets:Bank")}, args: list,
			want: "2026-10-03  #1     REWE\n    Assets:Bank         -23.47 EUR\n    Expenses:Groceries  +23.47 EUR\n",
		},
		{
			name: "a transfer is two linked transactions",
			setup: [][]string{addBank, addSavings, addDollars,
				{"cli", "transfer", "--from", "Assets:Bank", "--to", "Assets:Savings", "--amount", "500", "-d", "2026-10-05"},
				{"cli", "transfer", "--from", "Assets:Bank", "--to", "Assets:Dollars", "--amount", "100", "--to-amount", "108.50", "-d", "2026-10-06"}},
			args: []string{"cli", "list", "-n", "0"},
			want: "2026-10-05  #1     Transfer to Savings\n" +
				"    Assets:Bank         -500.00 EUR\n" +
				"    Equity:Transfers    +500.00 EUR\n" +
				"2026-10-05  #2     Transfer from Bank\n" +
				"    Assets:Savings      +500.00 EUR\n" +
				"    Equity:Transfers    -500.00 EUR\n" +
				"2026-10-06  #3     Transfer to Dollars\n" +
				"    Assets:Bank         -100.00 EUR\n" +
				"    Equity:Conversions  +100.00 EUR\n" +
				"2026-10-06  #4     Transfer from Bank\n" +
				"    Assets:Dollars      +108.50 USD\n" +
				"    Equity:Conversions  -108.50 USD\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_transfer(t *testing.T) {
	transfer := func(args ...string) []string { return append([]string{"cli", "transfer"}, args...) }
	accounts := [][]string{addBank, addSavings, addDollars}
	tcs := []cliCase{
		{
			name: "within one currency", setup: accounts,
			args: transfer("--from", "Assets:Bank", "--to", "Assets:Savings", "--amount", "500", "-d", "2026-10-05"),
			want: "#1 and #2  2026-10-05  Transferred 500.00 EUR from Assets:Bank to Assets:Savings\nCreated new account Equity:Transfers\n",
		},
		{
			name: "across currencies the amount received fixes the rate", setup: accounts,
			args: transfer("--from", "Assets:Bank", "--to", "Assets:Dollars", "--amount", "100", "--to-amount", "108.50", "-d", "2026-10-06"),
			want: "#1 and #2  2026-10-06  Transferred 100.00 EUR from Assets:Bank to Assets:Dollars (108.50 USD arrived, effective rate 1 EUR = 1.0850 USD; record it with: thaler cli rate set EUR USD 1.0850)\n" +
				"Created new account Equity:Conversions\n",
		},
		{
			name: "across currencies without the amount received", setup: accounts,
			args:    transfer("--from", "Assets:Bank", "--to", "Assets:Dollars", "--amount", "100"),
			wantErr: "EUR -> USD crosses currencies; say how much arrives in USD",
		},
		{
			name: "within one currency with an amount received", setup: accounts,
			args:    transfer("--from", "Assets:Bank", "--to", "Assets:Savings", "--amount", "100", "--to-amount", "100"),
			wantErr: "both accounts use EUR, so there is no separate amount received",
		},
		{
			name: "to the same account", setup: accounts,
			args:    transfer("--from", "Assets:Bank", "--to", "Assets:Bank", "--amount", "1"),
			wantErr: "source and destination are the same account",
		},
		{
			name: "of nothing", setup: accounts,
			args:    transfer("--from", "Assets:Bank", "--to", "Assets:Savings", "--amount", "0"),
			wantErr: "amount must be greater than zero",
		},
		{
			name: "from an account that does not exist", setup: accounts,
			args:    transfer("--from", "Assets:Nope", "--to", "Assets:Savings", "--amount", "1"),
			wantErr: "account does not exist: Assets:Nope",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_transfer_editAndDelete(t *testing.T) {
	transfer := func(args ...string) []string { return append([]string{"cli", "transfer"}, args...) }
	moved := [][]string{addBank, addSavings, addDollars,
		transfer("--from", "Assets:Bank", "--to", "Assets:Savings", "--amount", "500", "-d", "2026-10-05")}
	tcs := []cliCase{
		{
			name: "the amount of both halves", setup: moved,
			args: transfer("edit", "2", "--amount", "600"),
			want: "Changed the transfer:\n" +
				"#1  2026-10-05  Assets:Bank  -600.00 EUR  Transfer to Savings\n" +
				"#2  2026-10-05  Assets:Savings  +600.00 EUR  Transfer from Bank\n",
		},
		{
			name: "the day of both, then of the half that arrives", setup: moved,
			args: transfer("edit", "1", "--date", "2026-10-07", "--in-date", "2026-10-08"),
			want: "Changed the transfer:\n" +
				"#1  2026-10-07  Assets:Bank  -500.00 EUR  Transfer to Savings\n" +
				"#2  2026-10-08  Assets:Savings  +500.00 EUR  Transfer from Bank\n",
		},
		{
			name: "to another currency, with what arrived", setup: moved,
			args: transfer("edit", "1", "--to", "Assets:Dollars", "--to-amount", "540"),
			want: "Changed the transfer:\n" +
				"#1  2026-10-05  Assets:Bank  -500.00 EUR  Transfer to Dollars\n" +
				"#2  2026-10-05  Assets:Dollars  +540.00 USD  Transfer from Bank\n",
		},
		{
			name: "to another currency, without what arrived", setup: moved,
			args:    transfer("edit", "1", "--to", "Assets:Dollars"),
			wantErr: "EUR -> USD crosses currencies; say how much arrives in USD",
		},
		{
			name: "from an account in another currency, keeping the amount", setup: moved,
			args:    transfer("edit", "1", "--from", "Assets:Dollars"),
			wantErr: "amount is in EUR, but Assets:Dollars holds USD",
		},
		{
			name: "with nothing to change", setup: moved,
			args:    transfer("edit", "1"),
			wantErr: "nothing to change: give at least one of --from, --to, --amount, --to-amount, --date, --in-date, --memo",
		},
		{
			name: "not a transfer", setup: append(moved, []string{"cli", "add", "-a", "Assets:Bank", "--amount=-5", "-t", "Expenses:Food", "-d", "2026-10-06"}),
			args:    transfer("edit", "3", "--amount", "1"),
			wantErr: "transaction #3 is not one half of a transfer",
		},
		{
			name: "not a number", setup: moved,
			args:    transfer("delete", "x"),
			wantErr: `"x" is not a transaction number`,
		},
		{
			name: "delete without --yes lists the halves and deletes nothing", setup: moved,
			args: transfer("delete", "1"),
			want: "#1  2026-10-05  Assets:Bank  -500.00 EUR  Transfer to Savings\n" +
				"#2  2026-10-05  Assets:Savings  +500.00 EUR  Transfer from Bank\n" +
				"\nNothing was deleted. Run it again with --yes to delete both halves.\n",
		},
		{
			name: "delete with --yes deletes both", setup: moved,
			args: transfer("delete", "2", "--yes"),
			want: "Deleted the transfer:\n" +
				"#1  2026-10-05  Assets:Bank  -500.00 EUR  Transfer to Savings\n" +
				"#2  2026-10-05  Assets:Savings  +500.00 EUR  Transfer from Bank\n",
		},
		{
			name: "after a delete, nothing is left of it", setup: append(moved, transfer("delete", "2", "--yes")),
			args:    transfer("delete", "1"),
			wantErr: "transaction #1 does not exist",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_transfer_link(t *testing.T) {
	transfer := func(args ...string) []string { return append([]string{"cli", "transfer"}, args...) }
	add := func(account, amount, date, payee string) []string {
		return []string{"cli", "add", "-a", account, "--amount=" + amount, "-d", date, "-p", payee}
	}
	two := [][]string{addBank, addSavings, add("Assets:Bank", "-500", "2026-10-04", "Dauerauftrag"), add("Assets:Savings", "500", "2026-10-05", "Sparen")}
	tcs := []cliCase{
		{
			name: "two that cancel", setup: two,
			args: transfer("link", "2", "1"),
			want: "Linked as a transfer:\n" +
				"#1  2026-10-04  Assets:Bank  -500.00 EUR  Dauerauftrag\n" +
				"#2  2026-10-05  Assets:Savings  +500.00 EUR  Sparen\n" +
				"Created new account Equity:Transfers\n",
		},
		{
			name: "two that do not", setup: append(two, add("Assets:Savings", "400", "2026-10-05", "Teil")),
			args:    transfer("link", "1", "3"),
			wantErr: "#1 and #3 do not move the same amount",
		},
		{
			name: "unlinked again", setup: append(two, transfer("link", "1", "2")),
			args: transfer("unlink", "2"),
			want: "Unlinked #1 and #2: both are uncategorized now.\n",
		},
		{
			name: "unlinking what is no transfer", setup: two,
			args:    transfer("unlink", "1"),
			wantErr: "transaction #1 is not one half of a transfer",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_split(t *testing.T) {
	split := func(args ...string) []string { return append([]string{"cli", "split"}, args...) }
	shop := [][]string{addBank, {"cli", "add", "-a", "Assets:Bank", "--amount=-30", "-d", "2026-10-04", "-p", "REWE", "-t", "Expenses:Food"}}
	tcs := []cliCase{
		{
			name: "an ordinary transaction divided, the last line taking the rest", setup: shop,
			args: split("1", "--line", "Expenses:Food=20", "--line", "Expenses:Household"),
			want: "#1  2026-10-04  Assets:Bank  -30.00 EUR  REWE\n" +
				"  Expenses:Food  20.00 EUR\n" +
				"  Expenses:Household  10.00 EUR\n" +
				"Created new account Expenses:Household\n",
		},
		{
			name: "the amount and the lines changed together", setup: append(shop, split("1", "--line", "Expenses:Food=20", "--line", "Expenses:Household")),
			args: split("1", "--amount=-35", "--line", "Expenses:Food=25", "--line", "Expenses:Household"),
			want: "#1  2026-10-04  Assets:Bank  -35.00 EUR  REWE\n" +
				"  Expenses:Food  25.00 EUR\n" +
				"  Expenses:Household  10.00 EUR\n",
		},
		{
			name: "only the payee changes, the lines stay", setup: append(shop, split("1", "--line", "Expenses:Food=20", "--line", "Expenses:Household")),
			args: split("1", "-p", "Rewe Markt"),
			want: "#1  2026-10-04  Assets:Bank  -30.00 EUR  Rewe Markt\n" +
				"  Expenses:Food  20.00 EUR\n" +
				"  Expenses:Household  10.00 EUR\n",
		},
		{
			name: "one line is an ordinary transaction again", setup: append(shop, split("1", "--line", "Expenses:Food=20", "--line", "Expenses:Household")),
			args: split("1", "--line", "Expenses:Household"),
			want: "#1  2026-10-04  Assets:Bank  -30.00 EUR  REWE\n" +
				"  Expenses:Household  30.00 EUR\n",
		},
		{
			name: "lines that do not add up", setup: shop,
			args:    split("1", "--line", "Expenses:Food=20", "--line", "Expenses:Household=5"),
			wantErr: "the lines are 5.00 EUR short of the amount",
		},
		{
			name: "a new amount the lines do not fit", setup: shop,
			args:    split("1", "--amount=-35"),
			wantErr: "the lines are 5.00 EUR short of the amount",
		},
		{
			name: "a share that is no amount", setup: shop,
			args:    split("1", "--line", "Expenses:Food=abc"),
			wantErr: `--line Expenses:Food=abc: invalid amount "abc"`,
		},
		{
			name: "a share of nothing", setup: shop,
			args:    split("1", "--line", "Expenses:Food=0", "--line", "Expenses:Household"),
			wantErr: "--line Expenses:Food=0: a share of nothing; leave it out for the line that takes the rest",
		},
		{name: "nothing to change", setup: shop, args: split("1"), wantErr: "nothing to change: give --line, --amount, --date, --payee or --memo"},
		{
			name: "a transfer", setup: [][]string{addBank, addSavings, {"cli", "transfer", "--from", "Assets:Bank", "--to", "Assets:Savings", "--amount", "5"}},
			args:    split("1", "--line", "Expenses:Food"),
			wantErr: "transaction #1 cannot be split: only one on an account of yours with income or expense categories can",
		},
		{name: "one that does not exist", setup: shop, args: split("9", "--line", "Expenses:Food"), wantErr: "transaction #9 does not exist"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_check(t *testing.T) {
	tcs := []cliCase{
		{name: "an empty ledger", args: []string{"cli", "check"}, want: "OK: the ledger is consistent.\n"},
		{
			name: "after transfers and an import that pairs", files: map[string]string{"bank.csv": bankTransferCSV, "savings.csv": savingsTransferCSV},
			setup: [][]string{addBank, addSavings,
				{"cli", "import", "{dir}/bank.csv", "-a", "Assets:Bank"}, {"cli", "import", "{dir}/savings.csv", "-a", "Assets:Savings"},
				{"cli", "transfer", "--from", "Assets:Bank", "--to", "Assets:Savings", "--amount", "5"}},
			args: []string{"cli", "check"},
			want: "OK: the ledger is consistent.\n",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_newRoot(t *testing.T) {
	tcs := []cliCase{
		{name: "balance is a `cli` command now", args: []string{"balance"}, wantErr: `unknown command "balance" for "thaler"`},
		{name: "there is no tui subcommand", args: []string{"tui"}, wantErr: `unknown command "tui" for "thaler"`},
		{name: "--version prints the version of the build", args: []string{"--version"}, want: "thaler v1.2.3\n"},
		{name: "the scripting commands live under cli", setup: [][]string{addBank}, args: []string{"cli", "account", "list"}, want: "Assets:Bank  Assets, EUR\n"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

// Under `go test` stdin and stdout are not terminals, so the interface refuses
// to start instead of printing escape codes into a pipe.
func Test_runTUI(t *testing.T) {
	tcs := []cliCase{
		{name: "without a terminal", wantErr: "the interactive interface needs a terminal; for scripts use: thaler cli --help"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

// The interface follows the language setting; `thaler cli` does not (see Test_app_cliIsEnglish).
func Test_app_localizer(t *testing.T) {
	tcs := []struct {
		name string
		cfg  *config.Config
		env  map[string]string
		want string // the start of "no_accounts_yet", which tells the languages apart
	}{
		{name: "English when nothing says otherwise", cfg: &config.Config{}, want: "No accounts yet."},
		{name: "the language of the environment", cfg: &config.Config{}, env: map[string]string{"LANG": "de_DE.UTF-8"}, want: "Noch keine Konten."},
		{name: "config.toml wins over the environment", cfg: &config.Config{Language: "es"}, env: map[string]string{"LANG": "de_DE.UTF-8"}, want: "Todavía no hay cuentas."},
		{name: "config.toml can ask for English", cfg: &config.Config{Language: "en"}, env: map[string]string{"LANG": "de_DE.UTF-8"}, want: "No accounts yet."},
		{name: "a language Thaler does not speak is English", cfg: &config.Config{}, env: map[string]string{"LANG": "fr_FR.UTF-8"}, want: "No accounts yet."},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			a := &app{cfg: tc.cfg, getenv: func(name string) string { return tc.env[name] }}

			// when
			got := a.localizer().T("no_accounts_yet", nil)

			// then
			if !strings.HasPrefix(got, tc.want) {
				t.Errorf("got %q, want it to start with %q", got, tc.want)
			}
		})
	}
}

// `thaler cli` speaks English whatever the language of the machine or of config.toml:
// its output, its dates and its errors.
func Test_app_cliIsEnglish(t *testing.T) {
	const english = "No accounts yet. Create one with: thaler cli account add Assets:Sparkasse --currency EUR\n"
	list := []string{"cli", "account", "list"}
	add := []string{"cli", "add", "-a", "Assets:Bank", "--amount=-5", "--date", "01/10/2026"}
	same := []string{"cli", "transfer", "--from", "Assets:Bank", "--to", "Assets:Bank", "--amount", "5"}
	missing := []string{"cli", "transfer", "--from", "Assets:Nope", "--to", "Assets:Bank", "--amount", "5"}
	german := map[string]string{"LANG": "de_DE.UTF-8"}
	spanish := map[string]string{"LANG": "es_ES.UTF-8"}
	tcs := []cliCase{
		{name: "a German machine", args: list, env: german, want: english},
		{name: "a language in config.toml", args: list, files: map[string]string{"config.toml": "language = \"es\"\n"}, env: german, want: english},
		{name: "a slash date is ambiguous even in Spanish", setup: [][]string{addBank}, args: add, env: spanish, wantErr: `invalid date "01/10/2026": use YYYY-MM-DD or DD.MM.YYYY`},
		{name: "an error, on a Spanish machine", setup: [][]string{addBank}, args: same, env: spanish, wantErr: "source and destination are the same account"},
		{name: "an error with an argument, on a German machine", setup: [][]string{addBank}, args: missing, env: german, wantErr: "account does not exist: Assets:Nope"},
		{name: "an error, language set in the config", setup: [][]string{addBank}, args: missing, files: map[string]string{"config.toml": "language = \"de\"\n"}, wantErr: "account does not exist: Assets:Nope"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_app_dateFlag(t *testing.T) {
	add := []string{"cli", "add", "-a", "Assets:Bank", "--amount=-5", "--date", "01/10/2026"}
	tcs := []cliCase{
		{name: "slashes are ambiguous in English", setup: [][]string{addBank}, args: add, wantErr: `invalid date "01/10/2026": use YYYY-MM-DD or DD.MM.YYYY`},
		{name: "a date that is none", setup: [][]string{addBank}, args: []string{"cli", "add", "-a", "Assets:Bank", "--amount=-5", "--date", "1/2/x"}, wantErr: `invalid date "1/2/x": use YYYY-MM-DD or DD.MM.YYYY`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when, then
			tc.check(t, e)
		})
	}
}

func Test_app_errorLine(t *testing.T) {
	tcs := []struct {
		name string
		env  map[string]string
		err  error
		want string
	}{
		{name: "a plain error", err: errors.New("disk full"), want: "error: disk full"},
		{name: "a coded error in English", err: coded.New("account_not_found", map[string]any{"Name": "Assets:Nope"}), want: "error: account does not exist: Assets:Nope"},
		{name: "in English on a German machine", env: map[string]string{"LANG": "de_DE.UTF-8"}, err: coded.New("account_not_found", map[string]any{"Name": "Assets:Nope"}), want: "error: account does not exist: Assets:Nope"},
		{name: "wrapped", env: map[string]string{"LANG": "es_ES.UTF-8"}, err: fmt.Errorf("importing: %w", coded.New("already_imported", nil)), want: "error: importing: transaction already imported"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			a := &app{cfg: &config.Config{}, getenv: func(name string) string { return tc.env[name] }}

			// when
			got := a.errorLine(tc.err)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
