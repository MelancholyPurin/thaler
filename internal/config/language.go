package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/melancholypurin/thaler/internal/i18n"
)

var (
	languageLine   = regexp.MustCompile(`^(\s*language\s*=\s*)("[^"]*"|'[^']*')(.*)$`)
	languageSample = regexp.MustCompile(`^\s*#\s*language\s*=`)
)

// SetLanguage makes language = "code" the language setting of the config file,
// creating the file from the example first if there is none. Only that one line
// changes: an existing setting is replaced where it stands (its comment kept),
// otherwise the line goes below the commented sample of the setting if the file has
// one, else above the first table, where a top-level key has to be. Everything else,
// comments and rules included, stays as it was. The file is replaced in one step, so
// an interruption leaves the old one.
func SetLanguage(path, example, code string) error {
	if _, ok := i18n.Parse(code); !ok {
		return fmt.Errorf("unknown language %q", code)
	}
	if err := Ensure(path, example); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	line := "language = " + tomlString(code)
	lines := strings.Split(string(body), "\n") // a final newline leaves a last element of ""

	end, sample, replaced := len(lines), -1, false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[") {
			end = i // a key below a table header would belong to the table
			break
		}
		if m := languageLine.FindStringSubmatch(l); m != nil {
			lines[i] = m[1] + tomlString(code) + m[3]
			replaced = true
			break
		}
		if sample < 0 && languageSample.MatchString(l) {
			sample = i
		}
	}
	switch {
	case replaced:
	case sample >= 0:
		lines = insertLines(lines, sample+1, line)
	case end < len(lines):
		lines = insertLines(lines, end, line, "")
	case lines[len(lines)-1] == "":
		lines = insertLines(lines, len(lines)-1, line)
	default:
		lines = append(lines, line, "")
	}
	return replaceFile(path, []byte(strings.Join(lines, "\n")), info.Mode().Perm())
}

func insertLines(lines []string, at int, add ...string) []string {
	out := make([]string, 0, len(lines)+len(add))
	out = append(out, lines[:at]...)
	out = append(out, add...)
	return append(out, lines[at:]...)
}

// replaceFile writes data to a new file next to path and moves it into place.
func replaceFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".thaler-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once it has been renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
