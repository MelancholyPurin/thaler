# Thaler roadmap

Where Thaler is going, in three levels. Every item has an ID that never changes, so a branch, a pull request or a commit can say which story it delivers. Commit and pull request titles start with the story ID in brackets, and this file is where you look the ID up:

```
[TBU-2] internal/tui: Set a monthly target for a category
```

| Level | What it is | Size | ID | Example |
| --- | --- | --- | --- | --- |
| **Milestone** | A stage of the product that stands on its own and is worth naming | Months | `M-<NAME>` | `M-TUI` |
| **Epic** | A product requirement: a group of stories that together deliver one thing people can rely on. It can be released when it is complete | 5 to 40 stories | three letters | `TBU` (the TUI budget tab) |
| **Story** | One feature, e.g. "allow creating a monthly budget". It may take several pull requests | One feature | the epic's letters, a dash, a number | `TBU-2` |

**Reading and assigning IDs**
- A story's letters are its epic's key, so `TBU-2` belongs to `TBU`. The tables below list every key.
- Numbers count up within an epic. A new story takes the next free number, wherever it appears in the list; a number is never reused or renumbered, even when a story is dropped.
- Epic keys are mnemonic: a first letter of `T` marks a TUI epic, `W` a web epic and `P` a PWA epic, and the rest are plain (`BUD` is the budget domain in the core, `TBU` its tab in the TUI, `WBU` its page in the web app).
- Each story line is its ID and a one-line description of the feature, which is the full name to look up. The part after `internal/tui:` in a commit title is yours to word as the change needs.

Status: `[x]` delivered, `[ ]` planned. Stories are listed in the order they are likely to be done, but only the current milestone is planned in detail; the epics of later milestones are outlines that will be refined when their milestone starts.

## The milestones, in order

| ID | Milestone | Done when |
| --- | --- | --- |
| `M-TUI` | Feature-complete TUI that can be used locally | One person can run their finances in the terminal, in English, Spanish or German, with data that cannot be lost, on macOS, Linux and Windows, and new versions arrive on a regular release train |
| `M-SERVER` | Self-hostable multi-user server | `thaler serve` starts the app-gateway, the one public entry point to all the endpoints, in Docker on a small VPS; each household member has their own login; private accounts stay private |
| `M-WEB` | Web application (Next.js, React) | The family uses the browser, in English, Spanish or German, for everyday logging, budgets and reports, including from Windows, and the frontend is covered by Vitest and end-to-end tests |
| `M-PWA` | Installable progressive web app | Expenses can be logged from a phone, offline, and synced later |

### Epic keys

| Key | Epic | Milestone |
| --- | --- | --- |
| `LDG` | Ledger domain | `M-TUI` |
| `IMP` | Bank import pipeline | `M-TUI` |
| `BUD` | Budget domain | `M-TUI` |
| `REC` | Recurring transactions | `M-TUI` |
| `RPT` | Reports and projections | `M-TUI` |
| `SAF` | Backup, encryption and restore | `M-TUI` |
| `LOC` | Localization foundation | `M-TUI` |
| `CLI` | Command-line interface | `M-TUI` |
| `TLD` | Ledger tab | `M-TUI` |
| `TCT` | Categorizing and rules | `M-TUI` |
| `TIM` | Importing in the TUI | `M-TUI` |
| `TTR` | Transfers in the TUI | `M-TUI` |
| `TBU` | Budget tab | `M-TUI` |
| `TRP` | Reports tab | `M-TUI` |
| `TFC` | Forecast tab and recurring transactions | `M-TUI` |
| `TLC` | TUI in three languages | `M-TUI` |
| `PLT` | Installing and running everywhere | `M-TUI` |
| `REL` | The release train | `M-TUI` |
| `ENG` | Build, test and delivery | `M-TUI` |
| `DOC` | Documentation | `M-TUI` |
| `GAT` | The app-gateway | `M-SERVER` |
| `API` | Endpoints | `M-SERVER` |
| `AUT` | Users and sign-in | `M-SERVER` |
| `PRV` | Private accounts | `M-SERVER` |
| `REM` | TUI and CLI against a server | `M-SERVER` |
| `DEP` | Running a server | `M-SERVER` |
| `WBA` | Application shell | `M-WEB` |
| `WTE` | Frontend testing | `M-WEB` |
| `WLD` | Transactions page | `M-WEB` |
| `WIM` | Importing in the browser | `M-WEB` |
| `WBU` | Budget page | `M-WEB` |
| `WRP` | Reports page | `M-WEB` |
| `WFC` | Forecast page and recurring transactions | `M-WEB` |
| `WST` | Settings and household | `M-WEB` |
| `WLC` | Web app in three languages | `M-WEB` |
| `PIN` | Installable app | `M-PWA` |
| `PEN` | Logging an expense on a phone | `M-PWA` |
| `POF` | Working without a connection | `M-PWA` |
| `PNO` | Notifications | `M-PWA` |
| `PUX` | Phone experience | `M-PWA` |

