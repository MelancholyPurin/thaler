-- What a category may spend in a month (BUD-2).
--
-- One row per category and month, so that a month keeps the targets it was
-- planned with after the targets of later months change, and so that rollover
-- and copying a month's targets (BUD-4, BUD-5) have something to read. The
-- amount is whole minor units of `currency`, which is the base currency when the
-- target is set: Thaler compares spending with it after converting into that
-- currency (BUD-8), and the report says when the base currency has changed since.
-- Only expense categories have targets; the service enforces that.

CREATE TABLE budget_targets (
    account_id INTEGER NOT NULL REFERENCES accounts (id),
    month      TEXT NOT NULL CHECK (month GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'),
    amount     INTEGER NOT NULL CHECK (amount > 0),
    currency   TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (account_id, month)
) WITHOUT ROWID;

CREATE INDEX budget_targets_month ON budget_targets (month);
