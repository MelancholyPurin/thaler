-- Core schema.
--
-- Amounts are integers in the minor unit of their currency (cents for EUR,
-- USD and MXN). Dates are ISO strings (YYYY-MM-DD) so they sort and compare
-- lexicographically. Every posting carries its own currency; the Go layer
-- guarantees that the postings of each transaction sum to zero per currency
-- (see ledger.CheckBalanced and `thaler cli check`).

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) WITHOUT ROWID;

INSERT INTO settings (key, value) VALUES ('base_currency', 'EUR');

-- Account names are colon-separated paths whose first segment is the type,
-- e.g. Assets:Sparkasse or Expenses:Groceries. Assets and Liabilities are bound
-- to one currency; Income, Expenses and Equity may be NULL = any currency.
CREATE TABLE accounts (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL CHECK (type IN ('Assets', 'Liabilities', 'Income', 'Expenses', 'Equity')),
    currency   TEXT CHECK (currency IS NULL OR (length(currency) = 3 AND currency = upper(currency))),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    CHECK (currency IS NOT NULL OR type IN ('Income', 'Expenses', 'Equity'))
);

CREATE TABLE transactions (
    id          INTEGER PRIMARY KEY,
    date        TEXT NOT NULL CHECK (date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    payee       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    -- Set for imported rows only; makes re-importing an export idempotent.
    -- NULLs do not collide, so manual entries are unaffected.
    import_hash TEXT UNIQUE,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX transactions_date ON transactions (date);

CREATE TABLE postings (
    id             INTEGER PRIMARY KEY,
    transaction_id INTEGER NOT NULL REFERENCES transactions (id) ON DELETE CASCADE,
    account_id     INTEGER NOT NULL REFERENCES accounts (id),
    amount         INTEGER NOT NULL,
    currency       TEXT NOT NULL CHECK (length(currency) = 3 AND currency = upper(currency))
);

CREATE INDEX postings_transaction ON postings (transaction_id);
CREATE INDEX postings_account ON postings (account_id);

-- One unit of base is worth `rate` units of quote on `date`. The rate is kept
-- as decimal text and parsed exactly (math/big) so no precision is lost.
CREATE TABLE rates (
    date  TEXT NOT NULL CHECK (date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    base  TEXT NOT NULL CHECK (length(base) = 3),
    quote TEXT NOT NULL CHECK (length(quote) = 3),
    rate  TEXT NOT NULL,
    PRIMARY KEY (date, base, quote),
    CHECK (base <> quote)
) WITHOUT ROWID;