Decisions already taken: recurring transactions always **ask before posting**, never post by themselves; budget **rollover** is part of the budget epics; **Windows** is a supported platform (installing and running everywhere, `PLT`); the languages are **English** (the source language), **Spanish** and **German**, on every surface (the localization foundation is `LOC`); releases leave on a **release train that leaves every two weeks** (`REL`), and an epic that is not finished stays behind a feature flag instead of holding the train.

Also decided: versions follow **SemVer**, each train bumping the minor version and a hotfix the patch, with `1.0.0` at the end of `M-TUI` (`REL-2`); the app-gateway is a **module of the single `thaler` binary**, with a boundary clean enough to split later (`GAT-1`, `GAT-14`); the web app's localization library is **next-intl**, with **react-i18next** kept open as the alternative (`WLC-1`). Nothing is left undecided at this level.

---

## M-TUI: Feature-complete TUI that can be used locally

Single user, local SQLite file, terminal only. **Exit criteria:** every epic below is complete; one full month of real bank exports has been imported, categorized and reconciled against the bank; a backup has been restored for real; release binaries install on macOS, Linux and Windows.

**Suggested order:** backup and restore (`SAF`) and installing everywhere (`PLT`) first, so real data can go in safely; then the release train (`REL`), so what follows ships on it; then the localization foundation (`LOC`) with the string extraction in the TUI (`TLC`) while the interface is still small, since every screen added later is more strings to extract; then budgets (`BUD`, `TBU`); then the editing gaps (`TLD`, `TTR`, `TCT`); then reports (`RPT`, `TRP`); then recurring transactions and the forecast (`REC`, `TFC`). Build, test and docs stories (`ENG`, `DOC`) go in between features, one at a time.

### LDG: Ledger domain

The double-entry model and its service layer, which every surface (CLI, TUI, later the API) uses.

- [x] `LDG-1` Transactions made of postings that balance to zero, money held as whole minor units
- [x] `LDG-2` A chart of accounts with Assets, Liabilities, Income, Expenses and Equity
- [x] `LDG-3` A currency per account, a base currency, exact exchange rates, net worth in the base currency
- [x] `LDG-4` Add, edit, delete and restore a transaction; list and filter them
- [x] `LDG-5` Zero-amount rows, as banks book for card verification, can be stored and edited
- [x] `LDG-6` Set an account's balance as of a date
- [x] `LDG-7` Balances and net worth, now or as of a date
- [x] `LDG-8` A transfer between two of your accounts as two linked transactions, within one currency or across two
- [x] `LDG-9` Verify that every transaction balances and the database is intact
- [x] `LDG-10` Versioned schema migrations applied on open
- [x] `LDG-11` A uuid, a version and an owner on every row, ready for sync and multiple users
- [x] `LDG-12` One `Service` interface with a local implementation, with the repository kept private
- [ ] `LDG-13` Rename and close accounts without losing their history
- [ ] `LDG-14` Rename, merge and delete categories, moving their transactions
- [ ] `LDG-15` Edit a split transaction as a whole, keeping it balanced
- [ ] `LDG-16` Edit and delete both halves of a transfer together

### IMP: Bank import pipeline

Turning bank exports into transactions without duplicating or double-counting anything.

