package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/melancholypurin/thaler/internal/core"
)

func Test_percentText(t *testing.T) {
	tcs := []struct {
		name string
		in   core.SavingsRate
		want string
	}{
		{name: "not known", in: core.SavingsRate{}, want: "-"},
		{name: "a quarter", in: core.SavingsRate{BasisPoints: 2500, Known: true}, want: "25.00%"},
		{name: "a hundredth", in: core.SavingsRate{BasisPoints: 1, Known: true}, want: "0.01%"},
		{name: "nothing, known", in: core.SavingsRate{Known: true}, want: "0.00%"},
		{name: "all", in: core.SavingsRate{BasisPoints: 10000, Known: true}, want: "100.00%"},
		{name: "negative", in: core.SavingsRate{BasisPoints: -4167, Known: true}, want: "-41.67%"},
		{name: "negative, under one", in: core.SavingsRate{BasisPoints: -5, Known: true}, want: "-0.05%"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := percentText(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("percentText mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Two months of a ledger: September and October 2026, a salary of 2900.00 in each, and
// spending of 30.00 in September and 952.50 in October.
var (
	septOct = [][]string{addBank, earnSeptember, earnOctober, spendSeptember, spendGroceries, spendRent, spendKiosk}
	// the same spending with payees
	paidRewe  = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-50", "-t", "Expenses:Fun", "-d", "2026-10-05", "-p", "REWE"}
	paidAgain = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-25", "-t", "Expenses:Fun", "-d", "2026-10-06", "-p", " rewe "}
	paidKino  = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-12", "-t", "Expenses:Fun", "-d", "2026-10-07", "-p", "Kino"}
	paidNone  = []string{"cli", "add", "-a", "Assets:Bank", "--amount=-5", "-t", "Expenses:Fun", "-d", "2026-10-08"}
	dollars   = [][]string{
		{"cli", "account", "add", "Assets:Dollars", "-c", "USD"},
		{"cli", "add", "-a", "Assets:Dollars", "--amount=-9", "-t", "Expenses:Fun", "-d", "2026-10-12", "-p", "Diner"},
	}
)

func Test_report_spending(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "the biggest first, the total and the average per month",
			setup: septOct,
			args:  []string{"cli", "report", "spending", "--from", "2026-09", "--to", "2026-10"},
			want: "Spending by category, 2026-09 to 2026-10, in EUR\n\n" +
				"Category                 Total  Per month\n" +
				"Expenses:Rent           800.00     400.00\n" +
				"Expenses:Groceries      150.00      75.00\n" +
				"Expenses:Uncategorized   32.50      16.25\n" +
				"Total                   982.50     491.25\n",
		},
		{
			name:  "a column for each month",
			setup: septOct,
			args:  []string{"cli", "report", "spending", "--from", "2026-09", "--to", "2026-10", "--months"},
			want: "Spending by category, 2026-09 to 2026-10, in EUR\n\n" +
				"Category                2026-09  2026-10   Total\n" +
				"Expenses:Rent              0.00   800.00  800.00\n" +
				"Expenses:Groceries        30.00   120.00  150.00\n" +
				"Expenses:Uncategorized     0.00    32.50   32.50\n" +
				"Total                     30.00   952.50  982.50\n",
		},
		{
			name:  "no range: the 12 months up to this one",
			setup: septOct,
			args:  []string{"cli", "report", "spending"},
			want: "Spending by category, 2025-11 to 2026-10, in EUR\n\n" +
				"Category                 Total  Per month\n" +
				"Expenses:Rent           800.00      66.66\n" +
				"Expenses:Groceries      150.00      12.50\n" +
				"Expenses:Uncategorized   32.50       2.70\n" +
				"Total                   982.50      81.87\n",
		},
		{
			name:  "only an end",
			setup: septOct,
			args:  []string{"cli", "report", "spending", "--to", "2026-09"},
			want: "Spending by category, 2025-10 to 2026-09, in EUR\n\n" +
				"Category            Total  Per month\n" +
				"Expenses:Groceries  30.00       2.50\n" +
				"Total               30.00       2.50\n",
		},
		{
			name:  "a month with nothing",
			setup: septOct,
			args:  []string{"cli", "report", "spending", "--from", "2026-05", "--to", "2026-06"},
			want:  "Nothing was spent from 2026-05 to 2026-06.\n",
		},
		{
			name:  "a currency with no rate is left out, and said",
			setup: append([][]string{addBank}, dollars...),
			args:  []string{"cli", "report", "spending", "--from", "2026-10", "--to", "2026-10"},
			want: "Nothing was spent from 2026-10 to 2026-10.\n" +
				"warning: no exchange rate USD -> EUR, so USD is left out; add one with: thaler cli rate set USD EUR <rate>\n",
		},
		{name: "not a month", setup: [][]string{addBank}, args: []string{"cli", "report", "spending", "--from", "soon"}, wantErr: `invalid month "soon": use YYYY-MM`},
		{name: "backwards", setup: [][]string{addBank}, args: []string{"cli", "report", "spending", "--from", "2026-10", "--to", "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
		{name: "more than ten years", setup: [][]string{addBank}, args: []string{"cli", "report", "spending", "--from", "2016-10"}, wantErr: "a report covers at most 120 months"},
		{name: "no arguments", setup: [][]string{addBank}, args: []string{"cli", "report", "spending", "extra"}, wantErr: `unknown command "extra" for "thaler cli report spending"`},
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

func Test_report_cashflow(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "what came in, went out and was kept, and the share",
			setup: septOct,
			args:  []string{"cli", "report", "cashflow", "--from", "2026-09", "--to", "2026-10"},
			want: "Income against spending, 2026-09 to 2026-10, in EUR\n\n" +
				"Month     Income   Spent     Kept   Saved\n" +
				"2026-09  2900.00   30.00  2870.00  98.97%\n" +
				"2026-10  2900.00  952.50  1947.50  67.16%\n" +
				"Total    5800.00  982.50  4817.50  83.06%\n",
		},
		{
			name:  "a month with no income has no share, one that spent more than came in a negative one",
			setup: [][]string{addBank, spendGroceries, earnSeptember, {"cli", "add", "-a", "Assets:Bank", "--amount=-3000", "-t", "Expenses:Rent", "-d", "2026-09-20"}},
			args:  []string{"cli", "report", "cashflow", "--from", "2026-08", "--to", "2026-10"},
			want: "Income against spending, 2026-08 to 2026-10, in EUR\n\n" +
				"Month     Income    Spent     Kept   Saved\n" +
				"2026-08     0.00     0.00     0.00       -\n" +
				"2026-09  2900.00  3000.00  -100.00  -3.45%\n" +
				"2026-10     0.00   120.00  -120.00       -\n" +
				"Total    2900.00  3120.00  -220.00  -7.59%\n",
		},
		{
			name:  "a currency with no rate is said",
			setup: append([][]string{addBank}, dollars...),
			args:  []string{"cli", "report", "cashflow", "--from", "2026-10", "--to", "2026-10"},
			want: "Income against spending, 2026-10 to 2026-10, in EUR\n\n" +
				"Month    Income  Spent  Kept  Saved\n" +
				"2026-10    0.00   0.00  0.00      -\n" +
				"Total      0.00   0.00  0.00      -\n" +
				"warning: no exchange rate USD -> EUR, so USD is left out; add one with: thaler cli rate set USD EUR <rate>\n",
		},
		{name: "backwards", setup: [][]string{addBank}, args: []string{"cli", "report", "cashflow", "--from", "2026-10", "--to", "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
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

// manyPaid is 22 payees of 1.00 each in October, P01 to P22.
func manyPaid() [][]string {
	out := [][]string{addBank}
	for i := 1; i <= 22; i++ {
		out = append(out, []string{"cli", "add", "-a", "Assets:Bank", "--amount=-1", "-t", "Expenses:Fun", "-d", "2026-10-05", "-p", fmt.Sprintf("P%02d", i)})
	}
	return out
}

func Test_report_payees_listed(t *testing.T) {
	tcs := []struct {
		name  string
		args  []string
		lines int    // the lines of the table: the heading, a blank one, the header, the payees, the others, the total
		last  string // the line before the total
	}{
		{name: "twenty unless told", args: nil, lines: 3 + 20 + 1 + 1, last: "(2 other payees)   2.00  9.09%             2"},
		{name: "a limit", args: []string{"--limit", "5"}, lines: 3 + 5 + 1 + 1, last: "(17 other payees)  17.00  77.27%            17"},
		{name: "0 is all of them", args: []string{"--limit", "0"}, lines: 3 + 22 + 1, last: "P22  1.00  4.55%             1"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, nil, manyPaid()...)

			// when
			out, err := e.run(append([]string{"cli", "report", "payees", "--from", "2026-10", "--to", "2026-10"}, tc.args...)...)

			// then
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			got := struct {
				Lines int
				Last  string
			}{len(lines), strings.Join(strings.Fields(lines[len(lines)-2]), " ")}
			want := struct {
				Lines int
				Last  string
			}{tc.lines, strings.Join(strings.Fields(tc.last), " ")}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("listing mismatch (-want +got):\n%s\n%s", diff, out)
			}
		})
	}
}

func Test_report_payees(t *testing.T) {
	paid := [][]string{addBank, earnOctober, paidRewe, paidAgain, paidKino, paidNone}
	tcs := []cliCase{
		{
			name:  "the biggest first, told apart without regard to case and spacing, with share and count",
			setup: paid,
			args:  []string{"cli", "report", "payees", "--from", "2026-10", "--to", "2026-10"},
			want: "Payees, 2026-10 to 2026-10, in EUR\n\n" +
				"Payee       Amount   Share  Transactions\n" +
				"rewe         75.00  81.52%             2\n" +
				"Kino         12.00  13.04%             1\n" +
				"(no payee)    5.00   5.43%             1\n" +
				"Total        92.00                     4\n",
		},
		{
			name:  "a limit: the others are one line",
			setup: paid,
			args:  []string{"cli", "report", "payees", "--from", "2026-10", "--to", "2026-10", "--limit", "1"},
			want: "Payees, 2026-10 to 2026-10, in EUR\n\n" +
				"Payee             Amount   Share  Transactions\n" +
				"rewe               75.00  81.52%             2\n" +
				"(2 other payees)   17.00  18.48%             2\n" +
				"Total              92.00                     4\n",
		},
		{
			name:  "one other payee is not plural",
			setup: paid,
			args:  []string{"cli", "report", "payees", "--from", "2026-10", "--to", "2026-10", "--limit", "2"},
			want: "Payees, 2026-10 to 2026-10, in EUR\n\n" +
				"Payee            Amount   Share  Transactions\n" +
				"rewe              75.00  81.52%             2\n" +
				"Kino              12.00  13.04%             1\n" +
				"(1 other payee)    5.00   5.43%             1\n" +
				"Total             92.00                     4\n",
		},
		{
			name:  "a limit of 0 lists every payee",
			setup: paid,
			args:  []string{"cli", "report", "payees", "--from", "2026-10", "--to", "2026-10", "--limit", "0"},
			want: "Payees, 2026-10 to 2026-10, in EUR\n\n" +
				"Payee       Amount   Share  Transactions\n" +
				"rewe         75.00  81.52%             2\n" +
				"Kino         12.00  13.04%             1\n" +
				"(no payee)    5.00   5.43%             1\n" +
				"Total        92.00                     4\n",
		},
		{
			name:  "nothing spent",
			setup: [][]string{addBank, earnOctober},
			args:  []string{"cli", "report", "payees", "--from", "2026-10", "--to", "2026-10"},
			want:  "Nothing was spent from 2026-10 to 2026-10.\n",
		},
		{name: "a limit below zero", setup: [][]string{addBank}, args: []string{"cli", "report", "payees", "--limit", "-1"}, wantErr: "--limit cannot be negative"},
		{name: "backwards", setup: [][]string{addBank}, args: []string{"cli", "report", "payees", "--from", "2026-10", "--to", "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
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

func Test_report_networth(t *testing.T) {
	tcs := []cliCase{
		{
			name:  "the balance at the end of each month and the change",
			setup: septOct,
			args:  []string{"cli", "report", "networth", "--from", "2026-08", "--to", "2026-10"},
			want: "Net worth at the end of each month, 2026-08 to 2026-10, in EUR\n\n" +
				"Month    Net worth   Change\n" +
				"2026-08       0.00\n" +
				"2026-09    2870.00  2870.00\n" +
				"2026-10    4817.50  1947.50\n",
		},
		{
			name:  "it is what balance gives for the last day of the month",
			setup: septOct,
			args:  []string{"cli", "balance", "--as-of", "2026-09-30"},
			want:  "Balances as of 2026-09-30\n\n  Assets:Bank  2870.00 EUR\n\nNet worth: 2870.00 EUR\n",
		},
		{
			name:  "a currency with no rate is said",
			setup: append([][]string{addBank}, dollars...),
			args:  []string{"cli", "report", "networth", "--from", "2026-10", "--to", "2026-10"},
			want: "Net worth at the end of each month, 2026-10 to 2026-10, in EUR\n\n" +
				"Month    Net worth  Change\n" +
				"2026-10       0.00\n" +
				"warning: no exchange rate USD -> EUR, so USD is left out; add one with: thaler cli rate set USD EUR <rate>\n",
		},
		{
			name: "a rate entered later leaves a month that is past alone",
			setup: [][]string{
				addBank, {"cli", "account", "add", "Assets:Dollars", "-c", "USD"},
				{"cli", "add", "-a", "Assets:Dollars", "--amount=100", "-t", "Income:Gift", "-d", "2026-09-01"},
				{"cli", "rate", "set", "USD", "EUR", "0.90", "-d", "2026-09-15"},
				{"cli", "rate", "set", "USD", "EUR", "2", "-d", "2026-10-20"},
			},
			args: []string{"cli", "report", "networth", "--from", "2026-09", "--to", "2026-10"},
			want: "Net worth at the end of each month, 2026-09 to 2026-10, in EUR\n\n" +
				"Month    Net worth  Change\n" +
				"2026-09      90.00\n" +
				"2026-10     200.00  110.00\n",
		},
		{name: "backwards", setup: [][]string{addBank}, args: []string{"cli", "report", "networth", "--from", "2026-10", "--to", "2026-09"}, wantErr: "the report cannot start in 2026-10: that is after it ends (2026-09)"},
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
