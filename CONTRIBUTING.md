# Contributing to Thaler

Thaler is built one story at a time, from [ROADMAP.md](ROADMAP.md), by one maintainer. This page says how a change is made here and why the code is cut into the layers it has. The detailed rules live in [`.claude/rules/`](.claude/rules), one topic per file; they are written for people and for Claude Code alike, and where this page and a rule differ, the rule wins and this page is wrong.

## Before you start

You need Go (the version in `go.mod`), `make` and git. `tmux` lets `make smoke` drive the interface, and a C compiler lets `make test-race` run; both are optional locally and run in CI. The linter, govulncheck, GoReleaser and cosign are pinned in the `Makefile` and run through `go run` (cosign is built into `bin/`, since GoReleaser runs it by name), so nothing else is installed; the first run of each builds it, about a minute.

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

## The make targets and CI

`make` lists them all. The ones you will use:

| Target | What it does |
| --- | --- |
| `make check` | lint, the `go.mod` check, the script tests, the translations check, the tests in random order with the coverage floor, and the smoke test: most of what CI runs. A failing test prints the seed to repeat the order with `-shuffle=SEED`. |
| `make lint` | gofmt and golangci-lint, the pinned version with `.golangci.yml`; the same checks as CI. |
| `make tidy-check` | fails when `go.mod` or `go.sum` need a `go mod tidy`. |
| `make test` | the tests in random order, without coverage. |
| `make check-coverage` | the tests with coverage per package, failing when the total is under `COVERAGE_FLOOR` in the `Makefile`. |
| `make test-race` | the tests with the race detector. |
| `make fuzz` | feeds the amount, import and config parsers inputs it makes up, `FUZZTIME` each (default a minute), checking that what they accept is read right; an input that fails is kept in the package's `testdata/fuzz` and runs with the tests from then on. |
| `make vuln` | govulncheck against the dependencies and the standard library; needs the network. |
| `make smoke` | builds the binary and runs it through a backup, a restore, the interface and more on throwaway databases; `FROM=<tag>` also checks that a database of that release opens in this build. |
| `make translations-check` | fails when a Spanish or German page is behind its English (see Translating). |
| `make hooks` | runs `make lint` and `make tidy-check` before every `git push`; `git push --no-verify` skips it once. |
| `make build` | builds `./thaler` with its version from git. |
| `make release-check` | checks `.goreleaser.yaml` and builds the macOS and Linux archives into `dist/` without publishing; it skips the signing, which needs the Release workflow's identity. |
| `make release-verify` | checks a release in `dist/` (or `DIR`) the way a download is checked: `checksums.txt` signed by the Release workflow, every archive matching it. |
| `make release-notes` | the release notes of the commits in a range, grouped by epic from the `[ID]` in their titles (see the release train in the roadmap). |

**CI** first looks at which files a change touches (`.github/scripts/changed-paths.sh`, through the reusable workflow `changes.yml`). When only documentation and similar files changed (Markdown, `LICENSE`, `.gitignore`, `docs/`, `dependabot.yml`; not `docs/config*.md`, which a Go test checks) the Go jobs are skipped; any other file, including everything compiled in or configuring the build, runs them: lint with the `go.mod` check, the tests on Linux and macOS with the smoke test, and the race detector, in parallel. The translations check runs for every change. The last job, `Report Result`, passes when the translations are up to date and every Go job passed, or was rightly skipped, and fails in every other case; branch protection requires that one, so jobs can change without touching the settings. `make test-scripts` tests the scripts CI uses. govulncheck runs on every push, on every pull request that is not documentation only, and once a week, since advisories appear without any change here. Every workflow uses the same Go, the newest patch of the minor version in `go.mod`, which is what releases are built with. Dependabot opens a weekly pull request for the Go dependencies and one for the GitHub Actions; the tools pinned in the `Makefile` are bumped by hand.

## Conventions

**A vulnerability is reported privately**, never in an issue: see [SECURITY.md](SECURITY.md), which also says what Thaler protects. A change that weakens one of those protections says so in its pull request, and changes that page.

**Your data never goes in the repository** ([data-and-privacy.md](.claude/rules/data-and-privacy.md)). No real transactions, accounts or amounts in code, tests, fixtures, docs or commit messages; `.gitignore` excludes databases, CSV files and `config.toml`, and the one CSV fixture is invented. Try things on a throwaway database, never on the one you use.

**Nothing destructive happens by itself.** A restore keeps the database it replaces, an upgrade copies the database first, pruning lists and then asks. A new feature that removes or overwrites follows the same shape: check, keep the old one, ask, then act.

**Tests** ([go-tests.md](.claude/rules/go-tests.md)) are named after the method (`Test_Prune`, `Test_Prune_encrypted`), always table-driven even with one case, each case in `// given`, `// when`, `// then`, compared with `cmp.Diff(want, got)`, errors included. Contexts come from `t.Context()`. No `TestMain` and no package-wide setup: a slow fixture is built once on first use and copied for each test. Tests never read the clock, the machine's language or the environment. Fuzz tests are `Fuzz_Function`, check a property of what comes back, and the inputs they find are committed.

**Money is `ledger.Money`**, whole minor units in an `int64`; no float touches an amount. A transaction is postings that sum to zero.

**Errors a person sees are coded**: `coded.New("account_not_found", args)`, never English text, in `ledger`, `core`, `importer` and `config` (a test fails on a new `errors.New("…")` there). The code is stable and names a message in the catalog.

