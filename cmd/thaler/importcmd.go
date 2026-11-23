package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
		Short: "Import a bank export (CSV, OFX, QIF or CAMT.053) into an account",
		Long: `Import an export from your bank into one account: a CSV file, an OFX
statement (also .qfx), a QIF file or a CAMT.053 statement (XML). --profile auto (the default) finds how to
read it from the file itself; name a profile to choose one.

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
			profile, err := findImportProfile(profileName, args[0], cfg.Profiles)
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
				problems = append(problems, a.cli().T("import_line_problem", map[string]any{"Line": e.Line, "Reason": a.cli().Error(e.Err)}))
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
				Source:         filepath.Base(args[0]),
				Profile:        profile.Name,
			})
			for _, r := range res.Rejected {
				problems = append(problems, a.cli().T("import_line_problem", map[string]any{"Line": r.Line, "Reason": a.cli().Error(r.Err)}))
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
				printDryRun(out, res, parsed)
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
			for _, o := range res.Settled {
				fmt.Fprintf(out, "Settled %s of the schedule %s (#%d).\n", o.Date, o.Schedule, o.TransactionID)
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
	cmd.Flags().StringVar(&profileName, "profile", "auto", "how to read the file: \"auto\" finds it from the file; built-in \"sparkasse\" (CSV), \"ofx\", \"qif\" and \"camt\", or a profile defined in config.toml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be imported without writing anything")
	cmd.Flags().BoolVar(&ignoreErrors, "ignore-errors", false, "import the readable rows even if some rows are broken")
	_ = cmd.MarkFlagRequired("account")
	return cmd
}

// printDryRun lists what an import would do with each row, and what it would add up to.
func printDryRun(out io.Writer, res core.ImportResult, parsed *importer.ParseResult) {
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
	if parsed.Format == importer.FormatCSV {
		fmt.Fprintf(out, "The file was read as %s, its columns separated by %s.\n", parsed.Encoding, delimiterName(parsed.Delimiter))
	} else {
		fmt.Fprintf(out, "The file was read as %s, as %s.\n", strings.ToUpper(parsed.Format), parsed.Encoding)
	}
	if res.Adopted > 0 {
		fmt.Fprintf(out, "%d row(s) match a transaction you entered by hand and would take it over instead of being added (marked ↔).\n", res.Adopted)
	}
	if len(res.Settled) > 0 {
		fmt.Fprintf(out, "%d row(s) are the bank's booking of a scheduled transaction and would answer it (see thaler cli schedule due).\n", len(res.Settled))
	}
	if res.Transfers > 0 {
		fmt.Fprintf(out, "%d new row(s) would be paired as the other half of a transfer with a transaction on another account.\n", res.Transfers)
	}
}

// delimiterName is how the dry run names the character between columns.
func delimiterName(d rune) string {
	if d == '\t' {
		return "tabs"
	}
	return fmt.Sprintf("%q", string(d))
}

// findImportProfile is the profile named, or for "auto" the one that fits the file best.
func findImportProfile(name, path string, custom []importer.Profile) (importer.Profile, error) {
	if !strings.EqualFold(strings.TrimSpace(name), "auto") {
		return importer.FindProfile(name, custom)
	}
	f, err := os.Open(path)
	if err != nil {
		return importer.Profile{}, err
	}
	defer f.Close()
	found, ok := importer.Detect(f, custom)
	if !ok {
		return importer.Profile{}, fmt.Errorf("no import profile fits %s; choose one with --profile (available: %s)", filepath.Base(path), strings.Join(importer.ProfileNames(custom), ", "))
	}
	return importer.FindProfile(found, custom)
}
