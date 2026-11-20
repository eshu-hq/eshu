-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10878: the changed-evidence gate for the aws_cloud_runtime_drift reopen,
-- mirroring #8283's deployable_unit_reopen_fingerprints (migration 193).
--
-- One row per succeeded aws_cloud_runtime_drift work item: the scope and
-- generation its evidence load read (scope_id, generation_id), the ARN set
-- that generation carried (arn_ids, sorted), the per-ARN payload identity
-- (arn_payload_hashes, aligned with arn_ids, each the hex sha256 of the
-- fact payload text -- the same expression AWSDriftARNFingerprintSQL hashes),
-- and the input fingerprint (arn_fingerprint, the hex sha256 of the sorted
-- "arn newline payload-hash" pairs joined by newlines, or a fixed sentinel
-- for an empty set; Go twin: contract.ComputeARNInputFingerprint).
--
-- The deferred-maintenance reopen recomputes the fingerprint over the scope's
-- CURRENT active generation with the same SQL fragment and re-opens the item
-- when the row is missing, when the two values differ, or when logic_version
-- is not the handler's current version. A missing row always re-opens. The
-- recorded arn_ids/arn_payload_hashes let the next run diff its previous
-- snapshot against the fresh load and narrow to changed ARNs; removals
-- retract findings and never widen to whole-repo deletes (the
-- delta-generation dead-letter rule).
--
-- The handler writes the row after its durable write, fenced on the exact
-- reducer claim (work_item_id, last_attempt_at, live claim_until).
-- claimed_at keeps the claim the row came from, so a stale write can never
-- replace a row recorded by a later claim.
--
-- New table only, but the foreign key takes SHARE ROW EXCLUSIVE on
-- fact_work_items while the table is created, which briefly blocks queue
-- writers. That lock is bounded by the runner's lock_timeout and retry
-- (schema.go), as for the other FK-bearing tables on fact_work_items. The
-- primary key on work_item_id is the index the cascade from fact_work_items
-- probes, so retention deletes stay index lookups.
CREATE TABLE IF NOT EXISTS aws_drift_reopen_fingerprints (
    work_item_id TEXT PRIMARY KEY
        REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    arn_ids TEXT[] NOT NULL,
    arn_payload_hashes TEXT[] NOT NULL,
    arn_fingerprint TEXT NOT NULL,
    logic_version INTEGER NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);
