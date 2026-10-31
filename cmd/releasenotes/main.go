// Command releasenotes prints what a release changed, from the titles of the
// commits between two points of the history (see package releasenotes).
//
//	releasenotes -version v0.3.0 -date 2026-11-16 v0.2.0..HEAD
//	releasenotes -row -version v0.3.0 -date 2026-11-16 v0.2.0..HEAD
//
// The first prints the notes; the second the line for the "Releases" table of
// ROADMAP.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/melancholypurin/thaler/internal/releasenotes"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("releasenotes", flag.ContinueOnError)
	version := fs.String("version", "", "the version being released, for example v0.3.0 (required)")
	date := fs.String("date", "", "the day it leaves, YYYY-MM-DD (required)")
	roadmap := fs.String("roadmap", "ROADMAP.md", "the roadmap file, which names the epics (read from the current folder, not from -C)")
	dir := fs.String("C", ".", "the repository to read the history of")
	row := fs.Bool("row", false, "print the line for the Releases table of the roadmap instead of the notes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" || *date == "" || fs.NArg() != 1 {
		return fmt.Errorf("usage: releasenotes -version VERSION -date YYYY-MM-DD [-row] [-roadmap FILE] FROM..TO")
	}
	text, err := os.ReadFile(*roadmap)
	if err != nil {
		return err
	}
	epics := releasenotes.ParseEpics(string(text))
	if len(epics) == 0 {
		return fmt.Errorf("%s has no table of epic keys", *roadmap)
	}
	// The squash-merged commits of main, oldest first: one per pull request.
	cmd := exec.CommandContext(ctx, "git", "log", "--first-parent", "--reverse", "--format=%s", fs.Arg(0)) //nolint:gosec // the range is the maintainer's own argument
	cmd.Dir = *dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git log %s: %w", fs.Arg(0), err)
	}
	var titles []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			titles = append(titles, line)
		}
	}
	if *row {
		fmt.Fprintln(stdout, releasenotes.Row(*version, *date, titles, epics))
		return nil
	}
	_, err = io.WriteString(stdout, releasenotes.Notes(*version, *date, titles, epics))
	return err
}
