-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11131: the persisted cursor of the staged winners audit sweep.
--
-- The staged audit used to recompute every active winner in one tick and
-- deferred on its corpus budget once the impact-fact corpus passed 1,000,000
-- facts, so on QA (about 8.4M facts) it never completed. The sweep now walks
-- the keyed winners index order (package_id, repository_id, subject_digest,
-- c0, advisory_id) one bounded range at a time. This singleton row holds the
-- last identifier of the last completed range (all five NULL at sweep start)
-- and the last completed sweep, so a reducer roll resumes the sweep instead
-- of restarting it.
--
-- The writer advances the cursor in the same transaction as the range's
-- verify and repairs, by compare-and-set on sweep_id and the five cursor
-- columns: two pods cannot advance one range twice, and a lost race rolls
-- its repairs back. The row is created on first read by the reducer
-- (INSERT ... ON CONFLICT DO NOTHING), so this file stays DDL only (#7961).
--
-- Additive and idempotent. Older binaries never read the table; a rollback
-- leaves it unused, and a later roll forward resumes from the stored cursor.
CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_audit_sweep (
    singleton SMALLINT PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    sweep_id BIGINT NOT NULL DEFAULT 1,
    cursor_package_id TEXT,
    cursor_repository_id TEXT,
    cursor_subject_digest TEXT,
    cursor_c0 TEXT,
    cursor_advisory_id TEXT,
    sweep_started_at TIMESTAMPTZ,
    ranges_done BIGINT NOT NULL DEFAULT 0,
    sweep_facts BIGINT NOT NULL DEFAULT 0,
    sweep_candidates BIGINT NOT NULL DEFAULT 0,
    sweep_repairs BIGINT NOT NULL DEFAULT 0,
    sweep_discards BIGINT NOT NULL DEFAULT 0,
    last_sweep_completed_at TIMESTAMPTZ,
    last_sweep_seconds DOUBLE PRECISION,
    last_sweep_ranges BIGINT,
    last_sweep_facts BIGINT,
    last_sweep_candidates BIGINT,
    last_sweep_repairs BIGINT,
    last_sweep_discards BIGINT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT supply_chain_impact_winners_audit_sweep_cursor_shape CHECK (
        (cursor_package_id IS NULL AND cursor_repository_id IS NULL
            AND cursor_subject_digest IS NULL AND cursor_c0 IS NULL AND cursor_advisory_id IS NULL)
        OR
        (cursor_package_id IS NOT NULL AND cursor_repository_id IS NOT NULL
            AND cursor_subject_digest IS NOT NULL AND cursor_c0 IS NOT NULL AND cursor_advisory_id IS NOT NULL)
    )
);
