# Contributing to Thaler

Thaler is built one story at a time, from [ROADMAP.md](ROADMAP.md), by one maintainer. This page says how a change is made here and why the code is cut into the layers it has. The detailed rules live in [`.claude/rules/`](.claude/rules), one topic per file; they are written for people and for Claude Code alike, and where this page and a rule differ, the rule wins and this page is wrong.

## Before you start

You need Go (the version in `go.mod`), `make` and git. `tmux` lets `make smoke` drive the interface, and a C compiler lets `make test-race` run; both are optional locally and run in CI. The linter, govulncheck and GoReleaser are pinned in the `Makefile` and run through `go run`, so nothing else is installed; the first run of each builds it, about a minute.

```sh
make            # lists the targets
make check      # what CI runs, mostly: lint, go.mod check, script tests, tests with the coverage floor, smoke test
make hooks      # once per clone: runs the lint before every git push
```

Run anything by hand against a throwaway database and config, in a folder outside the clone:

```sh
make build
./thaler --db /tmp/t/thaler.db --config /tmp/t/config.toml
```

For anything bigger than a small fix, open an issue first: the order of the work is the roadmap's, and a story may already have decisions behind it.

## How a change is made

- **One story, one branch, one pull request.** A story is one feature with a permanent ID (`TIM-7`, `ENG-8`; see [roadmap.md](.claude/rules/roadmap.md) for the scheme). Branches are `<initials>/<ID>/<path>/<slug>`, for example `ah/SAF-9/internal/core/reminder-backup-age`; work that is no story is `ah/maintenance/<slug>`.
- **The title is `[ID] domain/service: Name`**, for example `[SAF-6] internal/core: Back up automatically before migration`, or `[MAINTENANCE] …`. Pull requests are squashed, the commit takes the pull request's title, and the release notes are written from those titles (`make release-notes`), so the ID must be a real one.
- **`main` takes changes only through a pull request** whose check `Report Result` passed. That one check stands for all the CI jobs, so jobs can change without touching the settings. The settings themselves are in [`.github/scripts/protect-main.sh`](.github/scripts/protect-main.sh).
- **A story merged unfinished goes behind a feature flag**, off unless `config.toml` turns it on ([architecture.md](.claude/rules/architecture.md)).
- **Decisions already taken stay taken** unless the maintainer reopens them: they are listed in [roadmap.md](.claude/rules/roadmap.md) (recurring transactions always ask, macOS and Linux only, three languages, the release train).

A story is done when ([definition-of-done.md](.claude/rules/definition-of-done.md)):

1. `make check` passes, and `make test-race` too after touching anything that runs in the background.
2. New behaviour has tests. A test that guards a safety property has been seen to fail: break the code on purpose once and watch it.
3. The real binary has been run once on a throwaway database. A path that must never break gets a step in [`.github/scripts/smoke.sh`](.github/scripts/smoke.sh).
4. What a person can see is documented: `README.md`, `config.example.toml` for config keys, the `?` help for keys.
5. The story is ticked in `ROADMAP.md`, with a line on what was built and what was decided.

## Conventions

**A vulnerability is reported privately**, never in an issue: see [SECURITY.md](SECURITY.md), which also says what Thaler protects. A change that weakens one of those protections says so in its pull request, and changes that page.

**Your data never goes in the repository** ([data-and-privacy.md](.claude/rules/data-and-privacy.md)). No real transactions, accounts or amounts in code, tests, fixtures, docs or commit messages; `.gitignore` excludes databases, CSV files and `config.toml`, and the one CSV fixture is invented. Try things on a throwaway database, never on the one you use.

**Nothing destructive happens by itself.** A restore keeps the database it replaces, an upgrade copies the database first, pruning lists and then asks. A new feature that removes or overwrites follows the same shape: check, keep the old one, ask, then act.

**Tests** ([go-tests.md](.claude/rules/go-tests.md)) are named after the method (`Test_Prune`, `Test_Prune_encrypted`), always table-driven even with one case, each case in `// given`, `// when`, `// then`, compared with `cmp.Diff(want, got)`, errors included. Contexts come from `t.Context()`. No `TestMain` and no package-wide setup: a slow fixture is built once on first use and copied for each test. Tests never read the clock, the machine's language or the environment. Fuzz tests are `Fuzz_Function`, check a property of what comes back, and the inputs they find are committed.

