package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/config"
)

func (a *app) backupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup [DEST]",
		Short: "Write a consistent snapshot of the database",
		Long: `Write a snapshot of the database using SQLite's VACUUM INTO, which is safe to
run at any time. Without DEST the file goes to a backups/ folder next to the
database, named by timestamp. The snapshot is a plain SQLite file and is NOT
encrypted: keep it on an encrypted disk or encrypt it yourself (for example
with age) before copying it anywhere else.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			dest := filepath.Join(config.BackupDir(a.dbPath()), config.BackupName(time.Now()))
			if len(args) == 1 {
				dest = args[0]
			}
			if err := svc.Backup(ctx, dest); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup written to %s (not encrypted)\n", dest)
			return nil
		},
	}
	return cmd
}

func (a *app) checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Verify that the ledger is consistent",
		Long: `Check that SQLite reports no corruption, that every transaction balances in
every currency, and that no posting is in a different currency than its account.
Exits with an error if a problem is found.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			problems, err := svc.Check(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(problems) == 0 {
				fmt.Fprintln(out, "OK: the ledger is consistent.")
				return nil
			}
			for _, p := range problems {
				fmt.Fprintln(out, "problem:", p)
			}
			return fmt.Errorf("%d problem(s) found", len(problems))
		},
	}
}
