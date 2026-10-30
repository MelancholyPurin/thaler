# Working on Thaler

Thaler is a local-first personal finance planner: Go, SQLite, a Bubble Tea TUI as the main interface, a Cobra CLI under `thaler cli`. A server, a Next.js web app and a PWA come later. It is AGPL-3.0. The README says what exists; [ROADMAP.md](ROADMAP.md) says what comes next.

The maintainer's rules live in [`.claude/rules/`](.claude/rules), one topic per file, and are loaded with this file. Read the one that applies before you start. When a new rule comes up, add it to the file for its topic, or start a new file, rather than growing this one.

| File | What it covers |
| --- | --- |
| [git-workflow.md](.claude/rules/git-workflow.md) | The maintainer does all the git work; branch names; PR and commit titles |
| [working-style.md](.claude/rules/working-style.md) | How to report, how to decide, pronouns, matching the code around you |
| [definition-of-done.md](.claude/rules/definition-of-done.md) | What has to be true before a task is finished |
| [go-tests.md](.claude/rules/go-tests.md) | How Go tests are written |
| [db-migrations.md](.claude/rules/db-migrations.md) | How database migrations are written |
| [data-and-privacy.md](.claude/rules/data-and-privacy.md) | No financial data in the repo; throwaway databases; nothing destructive by itself |
| [architecture.md](.claude/rules/architecture.md) | The `core.Service` boundary, money, config, the TUI's structure |
| [roadmap.md](.claude/rules/roadmap.md) | The ID scheme, one story at a time, the decisions already taken |
| [ci.md](.claude/rules/ci.md) | The workflows, and pinned tools |

## Commands

`make` lists the targets. The ones you need: `make check` (gofmt, golangci-lint, `go mod tidy` check, script tests, tests in random order: most of what CI runs), `make test-race`, `make vuln`, `make build`.
