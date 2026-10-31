# Database Migrations for Go

> **Current and planned.** This file describes migrations as they will be once the project has grown (MySQL-style services, protobuf) as well as how they are written today. A rule marked **(planned)** does not apply yet: do not act on it, and do not look for the tools it names. Today the database is SQLite and there is no protobuf. Migrations are applied by `golang-migrate` (since `ENG-12`), from `internal/core/migrations.go`.

Migrations are versioned SQL files applied by `golang-migrate`.

- Append only: write only `<timestamp>_*.up.sql`files in the `dbmigrations/`dir. Never create `.down.sql`.
- Instant DDL: `ALTER TABLE ... ADD COLUMN`msut append to the end - NEVER use `AFTER`/`FIRST` because those force a table rebuild and write lock on large tables.
- Register timestamp: EEvery new migration's numeric timestamp must be added o the `migrationScriptTimestamps` slice in the Go `*_test.go`. `golang-migrate` silently ignores unregistered scripts. On a merge conflict in that slice, rename your SQL file toa newer timestamp to update the slice.
- **(planned: MySQL and protobuf; today SQLite, with the column types the existing migrations use)** Column types: UUIDs = VARCHAR(36); user subjects = VARCHAR (255); protobuf enums = integer columns (`INT`/`TINYINT UNSIGNED`, matching proto numbers), no native SQL `ENUM`; nested/structured data = `JSON` columns serialized with `protojson`.

## In this repo

- The files live in `internal/core/dbmigrations/`, are embedded in the binary, and are named `<version>_<name>.up.sql` in lower case. The three that exist are dated (`20261022_init.up.sql`); **new ones use a full timestamp, `YYYYMMDDHHMMSS`** (every such number is bigger than every date, so they sort after them). Two files cannot share a version.
- The `migrationScriptTimestamps` slice is in `internal/core/migrations_test.go`. The test next to it fails when a file is missing from the slice, when the slice is not in order, and when a `.down.sql` file exists.
- `schema_migrations` holds one row, `(version, dirty)`: the last migration applied. A database written before `ENG-12` recorded the file names instead; opening it converts the table, after the copy below.
- An existing database that holds data is copied aside (`backups/pre-migration/`) before a migration changes it. A migration that cannot be copied first does not run.
- Each migration runs in one transaction, so one that fails changes nothing. `golang-migrate` still marks the database as half upgraded when that happens; when the failure came from the migration's own SQL, Thaler puts the version back to the one before. Any other failure leaves the mark, and the next start refuses the database and names the copy to restore.
- A database whose version is newer than the newest file in the binary is refused: update Thaler.
