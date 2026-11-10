-- An estimate with a tolerance (REC-5).
--
-- A bill that changes from one time to the next (electricity) is scheduled with
-- the amount it usually is and how far, in whole percent of that amount, a
-- bank row may differ from it and still be the bill. 0 is an exact amount, which
-- is what every schedule had before. The amount of the schedule stays the
-- estimate: what is booked when a date is confirmed or settled is what the
-- person says or the bank row says.

ALTER TABLE schedules ADD COLUMN tolerance INTEGER NOT NULL DEFAULT 0 CHECK (tolerance BETWEEN 0 AND 100);
