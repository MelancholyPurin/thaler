package config

import (
	"slices"

	"github.com/melancholypurin/thaler/internal/coded"
)

// Themes are the colour themes of the interface: "auto" follows the background of the
// terminal, the others are for a terminal that does not say what its background is, or
// whose person prefers the other.
var Themes = []string{"auto", "dark", "light"}

// ValidTheme says whether name is one of Themes.
func ValidTheme(name string) bool { return slices.Contains(Themes, name) }

// SetTheme makes theme = "name" the theme setting of the config file, changing only that
// line (see setString).
func SetTheme(path, example, name string) error {
	if !ValidTheme(name) {
		return coded.New("unknown_theme", map[string]any{"Name": name})
	}
	return setString(path, example, "theme", name)
}
