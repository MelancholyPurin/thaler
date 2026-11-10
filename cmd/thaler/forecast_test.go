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
