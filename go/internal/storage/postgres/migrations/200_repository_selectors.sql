-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7840: repository_selectors is the per-selector liveness marker for
-- repository_selection_observations (#7625, #7774). Every collector cycle
-- upserts one row for its selector -- including a guard-tripped or
-- listing-truncated cycle, which writes no observation rows -- advancing
-- last_cycle_at to the cycle time. The expired-observation sweep deletes a
-- not_listed row only when its selector's marker is older than the row's own
-- liveness window plus the expired-observation grace (or the selector has no
-- marker at all): a selector that is still cycling keeps the not_listed
-- history the mass-miss guard reads, while an abandoned selector's rows
-- (credential rotation, rules change, owner change) are finally reclaimed.
--
-- The backfill stamps every selector that already has observation rows with
-- the migration's clock_timestamp(), not its newest evaluated_at: a selector
-- guard-tripped since before this migration must not become reclaimable in
-- the window between bootstrap and its first post-migration cycle. The cost
-- is that already-abandoned selectors wait one window plus grace after this
-- migration lands. ON CONFLICT DO NOTHING keeps the backfill a one-time
-- stamp: this file runs on EVERY bootstrap (there is no applied-migration
-- ledger), so a plain insert-or-update would refresh abandoned markers on
-- every restart and nothing would ever be reclaimed.
--
-- Marker rows themselves are never deleted. They are bounded by the number
-- of selectors ever created, and a missing marker reads as abandoned, so
-- deleting one could only make its selector's rows reclaimable sooner.
--
-- No index beyond the primary key: the sweep joins on selector_id, and the
-- table holds one row per selector ever created.
CREATE TABLE IF NOT EXISTS repository_selectors (
    selector_id TEXT NOT NULL PRIMARY KEY,
    selector_kind TEXT NOT NULL CHECK (selector_kind IN ('github_org', 'explicit', 'gitlab_group')),
    selector_owner TEXT NOT NULL,
    last_cycle_at TIMESTAMPTZ NOT NULL,
    liveness_window_seconds INTEGER NOT NULL CHECK (liveness_window_seconds > 0)
);

INSERT INTO repository_selectors (selector_id, selector_kind, selector_owner, last_cycle_at, liveness_window_seconds)
SELECT DISTINCT ON (selector_id)
    selector_id, selector_kind, selector_owner, clock_timestamp(), liveness_window_seconds
FROM repository_selection_observations
ORDER BY selector_id, evaluated_at DESC
ON CONFLICT (selector_id) DO NOTHING;