- [x] `IMP-1` The Sparkasse CSV export, German number and date formats included
- [x] `IMP-2` Custom CSV profiles for other banks, defined in `config.toml`
- [x] `IMP-3` A hash per row, so importing the same file twice adds nothing
- [x] `IMP-4` Categorize rows with rules from the config, first match wins
- [x] `IMP-5` Preview what an import would do without writing
- [x] `IMP-6` Take an import back as a whole
- [x] `IMP-7` An imported row claims the matching transaction you entered by hand instead of duplicating it
- [x] `IMP-8` Pair the two halves of a transfer between your own accounts, in either import order
- [ ] `IMP-9` Run against real exports and fix what the sample file did not cover
- [ ] `IMP-10` Detect encoding and delimiter instead of assuming them
- [ ] `IMP-11` The CAMT.053 XML statement format
- [ ] `IMP-12` OFX and QIF files

### BUD: Budget domain

Targets per category, and what is left of them.

- [x] `BUD-1` Spending per category for a month, in the base currency
- [ ] `BUD-2` A monthly target per category
- [ ] `BUD-3` Spent, target and left per category, and the total
- [ ] `BUD-4` Unspent money (or overspending) carries into the next month, switched on per category
- [ ] `BUD-5` Start a month from the previous month's targets
- [ ] `BUD-6` Planned income against targets, and what is still unassigned
- [ ] `BUD-7` Group categories (Housing, Food) with a target and totals for the group
- [ ] `BUD-8` Spending in other currencies counted against base-currency targets

### REC: Recurring transactions

Things that happen on a schedule. Thaler **asks first**: it proposes an occurrence and you confirm, change or skip it.

- [ ] `REC-1` Define a schedule (rent monthly, salary on the 28th, a yearly insurance)
- [ ] `REC-2` Work out which occurrences are due up to a date
- [ ] `REC-3` Confirm, edit the amount, or skip a due occurrence; nothing is ever posted without that
- [ ] `REC-4` An imported bank row settles the occurrence it corresponds to
- [ ] `REC-5` An estimate with a tolerance for bills that change (electricity)
- [ ] `REC-6` Pause a schedule, give it an end date, skip a single occurrence
- [ ] `REC-7` Scheduled transfers, such as a monthly saving

### RPT: Reports and projections

Read-only queries over the ledger.

- [ ] `RPT-1` Spending per category across months
- [ ] `RPT-2` Net worth over time, in the base currency
- [ ] `RPT-3` Income against spending per month, and the savings rate
- [ ] `RPT-4` Where the most money goes, by payee
- [ ] `RPT-5` Projected balance per account from the recurring schedules
- [ ] `RPT-6` The day a balance would fall below a chosen amount
- [ ] `RPT-7` Any report as CSV

### SAF: Backup, encryption and restore

Nothing that happens, including a bad migration or a bad import, should be able to cost you your data.

- [x] `SAF-1` A consistent snapshot of the database, from the CLI, written private to you
- [x] `SAF-2` The snapshot is opened and integrity-checked before it is reported as done
- [x] `SAF-3` The backup popup, with the date and age of the last backup
- [x] `SAF-4` Encrypt a backup with a passphrase (age), from the TUI and the CLI, checked by decrypting it once
- [x] `SAF-5` Open an encrypted backup again with `thaler cli decrypt`
- [x] `SAF-6` Copy the database aside before a migration changes an existing one
- [x] `SAF-7` Restore a backup from the command line, keeping the replaced database
- [x] `SAF-8` Pick a backup, enter the passphrase if needed, verify it, swap it in, keep the old one aside
- [x] `SAF-9` A reminder when the last backup is older than a number of days you set
- [x] `SAF-10` Show that an encrypted backup is being made instead of freezing
- [x] `SAF-11` Keep the last N backups and offer to prune the rest

### LOC: Localization foundation

English is the source language; Spanish and German are translations. This epic is the machinery that every surface uses.

- [ ] `LOC-1` Message catalogs in Go, one file per language, with a tool that extracts new strings
- [ ] `LOC-2` The language setting: `config.toml`, then the environment (`LANG`), then English
- [ ] `LOC-3` Amounts, dates and numbers written the way the language writes them (`1,234.56` / `1.234,56`)
- [ ] `LOC-4` Typed amounts and dates read in the language's format, with the ambiguous cases still refused
- [ ] `LOC-5` Service errors carry a stable code, and the text is looked up in the catalog instead of being fixed in the code
- [ ] `LOC-6` "1 transaction" / "2 transactions" correct in all three languages
- [ ] `LOC-7` Display names for the built-in account types and starting categories, separate from the names stored in the ledger
- [ ] `LOC-8` A test and a CI step that fail when a key is missing from a language or no longer used
- [ ] `LOC-9` A pseudo-language that lengthens every string, to find text that does not fit before a translator does

