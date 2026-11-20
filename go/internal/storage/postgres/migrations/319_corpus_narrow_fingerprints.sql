-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10877: the changed-evidence gate for the workload_materialization and
-- service_catalog_correlation reopens, mirroring migration 193.
--
-- One row per succeeded work item: the repository set its narrowed
-- corpus-fenced resolved read asked for (repo_ids) and the input fingerprint
-- that same statement returned (input_fingerprint). The deferred-maintenance
-- reopen recomputes the fingerprint over repo_ids with the same SQL fragment
-- and re-opens the item when the row is missing, when the two values differ,
-- or when logic_version is not the handler's current version. A missing row
-- always re-opens. input_fingerprint is the hex sha256 of the sorted
-- resolved_id set of the active resolved rows touching repo_ids, or the
-- empty-resolved-input sentinel for an empty set; repo_ids is '{}' for an
-- item whose run read no by-repos input.
--
-- The handler writes the row after its writes, fenced on the exact reducer
-- claim (work_item_id, last_attempt_at, live claim_until), exactly like the
-- deployable_unit_correlation path (#8283). FK terms match 193: the foreign
-- key takes SHARE ROW EXCLUSIVE on fact_work_items while the table is
-- created, bounded by the runner's lock_timeout and retry (schema.go). The
-- primary key on work_item_id is the index retention deletes probe.
CREATE TABLE IF NOT EXISTS workload_materialization_reopen_fingerprints (
    work_item_id TEXT PRIMARY KEY
        REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    repo_ids TEXT[] NOT NULL,
    input_fingerprint TEXT NOT NULL,
    logic_version INTEGER NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS service_catalog_correlation_reopen_fingerprints (
    work_item_id TEXT PRIMARY KEY
        REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    repo_ids TEXT[] NOT NULL,
    input_fingerprint TEXT NOT NULL,
    logic_version INTEGER NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);
