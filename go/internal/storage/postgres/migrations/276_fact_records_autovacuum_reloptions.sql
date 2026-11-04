-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9975 (epic #9209): make autovacuum keep up with fact_records by scaling its
-- triggers to this table's churn instead of its row count. On the QA primary
-- (about 102.3M rows) the default 0.2 scale factor means a dead-row trigger of
-- about 20.5M and the default 0.2 insert scale factor an insert trigger of
-- about 19.6M. At 14.57M dead rows and 17.4M inserts since the last vacuum
-- neither fired, so the last autovacuum was days old and the keyed winners
-- probe paid 46.0 buffers per returned row (13,383 ms cold) walking dead
-- versions.
--
-- 0.02 puts the dead-row trigger at about 2.05M rows and 0.05 puts the insert
-- trigger at about 4.9M, both already exceeded on QA, so the first vacuum
-- starts within one autovacuum_naptime of this landing. At today's rates later
-- vacuums follow every 1.1 to 1.7 days (dead rows) and every 17 to 29 hours
-- (inserts), keeping dead rows near 2% of live instead of 12 to 20%.
-- Evidence: docs/internal/evidence/9975-fact-records-autovacuum-reloptions.md.
--
-- Deliberately not set, because each is an unmeasured theory on a primary that
-- is already CPU-throttled: autovacuum_vacuum_cost_delay and
-- autovacuum_vacuum_cost_limit (the first pass is sized by the server
-- defaults), any toast.* value (the TOAST relation inherits these when unset),
-- and any analyze knob (autoanalyze is not the defect).
--
-- SET on autovacuum storage parameters takes SHARE UPDATE EXCLUSIVE, which
-- does not conflict with INSERT, UPDATE, DELETE, or SELECT, and changes only
-- the catalog: no rewrite, no new files. It waits only behind a running vacuum
-- or ANALYZE of fact_records, bounded by the runner's lock_timeout and retry.
-- Setting a parameter to its current value is a no-op, so a replay is safe.
-- Keep this the only statement in the file. There is no reverse migration;
-- a later migration would RESET both parameters if ever needed.
ALTER TABLE fact_records SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_insert_scale_factor = 0.05
);
