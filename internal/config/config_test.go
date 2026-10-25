package config

import (
	"github.com/melancholypurin/thaler/internal/importer"
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

func TestAppendRuleKeepsEverythingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "# my rules, carefully ordered\nimport_dir = \"/x\"\n\n[[rule]]\ncontains = \"REWE\"   # the big one\ncategory = \"Expenses:Groceries\""
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil { // no newline at the end on purpose
		t.Fatal(err)
	}
	if err := AppendRule(path, "", importer.Rule{Contains: "  Lidl ", Category: "Expenses:Groceries"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(body), original+"\n") {
		t.Errorf("the existing text must be untouched, got:\n%s", body)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatalf("the result must still load: %v\n%s", err, body)
	}
	if len(c.Rules) != 2 || c.Rules[0].Contains != "REWE" || c.Rules[1].Contains != "Lidl" || c.Rules[1].Category != "Expenses:Groceries" || c.ImportDir != "/x" {
		t.Errorf("config = %+v; the new rule must come last", c)
	}

	// Several in a row stay in order.
	for _, name := range []string{"Aldi", "Edeka"} {
		if err := AppendRule(path, "", importer.Rule{Contains: name, Category: "Expenses:Groceries"}); err != nil {
			t.Fatal(err)
		}
	}
	if c, _ := Load(path, true); len(c.Rules) != 4 || c.Rules[3].Contains != "Edeka" {
		t.Errorf("rules = %+v", c.Rules)
	}
}

func TestAppendRuleEscapesWhateverTheBankSends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, text := range []string{
		`O'Reilly "Books" & Co`, `C:\Temp\x`, "Bäckerei Müller – Straße", "tab\there", "ctrl\x01char", "ends with a backslash \\", "[[rule]] fake",
	} {
		if err := AppendRule(path, "", importer.Rule{Contains: text, Category: "Expenses:Misc"}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Load(path, true)
	if err != nil {
		body, _ := os.ReadFile(path)
		t.Fatalf("the file must stay valid TOML: %v\n%s", err, body)
	}
	if len(c.Rules) != 7 {
		t.Fatalf("%d rules, want 7", len(c.Rules))
	}
	for i, want := range []string{`O'Reilly "Books" & Co`, `C:\Temp\x`, "Bäckerei Müller – Straße", "tab\there", "ctrl\x01char", "ends with a backslash \\", "[[rule]] fake"} {
		if c.Rules[i].Contains != want {
			t.Errorf("rule %d = %q, want %q", i, c.Rules[i].Contains, want)
		}
	}
}

func TestAppendRuleCreatesAndRefuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "config.toml")
	if err := AppendRule(path, "# sample\n[[rule]]\ncontains = \"X\"\ncategory = \"Expenses:X\"\n", importer.Rule{Contains: "Lidl", Category: "Expenses:Groceries"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil || len(c.Rules) != 1 || c.Rules[0].Contains != "Lidl" {
		t.Errorf("a new file starts from the template with its samples switched off: %+v, %v", c, err)
	}
	for name, r := range map[string]importer.Rule{
		"no text":     {Category: "Expenses:X"},
		"blank text":  {Contains: "  ", Category: "Expenses:X"},
		"no category": {Contains: "x"},
		"a regex":     {Regex: "x", Category: "Expenses:X"},
	} {
		if err := AppendRule(path, "", r); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if c, _ := Load(path, true); len(c.Rules) != 1 {
		t.Error("a refused rule must write nothing")
	}
}
