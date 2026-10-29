package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/melancholypurin/thaler/internal/config"
)

// BackupFile is a backup found in a folder.
type BackupFile struct {
	Path string
	Size int64
	When time.Time // last modified, which is how "new" and "old" are judged
	// Encrypted says the file is an age file, going by its ending (see
	// config.EncryptedSuffix); BackupIsEncrypted looks inside it instead.
	Encrypted bool
}

// ListBackups is the backups directly in dir, newest first: the files named like
// the ones Backup writes, plain and encrypted (see config.BackupGlobs). Other
// files and folders are not listed. A folder that is not there has none.
func ListBackups(dir string) ([]BackupFile, error) {
	var files []BackupFile
	for _, glob := range config.BackupGlobs {
		paths, err := filepath.Glob(filepath.Join(dir, glob))
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			info, err := os.Stat(p)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if info.Mode().IsRegular() {
				files = append(files, BackupFile{
					Path: p, Size: info.Size(), When: info.ModTime(),
					Encrypted: strings.HasSuffix(p, config.EncryptedSuffix),
				})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].When.Equal(files[j].When) {
			return files[i].When.After(files[j].When)
		}
		return files[i].Path > files[j].Path
	})
	return files, nil
}

// isBackupName says whether a file is named like a backup (see config.BackupGlobs).
func isBackupName(path string) bool {
	for _, glob := range config.BackupGlobs {
		if ok, _ := filepath.Match(glob, filepath.Base(path)); ok {
			return true
		}
	}
	return false
}
