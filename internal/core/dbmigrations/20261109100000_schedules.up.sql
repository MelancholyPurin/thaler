-- Schedules for recurring transactions (REC-1).
--
-- A schedule is a template and a rhythm: the payee, description and postings of
-- the transaction that repeats, and how often. It never posts anything by
-- itself (REC-3 asks first). The rhythm is "every `every` weeks, months or
-- years" counted from `start_date`: the first occurrence is the start date, and
-- a monthly schedule that starts on the 28th falls on the 28th. Which
-- occurrences are due is worked out when asked (REC-2), not stored, so editing
-- a schedule can never leave a stale list behind.
--
-- The postings are whole minor units, like the ones of a transaction, and point
-- at accounts that exist: Thaler creates a missing category when the schedule is
-- defined, as it does when a transaction is added.

CREATE TABLE schedules (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL COLLATE NOCASE UNIQUE CHECK (length(trim(name)) > 0),
    every       INTEGER NOT NULL CHECK (every >= 1),
    unit        TEXT NOT NULL CHECK (unit IN ('week', 'month', 'year')),
    start_date  TEXT NOT NULL CHECK (start_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    payee       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE schedule_postings (
    schedule_id INTEGER NOT NULL REFERENCES schedules (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    account_id  INTEGER NOT NULL REFERENCES accounts (id),
    amount      INTEGER NOT NULL,
    currency    TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency)),
    PRIMARY KEY (schedule_id, position)
) WITHOUT ROWID;