### CLI: Command-line interface

The scriptable surface (`thaler cli ...`), for cron and one-off jobs.

- [x] `CLI-1` Create, list and show accounts and their balances
- [x] `CLI-2` Add a transaction
- [x] `CLI-3` List and filter transactions
- [x] `CLI-4` Transfer between accounts, across currencies too
- [x] `CLI-5` Import a bank export, with a dry run
- [x] `CLI-6` Set and recategorize categories
- [x] `CLI-7` Balances and net worth as of a date
- [x] `CLI-8` Base currency and exchange rates
- [x] `CLI-9` Verify the ledger is consistent
- [ ] `CLI-10` Machine-readable output for every listing
- [ ] `CLI-11` Export transactions as CSV
- [ ] `CLI-12` Set and show budget targets
- [ ] `CLI-13` Manage schedules and confirm what is due
- [ ] `CLI-14` The reports of `RPT` (reports and projections), as commands
- [ ] `CLI-15` Help and messages in English, Spanish and German, following the language setting

### TLD: Ledger tab

The main screen: accounts, transactions and the forms to change them.

- [x] `TLD-1` Home, Accounts and Transactions panels with a detail pane, adapting to narrow and short terminals
- [x] `TLD-2` Log an expense with amount, payee and category in a few keystrokes
- [x] `TLD-3` Edit a transaction, and delete one after a confirmation
- [x] `TLD-4` Rename a transaction's title and give it an optional description
- [x] `TLD-5` Reach every field with Enter, the arrows or tab, and go back again
- [x] `TLD-6` Fuzzy search over date, payee, description, category and amount
- [x] `TLD-7` Undo the last changes of the session, repeatedly
- [x] `TLD-8` Move between months, this month, all time
- [x] `TLD-9` Create an account and set its opening balance
- [x] `TLD-10` A key reference popup and the Thaler banner
- [x] `TLD-11` Open `config.toml` in your editor from the TUI
- [ ] `TLD-12` Create and edit split transactions
- [ ] `TLD-13` Rename and archive accounts
- [ ] `TLD-14` Select several transactions to categorize or delete at once
- [ ] `TLD-15` Sort, and filter by amount, date range, category and account
- [ ] `TLD-16` Colour themes for dark and light terminals

### TCT: Categorizing and rules

Getting every transaction into the right category with as little typing as possible.

- [x] `TCT-1` A category picker that puts the one used for the same payee first and creates new categories as you type
- [x] `TCT-2` Make a rule while categorizing, and apply it to the matching uncategorized transactions
- [x] `TCT-3` Show only what still needs a category
- [ ] `TCT-4` List, edit, reorder and delete rules inside the TUI
- [ ] `TCT-5` Run the rules again over existing uncategorized transactions
- [ ] `TCT-6` Categorize a selection at once
- [ ] `TCT-7` Rename and merge categories
- [ ] `TCT-8` Suggest a category from how similar payees were categorized

### TIM: Importing in the TUI

- [x] `TIM-1` Pick an account and an export file from the TUI
- [x] `TIM-2` List exports from a configurable folder, or type another
- [x] `TIM-3` See what is new, what is a duplicate and what is a transfer pair before anything is written
- [x] `TIM-4` Take the whole import back with undo
- [ ] `TIM-5` Change a row's category in the review before committing
- [ ] `TIM-6` Choose or auto-detect the profile for a file
- [ ] `TIM-7` List earlier imports and undo any of them
- [ ] `TIM-8` Notice a new export in the import folder

### TTR: Transfers in the TUI

- [x] `TTR-1` Move money between two of your accounts
- [x] `TTR-2` Say what arrives and see the exchange rate it implies
- [x] `TTR-3` Transfer halves are marked `⇄` in the list and in the import review
- [ ] `TTR-4` Edit a transfer as one thing
- [ ] `TTR-5` Delete both halves together
- [ ] `TTR-6` Link two existing transactions as a transfer, and unlink them

### TBU: Budget tab

