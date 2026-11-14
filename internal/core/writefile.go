package core

import (
	"errors"
	"os"

	"github.com/melancholypurin/thaler/internal/coded"
)

// WriteNewFile writes data to a file that is not there yet, readable by you alone as
// the figures are your money's. A file that is there is never overwritten: it is
// refused with export_exists. A write that fails leaves no file behind.
func WriteNewFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return coded.New("export_exists", map[string]any{"Path": path})
	}
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
	}
	return err
}
