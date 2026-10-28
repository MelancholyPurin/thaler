package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler"
	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/tui"
)

// runTUI is what plain "thaler" does: open the interactive interface.
func (a *app) runTUI(cmd *cobra.Command, args []string) error {
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return errors.New("the interactive interface needs a terminal; for scripts use: thaler cli --help")
	}
	ctx := cmd.Context()
	svc, err := a.service(ctx)
	if err != nil {
		return err
	}
	return tui.Run(ctx, svc, tui.Options{
		LoadConfig:     a.loadConfig,
		ConfigPath:     a.configPath(),
		ConfigTemplate: thaler.ConfigExample,
		DatabasePath:   a.dbPath(),
		BackupDir:      config.BackupDir(a.dbPath()),
		Restore:        a.swapDatabase,
	})
}

// swapDatabase is how the interface restores a backup: it lets go of the open
// database, restores, and opens whatever is in place afterwards, which is the old
// database again if the restore was refused. The service it returns is the one
// to close at exit. Nothing is printed: the interface owns the screen.
func (a *app) swapDatabase(ctx context.Context, src, passphrase string) (core.Service, core.RestoreResult, error) {
	if a.svc != nil {
		_ = a.svc.Close()
		a.svc = nil
	}
	res, restoreErr := core.RestoreBackup(ctx, src, a.dbPath(), passphrase)
	svc, err := core.Open(ctx, a.dbPath())
	if err != nil {
		if restoreErr != nil {
			return nil, res, fmt.Errorf("%w; the database cannot be opened again either: %w", restoreErr, err)
		}
		return nil, res, fmt.Errorf("the backup is in place but cannot be opened: %w", err)
	}
	a.svc = svc
	return svc, res, restoreErr
}
