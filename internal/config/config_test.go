package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadImportDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "import_dir = \"~/Documents/bank\"\n\n[[rule]]\ncontains = \"REWE\"\ncategory = \"Expenses:Groceries\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.ImportDir != "~/Documents/bank" || len(c.Rules) != 1 {
		t.Errorf("config = %+v", c)
	}

	// A typo must be reported, not ignored.
	if err := os.WriteFile(path, []byte("import_folder = \"/tmp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, true); err == nil {
		t.Error("an unknown key must be rejected")
	}
}
