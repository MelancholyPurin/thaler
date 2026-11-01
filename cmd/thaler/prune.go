package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/core"
	"github.com/melancholypurin/thaler/internal/i18n"
	"github.com/melancholypurin/thaler/internal/units"
)

func (a *app) pruneCmd() *cobra.Command {
	var keep int
	var yes bool
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "List the old backups, and remove them with --yes",
		Long: `Keep the newest backups in each backups folder next to the database and
remove the older ones. The same number applies to three folders: the backups you
make, the databases that restores replaced (backups/replaced) and the copies made
before upgrades (backups/pre-migration). Only files named like backups are
touched; backups you wrote elsewhere never are.

Without --yes nothing is removed: the files that would be are listed. The number
to keep is --keep, or keep_backups in config.toml (default 10).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Not a.service(): this only looks at files, and has to work when the
			// database does not open.
			if !cmd.Flags().Changed("keep") {
				cfg, err := a.config()
				if err != nil {
					return err
				}
				keep = cfg.KeepCount()
				if keep == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "keep_backups is 0 in config.toml: all backups are kept. Say how many to keep with --keep N.")
					return nil
				}
			}
			plan, err := core.PlanPrune(a.dbPath(), keep)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if plan.Count() == 0 {
				fmt.Fprintln(out, a.localizer().T("nothing_to_remove", map[string]any{"Count": keep}))
				return nil
			}
			describe(out, a.localizer(), plan)
			if !yes {
				fmt.Fprintln(out, "\nNothing was removed. Run it again with --yes to remove these.")
				return nil
			}
			removed, err := core.Prune(plan)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "\n"+a.localizer().T("backups_removed", map[string]any{"Count": removed, "Size": units.HumanSize(plan.Size())}))
			return nil
		},
	}
	cmd.Flags().IntVar(&keep, "keep", 0, "how many of the newest backups to keep in each folder (default: keep_backups in config.toml, else 10)")
	cmd.Flags().BoolVar(&yes, "yes", false, "remove them, instead of only listing them")
	return cmd
}

// describe lists what a plan removes, folder by folder.
func describe(w io.Writer, loc *i18n.Localizer, plan core.PrunePlan) {
	fmt.Fprintln(w, loc.T("prune_listing", map[string]any{"Keep": plan.Keep, "Count": plan.Count(), "Size": units.HumanSize(plan.Size())}))
	for _, f := range plan.Folders {
		fmt.Fprintf(w, "\n%s: %s, %d of %d\n", loc.BackupFolder(f.Kind), f.Dir, len(f.Remove), f.Total)
		for _, file := range f.Remove {
			fmt.Fprintf(w, "  %s  %s  %s\n", file.When.Local().Format("2006-01-02 15:04"), filepath.Base(file.Path), units.HumanSize(file.Size))
		}
	}
}