**Text is in the catalog** (see Translating). Everything the interface shows is a message in `internal/i18n/locales/en.json`, with its Spanish and German translations written in the same story; counts use plural forms, never `(s)`; dates and amounts are written through the localizer. Every screen fits 80 by 24 in every language, which a test checks. `thaler cli` is the exception: English only, and not in the catalog.

**Migrations** ([db-migrations.md](.claude/rules/db-migrations.md)) are new `YYYYMMDDHHMMSS_name.up.sql` files, registered in `migrationScriptTimestamps`; never a down migration, never a change to a file already released.

**CI and tools** ([ci.md](.claude/rules/ci.md)): every tool is pinned in the `Makefile`, never `@latest`. A change to documentation only skips the Go jobs.

**Code** matches what is around it: comments say why, not what, and are full sentences.

## Translating

Thaler speaks English, Spanish and German. English is the source; the other two follow it in the same story.

**The interface.** Its messages are in `internal/i18n/locales/` (`en.json` is the source). `make i18n-todo LOCALE=es` prints what a language still lacks as JSON to merge into `es.json`, with the English on standard error. `make i18n-check` (part of `make test` and of CI) fails when a message is missing from a language marked `Complete` in `internal/i18n`, when a translation is left over from a message that is gone or has other arguments, when a message is not used, or when the code asks for one that does not exist. `language = "pseudo"` shows every message accented and 40% longer in brackets (`[Ñö áççöúñtš ýét. ~~~]`), to find text that does not fit before a translator does; it is not offered as a language. `thaler cli` is English only.

**The pages.** `README.md` and `docs/config.md` have Spanish and German translations beside them (`README.es.md`, `docs/config.de.md`). Each translation's first line names the English it was made from by its git hash:

```
<!-- translated from README.md at 1a2b3c4d5e6f -->
```

`make translations-check` (and CI, for every change) fails when the English has changed since, and says how: `git diff OLD NEW` with the two hashes it prints shows what changed in the English (once that is staged or committed). Translate that into both pages and make their first line name the new hash. A change to the English therefore comes with its translations, like a new message does. The other pages (this one, `SECURITY.md`, the roadmap, the code) are English only.

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

Two things this costs, on purpose: the TUI calls the service directly from its update loop, and the database has one connection (see Decisions worth knowing).

## Where the code is

```
cmd/thaler/           cobra commands (thin: parse flags, call core.Service, print) and wiring
internal/ledger/      Money, account naming, double-entry validation, FX rates
internal/importer/    CSV profiles, OFX and QIF, encoding and delimiter detection, dedupe keys, rules
internal/core/        the application layer, see below
internal/config/      config.toml loading and editing, default paths, feature flags
internal/i18n/        the messages in English, Spanish and German (locales/*.json) and the language lookup
internal/coded/       errors that carry a code instead of a text
internal/tui/         Bubble Tea interface: panel layer, lists, fuzzy search, category picker, detail pane
cmd/releasenotes/, cmd/i18ntodo/, internal/releasenotes/    the maintainer's tools
```

```
core/api.go           Service interface and the types that cross it
core/service.go       Local, the SQLite-backed Service: all rules (validation, import, reports)
core/repository.go    Local's SQL; unexported, so nothing outside core can bypass the rules
core/migrations.go    schema versioning
```

The schema lives in `internal/core/dbmigrations/`, applied by [golang-migrate](https://github.com/golang-migrate/migrate). Add `YYYYMMDDHHMMSS_name.up.sql` files for changes (the first three are dated `YYYYMMDD`, which sorts before any timestamp) and register the number in `migrationScriptTimestamps` in `internal/core/migrations_test.go`. The `schema_migrations` table holds the version of the last migration applied. A database that holds data is copied to `backups/pre-migration/` first; if a migration fails nothing is changed.

## Decisions worth knowing

- **No sqlc (yet).** Queries are plain SQL in `core/repository.go`, so moving to sqlc later is mechanical. Migrations are plain `.up.sql` files run by golang-migrate; there are no down migrations, the copy made before an upgrade is how to go back.
- **One database connection.** Fine for a single user. The TUI calls the service directly from its update loop (milliseconds on a local file); a network backend would need those calls moved into background commands, and this pool setting revisited.
- **Recategorizing re-books the category leg only.** A transaction's category is its single Income/Expenses/Equity posting; the amount, date and your own account never change. Transactions with no such posting or several (transfers, FX conversions) are refused.
- **Backups are plain SQLite files unless you encrypt them.** Encryption is by passphrase only (age with scrypt); there are no key files or public-key recipients yet, so an unattended encrypted backup needs the passphrase in a file (`--passphrase-file`). The live database itself is not encrypted (see [SECURITY.md](SECURITY.md)).
- **Rows have a uuid and a version, accounts have an owner.** Integer ids stay what the code uses; the uuid is the identity that survives export, restore and sync. `version` goes up on every edit so a stale client can be refused later. `accounts.owner_id` is NULL for shared accounts and a user for private ones. The `users` table exists but nothing reads these columns yet.
- **Dates are ISO strings**, money is `int64` minor units, exchange rates are exact rationals (`math/big`). No floats touch stored or reported amounts.

## License

Thaler is under the [GNU Affero General Public License v3.0](LICENSE). A contribution is made under the same licence.
