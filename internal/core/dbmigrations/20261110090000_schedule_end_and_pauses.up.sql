-- An end date and pauses for a schedule (REC-6).
--
-- `end_date` is the last day an occurrence may fall on; without one a schedule
-- goes on for ever. A pause is a span of days in which the schedule proposes
-- nothing: `from_date` is the day it was paused, `to_date` the day it was
-- resumed, which proposes again from that day on, and NULL while it is still
-- paused. The spans are kept, not just a flag, so that the dates that fell in a
-- pause stay dropped after the resume instead of coming back as a backlog, and
-- a later import does not take them for a payment owed.

ALTER TABLE schedules ADD COLUMN end_date TEXT CHECK (end_date IS NULL OR end_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]');

CREATE TABLE schedule_pauses (
    schedule_id INTEGER NOT NULL REFERENCES schedules (id) ON DELETE CASCADE,
    from_date   TEXT NOT NULL CHECK (from_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    to_date     TEXT CHECK (to_date IS NULL OR to_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    PRIMARY KEY (schedule_id, from_date)
) WITHOUT ROWID;
