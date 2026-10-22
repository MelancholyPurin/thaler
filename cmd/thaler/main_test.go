package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the command tree with args against a temporary database.
func run(t *testing.T, db string, args ...string) (string, error) {
	t.Helper()
	a := &app{}
	t.Cleanup(a.close)
	root := newRoot(a)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--db", db, "--config", filepath.Join(t.TempDir(), "none.toml")}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestCLIUnderCliCommand(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	if _, err := run(t, db, "cli", "account", "add", "Assets:Bank", "--currency", "EUR"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, db, "cli", "account", "list")
	if err != nil || !strings.Contains(out, "Assets:Bank") {
		t.Errorf("account list = %q, %v", out, err)
	}
}

func TestOldTopLevelCommandsAreGone(t *testing.T) {
	db := filepath.Join(t.TempDir(), "t.db")
	if _, err := run(t, db, "balance"); err == nil {
		t.Error("`thaler balance` must not exist any more; it is `thaler cli balance`")
	}
	if _, err := run(t, db, "tui"); err == nil {
		t.Error("there is no `thaler tui` subcommand; plain `thaler` opens the interface")
	}
}

func TestPlainThalerNeedsATerminal(t *testing.T) {
	// Under `go test` stdin/stdout are not terminals, so the UI refuses to start
	// instead of printing escape codes into a pipe.
	_, err := run(t, filepath.Join(t.TempDir(), "t.db"))
	if err == nil || !strings.Contains(err.Error(), "thaler cli") {
		t.Errorf("err = %v, want a hint to use thaler cli", err)
	}
}
