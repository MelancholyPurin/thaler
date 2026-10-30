-- Links the two halves of a transfer between your own accounts.
--
-- Each half is an ordinary imported transaction (its own import hash, so
-- re-importing either bank export still dedupes) booked against
-- Equity:Transfers instead of an expense or income category. transfer_with
-- points each half at the other, so a half is never paired twice, and an
-- unpaired half is the one with no link. Deleting a half clears the link on the
-- other.

ALTER TABLE transactions ADD COLUMN transfer_with INTEGER REFERENCES transactions (id) ON DELETE SET NULL;

CREATE INDEX transactions_transfer_with ON transactions (transfer_with);
