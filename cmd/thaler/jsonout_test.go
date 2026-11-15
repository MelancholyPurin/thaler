package main

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/ledger"
)

func Test_snakeCase(t *testing.T) {
	tcs := []struct{ name, in, want string }{
		{name: "one word", in: "Name", want: "name"},
		{name: "two words", in: "SpentTotal", want: "spent_total"},
		{name: "an acronym alone", in: "ID", want: "id"},
		{name: "an acronym at the end", in: "ScheduleID", want: "schedule_id"},
		{name: "three words", in: "HasPlannedIncome", want: "has_planned_income"},
		{name: "a word with a number", in: "Top3Items", want: "top3_items"},
		{name: "already lower", in: "base", want: "base"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := snakeCase(tc.in)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("snakeCase mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ratText(t *testing.T) {
	tcs := []struct{ name, in, want string }{
		{name: "a whole number", in: "2", want: "2"},
		{name: "two decimals", in: "0.92", want: "0.92"},
		{name: "a trailing zero is not kept", in: "1.0850", want: "1.085"},
		{name: "many decimals", in: "0.000012345678", want: "0.000012345678"},
		{name: "a big one", in: "1234567.5", want: "1234567.5"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			rat, ok := new(big.Rat).SetString(tc.in)
			if !ok {
				t.Fatal("not a number")
			}

			// when
			got := ratText(rat)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ratText mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// compact is JSON without its white space, to compare a document with a line.
func compact(t *testing.T, doc string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(doc)); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, doc)
	}
	return buf.String()
}

func Test_printJSON(t *testing.T) {
	type inner struct {
		ScheduleID int64
		Note       string
	}
	type outer struct {
		Name    string
		Price   ledger.Money
		Items   []inner
		None    []inner
		Rate    *big.Rat
		Missing *inner
		When    time.Time
		Flags   map[string]bool
		Count   int
		Ok      bool
		secret  string
	}
	tcs := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "a struct: keys in snake_case and in the order of the fields, an amount as text with its currency, lists never null",
			in: outer{
				Name: "x", Price: ledger.Money{Minor: -1250, Currency: "EUR"}, Items: []inner{{ScheduleID: 3, Note: "a"}},
				Rate: big.NewRat(23, 25), When: time.Date(2026, 10, 22, 14, 30, 0, 0, time.FixedZone("CEST", 2*3600)),
				Flags: map[string]bool{"b": true, "a": false}, Count: 7, Ok: true, secret: "hidden",
			},
			want: `{"name":"x","price":{"amount":"-12.50","currency":"EUR"},"items":[{"schedule_id":3,"note":"a"}],"none":[],"rate":"0.92","missing":null,` +
				`"when":"2026-10-22T12:30:00Z","flags":{"a":false,"b":true},"count":7,"ok":true}`,
		},
		{name: "a pointer is what it points to", in: &inner{ScheduleID: 1, Note: "n"}, want: `{"schedule_id":1,"note":"n"}`},
		{name: "a value in an interface is the value", in: struct{ Any any }{ledger.Money{Minor: 5, Currency: "EUR"}}, want: `{"any":{"amount":"0.05","currency":"EUR"}}`},
		{name: "a list of nothing is []", in: []int{}, want: `[]`},
		{name: "a list that is nil is [] too", in: []string(nil), want: `[]`},
		{name: "text is not escaped for HTML", in: []string{`a & b <c> "d"`}, want: `["a & b <c> \"d\""]`},
		{name: "a currency with no decimals", in: ledger.Money{Minor: 1200, Currency: "JPY"}, want: `{"amount":"1200","currency":"JPY"}`},
		{name: "a struct with no fields is {}", in: struct{ x int }{}, want: `{}`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			var out bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&out)

			// when
			err := printJSON(cmd, tc.in)

			// then
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, compact(t, out.String())); diff != "" {
				t.Errorf("JSON mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(true, strings.HasSuffix(out.String(), "}\n") || strings.HasSuffix(out.String(), "]\n")); diff != "" {
				t.Errorf("the document ends in a line feed (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_printJSON_unsupported(t *testing.T) {
	tcs := []struct {
		name string
		in   any
		want string
	}{
		{name: "a float", in: 1.5, want: "cannot write a float64 as JSON"},
		{name: "a map keyed by numbers", in: map[int]string{1: "a"}, want: "cannot write a map keyed by int as JSON"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			cmd := &cobra.Command{}
			cmd.SetOut(&bytes.Buffer{})

			// when
			err := printJSON(cmd, tc.in)

			// then
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Every listing, with --json, as a document: its keys are the contract of a script.
func Test_listings_json(t *testing.T) {
	money := func(amount, cur string) string { return `{"amount":"` + amount + `","currency":"` + cur + `"}` }
	bank := func(amount string) string { return money(amount, "EUR") }
	all := [][]string{
		addBank, addDollars, spendSeptember, earnOctober, addRewe, spendGroceries, targetFood,
		{"cli", "rate", "set", "USD", "EUR", "0.925", "-d", "2026-03-15"}, {"cli", "rate", "set", "EUR", "USD", "2", "-d", "2026-04-01"},
		{"cli", "account", "minimum", "Assets:Bank", "100"},
		{"cli", "schedule", "add", "Rent", "--unit", "month", "-a", "Assets:Bank", "--amount=-900", "-t", "Expenses:Rent", "--start", "2026-10-01"},
		{"cli", "add", "-a", "Assets:Dollars", "--amount=-9", "-t", "Expenses:Fun", "-d", "2026-10-12", "-p", "AT&T", "-m", `a "q" <b>`},
	}
	rent := `{"id":1,"name":"Rent","every":1,"unit":"month","start":"2026-10-01","end":"","transfer":false,"tolerance":0,"payee":"","description":"",` +
		`"postings":[{"account":"Assets:Bank","amount":` + bank("-900.00") + `},{"account":"Expenses:Rent","amount":` + bank("900.00") + `}],"pauses":[]}`
	tcs := []cliCase{
		{
			name: "accounts", setup: all, args: []string{"cli", "account", "list", "--json"},
			want: `[{"id":1,"name":"Assets:Bank","type":"Assets","currency":"EUR"},{"id":2,"name":"Assets:Dollars","type":"Assets","currency":"USD"},` +
				`{"id":6,"name":"Expenses:Fun","type":"Expenses","currency":""},{"id":3,"name":"Expenses:Groceries","type":"Expenses","currency":""},` +
				`{"id":5,"name":"Expenses:Rent","type":"Expenses","currency":""},{"id":4,"name":"Income:Salary","type":"Income","currency":""}]`,
		},
		{name: "no accounts: an empty list, not a sentence", args: []string{"cli", "account", "list", "--json"}, want: `[]`},
		{
			name: "transactions, with the text as it is", setup: all, args: []string{"cli", "list", "--json", "-n", "2"},
			want: `[{"id":4,"date":"2026-10-03","payee":"","description":"","import_hash":"","transfer_with":0,"postings":[` +
				`{"account":"Assets:Bank","amount":` + bank("-120.00") + `},{"account":"Expenses:Groceries","amount":` + bank("120.00") + `}]},` +
				`{"id":5,"date":"2026-10-12","payee":"AT&T","description":"a \"q\" <b>","import_hash":"","transfer_with":0,"postings":[` +
				`{"account":"Assets:Dollars","amount":` + money("-9.00", "USD") + `},{"account":"Expenses:Fun","amount":` + money("9.00", "USD") + `}]}]`,
		},
		{name: "no transactions", setup: [][]string{addBank}, args: []string{"cli", "list", "--json"}, want: `[]`},
		{
			name: "balances and the net worth", setup: all, args: []string{"cli", "balance", "--json"},
			want: `{"date":"2026-10-22","lines":[{"account":"Assets:Bank","type":"Assets","amount":` + bank("2726.53") + `},` +
				`{"account":"Assets:Dollars","type":"Assets","amount":` + money("-9.00", "USD") + `}],` +
				`"net_worth":{"base":"EUR","total":` + bank("2722.03") + `,"missing":[]}}`,
		},
		{
			name:  "a currency with no rate is in the data, not in a warning",
			setup: [][]string{addBank, addDollars, {"cli", "account", "balance", "Assets:Dollars", "5", "--as-of", "2026-10-22"}},
			args:  []string{"cli", "balance", "--json"},
			want: `{"date":"2026-10-22","lines":[{"account":"Assets:Dollars","type":"Assets","amount":` + money("5.00", "USD") + `}],` +
				`"net_worth":{"base":"EUR","total":` + bank("0.00") + `,"missing":["USD"]}}`,
		},
		{
			name: "a month's budget", setup: all, args: []string{"cli", "budget", "--month", "2026-10", "--json"},
			want: `{"month":"2026-10","base":"EUR","lines":[` +
				`{"name":"Expenses:Groceries","spent":` + bank("143.47") + `,"has_target":true,"target":` + bank("400.00") + `,"rollover":false,"carried":` + bank("0.00") + `,"left":` + bank("256.53") + `},` +
				`{"name":"Expenses:Fun","spent":` + bank("4.50") + `,"has_target":false,"target":` + bank("0.00") + `,"rollover":false,"carried":` + bank("0.00") + `,"left":` + bank("0.00") + `}],` +
				`"spent_total":` + bank("147.97") + `,"target_total":` + bank("400.00") + `,"carried_total":` + bank("0.00") + `,"left_total":` + bank("256.53") + `,"unbudgeted_total":` + bank("4.50") + `,` +
				`"income":[{"name":"Income:Salary","received":` + bank("2900.00") + `,"has_plan":false,"planned":` + bank("0.00") + `}],` +
				`"income_total":` + bank("2900.00") + `,"planned_income_total":` + bank("0.00") + `,"has_planned_income":false,"unassigned":` + bank("0.00") + `,"missing":[]}`,
		},
		{
			name: "exchange rates, exactly as entered", setup: all, args: []string{"cli", "rate", "list", "--json"},
			want: `[{"date":"2026-03-15","base":"USD","quote":"EUR","rate":"0.925"},{"date":"2026-04-01","base":"EUR","quote":"USD","rate":"2"}]`,
		},
		{name: "no rates", args: []string{"cli", "rate", "list", "--json"}, want: `[]`},
		{name: "minimum balances", setup: all, args: []string{"cli", "account", "minimum", "--json"}, want: `[{"account":"Assets:Bank","amount":` + bank("100.00") + `}]`},
		{name: "no minimum balances", setup: [][]string{addBank}, args: []string{"cli", "account", "minimum", "--json"}, want: `[]`},
		{name: "schedules", setup: all, args: []string{"cli", "schedule", "list", "--json"}, want: `[` + rent + `]`},
		{name: "no schedules", setup: [][]string{addBank}, args: []string{"cli", "schedule", "list", "--json"}, want: `[]`},
		{name: "what is due", setup: all, args: []string{"cli", "schedule", "due", "--json"}, want: `[{"schedule":` + rent + `,"date":"2026-10-01"}]`},
		{name: "nothing due", setup: [][]string{addBank}, args: []string{"cli", "schedule", "due", "--json"}, want: `[]`},
		{
			name: "the forecast and its warnings", setup: all, args: []string{"cli", "forecast", "--until", "2026-11-30", "--json"},
			want: `{"projection":{"from":"2026-10-22","to":"2026-11-30","accounts":[` +
				`{"name":"Assets:Bank","type":"Assets","start":` + bank("2726.53") + `,"days":[` +
				`{"date":"2026-10-22","change":` + bank("-900.00") + `,"balance":` + bank("1826.53") + `,"moves":[{"schedule_id":1,"schedule":"Rent","amount":` + bank("-900.00") + `,"overdue":true}]},` +
				`{"date":"2026-11-01","change":` + bank("-900.00") + `,"balance":` + bank("926.53") + `,"moves":[{"schedule_id":1,"schedule":"Rent","amount":` + bank("-900.00") + `,"overdue":false}]}]},` +
				`{"name":"Assets:Dollars","type":"Assets","start":` + money("-9.00", "USD") + `,"days":[]}]},"low_balances":[]}`,
		},
		{
			name:  "a warning of the forecast is in the data",
			setup: [][]string{addBank, {"cli", "account", "balance", "Assets:Bank", "1000", "--as-of", "2026-10-22"}, {"cli", "account", "minimum", "Assets:Bank", "500"}, {"cli", "schedule", "add", "Rent", "--unit", "month", "-a", "Assets:Bank", "--amount=-900", "-t", "Expenses:Rent", "--start", "2026-10-28"}},
			args:  []string{"cli", "forecast", "--until", "2026-10-31", "--json"},
			want: `{"projection":{"from":"2026-10-22","to":"2026-10-31","accounts":[{"name":"Assets:Bank","type":"Assets","start":` + bank("1000.00") + `,"days":[` +
				`{"date":"2026-10-28","change":` + bank("-900.00") + `,"balance":` + bank("100.00") + `,"moves":[{"schedule_id":1,"schedule":"Rent","amount":` + bank("-900.00") + `,"overdue":false}]}]}]},` +
				`"low_balances":[{"account":"Assets:Bank","minimum":` + bank("500.00") + `,"date":"2026-10-28","balance":` + bank("100.00") + `,"now":false,"schedule":"Rent"}]}`,
		},
		{
			name: "spending by category", setup: all, args: []string{"cli", "report", "spending", "--from", "2026-09", "--to", "2026-10", "--json"},
			want: `{"base":"EUR","months":["2026-09","2026-10"],"categories":[` +
				`{"name":"Expenses:Groceries","months":[` + bank("30.00") + `,` + bank("143.47") + `],"total":` + bank("173.47") + `},` +
				`{"name":"Expenses:Fun","months":[` + bank("0.00") + `,` + bank("4.50") + `],"total":` + bank("4.50") + `}],` +
				`"totals":[` + bank("30.00") + `,` + bank("147.97") + `],"total":` + bank("177.97") + `,"missing":[]}`,
		},
		{
			name: "income against spending", setup: all, args: []string{"cli", "report", "cashflow", "--from", "2026-09", "--to", "2026-10", "--json"},
			want: `{"base":"EUR","months":[` +
				`{"month":"2026-09","income":` + bank("0.00") + `,"spending":` + bank("30.00") + `,"net":` + bank("-30.00") + `,"savings":{"basis_points":0,"known":false}},` +
				`{"month":"2026-10","income":` + bank("2900.00") + `,"spending":` + bank("147.97") + `,"net":` + bank("2752.03") + `,"savings":{"basis_points":9490,"known":true}}],` +
				`"total":{"month":"","income":` + bank("2900.00") + `,"spending":` + bank("177.97") + `,"net":` + bank("2722.03") + `,"savings":{"basis_points":9386,"known":true}},"missing":[]}`,
		},
		{
			name: "the payees, with an empty name for none and a sum of nothing in the currency", setup: all, args: []string{"cli", "report", "payees", "--from", "2026-09", "--to", "2026-10", "--json"},
			want: `{"base":"EUR","from":"2026-09","to":"2026-10","payees":[` +
				`{"name":"","amount":` + bank("150.00") + `,"transactions":2},{"name":"REWE","amount":` + bank("23.47") + `,"transactions":1},{"name":"AT&T","amount":` + bank("4.50") + `,"transactions":1}],` +
				`"others":{"name":"","amount":` + bank("0.00") + `,"transactions":0},"other_payees":0,"total":` + bank("177.97") + `,"missing":[]}`,
		},
		{
			name: "payees past a limit", setup: all, args: []string{"cli", "report", "payees", "--from", "2026-09", "--to", "2026-10", "--limit", "1", "--json"},
			want: `{"base":"EUR","from":"2026-09","to":"2026-10","payees":[{"name":"","amount":` + bank("150.00") + `,"transactions":2}],` +
				`"others":{"name":"","amount":` + bank("27.97") + `,"transactions":2},"other_payees":2,"total":` + bank("177.97") + `,"missing":[]}`,
		},
		{
			name: "net worth", setup: all, args: []string{"cli", "report", "networth", "--from", "2026-09", "--to", "2026-10", "--json"},
			want: `{"base":"EUR","months":[{"month":"2026-09","total":` + bank("-30.00") + `},{"month":"2026-10","total":` + bank("2722.03") + `}],"missing":[]}`,
		},
		{name: "a range that is not one is an error, not a document", setup: [][]string{addBank}, args: []string{"cli", "report", "networth", "--from", "soon", "--json"}, wantErr: `invalid month "soon": use YYYY-MM`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, tc.files, tc.setup...)

			// when
			got, err := e.run(tc.args...)

			// then
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if tc.wantErr != "" {
				if diff := cmp.Diff("", got); diff != "" {
					t.Errorf("nothing is printed with an error (-want +got):\n%s", diff)
				}
				return
			}
			if diff := cmp.Diff(tc.want, compact(t, got)); diff != "" {
				t.Errorf("JSON mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_prune_json(t *testing.T) {
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	backups := map[string]int{ // how many days old
		"backups/thaler-20261022-090000.db": 0, "backups/thaler-20261021-090000.db": 1,
		"backups/thaler-20261020-090000.db.age": 2, "backups/replaced/thaler-20261001-100000-replaced.db": 21,
	}
	tcs := []struct {
		name    string
		config  string
		args    []string
		want    string // the document, the folder of the database as {db}
		wantErr string
		left    int // how many files are there afterwards
	}{
		{
			name: "what would be removed, nothing is",
			args: []string{"cli", "prune", "--keep", "1", "--json"},
			want: `{"keep":1,"folders":[{"kind":"backups","dir":"{db}/backups","total":3,"remove":[` +
				`{"path":"{db}/backups/thaler-20261021-090000.db","size":5,"when":"2026-10-21T12:00:00Z","encrypted":false},` +
				`{"path":"{db}/backups/thaler-20261020-090000.db.age","size":5,"when":"2026-10-20T12:00:00Z","encrypted":true}]}]}`,
			left: 4,
		},
		{name: "nothing to remove", args: []string{"cli", "prune", "--keep", "5", "--json"}, want: `{"keep":5,"folders":[]}`, left: 4},
		{name: "all are kept", config: "keep_backups = 0\n", args: []string{"cli", "prune", "--json"}, want: `{"keep":0,"folders":[]}`, left: 4},
		{name: "it only lists", args: []string{"cli", "prune", "--keep", "1", "--json", "--yes"}, wantErr: "--json only lists: run prune --yes without it to remove the files", left: 4},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			e := newCLIEnv(t, nil)
			if err := os.WriteFile(e.cfg, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			dbDir := filepath.Dir(e.db)
			for name, days := range backups {
				p := filepath.Join(dbDir, name)
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
					t.Fatal(err)
				}
				mtime := base.AddDate(0, 0, -days)
				if err := os.Chtimes(p, mtime, mtime); err != nil {
					t.Fatal(err)
				}
			}

			// when
			got, err := e.run(tc.args...)

			// then
			if diff := cmp.Diff(tc.wantErr, e.errText(err)); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
			if tc.wantErr == "" {
				if diff := cmp.Diff(tc.want, strings.ReplaceAll(compact(t, got), dbDir, "{db}")); diff != "" {
					t.Errorf("JSON mismatch (-want +got):\n%s", diff)
				}
			}
			files := 0
			if err := filepath.WalkDir(dbDir, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && (strings.Contains(p, "/backups/")) {
					files++
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.left, files); diff != "" {
				t.Errorf("files left mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// The table stays the answer without --json.
func Test_listings_noJSONByDefault(t *testing.T) {
	tcs := []cliCase{
		{name: "accounts", setup: [][]string{addBank}, args: []string{"cli", "account", "list"}, want: "Assets:Bank  Assets, EUR\n"},
		{name: "an unknown flag is still one", setup: [][]string{addBank}, args: []string{"cli", "account", "add", "X", "--json"}, wantErr: "unknown flag: --json"},
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
