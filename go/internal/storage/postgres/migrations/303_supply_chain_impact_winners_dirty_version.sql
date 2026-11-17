-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10827 (arbiter verdict R2: Global Key-Ordered Versioned Upsert): the
-- supply-chain impact winners dirty log collapses repeated marks of one
-- recompute unit at write time. One dirty row per pending recompute unit;
-- every re-mark of a queued key bumps dirty_version instead of appending a
-- row, so the resweep queue grows with distinct units, not with marks.
--
-- This file adds the version column the writer upserts bump. DDL only:
-- the backfill DELETEs that collapse the append-only backlog ride the next
-- migration (304), because table-locking DDL and bulk DML must not share a
-- file (#7961). The two partial unique indexes the writer upserts target
-- ship after the collapse, one single-statement CONCURRENTLY migration each
-- (305 for the fact five-tuple, 306 for the scope pair), and the 272/272a
-- trigger functions become key-ordered upserts after them (307, 308: the
-- ON CONFLICT clauses need the indexes to exist before any trigger can
-- fire them, so the function replacement lands after the builds).
--
-- The ADD COLUMN is catalog-only (PostgreSQL 11+ stores the constant
-- DEFAULT without rewriting the heap), so writers keep inserting while this
-- migration runs. The survivor keeps version 1: the count it stands for is
-- one collapsed unit going forward, and the drain tally sums versions from
-- here on.
ALTER TABLE supply_chain_impact_winners_dirty_log
    ADD COLUMN IF NOT EXISTS dirty_version BIGINT NOT NULL DEFAULT 1;
