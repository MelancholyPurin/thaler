-- Closing an account (LDG-13).
--
-- A closed account keeps its name, its transactions and its history: reports of past months
-- and the net worth of the time still count what it held. It is set aside, not removed: the
-- service lists it as closed, and refuses to book new money to it until it is reopened. The
-- column holds the time it was closed; NULL is an account that is open. Only Assets and
-- Liabilities accounts are closed, and only empty ones; the service enforces both.

ALTER TABLE accounts ADD COLUMN closed_at TEXT;
