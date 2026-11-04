-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9975 (epic #9209): make autovacuum keep up with fact_records by scaling its
-- triggers to this table's churn instead of its row count. On the QA primary
-- (about 102.3M rows) the default 0.2 scale factor means a dead-row trigger of
-- about 20.5M and the default 0.2 insert scale factor an insert trigger of
-- about 19.6M. At 14.57M dead rows and 17.4M inserts since the last vacuum
-- neither fired, so the last autovacuum was days old. The keyed winners probe
-- paid 46.0 buffers per returned row (13,383 ms cold, measured with an old
-- snapshot holder open) on index entries that yield no visible row. Dead row
-- versions that vacuum has not removed are the leading explanation, not an
-- established cause: the first vacuum is the test.
--
-- 0.02 puts the dead-row trigger at about 2.05M rows and 0.05 puts the insert
-- trigger at about 4.9M on the row count read before any vacuum; a vacuum that
-- completes first moves both. Where both are exceeded when this lands, the
-- first vacuum starts within one autovacuum_naptime; after a completed vacuum
-- it does not. Later vacuums follow the rates then in force (the evidence note
-- has the arithmetic). That keeps dead rows near 2% of live as a table-wide
-- average; hot rows can carry more between vacuums (arithmetic, unmeasured).
-- Evidence: docs/internal/evidence/9975-fact-records-autovacuum-reloptions.md.
--
-- Deliberately not set, because each is an unmeasured theory on a primary that
-- is already CPU-throttled: autovacuum_vacuum_cost_delay and
-- autovacuum_vacuum_cost_limit (the first pass is sized by the server
-- defaults), any toast.* value (the TOAST relation inherits these when unset),
-- and any analyze knob (autoanalyze is not the defect).
--
-- SET on autovacuum storage parameters takes SHARE UPDATE EXCLUSIVE. That lock
-- does not conflict with INSERT, UPDATE, DELETE, or SELECT, and the statement
-- changes only the catalog: no rewrite, no new files. It does conflict with the
-- lock a running autovacuum holds, and it does not wait behind an ordinary
-- one: after deadlock_timeout (1 s by default) PostgreSQL cancels that
-- autovacuum ("canceling autovacuum task" in the server log), the ALTER
-- proceeds, and the next worker restarts the table from block 0. Only an
-- anti-wraparound autovacuum, a manual VACUUM, or an ANALYZE makes the ALTER
-- wait, bounded by the runner's lock_timeout and retry budget. A replay leaves
-- the options unchanged but cancels and restarts a vacuum that is still
-- running, so do not re-issue this statement while the first vacuum runs.
-- Keep this the only statement in the file. There is no reverse migration;
-- a later migration would RESET both parameters if ever needed.
ALTER TABLE fact_records SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_insert_scale_factor = 0.05
);
