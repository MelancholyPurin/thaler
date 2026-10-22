package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
	"github.com/melancholypurin/thaler/internal/store"
)

func (a *app) importCmd() *cobra.Command {
	var account, profileName string
	var dryRun, ignoreErrors bool
	cmd := &cobra.Command{
		Use:   "import FILE",
		Short: "Import a bank CSV export into an account",
		Long: `Import a CSV export from your bank into one account.

Every row becomes a transaction between the account and a category chosen by
the rules in config.toml (see config.example.toml); rows that match no rule go
to Expenses:Uncategorized or Income:Uncategorized. Importing is idempotent:
rows that are already stored are recognised and skipped, so overlapping exports
are safe. Rows the bank marks as pending are ignored until they are booked.

Use --dry-run first to see what would happen without writing anything.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}
			profile, err := importer.FindProfile(profileName, cfg.Profiles)
			if err != nil {
				return err
			}
			rules, err := importer.NewRuleset(cfg.Rules)
			if err != nil {
				return fmt.Errorf("config rules: %w", err)
			}
			acc, err := st.GetAccount(ctx, account)
			if err != nil {
				return err
			}
			if acc.Currency == "" {
				return fmt.Errorf("%s has no fixed currency; import into an Assets or Liabilities account", acc.Name)
			}

			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			res, err := importer.Parse(f, profile, acc.Currency)
			if err != nil {
				return err
			}

			problems := append([]importer.RowError(nil), res.Errors...)
			var rows []importer.Row
			for _, r := range res.Rows {
				if r.Amount.Currency != acc.Currency {
					problems = append(problems, importer.RowError{
						Line: r.Line,
						Err:  fmt.Errorf("currency %s does not match %s (%s)", r.Amount.Currency, acc.Name, acc.Currency),
					})
					continue
				}
				rows = append(rows, r)
			}
			if len(problems) > 0 {
				fmt.Fprintf(out, "%d row(s) could not be used:\n", len(problems))
				for i, p := range problems {
					if i == 10 {
						fmt.Fprintf(out, "  ... and %d more\n", len(problems)-10)
						break
					}
					fmt.Fprintf(out, "  %v\n", p)
				}
				if !ignoreErrors {
					return fmt.Errorf("nothing imported; fix the file or the profile, or use --ignore-errors to import the remaining rows")
				}
			}

			dedupe := importer.NewDeduper(strconv.FormatInt(acc.ID, 10))
			txs := make([]store.NewTransaction, 0, len(rows))
			categories := make([]string, 0, len(rows))
			uncategorized := 0
			for _, r := range rows {
				category, ok := rules.Match(r.Payee, r.Purpose, r.BookingText)
				if !ok {
					uncategorized++
					category = ledger.UncategorizedExpense
					if r.Amount.Minor > 0 {
						category = ledger.UncategorizedIncome
					}
				}
				description := r.Purpose
				if description == "" {
					description = r.BookingText
				}
				txs = append(txs, store.NewTransaction{
					Date:        r.Date,
					Payee:       r.Payee,
					Description: description,
					ImportHash:  dedupe.Key(r),
					Postings: []ledger.Posting{
						{Account: acc.Name, Amount: r.Amount},
						{Account: category, Amount: r.Amount.Neg()},
					},
				})
				categories = append(categories, category)
			}

			if dryRun {
				newCount, dupCount := 0, 0
				lines := make([]column, 0, len(txs))
				for i, tx := range txs {
					exists, err := st.HasImportHash(ctx, tx.ImportHash)
					if err != nil {
						return err
					}
					status := "new"
					if exists {
						status = "dup"
						dupCount++
					} else {
						newCount++
					}
					lines = append(lines, column{
						label:  fmt.Sprintf("%-3s  %s  %-28s  %s", status, tx.Date.Format(ledger.DateLayout), truncate(categories[i], 28), truncate(tx.Payee, 40)),
						amount: signed(tx.Postings[0].Amount),
					})
				}
				printColumns(out, "", lines)
				fmt.Fprintf(out, "\nDry run: %d new, %d already imported, %d pending row(s) ignored. Nothing was written.\n",
					newCount, dupCount, res.Skipped)
				return nil
			}

			batch, err := st.AddTransactions(ctx, txs)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Imported %d new transaction(s) into %s; %d already present (skipped); %d pending row(s) ignored.\n",
				batch.Inserted, acc.Name, batch.Duplicates, res.Skipped)
			for _, name := range batch.Created {
				fmt.Fprintf(out, "Created new account %s\n", name)
			}
			if uncategorized > 0 {
				fmt.Fprintf(out, "%d row(s) in this file matched no rule and are uncategorized; review them with: thaler cli list -a %s\n",
					uncategorized, ledger.UncategorizedExpense)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&account, "account", "a", "", "account to import into, e.g. Assets:Sparkasse (required)")
	cmd.Flags().StringVar(&profileName, "profile", "sparkasse", "CSV profile: built-in \"sparkasse\" or one defined in config.toml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be imported without writing anything")
	cmd.Flags().BoolVar(&ignoreErrors, "ignore-errors", false, "import the readable rows even if some rows are broken")
	_ = cmd.MarkFlagRequired("account")
	return cmd
}