- [x] `TBU-1` Spending per category for the chosen month
- [ ] `TBU-2` Set and clear a category's monthly target in a popup
- [ ] `TBU-3` Spent against target with what is left, and overspending in red
- [ ] `TBU-4` Switch rollover on or off per category and see the carried amount
- [ ] `TBU-5` Copy last month's targets into this one
- [ ] `TBU-6` Open a category to see the transactions behind its number
- [ ] `TBU-7` Collapse and expand category groups
- [ ] `TBU-8` A "left this month" figure on the Home panel

### TRP: Reports tab

- [ ] `TRP-1` Spending per category over the months, as text charts
- [ ] `TRP-2` Net worth history
- [ ] `TRP-3` Income against spending per month
- [ ] `TRP-4` The payees that cost the most
- [ ] `TRP-5` Choose the range a report covers
- [ ] `TRP-6` Save the shown report as CSV

### TFC: Forecast tab and recurring transactions

- [ ] `TFC-1` See all schedules and when each is next due
- [ ] `TFC-2` Create, edit, pause and end a schedule
- [ ] `TFC-3` Go through what is due and confirm, change or skip each
- [ ] `TFC-4` Projected balance per account, week by week
- [ ] `TFC-5` Warn about the day a balance would dip below your chosen amount
- [ ] `TFC-6` Look at one account or all of them

### TLC: TUI in three languages

- [ ] `TLC-1` Every piece of text in the interface comes from the catalog
- [ ] `TLC-2` The Spanish translation
- [ ] `TLC-3` The German translation
- [ ] `TLC-4` Every screen and popup fits at 80 columns in all three languages, checked by tests
- [ ] `TLC-5` Change the language from the TUI without restarting
- [ ] `TLC-6` Key hints and the help popup in the language; the keys themselves stay the same
- [ ] `TLC-7` A native speaker reads the Spanish and the German before a release

### PLT: Installing and running everywhere

- [ ] `PLT-1` `thaler --version`, set when the binary is built
- [ ] `PLT-2` The binaries for macOS, Linux and Windows that each release ships, with checksums (built by `REL-9`)
- [ ] `PLT-3` Config and data folders where Windows expects them
- [ ] `PLT-4` Open the config in a Windows editor, and behave on Windows terminals
- [ ] `PLT-5` Run the tests on macOS and Windows as well as Linux
- [ ] `PLT-6` Install instructions per platform
- [ ] `PLT-7` Homebrew and Scoop packages

### REL: The release train

A release leaves on a fixed schedule and carries whatever is complete. Work that is not finished stays behind a flag, so a late epic waits for the next train instead of holding up this one. This is what makes it possible to release a finished epic without waiting for a whole milestone.

- [ ] `REL-1` The schedule of the train (decided: every two weeks) and the cut-off before it
- [ ] `REL-2` SemVer: each train bumps the minor version, a hotfix the patch, and `1.0.0` marks the end of `M-TUI`; plus the rule for which versions can upgrade to which
- [ ] `REL-3` A release branch is cut from `main` on the train's day, and only fixes go onto it
- [ ] `REL-4` Feature flags in the configuration, so unfinished stories can be merged and stay off
- [ ] `REL-5` Release notes written from the `[ID]` in the commit titles, grouped by epic
- [ ] `REL-6` A release candidate and a smoke-test checklist, including making and restoring a backup
- [ ] `REL-7` Tests that upgrade a database from the previous train, with data in it
- [ ] `REL-8` How a fix is made on the release branch and carried back to `main`
- [ ] `REL-9` One workflow cuts, tags and builds everything the train ships: binaries, and later the image and the web app
- [ ] `REL-10` The published schedule, and which release shipped each story, recorded in this file
- [ ] `REL-11` Signed checksums for every artifact

### ENG: Build, test and delivery

