# Database Migrations for Go

> **Current and planned.** This file describes migrations as they will be once the project has grown (MySQL-style services, protobuf, `golang-migrate`) as well as how they are written today. A rule marked **(planned)** does not apply yet: do not act on it, and do not look for the tools it names. Today the database is SQLite, there is no protobuf, and migrations are applied by Thaler's own small runner in `internal/core/migrations.go`. Moving to `golang-migrate` is `ENG-12` in the roadmap; until it lands, the runner is the truth.

Migrations are versioned SQL files applied by `golang-migrate` **(planned: `ENG-12`; today Thaler's own runner)**.

- Append only: write only `<timestamp>_*.up.sql`files in the `dbmigrations/`dir. Never create `.down.sql`.
- Instant DDL: `ALTER TABLE ... ADD COLUMN`msut append to the end - NEVER use `AFTER`/`FIRST` because those force a table rebuild and write lock on large tables.
- **(planned, with `golang-migrate`)** Register timestamp: EEvery new migration's numeric timestamp must be added o the `migrationScriptTimestamps` slice in the Go `*_test.go`. `golang-migrate` silently ignores unregistered scripts. On a merge conflict in that slice, rename your SQL file toa newer timestamp to update the slice.
- **(planned: MySQL and protobuf; today SQLite, with the column types the existing migrations use)** Column types: UUIDs = VARCHAR(36); user subjects = VARCHAR (255); protobuf enums = integer columns (`INT`/`TINYINT UNSIGNED`, matching proto numbers), no native SQL `ENUM`; nested/structured data = `JSON` columns serialized with `protojson`.

## In this repo

- The files live in `internal/core/dbmigrations/` and are named `YYYYMMDD_name.up.sql`. They are embedded in the binary and recorded by file name in the `schema_migrations` table. The runner applies, in name order, every file not yet recorded, so a file dated earlier than one already applied is still picked up; `golang-migrate` will not do that (hence the timestamp rule above).
- An existing database that holds data is copied aside (`backups/pre-migration/`) before a migration changes it. A migration that cannot be copied first does not run.
