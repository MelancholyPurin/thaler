-- Scheduled transfers (REC-7).
--
-- A schedule with `transfer` set moves money between two of your own accounts
-- instead of booking a transaction against a category. Its two postings are the
-- amount that leaves one account (negative) and the amount that arrives in the
-- other (positive, in that account's currency); confirming an occurrence stores
-- a transfer, two linked transactions through Equity:Transfers or
-- Equity:Conversions, as `thaler cli transfer` does.

ALTER TABLE schedules ADD COLUMN transfer INTEGER NOT NULL DEFAULT 0 CHECK (transfer IN (0, 1));
