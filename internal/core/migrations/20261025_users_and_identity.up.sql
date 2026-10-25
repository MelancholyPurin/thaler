-- Groundwork for several people sharing one database and, later, several
-- devices syncing with it. Nothing here changes what Thaler does today: every
-- new column is either filled in below or allowed to be NULL, and nothing reads
-- them yet except the integrity check.
--
--   users                  who can sign in. Empty until the first user is created.
--   accounts.owner_id      NULL = shared with everyone, otherwise private to that user.
--   *.created_by           who entered the row; NULL = before users existed.
--   *.uuid                 a stable identity that survives export, restore and sync;
--                          the integer ids stay what the rest of the code uses.
--   *.version              starts at 1 and goes up on every edit, so a client can say
--                          "I edited version 3" and be refused if it is stale.
--   *.updated_at           NULL until the row is first edited (see created_at).

CREATE TABLE users (
    id         INTEGER PRIMARY KEY,
    uuid       TEXT NOT NULL UNIQUE DEFAULT (
                   lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
                   substr(lower(hex(randomblob(2))), 2) || '-' ||
                   substr('89ab', abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))), 2) || '-' ||
                   lower(hex(randomblob(6)))),
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(trim(name)) > 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- SQLite cannot add a column with a computed default or a UNIQUE constraint,
-- so the uuid columns are added empty, filled for the rows that exist, and
-- locked down with a unique index. The repository supplies the uuid of every
-- row inserted from now on, and `thaler cli check` reports any that lack one.
-- (The uuid expression above is the version 4 layout: random except for the
-- version and variant bits.)

ALTER TABLE accounts ADD COLUMN uuid TEXT;
ALTER TABLE accounts ADD COLUMN owner_id INTEGER REFERENCES users (id);
ALTER TABLE accounts ADD COLUMN created_by INTEGER REFERENCES users (id);
ALTER TABLE accounts ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE accounts ADD COLUMN updated_at TEXT;

ALTER TABLE transactions ADD COLUMN uuid TEXT;
ALTER TABLE transactions ADD COLUMN created_by INTEGER REFERENCES users (id);
ALTER TABLE transactions ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE transactions ADD COLUMN updated_at TEXT;

UPDATE accounts SET uuid =
    lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
    substr(lower(hex(randomblob(2))), 2) || '-' ||
    substr('89ab', abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))), 2) || '-' ||
    lower(hex(randomblob(6)));

UPDATE transactions SET uuid =
    lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
    substr(lower(hex(randomblob(2))), 2) || '-' ||
    substr('89ab', abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))), 2) || '-' ||
    lower(hex(randomblob(6)));

CREATE UNIQUE INDEX accounts_uuid ON accounts (uuid);
CREATE UNIQUE INDEX transactions_uuid ON transactions (uuid);

-- Who may see an account is decided by owner_id, so looking accounts up by
-- owner has to be cheap.
CREATE INDEX accounts_owner ON accounts (owner_id);
