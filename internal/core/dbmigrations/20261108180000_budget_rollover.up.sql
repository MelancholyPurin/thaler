-- Carry-over of a category's budget (TBU-4).
--
-- A category with a row here carries what is left of its target, or what it
-- overspent, into the next month, from `from_month` on: that month starts with
-- nothing carried in, the one after it with what the first one had left. The
-- carried amounts are not stored. They are worked out when shown from the
-- targets and the spending of the months since, so editing or importing an older
-- transaction can never leave a stale figure behind.
-- Only expense categories have one; the service enforces that.

CREATE TABLE budget_rollover (
    account_id INTEGER PRIMARY KEY REFERENCES accounts (id),
    from_month TEXT NOT NULL CHECK (from_month GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]'),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) WITHOUT ROWID;
