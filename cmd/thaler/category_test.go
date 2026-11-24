package main

import "testing"

func Test_category_set(t *testing.T) {
	addUncategorized := []string{"cli", "add", "-a", "Assets:Bank", "--amount=-5", "-t", "Expenses:Uncategorized", "-p", "Kiosk", "-d", "2026-10-02"}
	tcs := []cliCase{
		{
			name: "to a category that is new", setup: [][]string{addBank, addUncategorized},
			args: []string{"cli", "category", "set", "1", "Expenses:Food"},
			want: "Created new account Expenses:Food\nTransaction #1 is now in Expenses:Food\n",
		},
		{
			name: "again, which changes nothing", setup: [][]string{addBank, addUncategorized, {"cli", "category", "set", "1", "Expenses:Food"}},
			args: []string{"cli", "category", "set", "1", "Expenses:Food"},
			want: "Transaction #1 is now in Expenses:Food\n",
		},
		{name: "an id that is not a number", setup: [][]string{addBank}, args: []string{"cli", "category", "set", "x", "Expenses:Food"}, wantErr: `invalid transaction id "x"`},
		{name: "an id that is not positive", setup: [][]string{addBank}, args: []string{"cli", "category", "set", "0", "Expenses:Food"}, wantErr: `invalid transaction id "0"`},
		{
			name: "a transaction that does not exist", setup: [][]string{addBank},
			args:    []string{"cli", "category", "set", "99", "Expenses:Food"},
			wantErr: "transaction #99 does not exist",
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

func Test_recategorize(t *testing.T) {
	rules := map[string]string{"config.toml": "[[rule]]\ncontains = \"rewe\"\ncategory = \"Expenses:Groceries\"\n"}
	addUncategorized := func(payee, amount, day string) []string {
		return []string{"cli", "add", "-a", "Assets:Bank", "--amount=" + amount, "-t", "Expenses:Uncategorized", "-p", payee, "-d", day}
	}
	two := [][]string{addBank, addUncategorized("REWE Markt", "-23.47", "2026-10-02"), addUncategorized("Kiosk", "-5", "2026-10-03")}
	tcs := []cliCase{
		{name: "nothing uncategorized", files: rules, setup: [][]string{addBank}, args: []string{"cli", "recategorize"}, want: "No rule matches any of the 0 uncategorized transaction(s).\n"},
		{
			name: "a dry run shows what would move", files: rules, setup: two,
			args: []string{"cli", "recategorize", "--dry-run"},
			want: "2026-10-02  #1     Expenses:Groceries            REWE Markt  -23.47 EUR\n\n" +
				"Dry run: 1 of 2 uncategorized transaction(s) would move. Nothing was written.\n",
		},
		{
			name: "and then it moves", files: rules, setup: two,
			args: []string{"cli", "recategorize"},
			want: "2026-10-02  #1     Expenses:Groceries            REWE Markt  -23.47 EUR\n" +
				"Created new account Expenses:Groceries\n\n" +
				"Moved 1 of 2 uncategorized transaction(s); 1 still uncategorized.\n",
		},
		{
			name: "a second time, nothing is left for the rule", files: rules, setup: append(append([][]string{}, two...), []string{"cli", "recategorize"}),
			args: []string{"cli", "recategorize"},
			want: "No rule matches any of the 1 uncategorized transaction(s).\n",
		},
		{
			name: "a dry run writes nothing", files: rules, setup: append(append([][]string{}, two...), []string{"cli", "recategorize", "--dry-run"}),
			args: []string{"cli", "recategorize", "--dry-run"},
			want: "2026-10-02  #1     Expenses:Groceries            REWE Markt  -23.47 EUR\n\n" +
				"Dry run: 1 of 2 uncategorized transaction(s) would move. Nothing was written.\n",
		},
		{name: "without rules", setup: two, args: []string{"cli", "recategorize"}, want: "No rule matches any of the 2 uncategorized transaction(s).\n"},
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
