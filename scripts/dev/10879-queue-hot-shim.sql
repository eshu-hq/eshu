-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10879 cheapest theory shim (NOT production code, NOT a migration).
--
-- Question: does moving the lease triple (lease_owner, claim_until,
-- last_attempt_at) off the queue table onto a narrow fillfactor-70 table with
-- no secondary index let heartbeat renewals go HOT and stop index writes,
-- while claim (availability change) still writes indexes as before?
--
-- BEFORE shape mirrors production's indexed-write problem on a subset of the
-- real indexes (migrations/005): heartbeat touches indexed updated_at, so it
-- can never be HOT. AFTER shape mirrors arbiter decision 1/3: heartbeat
-- writes claim_until only, on a lease table whose only index is the PK.
-- The shim omits the conflict unique index (decision 2): conflict cols are
-- immutable after claim, so it takes an insert-time write only, never a
-- renewal write. That omission is named, not hidden.
--
-- RED/GREEN mapping (TDD spirit, no production edit): the BEFORE phase
-- asserts the bug (HOT share < 10%, indexes grow); the AFTER phase asserts
-- the fix (HOT share > 50%, main indexes flat). The runner exits nonzero if
-- either assert fails.
--
-- Scale: N=2000 rows; heartbeats are H=25 per-row autocommitted renewals
-- driven by the runner (production heartbeat is one row per transaction, so a
-- single-transaction full-table sweep would defeat heap pruning and understate
-- HOT). Ratios are the claim; absolute bytes/timings are local-box only and
-- NOT equated with QA hardware. The runner generates hb_before.sql /
-- hb_after.sql (marked \timing off, timed as whole phases).

\timing on

-- ---------------------------------------------------------------- setup --
DROP TABLE IF EXISTS q_lease;
DROP TABLE IF EXISTS q_before;
DROP TABLE IF EXISTS q_main;

-- BEFORE: today's shape (subset). fillfactor 100 = production (no fillfactor).
CREATE TABLE q_before (
    work_item_id   TEXT PRIMARY KEY,
    status         TEXT NOT NULL,
    lease_owner    TEXT NULL,
    claim_until    TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    attempt_count  INTEGER NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL
) WITH (fillfactor = 100);
CREATE INDEX idx_before_status ON q_before (status, updated_at);
CREATE INDEX idx_before_claim ON q_before (claim_until) WHERE claim_until IS NOT NULL;

-- AFTER: main keeps availability cols; lease is narrow, fillfactor 70, PK only.
CREATE TABLE q_main (
    work_item_id  TEXT PRIMARY KEY,
    status        TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL
) WITH (fillfactor = 100);
CREATE INDEX idx_main_status ON q_main (status, updated_at);
CREATE TABLE q_lease (
    work_item_id    TEXT PRIMARY KEY REFERENCES q_main (work_item_id) ON DELETE CASCADE,
    lease_owner     TEXT NOT NULL,
    claim_until     TIMESTAMPTZ NOT NULL,
    last_attempt_at TIMESTAMPTZ NOT NULL
) WITH (fillfactor = 70);

-- Identical seed on both sides, then VACUUM so stats start clean.
INSERT INTO q_before (work_item_id, status, updated_at)
SELECT 'w' || lpad(g::text, 7, '0'), 'pending', clock_timestamp() FROM generate_series(1, 2000) g;
INSERT INTO q_main (work_item_id, status, updated_at)
SELECT 'w' || lpad(g::text, 7, '0'), 'pending', clock_timestamp() FROM generate_series(1, 2000) g;
VACUUM (ANALYZE) q_before;
VACUUM (ANALYZE) q_main;
VACUUM (ANALYZE) q_lease;

-- Baseline snapshots (stats + sizes + WAL). The force-flush precedes every
-- stat read: pgstat flushes asynchronously and an in-session read otherwise
-- races pending counters (observed 0/0 mid-run on PG18).
SELECT pg_stat_force_next_flush();
SELECT 'baseline_before' AS phase, n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = 'q_before';
SELECT pg_stat_force_next_flush();
SELECT 'baseline_main' AS phase, n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = 'q_main';
SELECT 'baseline_sizes' AS phase,
    pg_relation_size('q_before') AS heap_before,
    pg_relation_size('idx_before_status') AS idx_before_status,
    pg_relation_size('idx_before_claim') AS idx_before_claim,
    pg_relation_size('q_main') AS heap_main,
    pg_relation_size('idx_main_status') AS idx_main_status;
