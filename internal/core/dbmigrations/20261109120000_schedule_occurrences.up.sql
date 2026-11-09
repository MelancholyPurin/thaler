-- What was done with an occurrence of a schedule (REC-3).
--
-- Thaler proposes an occurrence and asks. The answer is one row here: 'posted'
-- with the transaction it became, or 'skipped'. An occurrence with no row is
-- still due. The date is the occurrence's own date, not the day it was booked.
--
-- Deleting the transaction removes its row, so an occurrence whose transaction
-- was deleted is due again and gets asked about again. Deleting a schedule
-- removes its rows; the transactions stay.

CREATE TABLE schedule_occurrences (
    schedule_id    INTEGER NOT NULL REFERENCES schedules (id) ON DELETE CASCADE,
    date           TEXT NOT NULL CHECK (date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    status         TEXT NOT NULL CHECK (status IN ('posted', 'skipped')),
    transaction_id INTEGER REFERENCES transactions (id) ON DELETE CASCADE,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (schedule_id, date),
    CHECK ((status = 'posted') = (transaction_id IS NOT NULL))
) WITHOUT ROWID;
