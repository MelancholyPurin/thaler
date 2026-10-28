package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
)

func (a *app) backupCmd() *cobra.Command {
	var encrypt bool
	var passFile string
	cmd := &cobra.Command{
		Use:   "backup [DEST]",
		Short: "Write a consistent snapshot of the database",
		Long: `Write a snapshot of the database using SQLite's VACUUM INTO, which is safe to
run at any time, then open it and check it. Without DEST the file goes to a
backups/ folder next to the database, named by timestamp.

The snapshot is a plain SQLite file and is NOT encrypted, unless you ask:
--encrypt asks for a passphrase (twice) and writes an age file, and
--passphrase-file reads the passphrase from the first line of a file, for
scripts. An encrypted backup ends in .age. Lose the passphrase and it cannot be
opened; open it again with "thaler cli decrypt".`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pass, err := a.newPassphrase(encrypt, passFile)
			if err != nil {
				return err
			}
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			dest := filepath.Join(config.BackupDir(a.dbPath()), config.BackupName(time.Now()))
			if len(args) == 1 {
				dest = args[0]
			}
			if pass == "" {
				if err := svc.Backup(ctx, dest); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Backup written to %s (not encrypted)\n", dest)
				return nil
			}
			dest = config.EncryptedName(dest)
			if err := svc.BackupEncrypted(ctx, dest, pass); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Encrypted backup written to %s\n", dest)
			return nil
		},
	}
	cmd.Flags().BoolVar(&encrypt, "encrypt", false, "encrypt the backup with a passphrase asked for on the terminal")
	cmd.Flags().StringVar(&passFile, "passphrase-file", "", "encrypt the backup with the passphrase on the first line of this file")
	return cmd
}

func (a *app) decryptCmd() *cobra.Command {
	var passFile string
	cmd := &cobra.Command{
		Use:   "decrypt SRC [DEST]",
		Short: "Open an encrypted backup as a database file",
		Long: `Decrypt a backup made with "thaler cli backup --encrypt" into a plain SQLite
file and check that it is a healthy database. Without DEST the file is SRC
without its .age ending. An existing file is never overwritten. To use the
result, point Thaler at it: thaler --db DEST. The passphrase is asked for on
the terminal, or read from the first line of --passphrase-file.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := args[0]
			dest := strings.TrimSuffix(src, config.EncryptedSuffix)
			if len(args) == 2 {
				dest = args[1]
			}
			if len(args) == 1 && dest == src {
				return fmt.Errorf("%s does not end in %s: say where to write the result", src, config.EncryptedSuffix)
			}
			var pass string
			var err error
			if passFile != "" {
				pass, err = readPassphraseFile(passFile)
			} else {
				pass, err = a.passphrase("Passphrase: ")
			}
			if err != nil {
				return err
			}
			if err := core.DecryptBackup(cmd.Context(), src, dest, pass); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Decrypted to %s (use it with: thaler --db %s)\n", dest, dest)
			return nil
		},
	}
	cmd.Flags().StringVar(&passFile, "passphrase-file", "", "read the passphrase from the first line of this file")
	return cmd
}

func (a *app) restoreCmd() *cobra.Command {
	var passFile string
	cmd := &cobra.Command{
		Use:   "restore SRC",
		Short: "Replace the database with a backup",
		Long: `Replace the database with the backup SRC, a plain backup or an encrypted one
(the passphrase is asked for on the terminal, or read from the first line of
--passphrase-file). The backup is read and checked first, so one that is damaged,
not a Thaler database or made by a newer version is refused with nothing changed.
The database it replaces is never deleted: it is moved to backups/replaced next to
the database, and running "thaler cli restore" on that file undoes the restore.

Close every other Thaler before restoring. A backup from an older version is
upgraded, after a copy of it, the next time Thaler starts.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Not a.service(): restoring has to work when the database in place is
			// damaged, and opening it would upgrade one that is about to be replaced.
			src, dbPath := args[0], a.dbPath()
			encrypted, err := core.BackupIsEncrypted(src)
			if err != nil {
				return err
			}
			var pass string
			switch {
			case !encrypted:
			case passFile != "":
				pass, err = readPassphraseFile(passFile)
			default:
				pass, err = a.passphrase("Passphrase: ")
			}
			if err != nil {
				return err
			}
			res, err := core.RestoreBackup(cmd.Context(), src, dbPath, pass)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), restoreMessage(src, dbPath, a.dbFlag != "", res))
			return nil
		},
	}
	cmd.Flags().StringVar(&passFile, "passphrase-file", "", "read the passphrase of an encrypted backup from the first line of this file")
	return cmd
}

// restoreMessage says what a restore did, and how to undo it. customDB is true
// when the database was named with --db, which the undo command then has to repeat.
func restoreMessage(src, dbPath string, customDB bool, res core.RestoreResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Restored %s as %s\n", src, dbPath)
	if res.Replaced == "" {
		b.WriteString("There was no database to replace.\n")
	} else {
		undo := "thaler cli restore " + res.Replaced
		if customDB {
			undo = "thaler --db " + dbPath + " cli restore " + res.Replaced
		}
		fmt.Fprintf(&b, "The database it replaced is kept at %s\nTo undo this, run: %s\n", res.Replaced, undo)
	}
	if res.Upgrade {
		b.WriteString("It was made by an older version of Thaler, so it is upgraded, after a copy of it, the next time Thaler starts.\n")
	}
	return b.String()
}

// newPassphrase is the passphrase a backup is to be encrypted with: from the
// file, or asked for twice, or "" when it is not to be encrypted.
func (a *app) newPassphrase(encrypt bool, file string) (string, error) {
	switch {
	case file != "":
		return readPassphraseFile(file)
	case !encrypt:
		return "", nil
	}
	first, err := a.passphrase("Passphrase: ")
	if err != nil {
		return "", err
	}
	if first == "" {
		return "", errors.New("the passphrase is empty")
	}
	again, err := a.passphrase("Repeat: ")
	if err != nil {
		return "", err
	}
	if first != again {
		return "", errors.New("the two passphrases differ")
	}
	return first, nil
}

func (a *app) passphrase(prompt string) (string, error) {
	if a.ask != nil {
		return a.ask(prompt)
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		return "", errors.New("a passphrase is asked for on a terminal; for scripts use --passphrase-file")
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(os.Stdin.Fd())
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

// readPassphraseFile is the first line of the file, without its line ending.
func readPassphraseFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(b), "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return "", fmt.Errorf("%s has no passphrase on its first line", path)
	}
	return line, nil
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