SELECT pg_current_wal_lsn() AS wal_baseline \gset

-- ------------------------------------------------------- BEFORE: claim --
-- One availability write per row (mirrors claimed CTE: status + updated_at).
UPDATE q_before
SET status = 'claimed', lease_owner = 'worker-1',
    claim_until = clock_timestamp() + interval '5 minutes',
    last_attempt_at = clock_timestamp(), attempt_count = attempt_count + 1,
    updated_at = clock_timestamp();
SELECT pg_stat_force_next_flush();
SELECT 'after_before_claim' AS phase, n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = 'q_before';

-- --------------------------------------------------- BEFORE: heartbeat --
-- Production heartbeat shape (reducer_queue.go:40-50): claim_until + updated_at.
-- The renewals themselves run from the runner-generated hb_before.sql: one row
-- per autocommitted transaction, round-robin, exactly like worker heartbeats.
SELECT pg_current_wal_lsn() AS wal_bhb0 \gset
\include :hb_before
SELECT pg_current_wal_lsn() AS wal_bhb1 \gset
SELECT pg_stat_force_next_flush();
SELECT 'after_before_hb' AS phase, n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = 'q_before';
SELECT 'before_hb_wal' AS phase, pg_wal_lsn_diff(:'wal_bhb1', :'wal_bhb0') AS wal_bytes;
SELECT 'before_hb_sizes' AS phase,
    pg_relation_size('q_before') AS heap_before,
    pg_relation_size('idx_before_status') AS idx_before_status,
    pg_relation_size('idx_before_claim') AS idx_before_claim;
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF, SUMMARY ON)
UPDATE q_before SET claim_until = clock_timestamp() + interval '5 minutes', updated_at = clock_timestamp()
WHERE work_item_id = 'w0000001';

-- --------------------------------------------------------- AFTER: claim --
-- Availability write stays on main; lease row inserted in the same step.
SELECT pg_current_wal_lsn() AS wal_ac0 \gset
UPDATE q_main SET status = 'claimed', attempt_count = attempt_count + 1, updated_at = clock_timestamp();
INSERT INTO q_lease (work_item_id, lease_owner, claim_until, last_attempt_at)
SELECT work_item_id, 'worker-1', clock_timestamp() + interval '5 minutes', clock_timestamp() FROM q_main;
SELECT pg_current_wal_lsn() AS wal_ac1 \gset
SELECT pg_stat_force_next_flush();
SELECT 'after_after_claim' AS phase, n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = 'q_main';
SELECT pg_stat_force_next_flush();
SELECT 'after_after_claim_lease' AS phase, n_tup_upd, n_tup_hot_upd, n_live_tup FROM pg_stat_user_tables WHERE relname = 'q_lease';
SELECT 'after_claim_wal' AS phase, pg_wal_lsn_diff(:'wal_ac1', :'wal_ac0') AS wal_bytes;

-- ----------------------------------------------------- AFTER: heartbeat --
-- Lease-only renewal: claim_until only, no indexed column, PK-only table.
-- Renewals run from the runner-generated hb_after.sql, one row per transaction.
SELECT pg_current_wal_lsn() AS wal_ahb0 \gset
\include :hb_after
SELECT pg_current_wal_lsn() AS wal_ahb1 \gset
SELECT pg_stat_force_next_flush();
SELECT 'after_after_hb' AS phase, n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = 'q_lease';
SELECT pg_stat_force_next_flush();
SELECT 'after_after_hb_main' AS phase, n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = 'q_main';
SELECT 'after_hb_wal' AS phase, pg_wal_lsn_diff(:'wal_ahb1', :'wal_ahb0') AS wal_bytes;
SELECT 'after_hb_sizes' AS phase,
    pg_relation_size('q_main') AS heap_main,
    pg_relation_size('idx_main_status') AS idx_main_status,
    pg_relation_size('q_lease') AS heap_lease,
    pg_relation_size('q_lease_pkey') AS pkey_lease;
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF, SUMMARY ON)
UPDATE q_lease SET claim_until = clock_timestamp() + interval '5 minutes' WHERE work_item_id = 'w0000001';

-- Cross-check: HOT-eligible only where no indexed column changed.
SELECT 'hot_eligible_check' AS phase,
    (SELECT count(*) FROM pg_indexes WHERE tablename = 'q_lease') AS lease_index_count,
    (SELECT count(*) FROM pg_indexes WHERE tablename = 'q_before') AS before_index_count;
