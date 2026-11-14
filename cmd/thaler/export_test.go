package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var (
	// a formula for a payee, a comma and a quote in a description, and a day that is on its own
	addFormula = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-9.99", "-t", "Expenses:Fun", "-p", "=EVIL()", "-m", `Say "hi", then go`, "-d", "2026-10-05"}
	addGift    = []string{"cli", "add", "-a", "Assets:Savings", "--amount=500", "-t", "Income:Gift", "-d", "2026-11-01"}
	// the ledger of the cases: ids 1 to 3, in this order
	ledgerOf3 = [][]string{addBank, spendSeptember, earnOctober, addRewe}
)

func Test_export(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "a line for each posting, oldest first",
			setup: ledgerOf3,
			args:  []string{"cli", "export"},
			want: "id,date,payee,description,account,amount,currency\n" +
				"1,2026-09-12,,,Assets:Bank,-30.00,EUR\n" +
				"1,2026-09-12,,,Expenses:Groceries,30.00,EUR\n" +
				"2,2026-10-02,,,Assets:Bank,2900.00,EUR\n" +
				"2,2026-10-02,,,Income:Salary,-2900.00,EUR\n" +
				"3,2026-10-03,REWE,,Assets:Bank,-23.47,EUR\n" +
				"3,2026-10-03,REWE,,Expenses:Groceries,23.47,EUR\n",
		},
		{
			name:  "a quote and a comma are quoted, a formula is made text",
			setup: [][]string{addBank, addFormula},
			args:  []string{"cli", "export"},
			want: "id,date,payee,description,account,amount,currency\n" +
				`1,2026-10-05,'=EVIL(),"Say ""hi"", then go",Assets:Bank,-9.99,EUR` + "\n" +
				`1,2026-10-05,'=EVIL(),"Say ""hi"", then go",Expenses:Fun,9.99,EUR` + "\n",
		},
		{
			name:  "one account",
			setup: append(append([][]string{}, ledgerOf3...), addSavings, addGift),
			args:  []string{"cli", "export", "-a", "Assets:Savings"},
			want: "id,date,payee,description,account,amount,currency\n" +
				"4,2026-11-01,,,Assets:Savings,500.00,EUR\n" +
				"4,2026-11-01,,,Income:Gift,-500.00,EUR\n",
		},
		{
			name:  "a category, with the account of each transaction",
			setup: ledgerOf3,
			args:  []string{"cli", "export", "--account", "Expenses:Groceries", "--from", "2026-10-01"},
			want: "id,date,payee,description,account,amount,currency\n" +
				"3,2026-10-03,REWE,,Assets:Bank,-23.47,EUR\n" +
				"3,2026-10-03,REWE,,Expenses:Groceries,23.47,EUR\n",
		},
		{
			name:  "from and to, both days included",
			setup: ledgerOf3,
			args:  []string{"cli", "export", "--from", "2026-10-02", "--to", "2026-10-02"},
			want: "id,date,payee,description,account,amount,currency\n" +
				"2,2026-10-02,,,Assets:Bank,2900.00,EUR\n" +
				"2,2026-10-02,,,Income:Salary,-2900.00,EUR\n",
		},
		{
			name:  "days written as in the interface",
			setup: ledgerOf3,
			args:  []string{"cli", "export", "--from", "02.10.2026", "--to", "02.10.2026"},
			want: "id,date,payee,description,account,amount,currency\n" +
				"2,2026-10-02,,,Assets:Bank,2900.00,EUR\n" +
				"2,2026-10-02,,,Income:Salary,-2900.00,EUR\n",
		},
		{
			name:  "nothing: the header alone",
			setup: [][]string{addBank},
			args:  []string{"cli", "export"},
			want:  "id,date,payee,description,account,amount,currency\n",
		},
		{
			name:  "a currency with no decimals",
			setup: [][]string{{"cli", "account", "add", "Assets:Yen", "-c", "JPY"}, {"cli", "add", "-a", "Assets:Yen", "--amount=-1200", "-t", "Expenses:Fun", "-d", "2026-10-05"}},
			args:  []string{"cli", "export"},
			want: "id,date,payee,description,account,amount,currency\n" +
				"1,2026-10-05,,,Assets:Yen,-1200,JPY\n" +
				"1,2026-10-05,,,Expenses:Fun,1200,JPY\n",
		},
		{name: "a day that is not one", setup: [][]string{addBank}, args: []string{"cli", "export", "--from", "soon"}, wantErr: `invalid date "soon": use YYYY-MM-DD or DD.MM.YYYY`},
		{name: "two destinations", setup: [][]string{addBank}, args: []string{"cli", "export", "a.csv", "b.csv"}, wantErr: "accepts at most 1 arg(s), received 2"},
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

