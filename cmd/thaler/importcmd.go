package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/importer"
	"github.com/melancholypurin/thaler/internal/ledger"
)

var errNothingImported = errors.New("nothing imported; fix the file or the profile, or use --ignore-errors to import the remaining rows")

// printProblems lists the rows that could not be used, at most ten of them.
func printProblems(out io.Writer, problems []string) {
	fmt.Fprintf(out, "%d row(s) could not be used:\n", len(problems))
	for i, p := range problems {
		if i == 10 {
			fmt.Fprintf(out, "  ... and %d more\n", len(problems)-10)
			break
		}
		fmt.Fprintf(out, "  %s\n", p)
	}
}

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
			svc, err := a.service(ctx)
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
			acc, err := svc.GetAccount(ctx, account)
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
			parsed, err := importer.Parse(f, profile, acc.Currency)
			if err != nil {
				return err
			}

			var problems []string
			for _, e := range parsed.Errors {
				problems = append(problems, e.Error())
			}
			if len(problems) > 0 && !ignoreErrors {
				printProblems(out, problems)
				return errNothingImported
			}
			res, err := svc.Import(ctx, core.ImportRequest{
				Account:        acc.Name,
				Rows:           parsed.Rows,
				Rules:          cfg.Rules,
				DryRun:         dryRun,
				IgnoreRejected: ignoreErrors,
			})
			for _, r := range res.Rejected {
				problems = append(problems, fmt.Sprintf("line %d: %s", r.Line, r.Reason))
			}
			if errors.Is(err, core.ErrRowsRejected) {
				printProblems(out, problems)
				return errNothingImported
			}
			if err != nil {
				return err
			}
			if len(problems) > 0 {
				printProblems(out, problems)
			}

			if dryRun {
				lines := make([]column, 0, len(res.Rows))
				for _, r := range res.Rows {
					status := "new"
					if r.Duplicate {
						status = "dup"
					}
					category := r.Category
					if r.Transfer != nil {
						category = fmt.Sprintf("⇄ #%d %s", r.Transfer.ID, r.Transfer.Account)
					}
					if r.Adopts != nil {
						category = fmt.Sprintf("↔ #%d %s", r.Adopts.ID, r.Adopts.Payee)
					}
					lines = append(lines, column{
						label:  fmt.Sprintf("%-3s  %s  %-28s  %s", status, r.Row.Date.Format(ledger.DateLayout), truncate(category, 28), truncate(r.Row.Payee, 40)),
						amount: signed(r.Row.Amount),
					})
				}
				printColumns(out, "", lines)
				fmt.Fprintf(out, "\nDry run: %d new, %d already imported, %d pending row(s) ignored. Nothing was written.\n",
					res.Inserted, res.Duplicates, parsed.Skipped)
				if res.Adopted > 0 {
					fmt.Fprintf(out, "%d row(s) match a transaction you entered by hand and would take it over instead of being added (marked ↔).\n", res.Adopted)
				}
				if res.Transfers > 0 {
					fmt.Fprintf(out, "%d new row(s) would be paired as the other half of a transfer with a transaction on another account.\n", res.Transfers)
				}
				return nil
			}

			fmt.Fprintf(out, "Imported %d new transaction(s) into %s; %d already present (skipped); %d pending row(s) ignored.\n",
				res.Inserted, acc.Name, res.Duplicates, parsed.Skipped)
			for _, name := range res.Created {
				fmt.Fprintf(out, "Created new account %s\n", name)
			}
			if res.Adopted > 0 {
				fmt.Fprintf(out, "%d row(s) matched a transaction you entered by hand; it now stands for the bank row and nothing was added twice.\n", res.Adopted)
			}
			if res.Transfers > 0 {
				fmt.Fprintf(out, "%d row(s) were paired as the other half of a transfer with a transaction on another account (%s).\n",
					res.Transfers, ledger.TransferAccount)
			}
			if res.Uncategorized > 0 {
				fmt.Fprintf(out, "%d row(s) in this file matched no rule and are uncategorized; review them with: thaler cli list -a %s\n",
					res.Uncategorized, ledger.UncategorizedExpense)
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
