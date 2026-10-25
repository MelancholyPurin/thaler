package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestEnsureCreatesAnInertConfig(t *testing.T) {
	example, err := os.ReadFile("../../config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "thaler", "config.toml") // the folder does not exist yet
	if err := Ensure(path, string(example)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat = %v, %v; want a private file", info, err)
	}
	// The sample rules are there to read, but switched off.
	c, err := Load(path, true)
	if err != nil {
		t.Fatalf("the generated file must load: %v", err)
	}
	if len(c.Rules) != 0 || len(c.Profiles) != 0 || c.ImportDir != "" {
		t.Errorf("a new config must change nothing, got %+v", c)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `# contains = "REWE"`) || !strings.Contains(string(body), "# Created by Thaler") {
		t.Errorf("the sample rules should be kept as comments:\n%s", body)
	}

	// An existing file is never touched.
	if err := os.WriteFile(path, []byte("import_dir = \"/x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(path, string(example)); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(path, true); err != nil || c.ImportDir != "/x" {
		t.Errorf("Ensure overwrote an existing config: %+v, %v", c, err)
	}
}
