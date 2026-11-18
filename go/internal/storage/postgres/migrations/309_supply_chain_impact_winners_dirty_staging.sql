-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10837 direction C (hot-key coalescing): the shared staging queue the
-- capture owners append to. Every capture path (fact_upsert, fact_retract,
-- the activation/operator/scanner triggers, the audit queue, and scope-kind
-- rows) plain-INSERTs one row per mark here; the maintainer tick collapses
-- each wave into the versioned dirty log with one key-ordered GROUP BY
-- merge-upsert and a seq-bounded delete before admission.
--
-- The table carries NO unique index on the hot path: a re-mark of a queued
-- key appends another staged row instead of waiting on a conflict arbiter,
-- so concurrent writers over the same hot keys never serialize on each
-- other. Ordering for the seq-bounded collapse delete comes from an explicit
-- sequence plus a NON-UNIQUE btree on seq (append-ordered inserts, no
-- arbiter wait). The shape CHECK is the twin of migration 272's, so fact
-- and scope rows validate identically.
--
-- DDL only: the table starts empty, so no DML rides this file (#7961).
CREATE SEQUENCE IF NOT EXISTS supply_chain_impact_winners_dirty_staging_seq;

CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_dirty_staging (
    seq BIGINT NOT NULL DEFAULT nextval('supply_chain_impact_winners_dirty_staging_seq'),
    kind TEXT NOT NULL CHECK (kind IN ('fact', 'scope')),
    source TEXT NOT NULL CHECK (source IN
        ('fact_upsert', 'fact_retract', 'activation', 'operator', 'scanner', 'audit')),
    package_id TEXT,
    repository_id TEXT,
    subject_digest TEXT,
    c0 TEXT,
    advisory_id TEXT,
    scope_id TEXT,
    generation_id TEXT,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT supply_chain_impact_winners_dirty_staging_shape CHECK (
        (kind = 'fact'
            AND package_id IS NOT NULL AND repository_id IS NOT NULL
            AND subject_digest IS NOT NULL AND c0 IS NOT NULL AND advisory_id IS NOT NULL
            AND scope_id IS NULL AND generation_id IS NULL)
        OR
        (kind = 'scope'
            AND scope_id IS NOT NULL AND generation_id IS NOT NULL
            AND package_id IS NULL AND repository_id IS NULL
            AND subject_digest IS NULL AND c0 IS NULL AND advisory_id IS NULL)
    )
) WITH (
    autovacuum_vacuum_scale_factor = 0,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0,
    autovacuum_analyze_threshold = 5000
);

-- Non-unique ordering index for the collapse's seq-bounded delete (oldest
-- seq first). Deliberately NOT unique: a unique enforcement would put an
-- arbiter back on the insert path the staging exists to free.
CREATE INDEX IF NOT EXISTS supply_chain_impact_winners_dirty_staging_seq_idx
    ON supply_chain_impact_winners_dirty_staging (seq);
