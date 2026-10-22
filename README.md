# thaler

A local-first personal finance planner. One Go binary, one SQLite file, no server, no bank sync.

- Double-entry ledger (accounts, transactions, postings), amounts stored as integer minor units
- Multi-currency: base currency EUR by default, switchable to USD, MXN or any other code
- Idempotent CSV import for **Hamburger Sparkasse** exports, with rule-based categorisation
- Run `thaler` for the interactive terminal UI (lazygit-style panels); every operation is also available as a `thaler cli …` subcommand, so it is scriptable (cron, shell)

> **Status: early, single-user.** It works locally and has tests, but the Sparkasse import profile has not been
> tried on a real bank export yet, and there is no multi-user mode or server (see the [roadmap](#roadmap)).

## Quick start

Requires Go 1.26 or newer. Install straight from GitHub:

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

Export your transactions from Sparkasse online banking as CSV, choosing the **CSV-CAMT** format (the CSV-MT940 variant uses the same column names that thaler reads).

Data lives in `~/.local/share/thaler/thaler.db`, config in `~/.config/thaler/config.toml`. Override with `--db` / `--config`, or `THALER_DB`, `XDG_DATA_HOME`, `XDG_CONFIG_HOME`.

Your financial data never belongs in this repository: `.gitignore` excludes databases, CSV exports (except the invented test fixture) and `config.toml`.

## Categorising: `config.toml`

Copy `config.example.toml` to `~/.config/thaler/config.toml` and add rules:

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
| `c` | categorize the selected transaction |
| `e` / `d` | edit / delete the selected transaction (delete asks first) |
| `z` | undo the last change made in this session: add, edit, delete or categorize, repeatable |
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
- **Payee:** typing the start of a known payee highlights it, and Enter completes it. `↑` drops the suggestion if you meant something new.
- **Category:** the category that payee went to last time is offered first (marked "same payee"), then categories of the right kind by how often you use them. Type to filter, or type a full new name such as `Expenses:Gifts`. The last entry, "decide later", books it as uncategorized.
- **Date** defaults to today: `y` is yesterday, `-2` is two days ago, or type `2026-10-20` / `20.10.2026`. `tab` moves between fields, `ctrl+s` saves from anywhere and `esc` cancels. Mistakes are reported in the form and nothing is written.

In the category picker the category you last used for the same payee comes first (marked "same payee"), then the categories of the matching kind (income for money in, expenses for money out), most used first. Type to filter, or type a full new name such as `Expenses:Gifts` to create it. Transfers and split transactions are shown but cannot be recategorized there.

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
| `thaler cli add -a ACCOUNT --amount=-12.50 -t CATEGORY -p PAYEE -d DATE` | one income or expense |
| `thaler cli transfer --from A --to B --amount N [--to-amount M]` | between your own accounts, also across currencies |
| `thaler cli import FILE -a ACCOUNT [--profile P] [--dry-run]` | bank CSV import |
| `thaler cli list [-a ACCOUNT] [--from D] [--to D] [-n 30]` | transactions, newest last |
| `thaler cli recategorize [--dry-run]` | apply the config.toml rules to transactions still uncategorized |
| `thaler cli category set ID CATEGORY` | move one transaction to another category (created if new) |
| `thaler cli balance [--as-of D] [--all]` | balances and net worth |
| `thaler cli currency [set CODE]`, `thaler cli rate set/list` | base currency and exchange rates |
| `thaler cli backup [DEST]` | consistent snapshot via `VACUUM INTO` (**not encrypted**) |
| `thaler cli check` | verifies every transaction balances per currency and the database is intact |

Write negative amounts as `--amount=-12.50`. Amounts accept `12.50`, `12,50` and `1.234,56`; ambiguous input such as `1,234` is rejected rather than guessed.

## Layout

```
cmd/thaler/              cobra commands (thin: parse flags, call the packages below, print)
internal/ledger/      Money, account naming, double-entry validation, FX rates   (stdlib only)
internal/importer/    CSV profiles, Windows-1252 decoding, dedupe keys, rules    (stdlib only)
internal/store/       SQLite access, embedded migrations, backup, consistency check
internal/config/      config.toml loading and default paths
internal/tui/         Bubble Tea interface: panel layer, lists, fuzzy search, category picker, detail pane
```

The schema lives in `internal/store/migrations/`. Add `0002_*.sql` for changes; the version is tracked in SQLite's `user_version`.

## Decisions worth knowing

- **No sqlc, no goose (yet).** Queries are plain SQL in `store.go` and migrations are plain `NNNN_name.sql` files tracked in SQLite's `user_version`, so moving to sqlc/goose later is mechanical.
- **One database connection.** Fine for a single-user CLI. The TUI queries the database directly from its update loop (milliseconds on a local file); anything slow would need to move into background commands and this pool setting revisited.
- **Recategorizing re-books the category leg only.** A transaction's category is its single Income/Expenses/Equity posting; the amount, date and your own account never change. Transactions with no such posting or several (transfers, FX conversions) are refused.
- **Backups are plain SQLite files.** Encryption (age) is not implemented; keep them on an encrypted disk for now.
- **Dates are ISO strings**, money is `int64` minor units, exchange rates are exact rationals (`math/big`). No floats touch stored or reported amounts.

## Roadmap

Planned, not built yet. The goal is to keep the local-first tool and let a small household (up to about four people) log expenses in one self-hosted place:

- A `thaler serve` mode: a JSON API over the same core, per-user tokens, a Docker image and a small VPS (or any always-on machine). The TUI and CLI will be able to talk to either the local database or a server.
- **Private accounts** that are fully hidden from the other members: not visible, and excluded from shared category totals and the household net worth.
- A phone-friendly web app for quick expense entry.
- Budget targets and envelopes, recurring transactions, forecast and reports (the Budget, Forecast and Reports screens already exist in the UI; only the first shows data).

## License

Copyright (C) 2026 MelancholyPurin. Thaler is free software under the [GNU Affero General Public License v3.0](LICENSE): you can use, modify and share it, and if you run a modified version as a network service you must offer its source to the users of that service.
