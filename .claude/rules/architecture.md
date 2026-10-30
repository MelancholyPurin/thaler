# Code architecture

- `internal/core` is the only code that touches the database. The TUI and the CLI depend on the `core.Service` interface and nothing else, so a server client can replace the local SQLite backend later. File-level operations that must work when the database does not open (restore, decrypt, prune) live in `core` but work on files, outside `Service`.
- Money is `ledger.Money`, whole minor units in an `int64`; never floats. Transactions are postings that sum to zero.
- `config.toml` rejects unknown keys. Defaults live in `internal/config` next to the key (`DefaultBackupReminderDays`), as a pointer when "unset" must differ from `0`.
- The backup folders are defined once, in `internal/config` (`BackupDir`, `ReplacedDir`, `PreMigrationDir`), and the one listing of backups is `core.ListBackups`. Do not rebuild those paths or globs elsewhere.
- **TUI:** one `mode` per popup, its state in a pointer on `Model`, keys in `updateX`, drawing in `xPopup`. Work that can take a moment (backups, restores) runs through `m.start(note, cmd)` so the screen keeps drawing; the popup ignores keys meanwhile, and `ctrl+c` waits. Tests finish that work with `settle(m)`. A new key goes in the `?` help list, the README key table and, if it matters, the bottom-bar hints.
- User-facing text is English now; it will be extracted for Spanish and German (`LOC`, `TLC`), so keep strings whole sentences and out of clever concatenation.