- [x] `ENG-1` One command for build, test, lint, coverage and checks
- [x] `ENG-2` Golangci-lint, pinned, same locally and in CI
- [x] `ENG-3` Table-driven tests with given-when-then and `cmp.Diff` everywhere
- [x] `ENG-4` Lint, test and race detector as parallel jobs, a `go.mod` check, shuffled tests, and a single `result` check
- [x] `ENG-5` Govulncheck on every change and weekly
- [x] `ENG-6` Weekly dependency updates
- [ ] `ENG-7` Run the lint locally before a push
- [ ] `ENG-8` Fuzz the amount, CSV and config parsers
- [ ] `ENG-9` Fail the check when coverage drops under a floor
- [ ] `ENG-10` Split `(*Local).Import` and flatten the deeply nested code the linter flagged
- [ ] `ENG-11` Branch protection requiring `result`, squash merge only, branches deleted after merge
- [x] `ENG-12` Run the database migrations with `golang-migrate`: one version number, a test that every migration file is registered in order, the copy before an upgrade and the refusal of a newer database kept, and databases written by the current runner converted

### DOC: Documentation

- [x] `DOC-1` Install, usage, formats and design notes
- [ ] `DOC-2` Conventions: test style, branch per feature, never commit financial data
- [ ] `DOC-3` How to report a vulnerability, and what Thaler does and does not protect
- [ ] `DOC-4` The layers and why they are cut where they are
- [ ] `DOC-5` A walkthrough of the first month with real data
- [ ] `DOC-6` Every `config.toml` key
- [ ] `DOC-7` What changed in each release, assembled by the release train from the story IDs in the commit titles (see `REL-5`)
- [ ] `DOC-8` The README and the user guide in Spanish and German

---

## M-SERVER: Self-hostable multi-user server

A household of up to about four people shares one self-hosted Thaler. **Exit criteria:** `thaler serve` runs from a Docker image on a small VPS; each member signs in with their own credentials; a private account is invisible and uncounted for everyone else; the TUI and CLI can work against the server.

### GAT: The app-gateway

The central entry point. Every client (the web app, the PWA, the TUI and CLI in remote mode) talks only to the app-gateway, which exposes the endpoints listed under `API` and is the only part of Thaler reachable from outside. It starts with `thaler serve`.

- [ ] `GAT-1` The app-gateway as a module of the `thaler` binary, started by `thaler serve`: one listener, one configuration, and the only thing a deployment exposes
- [ ] `GAT-2` Versioned routes (`/v1/...`) that turn each request into one `Service` call
- [ ] `GAT-3` A token or session is checked before any handler runs
- [ ] `GAT-4` The caller (user and household) travels with every request, so the visibility rules of private accounts (`PRV`) apply to all endpoints in one place
- [ ] `GAT-5` One error format and one mapping from `Service` errors to status codes, with stable error codes
- [ ] `GAT-6` The OpenAPI description is served by the gateway and kept in step with its routes by a test
- [ ] `GAT-7` How `/v1` changes, how old versions are deprecated, how clients learn of it
- [ ] `GAT-8` A retried request never creates a duplicate, which offline clients depend on
- [ ] `GAT-9` Request size caps and rate limits per caller
- [ ] `GAT-10` The web app's origin is allowed, nothing else
- [ ] `GAT-11` Request IDs, structured access logs, and a metrics endpoint
- [ ] `GAT-12` Liveness and readiness endpoints for the container
- [ ] `GAT-13` Live updates, so a second device sees a change without reloading
- [ ] `GAT-14` The line between the gateway and the core, drawn and tested so the core could be split into its own service without touching the endpoints

### API: Endpoints

The resources the app-gateway exposes. Everything that is the same for every endpoint (routing, sign-in checks, errors, limits, the spec) belongs to `GAT`, not here.

- [ ] `API-1` List, create, archive accounts
- [ ] `API-2` List, add, edit, delete, restore transactions
- [ ] `API-3` Create and edit transfers
- [ ] `API-4` Upload an export, review it, commit it, undo it
- [ ] `API-5` Targets and progress
- [ ] `API-6` Schedules and the due list
- [ ] `API-7` The reports and the forecast
- [ ] `API-8` Create and download a backup on the server
- [ ] `API-9` Paged and filtered listings
- [ ] `API-10` List, create, rename and merge categories
- [ ] `API-11` List, create, reorder and delete categorization rules
- [ ] `API-12` Language negotiation: answers carry error codes plus a message in the caller's language

### AUT: Users and sign-in

