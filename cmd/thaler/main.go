// Command thaler is a local-first personal finance planner.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
)

// app holds state shared by all commands. The database and config file are
// opened lazily so that --help and shell completion never create files.
type app struct {
	dbFlag  string
	cfgFlag string
	svc     core.Service
	cfg     *config.Config
}

func (a *app) dbPath() string {
	if a.dbFlag != "" {
		return a.dbFlag
	}
	return config.DefaultDBPath()
}

// service opens the database on first use. Everything the commands do goes
// through core.Service, so they work the same against a remote backend later.
func (a *app) service(ctx context.Context) (core.Service, error) {
	if a.svc != nil {
		return a.svc, nil
	}
	svc, err := core.Open(ctx, a.dbPath())
	if err != nil {
		return nil, err
	}
	a.svc = svc
	return svc, nil
}

func (a *app) config() (*config.Config, error) {
	if a.cfg != nil {
		return a.cfg, nil
	}
	path, explicit := a.cfgFlag, a.cfgFlag != ""
	if !explicit {
		path = config.DefaultConfigPath()
	}
	c, err := config.Load(path, explicit)
	if err != nil {
		return nil, err
	}
	a.cfg = c
	return c, nil
}

func (a *app) close() {
	if a.svc != nil {
		_ = a.svc.Close()
	}
}

func main() {
	a := &app{}
	err := newRoot(a).ExecuteContext(context.Background())
	a.close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// newRoot builds the command tree. Run without arguments, thaler opens the
// interactive interface; everything scriptable lives under "thaler cli".
func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "thaler",
		Short: "Local-first personal finance planner",
		Long: `Local-first personal finance planner.

Run without arguments to open the interactive interface. For scripting, cron
and one-off commands use "thaler cli"; see "thaler cli --help".`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          a.runTUI,
	}
	root.PersistentFlags().StringVar(&a.dbFlag, "db", "", "path to the SQLite database (default: $THALER_DB or ~/.local/share/thaler/thaler.db)")
	root.PersistentFlags().StringVar(&a.cfgFlag, "config", "", "path to config.toml (default: ~/.config/thaler/config.toml)")

	cli := &cobra.Command{
		Use:   "cli",
		Short: "Scriptable command-line interface (accounts, import, balance, ...)",
	}
	cli.AddCommand(
		a.accountCmd(),
		a.addCmd(),
		a.transferCmd(),
		a.listCmd(),
		a.importCmd(),
		a.categoryCmd(),
		a.recategorizeCmd(),
		a.balanceCmd(),
		a.currencyCmd(),
		a.rateCmd(),
		a.backupCmd(),
		a.checkCmd(),
	)
	root.AddCommand(cli)
	return root
}
