package main

import "testing"

func Test_currency(t *testing.T) {
	addPesos := []string{"cli", "account", "add", "Assets:Pesos", "-c", "MXN"}
	dollarRate := []string{"cli", "rate", "set", "USD", "EUR", "0.92", "-d", "2026-10-01"}
	tcs := []cliCase{
		{name: "the base alone", args: []string{"cli", "currency"}, want: "Base currency: EUR\n"},
		{name: "accounts in the base currency only", setup: [][]string{addBank}, args: []string{"cli", "currency"}, want: "Base currency: EUR\n"},
		{
			name: "other currencies, with a rate and without one", setup: [][]string{addBank, addDollars, addPesos, dollarRate},
			args: []string{"cli", "currency"},
			want: "Base currency: EUR\n\n" +
				"Other currencies in use:\n" +
				"  MXN: no exchange rate yet; add one with: thaler cli rate set MXN EUR <rate>\n" +
				"  1 USD = 0.9200 EUR\n",
		},
		{
			name: "a rate from after today is not used yet", setup: [][]string{addBank, addDollars, {"cli", "rate", "set", "USD", "EUR", "0.92", "-d", "2026-10-23"}},
			args: []string{"cli", "currency"},
			want: "Base currency: EUR\n\n" +
				"Other currencies in use:\n" +
				"  USD: no exchange rate yet; add one with: thaler cli rate set USD EUR <rate>\n",
		},
		{name: "set, written in lower case", args: []string{"cli", "currency", "set", "usd"}, want: "Base currency is now USD\n"},
		{
			name: "set, and the euro is one of the others now", setup: [][]string{addBank, addDollars, dollarRate, {"cli", "currency", "set", "USD"}},
			args: []string{"cli", "currency"},
			want: "Base currency: USD\n\n" +
				"Other currencies in use:\n" +
				"  1 EUR = 1.0870 USD\n",
		},
		{name: "set a code that is not one", args: []string{"cli", "currency", "set", "EURO"}, wantErr: `invalid currency code "EURO": want three letters such as EUR, USD or MXN`},
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
