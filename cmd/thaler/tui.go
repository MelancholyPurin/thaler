package main

import (
	"errors"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler"
	"github.com/melancholypurin/thaler/internal/config"
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
	})
}
