package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/melancholypurin/thaler/internal/config"
)

// Pruning removes the oldest backups so that they do not pile up. It is in two
// steps, a plan and then the removal of what the plan lists, so that a person can
// be shown exactly what would go and agree to it: nothing here runs by itself.
//
// Three folders hold files that Thaler wrote: the backups you make, the
// databases that restores replaced, and the copies made before upgrades. In each
// the newest few are kept; only files named like a backup are ever listed, so
// anything else a person put there stays.

// PruneFolder is what a plan would remove from one folder.
type PruneFolder struct {
	// Label says what the folder holds, for a person to read.
	Label string
	Dir   string
	// Total is how many backups the folder holds, Remove the oldest of them.
	Total  int
	Remove []BackupFile // newest first
}

// PrunePlan is what would be removed, folder by folder. A folder with nothing to
// remove is left out.
type PrunePlan struct {
	Keep    int
	Folders []PruneFolder
}

// Count is how many files the plan removes.
func (p PrunePlan) Count() int {
	n := 0
	for _, f := range p.Folders {
		n += len(f.Remove)
	}
	return n
}

// Size is how many bytes the plan frees.
func (p PrunePlan) Size() int64 {
	var n int64
	for _, f := range p.Folders {
		for _, file := range f.Remove {
			n += file.Size
		}
	}
	return n
}

// PlanPrune lists the backups next to the database at dbPath that are not among
// the newest keep of their folder. keep must be at least 1: pruning never removes
// every backup.
func PlanPrune(dbPath string, keep int) (PrunePlan, error) {
	if keep < 1 {
		return PrunePlan{}, fmt.Errorf("keep %d backups: it must be at least 1", keep)
	}
	plan := PrunePlan{Keep: keep}
	for _, f := range []PruneFolder{
		{Label: "backups", Dir: config.BackupDir(dbPath)},
		{Label: "replaced by a restore", Dir: config.ReplacedDir(dbPath)},
		{Label: "copies before an upgrade", Dir: config.PreMigrationDir(dbPath)},
	} {
		files, err := ListBackups(f.Dir)
		if err != nil {
			return PrunePlan{}, err
		}
		f.Total = len(files)
		if len(files) <= keep {
			continue
		}
		f.Remove = files[keep:]
		plan.Folders = append(plan.Folders, f)
	}
	return plan, nil
}

// Prune removes the files of a plan, and the -wal and -shm files that may lie
// beside a database kept by a restore. It returns how many files it removed. It
// stops at the first one it cannot remove, and a file that is already gone counts
// as removed. A path that is not named like a backup is refused before anything
// is removed, so a plan that was altered cannot reach other files.
func Prune(plan PrunePlan) (int, error) {
	for _, f := range plan.Folders {
		for _, file := range f.Remove {
			if !isBackupName(file.Path) {
				return 0, fmt.Errorf("%s is not named like a backup: nothing was removed", file.Path)
			}
		}
	}
	removed := 0
	for _, f := range plan.Folders {
		for _, file := range f.Remove {
			if err := os.Remove(file.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return removed, fmt.Errorf("removed %d of %d: %w", removed, plan.Count(), err)
			}
			for _, side := range []string{"-wal", "-shm"} {
				if err := os.Remove(file.Path + side); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return removed, fmt.Errorf("removed %d of %d: %w", removed, plan.Count(), err)
				}
			}
			removed++
		}
	}
	return removed, nil
}
