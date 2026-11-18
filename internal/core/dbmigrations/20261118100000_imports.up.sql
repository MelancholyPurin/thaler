-- A record of each import (TIM-7), so that an import made earlier can be listed and
-- taken back, not only the one made in this session.
--
-- `imports` is the record: the account, the name of the file (the name only, not the
-- folder), the profile that read it and when. Everything an import did is tied to it:
-- the transactions it added carry its id; the entries you had made by hand that a bank
-- row took over are in `import_adoptions`; and the pairings it made with a transaction
-- of another account are in `import_links`, with the category that other half had
-- before, which taking the import back puts it in again.
--
-- Deleting a transaction frees its place in the record by itself (the id on the
-- transaction goes, and a row of the two other tables with it), so a record counts only
-- what is still there. Imports made before this migration have no record.

CREATE TABLE imports (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts (id),
    source      TEXT NOT NULL,
    profile     TEXT NOT NULL,
    imported_at TEXT NOT NULL
);

ALTER TABLE transactions ADD COLUMN import_id INTEGER REFERENCES imports (id) ON DELETE SET NULL;

CREATE INDEX transactions_import_id ON transactions (import_id);

CREATE TABLE import_adoptions (
    transaction_id INTEGER PRIMARY KEY REFERENCES transactions (id) ON DELETE CASCADE,
    import_id      INTEGER NOT NULL REFERENCES imports (id) ON DELETE CASCADE
);

CREATE TABLE import_links (
    transaction_id INTEGER PRIMARY KEY REFERENCES transactions (id) ON DELETE CASCADE,
    import_id      INTEGER NOT NULL REFERENCES imports (id) ON DELETE CASCADE,
    other_id       INTEGER NOT NULL REFERENCES transactions (id) ON DELETE CASCADE,
    other_was      TEXT NOT NULL
);

CREATE INDEX import_adoptions_import_id ON import_adoptions (import_id);
CREATE INDEX import_links_import_id ON import_links (import_id);
