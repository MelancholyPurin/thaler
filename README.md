# thaler

A local-first personal finance planner. One Go binary, one SQLite file, no server, no bank sync.

- Double-entry ledger (accounts, transactions, postings), amounts stored as integer minor units
- Multi-currency: base currency EUR by default, switchable to USD, MXN or any other code
- Idempotent CSV import for **Hamburger Sparkasse** exports, with rule-based categorisation
- Run `thaler` for the interactive terminal UI (lazygit-style panels); every operation is also available as a `thaler cli …` subcommand, so it is scriptable (cron, shell)

> **Status: early, single-user.** It works locally and has tests, but the Sparkasse import profile has not been
> tried on a real bank export yet, and there is no multi-user mode or server (see the [roadmap](#roadmap)).

## Quick start

Requires Go 1.27 or newer. Install straight from GitHub:

```sh
go install github.com/melancholypurin/thaler/cmd/thaler@latest
```

or build from a clone:

```sh
go test ./...
go build -o thaler ./cmd/thaler

./thaler cli account add Assets:Sparkasse --currency EUR
./thaler cli import --account Assets:Sparkasse --dry-run export.csv   # preview, writes nothing
./thaler cli import --account Assets:Sparkasse export.csv
./thaler cli balance
./thaler cli list -a Expenses:Uncategorized

./thaler                         # no arguments: the interactive UI
```

For development, `make` lists the shortcuts: `make test`, `make lint` (gofmt and golangci-lint with the pinned version and `.golangci.yml`; the same checks as CI, and the first run builds the linter, about a minute), `make tidy-check` (fails if `go.mod` or `go.sum` need a `go mod tidy`), `make check` (lint, tidy-check and the tests, in a random order: a failure prints the seed to repeat it with `-shuffle=SEED`), `make test-race` (the tests with the race detector), `make vuln` (govulncheck against the dependencies and the standard library; needs the network), `make check-coverage` (per-package and total test coverage) and `make build`. CI first looks at which files a change touches (`.github/scripts/changed-paths.sh`, shared through the reusable workflow `changes.yml`). If only documentation and similar files changed (Markdown, `LICENSE`, `.gitignore`, `docs/`, `dependabot.yml`) the Go checks are skipped; any other file, including everything that is compiled in or configures the build, runs them. Otherwise it runs lint and the `go.mod` check, the tests, and the race detector as three parallel jobs. A last job, `Report Result`, passes when all three passed, or when all three were rightly skipped, and fails in every other case (require that one in branch protection, so jobs can change without touching the settings). `make test-scripts` tests the filter itself. Besides those it runs govulncheck on every push and on every pull request that is not documentation only, and once a week, since advisories appear without any change here. Dependabot opens one weekly pull request for routine Go dependency updates and one for the GitHub Actions; the pinned golangci-lint and govulncheck versions in the `Makefile` are not covered by it and are bumped by hand.

Export your transactions from Sparkasse online banking as CSV, choosing the **CSV-CAMT** format (the CSV-MT940 variant uses the same column names that thaler reads).

Data lives in `~/.local/share/thaler/thaler.db`, config in `~/.config/thaler/config.toml`. Override with `--db` / `--config`, or `THALER_DB`, `XDG_DATA_HOME`, `XDG_CONFIG_HOME`.

Your financial data never belongs in this repository: `.gitignore` excludes databases, CSV exports (except the invented test fixture) and `config.toml`.

## Categorising: `config.toml`

In the interface, press `C` on any screen to open the config in your editor (`$VISUAL`, else `$EDITOR`, else `nano` or `vi`; with a graphical editor use its wait flag, e.g. `EDITOR="code --wait"`). If there is no file yet it is created from `config.example.toml` with every sample switched off. When the editor closes, Thaler re-reads the file and tells you if it has a mistake. Without the interface, copy `config.example.toml` to `~/.config/thaler/config.toml` yourself. Add rules like this:

```toml
[[rule]]
contains = "REWE"
category = "Expenses:Groceries"
```

The first matching rule wins. Anything unmatched lands in `Expenses:Uncategorized` / `Income:Uncategorized`. Rules run at import time, and again on demand: after editing your rules, `thaler cli recategorize --dry-run` previews and `thaler cli recategorize` applies them to everything still uncategorized. Transactions you categorised yourself are never touched.

Re-importing the same or an overlapping export is safe: every row gets a stable hash (account, date, amount, counterparty, IBAN, purpose, booking text). Two truly identical bookings on one day are numbered, so they are kept as two. Rows the bank marks *Umsatz vorgemerkt* (pending) are ignored until booked.

## Terminal UI

Plain `thaler` (no arguments) opens an interface laid out like lazygit: panels on the left, and a detail pane on the right that shows whatever is selected.

```
╭─ [1] Home ───────────────────────────────╮╭─ thaler ─────────────────────────────────────────╮
│thaler  local-first finance               ││                                                  │
╰──────────────────────────────────────────╯│             ╺┳╸ ╻ ╻ ┏━┓ ╻   ┏━╸ ┏━┓              │
╭─ [2] Accounts ───────────────────────────╮│              ┃  ┣━┫ ┣━┫ ┃   ┣╸  ┣┳┛              │
│All accounts                    -42.00 EUR││              ╹  ╹ ╹ ╹ ╹ ┗━╸ ┗━╸ ╹┗╸              │
│Bank                            -47.00 EUR││                                                  │
│Savings                           5.00 EUR││           local-first personal finance           │
╰───────────────────────────────── 1 of 3 ─╯│                                                  │
╭─ [3] Transactions ───────────────────────╮│   Net worth       -42.00 EUR                     │
│2026-10-04 » to savings              -5.00││   Accounts        2                              │
│2026-10-03 ● Kino                   -12.00││   Transactions    4                              │
│2026-10-02 ● REWE                   -20.00││   Period          2026-10-01 → 2026-10-04        │
│2026-10-01   REWE                   -10.00││   Uncategorized   2  ●  press 3, then u to review│
│                                          ││   Base currency   EUR                            │
│                                          ││                                                  │
╰───────────────────────────────── 1 of 4 ─╯╰──────────────────────────────────────────────────╯
```

The strip along the top switches between **screens**, and the period on its right (`[ Oct 2026 ]`) scopes all of them. It starts on the current month.

| Screen | Key | What it shows |
| --- | --- | --- |
| Ledger | `L` / `F1` | the three panels below: accounts and the transaction history for the period |
| Budget | `B` / `F2` | income and spending by category for the period, with share bars; months on the left. Targets and envelopes will become columns here |
| Forecast | `F` / `F3` | not built yet |
| Reports | `R` / `F4` | not built yet |

**Ledger panels**

| Panel | Left column | Right pane while focused |
| --- | --- | --- |
| `1` Home | a small tile | title screen: net worth, counts, period, uncategorized count |
| `2` Accounts | "All accounts" and each asset/liability with its balance | account summary: balance, activity, this month's money in and out |
| `3` Transactions | the history, newest first (`●` uncategorized, `»` transfer) | the selected transaction: postings, account, category |

Selecting an account in panel 2 limits the transaction history to that account.

| Key | Action |
| --- | --- |
| `L` `B` `F` `R` (or `F1`–`F4`) | switch screen |
| `[` `]` | previous / next month |
| `t` / `T` | this month / all time |
| `1` `2` `3`, `tab` / `shift+tab` | Ledger: focus a panel |
| `↑` `↓` / `j` `k`, `g` `G`, `PgUp` `PgDn` | move in the focused panel (on Budget: pick the month) |
| `enter` | open the next panel (Home, Accounts) / categorize (Transactions) |
| `a` | add a transaction (see below) |
| `i` | import a bank export (see below) |
| `S` | back up the database: see when the last backup was made and write a new one (see below) |
| `ctrl+p` | remove the oldest backups, after listing them (see below) |
| `ctrl+r` | restore a backup in place of the database; the one it replaces is kept (see below) |
| `C` | open `config.toml` in your editor (rules, import folder, bank profiles) |
| `n` / `b` | new account / set the selected account's balance (see below) |
| `c` | categorize the selected transaction |
| `e` / `r` / `d` | edit / rename / delete the selected transaction (`r` opens the edit form on the title; delete asks first) |
| `z` | undo the last change made in this session: add, edit, delete, categorize or import, repeatable |
| `/` | fuzzy search over date, payee, description, category and amount (every word must match; `esc` clears) |
| `u` | show only uncategorized / all |
| `?` | key reference popup |
| `q` | quit |

**Logging an expense** is built to take a few keystrokes. Press `a`:

```
amount ⏎   payee ⏎   category ⏎        e.g.   8.40 ⏎  rew ⏎  ⏎
```

- The **account** is the one selected in the Accounts panel. With "All accounts" selected it is the account you used last (in this session, or else the most recent transaction in the database). Amounts are in that account's currency, so there is no currency to type.
- **Amount:** `12.50` is an expense, `+2450` is income.
- **Payee** is the title the transaction has in the list (rename it later with `r`): typing the start of a known payee highlights it, and Enter completes it. `↑` drops the suggestion if you meant something new.
- **Category:** the category that payee went to last time is offered first (marked "same payee"), then categories of the right kind by how often you use them. Type to filter, or type a full new name such as `Expenses:Gifts`. The last entry, "decide later", books it as uncategorized.
- **Description** is optional and comes after the date (`tab` from there; it is not in the way of the quick path above). Use it for a note about what the transaction was for. It is shown beside the transaction, searched by `/`, and for an imported row it starts out as the bank's purpose text, which you can edit or clear.
- **Date** defaults to today: `y` is yesterday, `-2` is two days ago, or type `2026-10-20` / `20.10.2026`. Mistakes are reported in the form and nothing is written.

**Moving around the form.** The line under the form says what `Enter` does on the field you are on. Adding is built to be quick, so there `Enter` on the *category* saves; everywhere else `Enter` moves on, and when you edit it walks through every field and saves on the *description*, so all of them can be reached with `Enter` alone. To go back, press `↑` (on the amount, date and description, `↑`/`↓` move between fields; in the payee and category lists `↑` first moves through the suggestions and then goes back) or `shift+tab`; `tab` goes forward. `ctrl+s` saves from anywhere and `esc` cancels.

**Creating an account** needs no command line. Press `n`, type a name (`Sparkasse` becomes `Assets:Sparkasse`), pick Assets or Liabilities, and confirm the currency (it starts as your base currency). If you already know what the account holds, fill in **Balance** and **As of**; leave them empty to start from nothing.

**Moving money between your accounts.** Press `x`. Type the amount; the accounts start as the selected one and another with the same currency, and `tab` then `◂ ▸` changes them (choosing the other field's account swaps the two). When the accounts hold different currencies a *Received* field appears: type what actually arrived, which fixes the exchange rate you got (`1 EUR = 1.0850 USD` is shown as you type). A transfer is booked as two linked transactions, one on each account, like the two lines on your bank statements: they go to `Equity:Transfers` (or `Equity:Conversions` across currencies), so a transfer is neither income nor spending. Each half is an ordinary row you can rename or redate; recategorizing one half, or changing its amount, dissolves the pair. `z` takes back both. On the command line: `thaler cli transfer --from A --to B --amount N [--to-amount M]`. Importing the bank statements later takes the two halves over (see below) instead of adding them again.

**Matching your bank's balance.** An import only brings in movements, so an account's balance is wrong until Thaler knows where it started. Select the account and press `b`, then type what your bank shows on a given day. Thaler books the difference as one *opening balance* transaction against `Equity:Opening`, dated the day before the account's first transaction, so the whole history adds up. Press `b` again after the next import and it corrects that same transaction instead of adding another. For a credit card or loan, type the amount you owe (a card showing 500.00 owed is `500`). `z` takes the change back. On the command line: `thaler cli account balance ACCOUNT AMOUNT --as-of DATE`.

**Importing a bank export** works from the interface too. Press `i`:

- **File:** the CSV files in `~/Downloads` are listed, newest first, so the export you just downloaded is already highlighted. Type a few letters to narrow the list, or paste or drag a file path into the field. Type or drop a *folder* path (say `~/Desktop`) to list that folder's CSV files instead. To list a different folder every time, set `import_dir = "~/Documents/bank-exports"` in `config.toml`.
- **Account** (default: the one selected in the Accounts panel) and **profile** (default: `sparkasse`, or one from `config.toml`) are changed with `tab` and `←` `→`.
- `enter` shows a **review** and writes nothing: how many rows are new, how many were imported before, how many no rule matched, and each row with the category the rules chose. A second `enter` imports. If some rows cannot be read, they are listed and `y` imports the rest.
- Afterwards the account's uncategorized rows are shown, ready for `c`, and `z` takes the whole import back.

**Entries you made by hand are not added twice.** If you log expenses yourself and later import the statement, each bank row is checked against what you typed: the same account, exactly the same amount, dated within three days, an ordinary entry (one account, one category) that has not been matched to a bank row yet. When one is found, the row *takes over* that entry. Nothing is added; the entry just remembers which bank row it is, so importing the file again recognises it. Your payee, date and category stay as you typed them, whatever your rules would have chosen. The review shows these rows as `↔ #12 REWE` before anything is written, each entry is taken over at most once (the closest date first), and `z` lets go of it again. This works for the two halves of a transfer you entered with `x`, too. Entries that are splits, that move between two of your accounts in one transaction, or are an opening balance are never matched.

**Transfers between your own accounts.** Import your current account and your savings account and the same transfer appears in both exports, once going out and once coming in. Counted as an expense and as income, it would inflate both on the Budget screen. So when a new row has the exact opposite amount of a transaction on another of your accounts, within three days, and neither is categorized yet, Thaler pairs them: both are booked to `Equity:Transfers` (so they are neither income nor spending), marked `⇄`, and linked to each other. The import review shows `⇄ Bank` on a paired row before anything is written, and `z` takes the pairing back with the import. It works in either order, each row is paired at most once, and a row a rule has already put in a real category (rent, say) is left alone. Add a rule with the category `Equity:Transfers` to mark a transfer whose other half has not been imported yet; the balance of `Equity:Transfers` is what is still waiting. If you recategorize or change one half, the pair is dissolved and the other half goes back to uncategorized so you notice it.

Rules and profiles are read from `config.toml` each time you press `i`, so you can edit them in another window and import again.

**Making a rule while you categorize.** In the category picker, press `tab` to move to a *Rule* field. It starts with the payee; trim it to the part that matters (`REWE Markt GmbH Filiale 12` → `REWE`). As you type, the picker says how many other uncategorized transactions it would catch. `enter` then categorizes this transaction, appends the rule to `config.toml`, and categorizes the matching ones too. `z` takes the categories back in one step; the rule stays in the file (press `C` to remove it). Only the end of the file is ever written, so your comments and the order of your rules are untouched. Rules are checked top to bottom and the first match wins, so if an earlier rule already decides this payee differently the picker names that rule and refuses, instead of adding one that could never apply.

In the category picker the category you last used for the same payee comes first (marked "same payee"), then the categories of the matching kind (income for money in, expenses for money out), most used first. Type to filter, or type a full new name such as `Expenses:Gifts` to create it. Transfers and split transactions are shown but cannot be recategorized there.

**Backing up.** Press `S` to open the backup popup. It shows the database, when the last backup was made ("2026-10-22 14:00 · 3 days ago"), and how many are in the backups folder, which sits next to the database (`backups/`). `enter` writes a snapshot to the file name in the *Save to* line, which starts out as `thaler-<date>-<time>.db` in that folder. You can type any other path instead (`~` is your home folder), or just a folder, which gets the timestamped name inside it. A file that already exists is never overwritten. After writing, Thaler opens the snapshot and runs SQLite's integrity check on it; a snapshot that fails is removed and reported, so a backup that is announced is one that opens.

**A reminder to back up.** When the newest backup in the backups folder is 7 days old or older, or there is none and you have transactions, the bottom bar leads with "Last backup 9 days ago: S makes a new one" (or "No backup yet") until you make one. Set `backup_reminder_days = 14` in `config.toml` for another interval, or `0` to switch the reminder off. Only backups in the backups folder count, as in the popup; ones you wrote elsewhere, the copies in `replaced/` and `pre-migration/` do not. It is checked when Thaler starts, after a backup, after you edit the config, and (at most once a minute) on a key press, so a backup made by a script while Thaler is open ends the reminder too.

**Pruning old backups.** Backups pile up, and nothing removes one by itself. Thaler keeps the newest 10 in each of three folders: the backups you make (`backups/`), the databases that restores replaced (`backups/replaced/`) and the copies made before upgrades (`backups/pre-migration/`). When a folder holds more, the bottom bar says "3 old backups can be removed: ctrl+p". `ctrl+p` lists exactly which files would go, oldest ones, with their dates and sizes, and `enter` removes them; `esc` leaves them. "Old" is the time a file was last changed, plain and encrypted backups count together, and the newest ones always stay. Only files named like a backup (`thaler-*.db`, `thaler-*.db.age`) are ever listed, so anything else in those folders, and backups you wrote elsewhere, are never touched. Set `keep_backups = 20` in `config.toml` for another number, or `0` to keep everything and stop the offer. From a script, `thaler cli prune` lists what would go and `thaler cli prune --yes` removes it. Removing is permanent; a removed backup is not in the trash.

**Encrypting a backup.** By default a backup is a plain SQLite file, which anyone who gets hold of it can read. Press `tab` to the *Passphrase* line and type one (it shows as `•`), then `tab` to *Repeat* and type it again, so a typo cannot lock you out. The backup is then written as an [age](https://age-encryption.org) file ending in `.age`, sealed with a key derived from the passphrase by scrypt (it takes a second or two, on purpose; the popup shows a spinner and what it is doing meanwhile, and ignores keys until it is done, apart from `ctrl+c`, which quits right after). Thaler decrypts the result once before it says it is done, to be sure the passphrase it holds opens it. The unencrypted copy exists only for a moment, in a private folder next to the backup, and is deleted. **There is no way to recover a backup whose passphrase is lost.** The popup and the status line say whether a backup is encrypted, and the *Last backup* line says so for the newest one.

**A copy before every upgrade.** When a new version of Thaler has to change the structure of a database that already holds data, it first writes a checked copy to `backups/pre-migration/` next to the database, named `thaler-<time>-before-<migration>.db`, and says where it is on standard error. If the copy cannot be made, the upgrade does not happen and nothing is changed. A new database, one that is already up to date, and one written by a newer version (which is refused) get no copy. These copies are plain, not encrypted, and are kept apart from the backups you make so that they never count as your "last backup"; `ctrl+p` and `thaler cli prune` prune them (see below).

**Restoring.** `thaler cli restore FILE` replaces the database with a backup, plain or encrypted (it asks for the passphrase, or reads `--passphrase-file`). The backup is read into a new file and checked first: one that is damaged, is not a Thaler database, or was made by a newer version is refused with nothing changed. Only then is the database in place moved, never deleted, to `backups/replaced/` next to it, and the backup put in its place. Running `thaler cli restore` on the file in `backups/replaced/` undoes a restore (the command prints it). It works when the database in place is damaged or missing. A backup from an older version is upgraded, after a copy of it, the next time Thaler starts. Close every other Thaler first; the restore does not look for one.

**Restoring from the interface.** `ctrl+r` opens the restore popup. It lists the backups in the backups folder, newest first, then the databases earlier restores replaced (marked "replaced by a restore"), so a restore can be taken back from the same place. Choose one with `↑`/`↓`; for an encrypted backup `tab` to *Passphrase* and type it. `enter` asks once more, naming the file and where your current database will be kept, and a second `enter` (or `y`) restores. Nothing happens until then, and a backup that is damaged, has the wrong passphrase or is not a Thaler database is refused with the database untouched and the popup still open. After a restore the screen shows the restored data and undo starts afresh (it cannot reach back across it). A backup that is not in the folder: `thaler cli restore FILE`.

To open an encrypted backup again, `thaler cli decrypt FILE.age [DEST]` asks for the passphrase and writes a plain database (default: the file name without `.age`; an existing file is never overwritten, and a wrong passphrase leaves nothing behind). Use the result with `thaler --db DEST`. Because it is a standard age file, the `age` tool opens it too (`age -d`).

On terminals narrower than 76 columns the detail pane is hidden, and below 14 rows only the focused panel is shown.

## Multi-currency

- Every account has a currency (Assets/Liabilities) or accepts any (Income/Expenses/Equity). Amounts are never converted when stored.
- **Base currency** only affects reports: `thaler cli currency` shows it, `thaler cli currency set USD` changes it.
- **Exchange rates** are recorded by you: `thaler cli rate set USD EUR 0.92` means 1 USD = 0.92 EUR. One direction is enough; the inverse is derived, and USD → MXN is routed through any currency that has rates to both (EUR, say). Reports use the latest rate on or before the report date.
- `thaler cli balance` shows each account in its own currency plus a net worth in the base currency. Currencies without a usable rate are named in a warning and left out of the total instead of being guessed.
- Moving money between currencies: `thaler cli transfer --from Assets:Sparkasse --to Assets:Cash-USD --amount 100 --to-amount 108.50`. It is booked through `Equity:Conversions` so each currency balances on its own, and the effective rate is printed.

```sh
./thaler cli account add Assets:Cash-USD --currency USD
./thaler cli account add Assets:Cash-MXN --currency MXN
./thaler cli rate set USD EUR 0.92
./thaler cli rate set MXN EUR 0.05
./thaler cli currency set USD
./thaler cli balance
```

## Commands

Run without arguments, `thaler` opens the terminal UI (see above). Everything else lives under `thaler cli`; run `thaler cli --help` for the list. The interface needs a terminal, so scripts and cron jobs always use `thaler cli`.

| Command | Purpose |
| --- | --- |
| `thaler cli account add NAME -c EUR` / `thaler cli account list` | chart of accounts; first segment is the type (`Assets:`, `Liabilities:`, `Income:`, `Expenses:`, `Equity:`) |
| `thaler cli account balance ACCOUNT AMOUNT [--as-of DATE]` | make the account hold what your bank shows, via an opening balance |
| `thaler cli add -a ACCOUNT --amount=-12.50 -t CATEGORY -p PAYEE -d DATE` | one income or expense |
| `thaler cli transfer --from A --to B --amount N [--to-amount M]` | between your own accounts, also across currencies |
| `thaler cli import FILE -a ACCOUNT [--profile P] [--dry-run]` | bank CSV import |
| `thaler cli list [-a ACCOUNT] [--from D] [--to D] [-n 30]` | transactions, newest last |
| `thaler cli recategorize [--dry-run]` | apply the config.toml rules to transactions still uncategorized |
| `thaler cli category set ID CATEGORY` | move one transaction to another category (created if new) |
| `thaler cli balance [--as-of D] [--all]` | balances and net worth |
| `thaler cli currency [set CODE]`, `thaler cli rate set/list` | base currency and exchange rates |
| `thaler cli backup [DEST] [--encrypt \| --passphrase-file F]` | consistent snapshot via `VACUUM INTO`, checked after it is written; **not encrypted** unless `--encrypt` (asks for a passphrase twice) or `--passphrase-file` (first line of a file, for scripts) is given, which writes an age file ending in `.age`; also `S` in the interface |
| `thaler cli decrypt SRC [DEST] [--passphrase-file F]` | open an encrypted backup as a plain database file, and check it |
| `thaler cli prune [--keep N] [--yes]` | list the backups beyond the newest N in each backups folder (default 10, or `keep_backups`); with `--yes`, remove them |
| `thaler cli restore SRC [--passphrase-file F]` | replace the database with a backup (plain or encrypted), keeping the one it replaces in `backups/replaced/`; also `ctrl+r` in the interface |
| `thaler cli check` | verifies every transaction balances per currency and the database is intact |

Write negative amounts as `--amount=-12.50`. Amounts accept `12.50`, `12,50` and `1.234,56`; ambiguous input such as `1,234` is rejected rather than guessed.

## Layout

```
cmd/thaler/           cobra commands (thin: parse flags, call core.Service, print) and wiring
internal/ledger/      Money, account naming, double-entry validation, FX rates   (stdlib only)
internal/importer/    CSV profiles, Windows-1252 decoding, dedupe keys, rules    (stdlib only)
internal/core/        the application layer, see below
internal/config/      config.toml loading and default paths
internal/tui/         Bubble Tea interface: panel layer, lists, fuzzy search, category picker, detail pane
```

`internal/core` is the only code that touches the database. The TUI and the CLI depend on its `Service` interface and nothing else, so a client for a remote server can replace the local SQLite backend later without changing them.

```
core/api.go           Service interface and the types that cross it
core/service.go       Local, the SQLite-backed Service: all rules (validation, import, reports)
core/repository.go    Local's SQL; unexported, so nothing outside core can bypass the rules
core/migrations.go    schema versioning
```

The schema lives in `internal/core/dbmigrations/`. Add `YYYYMMDD_name.up.sql` files for changes; applied files are recorded by name in the `schema_migrations` table.

## Decisions worth knowing

- **No sqlc, no goose (yet).** Queries are plain SQL in `core/repository.go` and migrations are plain `YYYYMMDD_name.up.sql` files tracked in a `schema_migrations` table, so moving to sqlc/goose later is mechanical.
- **One database connection.** Fine for a single-user CLI. The TUI calls the service directly from its update loop (milliseconds on a local file); a network backend would need those calls moved into background commands, and this pool setting revisited.
- **Recategorizing re-books the category leg only.** A transaction's category is its single Income/Expenses/Equity posting; the amount, date and your own account never change. Transactions with no such posting or several (transfers, FX conversions) are refused.
- **Backups are plain SQLite files unless you encrypt them.** Encryption is by passphrase only (age with scrypt); there are no key files or public-key recipients yet, so an unattended encrypted backup needs the passphrase in a file (`--passphrase-file`). The live database itself is not encrypted: use full-disk encryption for that.
- **Rows have a uuid and a version, accounts have an owner.** Integer ids stay what the code uses; the uuid is the identity that survives export, restore and sync. `version` goes up on every edit so a stale client can be refused later. `accounts.owner_id` is NULL for shared accounts and a user for private ones. The `users` table exists but nothing reads these columns yet.
- **Dates are ISO strings**, money is `int64` minor units, exchange rates are exact rationals (`math/big`). No floats touch stored or reported amounts.

## Roadmap

[ROADMAP.md](ROADMAP.md) lists what is built and what is planned, as milestones, epics and stories with permanent IDs. In short: first a feature-complete terminal interface that works well locally, in English, Spanish and German (`M-TUI`: budgets with rollover, reports, recurring transactions and a forecast, restore, Windows support, and releases on a regular train), then a self-hostable server whose single entry point, the app-gateway, exposes the API, so a small household (up to about four people) can share one Thaler, with private accounts that stay hidden from the others (`M-SERVER`), then a Next.js web application tested with Vitest (`M-WEB`), and finally an installable mobile web app that works offline (`M-PWA`).

## License

Copyright (C) 2026 MelancholyPurin. Thaler is free software under the [GNU Affero General Public License v3.0](LICENSE): you can use, modify and share it, and if you run a modified version as a network service you must offer its source to the users of that service.
