-- The lowest an asset account should be allowed to fall (RPT-6).
--
-- An account with a row here is warned about when its projected balance would
-- fall below `amount`, in the account's own currency, in minor units. An account
-- with no row has no limit: nothing is assumed, so a balance that is meant to
-- run down is never reported. Only Assets accounts have one; the service
-- enforces that. The amount may be negative, for an overdraft that is agreed.

CREATE TABLE account_minimums (
    account_id INTEGER PRIMARY KEY REFERENCES accounts (id),
    amount     INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) WITHOUT ROWID;