**Money is `ledger.Money`**, whole minor units in an `int64`; no float touches an amount. A transaction is postings that sum to zero.

**Errors a person sees are coded**: `coded.New("account_not_found", args)`, never English text, in `ledger`, `core`, `importer` and `config` (a test fails on a new `errors.New("…")` there). The code is stable and names a message in the catalog.

**Text is in the catalog.** Everything the interface shows is a message in `internal/i18n/locales/en.json`, with its Spanish and German translations written in the same story; counts use plural forms, never `(s)`; dates and amounts are written through the localizer. Every screen fits 80 by 24 in every language, which a test checks. `thaler cli` is the exception: English only, and not in the catalog.

**Migrations** ([db-migrations.md](.claude/rules/db-migrations.md)) are new `YYYYMMDDHHMMSS_name.up.sql` files, registered in `migrationScriptTimestamps`; never a down migration, never a change to a file already released.

**CI and tools** ([ci.md](.claude/rules/ci.md)): every tool is pinned in the `Makefile`, never `@latest`. A change to documentation only skips the Go jobs.

**Code** matches what is around it: comments say why, not what, and are full sentences.

## The layers, and why they are cut there

```
cmd/thaler          the binary: flags, wiring, the cobra commands (thaler cli …)
internal/tui        the Bubble Tea interface
      │  both see only core.Service and the types that cross it
      ▼
internal/core       the application: every rule, and the only code that touches SQLite
      │
      ├──► internal/config     config.toml: settings, rules, bank profiles
      ├──► internal/importer   reading bank files: CSV, OFX, QIF; rules; dedupe keys
      ▼
internal/ledger     money, currencies, account names, balanced transactions
internal/coded      errors that carry a code instead of a text   (used by all of the above)
internal/i18n       the messages and the localizer               (used by the front ends)
```

The arrows point one way: `ledger` knows nothing of the others, `importer` knows only `ledger`, and nothing below the front ends knows the interface exists. Each cut is there for a reason that is still coming:

- **`ledger` is the bookkeeping, and nothing else.** No files, no database, no language. What makes an amount, a currency or a balanced transaction is the part that must never be wrong, so it is kept where it can be tested alone, exhaustively and by fuzzing, and where the server will use it unchanged.
- **`importer` reads bank files and does not store them.** It turns bytes into rows and leaves deciding what to keep to `core`. That keeps the parsers pure, fuzzable on any bytes, and free of a database they would only need for tests; the dry run of an import is the same parse without the store.
- **`core` is the only code that touches the database**, behind the `Service` interface. The TUI and the CLI depend on that interface and nothing else, so that a client for a server (`M-SERVER`) can replace the local SQLite backend without changing either. The SQL is unexported (`repository.go`), so nothing can write around the rules. A few things work on files rather than through `Service`, restore, decrypt and prune, because they must work when the database does not open.
- **`config` is a file the person owns**, not a table. It is read strictly (an unknown key is an error, not ignored), and the rules editor changes it in place, keeping comments and everything else, with an undo that puts the file back byte for byte.
- **Errors carry codes, not words (`coded`).** `ledger` and `core` decide *what* went wrong; the front end decides how to say it, in the person's language. The server will answer each caller in theirs from the same error.
- **The front ends are thin.** `cmd/thaler` parses flags, calls the service and prints; `internal/tui` draws and handles keys. Neither holds a bookkeeping rule, so the web app and the PWA, when they come, will not have to copy one.
- **Tooling lives beside the product, not in it**: `cmd/releasenotes` and `cmd/i18ntodo` are commands for the maintainer, and `internal/releasenotes` is their logic, with tests.

Two things this costs, on purpose: the TUI calls the service directly from its update loop, which is milliseconds on a local file but would need moving to background commands for a network backend; and the database has one connection, enough for one person. Both are noted in the README's "Decisions worth knowing".

## License

Thaler is under the [GNU Affero General Public License v3.0](LICENSE). A contribution is made under the same licence.
