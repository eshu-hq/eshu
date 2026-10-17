-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8283: the changed-evidence gate for the deployable_unit_correlation reopen.
--
-- One row per succeeded deployable_unit_correlation work item: the repository
-- set its corpus-fenced resolved read asked for (repo_ids) and the input
-- fingerprint that same statement returned (input_fingerprint). The
-- maintenance reopen recomputes the fingerprint over repo_ids with the same
-- SQL fragment and re-opens the item when the row is missing, when the two
-- values differ, or when logic_version is not the handler's current version,
-- so a wave stops replaying items whose resolved input did not change. A
-- missing row always re-opens. input_fingerprint is the hex sha256 of the sorted
-- resolved_id set of the active resolved rows touching repo_ids, or a fixed
-- sentinel for an empty set; repo_ids is '{}' for an item with no workload
-- candidate.
--
-- The handler writes the row after its graph writes, fenced on the exact
-- reducer claim (work_item_id, last_attempt_at, live claim_until). claimed_at
-- keeps the claim the row came from, so a stale write can never replace a row
-- recorded by a later claim.
--
-- New table only, but the foreign key takes SHARE ROW EXCLUSIVE on
-- fact_work_items while the table is created, which briefly blocks queue
-- writers. That lock is bounded by the runner's lock_timeout and retry
-- (schema.go), as for the other FK-bearing tables on fact_work_items. The
-- primary key on work_item_id is the index the cascade from fact_work_items
-- probes, so retention deletes stay index lookups.
CREATE TABLE IF NOT EXISTS deployable_unit_reopen_fingerprints (
    work_item_id TEXT PRIMARY KEY
        REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    repo_ids TEXT[] NOT NULL,
    input_fingerprint TEXT NOT NULL,
    logic_version INTEGER NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);
