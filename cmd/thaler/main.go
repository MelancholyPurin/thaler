// Command thaler is a local-first personal finance planner.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/config"
	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
)

// app holds state shared by all commands. The database and config file are
// opened lazily so that --help and shell completion never create files.
type app struct {
	// version is what --version prints; empty only in tests that do not ask.
	version string
	// loc says the messages in the language of the person; see localizer.
	loc *i18n.Localizer
	// getenv is os.Getenv, or a stand-in in a test (the language comes from the
	// environment unless config.toml sets it).
	getenv  func(string) string
	dbFlag  string
	cfgFlag string
	svc     core.Service
	cfg     *config.Config
	// ask reads a passphrase; nil means from the terminal. Tests replace it.
	ask func(prompt string) (string, error)
	// notice receives messages that are not a command's result; nil means
	// standard error. Tests replace it.
	notice io.Writer
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
	if copied := svc.PreMigrationBackup(); copied != "" {
		fmt.Fprintln(a.noticeOut(), a.localizer().T("upgrade_notice", map[string]any{"Copy": copied}))
	}
	a.svc = svc
	return svc, nil
}

// localizer is the language of the messages: config.toml, else the environment,
// else English. A config file that cannot be read does not stop the messages: the
// command that needs it reports that itself.
func (a *app) localizer() *i18n.Localizer {
	if a.loc != nil {
		return a.loc
	}
	setting := ""
	if c, err := a.config(); err == nil {
		setting = c.Language
	}
	getenv := a.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	a.loc = i18n.New(i18n.Detect(setting, getenv))
	return a.loc
}

// noticeOut is where messages that are not a command's result go: standard error.
func (a *app) noticeOut() io.Writer {
	if a.notice != nil {
		return a.notice
	}
	return os.Stderr
}

func (a *app) config() (*config.Config, error) {
	if a.cfg != nil {
		return a.cfg, nil
	}
	c, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	a.cfg = c
	return c, nil
}

// loadConfig reads the config file afresh. The TUI uses it so that rules edited
// while it is open apply to the next import.
func (a *app) loadConfig() (*config.Config, error) {
	return config.Load(a.configPath(), a.cfgFlag != "")
}

// configPath is the --config file, or else the default location.
func (a *app) configPath() string {
	if a.cfgFlag != "" {
		return a.cfgFlag
	}
	return config.DefaultConfigPath()
}

func (a *app) close() {
	if a.svc != nil {
		_ = a.svc.Close()
	}
}

// errorLine is what main prints when a command fails: the error in the language of
// the person, a coded error of the service looked up in the catalog.
func (a *app) errorLine(err error) string {
	return "error: " + a.localizer().Error(err)
}

func main() {
	a := &app{version: currentVersion()}
	err := newRoot(a).ExecuteContext(context.Background())
	a.close()
	if err != nil {
		fmt.Fprintln(os.Stderr, a.errorLine(err))
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
		Version:       a.version,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          a.runTUI,
	}
	root.SetVersionTemplate("thaler {{.Version}}\n")
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
		a.decryptCmd(),
		a.restoreCmd(),
		a.pruneCmd(),
		a.checkCmd(),
	)
	root.AddCommand(cli)
	return root
}
