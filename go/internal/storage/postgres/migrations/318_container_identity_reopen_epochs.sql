-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10878: the changed-evidence gate for the container_image_identity reopen,
-- mirroring #8283's deployable_unit_reopen_fingerprints (migration 193) and
-- #10878's aws_drift_reopen_fingerprints (migration 317).
--
-- One row per succeeded container_image_identity work item: the identity
-- epoch its run read (epoch_count, epoch_max_observed_at,
-- epoch_fingerprint -- the count, max observed_at, and ordered-set SHA-256
-- the probe query already returns). The deferred-maintenance reopen
-- recomputes the epoch once per pass and re-opens the item only when the row
-- is missing, when logic_version is not the handler's current version, or
-- when any of the three epoch fields differs. A missing row always re-opens.
-- The epoch covers the active identity set fleet-wide (not per scope), so a
-- quiet fleet converges the whole domain to skips; any active-set insert or
-- delete moves the epoch and re-opens every recorded item, which then
-- re-derives through the already-shared once-per-epoch fleet load.
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
CREATE TABLE IF NOT EXISTS container_identity_reopen_epochs (
    work_item_id TEXT PRIMARY KEY
        REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    epoch_count BIGINT NOT NULL,
    epoch_max_observed_at TIMESTAMPTZ NULL,
    epoch_fingerprint TEXT NOT NULL,
    logic_version INTEGER NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);