- [ ] `AUT-1` Create and manage the household's users
- [ ] `AUT-2` Passwords stored with a modern slow hash
- [ ] `AUT-3` Per-user API tokens for the CLI and TUI
- [ ] `AUT-4` Password sign-in with a session for the web app
- [ ] `AUT-5` One household with its members
- [ ] `AUT-6` An admin and ordinary members
- [ ] `AUT-7` An admin-assisted reset
- [ ] `AUT-8` Slow down repeated failed sign-ins
- [ ] `AUT-9` Who changed what, and when

### PRV: Private accounts

- [ ] `PRV-1` Mark an account as private to its owner
- [ ] `PRV-2` Nothing about a private account is returned to anyone else
- [ ] `PRV-3` Private accounts are left out of shared category totals and household net worth
- [ ] `PRV-4` What a transfer between a private and a shared account shows to others
- [ ] `PRV-5` Tests that try to find any way a private figure shows up for someone else
- [ ] `PRV-6` What a backup contains, and who can make one

### REM: TUI and CLI against a server

- [ ] `REM-1` A `Service` that talks to the app-gateway
- [ ] `REM-2` The TUI's service calls move into background commands, so a slow network does not freeze it
- [ ] `REM-3` Choose a server and a token, or the local database
- [ ] `REM-4` Clear behaviour when the server is unreachable
- [ ] `REM-5` Every CLI command works against a server

### DEP: Running a server

- [ ] `DEP-1` A small image, with the app-gateway as its entry point, published with each release
- [ ] `DEP-2` A compose file with a data volume
- [ ] `DEP-3` Documented HTTPS behind a reverse proxy
- [ ] `DEP-4` Upgrading with a backup first, and rolling back
- [ ] `DEP-5` Backups on a schedule
- [ ] `DEP-6` Encrypt unattended backups to a public key, so no passphrase sits in a file
- [ ] `DEP-7` A start-to-finish guide for a small VPS

---

## M-WEB: Web application

A Next.js and React application for the family, served by the server from `M-SERVER`. **Exit criteria:** everyday logging, categorizing, importing, budgeting and reports can be done in a desktop browser, on Windows, macOS and Linux.

### WBA: Application shell

- [ ] `WBA-1` A Next.js and TypeScript project in this repository
- [ ] `WBA-2` A typed client generated from the OpenAPI description
- [ ] `WBA-3` Sign in and out, and keep the session
- [ ] `WBA-4` Navigation and a responsive page shell
- [ ] `WBA-5` Design tokens and light and dark themes
- [ ] `WBA-6` Consistent loading, empty and error states
- [ ] `WBA-7` Build and lint the web app in CI (its tests are `WTE-9`)
- [ ] `WBA-8` How the built app is served, from the Go binary or its own container

### WTE: Frontend testing

Vitest for the frontend, written in the same table style as the Go tests: a list of cases, then given, when and then in the loop.

- [ ] `WTE-1` Vitest set up for the web app, with one command to run it
- [ ] `WTE-2` The repository's test convention in TypeScript: `it.each` over a table of cases, with given, when and then
- [ ] `WTE-3` React Testing Library for components and hooks
- [ ] `WTE-4` A mock of the app-gateway built from the OpenAPI description, so tests run without a server
- [ ] `WTE-5` A test that fails when the client and the spec drift apart
- [ ] `WTE-6` Render each screen in English, Spanish and German; fail on a missing key or text that breaks the layout
- [ ] `WTE-7` Automated accessibility checks in the component tests
- [ ] `WTE-8` A coverage report and a floor
- [ ] `WTE-9` The Vitest run is a CI job and part of the `result` gate
- [ ] `WTE-10` Browser tests of the main flows
- [ ] `WTE-11` Tests for the service worker and the offline queue (for `M-PWA`)

### WLD: Transactions page

- [ ] `WLD-1` Accounts and transactions with period navigation
- [ ] `WLD-2` Search, sort and filter
- [ ] `WLD-3` Add a transaction, fast
- [ ] `WLD-4` Edit and delete, with undo
- [ ] `WLD-5` The category picker, with rules
- [ ] `WLD-6` Create and edit splits
- [ ] `WLD-7` Create and edit transfers
- [ ] `WLD-8` Select several and act on them
- [ ] `WLD-9` Keyboard shortcuts for everything common

### WIM: Importing in the browser