func Test_export_file(t *testing.T) {
	tcs := []struct {
		name     string
		setup    [][]string
		prior    *string // what the file holds already
		args     []string
		want     string // what the command says
		wantErr  string
		wantFile string
	}{
		{
			name:  "a new file, readable by you alone",
			setup: ledgerOf3,
			args:  []string{"cli", "export", "{dir}/out.csv", "--from", "2026-10-01"},
			want:  "Wrote 2 transactions to {dir}/out.csv\n",
			wantFile: "id,date,payee,description,account,amount,currency\n" +
				"2,2026-10-02,,,Assets:Bank,2900.00,EUR\n2,2026-10-02,,,Income:Salary,-2900.00,EUR\n" +
				"3,2026-10-03,REWE,,Assets:Bank,-23.47,EUR\n3,2026-10-03,REWE,,Expenses:Groceries,23.47,EUR\n",
		},
		{
			name:  "one transaction is not plural",
			setup: ledgerOf3,
			args:  []string{"cli", "export", "{dir}/out.csv", "--from", "2026-10-03"},
			want:  "Wrote 1 transaction to {dir}/out.csv\n",
			wantFile: "id,date,payee,description,account,amount,currency\n" +
				"3,2026-10-03,REWE,,Assets:Bank,-23.47,EUR\n3,2026-10-03,REWE,,Expenses:Groceries,23.47,EUR\n",
		},
		{
			name:     "none: the header alone",
			setup:    [][]string{addBank},
			args:     []string{"cli", "export", "{dir}/out.csv"},
			want:     "Wrote 0 transactions to {dir}/out.csv\n",
			wantFile: "id,date,payee,description,account,amount,currency\n",
		},
		{
			name:     "a file that is there is never overwritten",
			setup:    ledgerOf3,
			prior:    new("keep me"),
			args:     []string{"cli", "export", "{dir}/out.csv"},
			wantErr:  "{dir}/out.csv is already there: give another name",
			wantFile: "keep me",
		},
		{
			name:    "a folder that is not there",
			setup:   ledgerOf3,
			args:    []string{"cli", "export", "{dir}/missing/out.csv"},
			wantErr: "open {dir}/missing/out.csv: no such file or directory",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, nil, tc.setup...)
			path := filepath.Join(e.dir, "out.csv")
			if tc.prior != nil {
				if err := os.WriteFile(path, []byte(*tc.prior), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// when
			got, err := e.run(tc.args...)

			// then
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			body, readErr := os.ReadFile(path)
			if tc.wantFile == "" {
				if diff := cmp.Diff(true, os.IsNotExist(readErr)); diff != "" {
					t.Errorf("no file was to be written (-want +got):\n%s", diff)
				}
				return
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			if diff := cmp.Diff(tc.wantFile, string(body)); diff != "" {
				t.Errorf("file mismatch (-want +got):\n%s", diff)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(os.FileMode(0o600), info.Mode().Perm()); diff != "" {
				t.Errorf("mode mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// list takes the same days as export, from the one helper.
func Test_list_days(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "from and to, both days included",
			setup: ledgerOf3,
			args:  []string{"cli", "list", "--from", "2026-10-02", "--to", "2026-10-02"},
			want:  "2026-10-02  #2     \n    Assets:Bank    +2900.00 EUR\n    Income:Salary  -2900.00 EUR\n",
		},
		{
			name:  "days written as in the interface",
			setup: ledgerOf3,
			args:  []string{"cli", "list", "--to", "30.09.2026"},
			want:  "2026-09-12  #1     \n    Assets:Bank         -30.00 EUR\n    Expenses:Groceries  +30.00 EUR\n",
		},
		{name: "a day that is not one", setup: [][]string{addBank}, args: []string{"cli", "list", "--to", "soon"}, wantErr: `invalid date "soon": use YYYY-MM-DD or DD.MM.YYYY`},
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
