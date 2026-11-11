package main

import "testing"

func Test_forecast(t *testing.T) {
	funded := []string{"cli", "account", "balance", "Assets:Bank", "2000", "--as-of", "2026-10-22"}
	rentOct := []string{"cli", "schedule", "add", "Rent", "--unit", "month", "-a", "Assets:Bank", "--amount=-900", "-t", "Expenses:Rent", "--start", "2026-10-28"}
	tcs := []cliCase{
		{
			name:  "nothing scheduled",
			setup: [][]string{addBank, funded},
			args:  []string{"cli", "forecast"},
			want:  "No schedule moves an account up to 2027-01-20. Add one with: thaler cli schedule add\n",
		},
		{
			name:  "ninety days ahead",
			setup: [][]string{addBank, funded, rentOct},
			args:  []string{"cli", "forecast"},
			want: "Projected balances from 2026-10-22 up to 2027-01-20\n\nAssets:Bank, today 2000.00 EUR\n" +
				"Date             Change      Balance  Schedules\n" +
				"2026-10-28  -900.00 EUR  1100.00 EUR       Rent\n" +
				"2026-11-28  -900.00 EUR   200.00 EUR       Rent\n" +
				"2026-12-28  -900.00 EUR  -700.00 EUR       Rent\n",
		},
		{
			name:  "up to a date, which is included",
			setup: [][]string{addBank, funded, rentOct},
			args:  []string{"cli", "forecast", "--until", "2026-11-28"},
			want: "Projected balances from 2026-10-22 up to 2026-11-28\n\nAssets:Bank, today 2000.00 EUR\n" +
				"Date             Change      Balance  Schedules\n" +
				"2026-10-28  -900.00 EUR  1100.00 EUR       Rent\n" +
				"2026-11-28  -900.00 EUR   200.00 EUR       Rent\n",
		},
		{
			name:  "dates nobody answered are counted today",
			setup: [][]string{addBank, funded, addRent},
			args:  []string{"cli", "forecast", "--until", "2026-10-22"},
			want: "Projected balances from 2026-10-22 up to 2026-10-22\n\nAssets:Bank, today 2000.00 EUR\n" +
				"Date              Change     Balance                                         Schedules\n" +
				"2026-10-22  -1800.00 EUR  200.00 EUR  Rent (not answered yet), Rent (not answered yet)\n",
		},
		{
			name:  "a warning for the day it would fall below the minimum",
			setup: [][]string{addBank, funded, rentOct, {"cli", "account", "minimum", "Assets:Bank", "500"}},
			args:  []string{"cli", "forecast", "--until", "2026-12-31"},
			want: "Projected balances from 2026-10-22 up to 2026-12-31\n\nAssets:Bank, today 2000.00 EUR\n" +
				"Date             Change      Balance  Schedules\n" +
				"2026-10-28  -900.00 EUR  1100.00 EUR       Rent\n" +
				"2026-11-28  -900.00 EUR   200.00 EUR       Rent\n" +
				"2026-12-28  -900.00 EUR  -700.00 EUR       Rent\n" +
				"\nwarning: Assets:Bank would fall to 200.00 EUR on 2026-11-28 (Rent takes the most), below its minimum of 500.00 EUR\n",
		},
		{
			name:  "no warning while it stays above",
			setup: [][]string{addBank, funded, rentOct, {"cli", "account", "minimum", "Assets:Bank", "500"}},
			args:  []string{"cli", "forecast", "--until", "2026-10-31"},
			want: "Projected balances from 2026-10-22 up to 2026-10-31\n\nAssets:Bank, today 2000.00 EUR\n" +
				"Date             Change      Balance  Schedules\n" +
				"2026-10-28  -900.00 EUR  1100.00 EUR       Rent\n",
		},
		{
			name:  "below it already, with nothing scheduled",
			setup: [][]string{addBank, funded, {"cli", "account", "minimum", "Assets:Bank", "2500"}},
			args:  []string{"cli", "forecast", "--until", "2026-10-31"},
			want: "No schedule moves an account up to 2026-10-31. Add one with: thaler cli schedule add\n" +
				"\nwarning: Assets:Bank holds 2000.00 EUR, below its minimum of 2500.00 EUR, already today\n",
		},
		{
			name:    "a day before today",
			setup:   [][]string{addBank, rentOct},
			args:    []string{"cli", "forecast", "--until", "2026-10-21"},
			wantErr: "the projection starts today (2026-10-22); 2026-10-21 is before that",
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

func Test_account_minimum(t *testing.T) {
	minimum := func(args ...string) []string { return append([]string{"cli", "account", "minimum"}, args...) }
	addCard := []string{"cli", "account", "add", "Liabilities:Card", "-c", "EUR"}
	tcs := []cliCase{
		{
			name:  "none yet",
			setup: [][]string{addBank},
			args:  minimum(),
			want:  "No minimum balances. Set one with: thaler cli account minimum Assets:Bank 500\n",
		},
		{
			name:  "setting one",
			setup: [][]string{addBank},
			args:  minimum("Assets:Bank", "500"),
			want:  "Assets:Bank: warn when the balance would fall below 500.00 EUR\n",
		},
		{
			name:  "an agreed overdraft",
			setup: [][]string{addBank},
			args:  minimum("Assets:Bank", "--amount=-300"),
			want:  "Assets:Bank: warn when the balance would fall below -300.00 EUR\n",
		},
		{
			name:  "listing, with a new amount replacing the old",
			setup: [][]string{addBank, addSavings, minimum("Assets:Bank", "500"), minimum("Assets:Savings", "1000"), minimum("Assets:Bank", "250")},
			args:  minimum(),
			want:  "Assets:Bank      250.00 EUR\nAssets:Savings  1000.00 EUR\n",
		},
		{
			name:  "clearing",
			setup: [][]string{addBank, minimum("Assets:Bank", "500")},
			args:  minimum("Assets:Bank", "--clear"),
			want:  "Assets:Bank has no minimum balance\n",
		},
		{
			name:    "clearing needs the account",
			setup:   [][]string{addBank},
			args:    minimum("--clear"),
			wantErr: "--clear needs the account",
		},
		{
			name:    "clearing takes no amount",
			setup:   [][]string{addBank},
			args:    minimum("Assets:Bank", "5", "--clear"),
			wantErr: "--clear takes the account only, not an amount",
		},
		{
			name:    "an account without an amount",
			setup:   [][]string{addBank},
			args:    minimum("Assets:Bank"),
			wantErr: "give the amount, or --clear to take the minimum away",
		},
		{
			name:    "a liability",
			setup:   [][]string{addCard},
			args:    minimum("Liabilities:Card", "0"),
			wantErr: "Liabilities:Card is not an Assets account: only an Assets account can have a minimum balance",
		},
		{
			name:    "an amount that is not one",
			setup:   [][]string{addBank},
			args:    minimum("Assets:Bank", "lots"),
			wantErr: `invalid amount "lots"`,
		},
		{
			name:    "the amount said twice",
			setup:   [][]string{addBank},
			args:    minimum("Assets:Bank", "5", "--amount=6"),
			wantErr: "--amount goes with the account and no amount",
		},
		{
			name:    "no such account",
			args:    minimum("Assets:Nowhere", "5"),
			wantErr: "account does not exist: Assets:Nowhere",
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