- [ ] `WIM-1` Upload an export by choosing or dropping a file
- [ ] `WIM-2` See what is new, duplicate or paired, and fix categories
- [ ] `WIM-3` Pick or manage bank profiles
- [ ] `WIM-4` Past imports, with undo
- [ ] `WIM-5` Manage categorization rules

### WBU: Budget page

- [ ] `WBU-1` The month's targets and progress
- [ ] `WBU-2` Set targets and rollover
- [ ] `WBU-3` Category groups
- [ ] `WBU-4` From a category to its transactions
- [ ] `WBU-5` Start a month from the last one

### WRP: Reports page

- [ ] `WRP-1` Spending per category over time
- [ ] `WRP-2` Net worth history
- [ ] `WRP-3` Income against spending
- [ ] `WRP-4` Biggest payees
- [ ] `WRP-5` Download any report

### WFC: Forecast page and recurring transactions

- [ ] `WFC-1` List, create, edit, pause schedules
- [ ] `WFC-2` Confirm, change or skip what is due
- [ ] `WFC-3` Projected balances as a chart
- [ ] `WFC-4` The day a balance would dip too low
- [ ] `WFC-5` A home page with what matters this week

### WST: Settings and household

- [ ] `WST-1` Create, rename, archive accounts
- [ ] `WST-2` Manage categories and groups
- [ ] `WST-3` Add and remove household members
- [ ] `WST-4` Choose which accounts are private
- [ ] `WST-5` Make, download and restore backups
- [ ] `WST-6` Language, base currency, password

### WLC: Web app in three languages

- [ ] `WLC-1` next-intl, and message catalogs for English, Spanish and German in plain ICU message files, with the library used through a thin layer so that react-i18next stays a possible swap
- [ ] `WLC-2` The language comes from the member's setting, then the browser, then English
- [ ] `WLC-3` Money, dates and numbers formatted for the language and the account's currency
- [ ] `WLC-4` Change the language from the page; it is remembered for the member
- [ ] `WLC-5` API error codes shown as text in the language
- [ ] `WLC-6` Plural forms in all three languages
- [ ] `WLC-7` The Spanish translation
- [ ] `WLC-8` The German translation
- [ ] `WLC-9` CI fails on a missing or unused key
- [ ] `WLC-10` A native speaker reads the Spanish and the German before a release

---

## M-PWA: Installable progressive web app

The web app, installable on a phone and usable without a connection. **Exit criteria:** a family member installs it, logs an expense on the bus with no signal, and it appears on everyone's screen once the phone is back online.

### PIN: Installable app

- [ ] `PIN-1` Name, icons and colours so browsers offer to install it
- [ ] `PIN-2` A service worker that serves the app shell
- [ ] `PIN-3` A clear way to install it
- [ ] `PIN-4` New versions arrive without stale caches
- [ ] `PIN-5` Launch screens and icons for iOS and Android

### PEN: Logging an expense on a phone

- [ ] `PEN-1` A mobile-first form with a number pad
- [ ] `PEN-2` Recent payees and categories one tap away
- [ ] `PEN-3` Open straight to "add an expense"
- [ ] `PEN-4` Attach a photo of a receipt
- [ ] `PEN-5` Share an amount or a receipt into the app from another app

### POF: Working without a connection

- [ ] `POF-1` The last-seen accounts, transactions and budget are readable offline
- [ ] `POF-2` Expenses added offline wait in a queue
- [ ] `POF-3` The queue is sent when the connection returns, without duplicates
- [ ] `POF-4` A change made on two devices is detected with the row version and resolved clearly
- [ ] `POF-5` Always show whether you are online and what is waiting

### PNO: Notifications

- [ ] `PNO-1` Web push set-up, per device and per member
- [ ] `PNO-2` Something recurring is due
- [ ] `PNO-3` A category is close to or over its target
- [ ] `PNO-4` The server has not been backed up
- [ ] `PNO-5` Choose what you are told about
- [ ] `PNO-6` Notifications written in the member's language

### PUX: Phone experience

- [ ] `PUX-1` Touch-sized controls and thumb-reachable actions
- [ ] `PUX-2` Swipe to categorize or delete, pull to refresh
- [ ] `PUX-3` Sign in with a passkey or biometrics
- [ ] `PUX-4` Hide amounts when the app is in the background
- [ ] `PUX-5` Fast start and smooth scrolling on a low-end phone
