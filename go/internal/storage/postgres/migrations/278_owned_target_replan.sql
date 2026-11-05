-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9777 step 2 (change-driven re-query): the owned-target replan queue and
-- its commit watermark.
--
-- owned_target_replan holds one row per changed (collector instance,
-- advisory, ecosystem, package) tuple the coordinator has not yet replanned.
-- Admission dedups on that tuple: a re-presented feed row refreshes the row
-- only when its feed timestamp is strictly newer, so rewinds are duplicates,
-- never misses. A drained row is marked enqueued, never deleted, so a
-- re-presented fact after the drain still finds its tuple.
--
-- owned_target_replan_watermarks holds one row per collector instance: the
-- feed timestamp of the newest drained record (committed_feed_timestamp,
-- NULL until the first drain commits) and the last successful poll time
-- (last_poll_at, NULL until the first successful pass). The commit timestamp
-- advances only inside the drain-commit transaction by compare-and-set, in
-- the same statement batch that marks the covered rows enqueued, so the
-- watermark can never advance past a row that transaction did not mark.
-- last_poll_at rides a separate monotonic statement because quiet days have
-- no drain transaction; the osv_delta_watermark_stale alarm reads it.
--
-- The only read is the per-instance pending drain (ordered by feed time),
-- served by the pending index; the keyed admit and the keyed commit/watermark
-- writes are served by the unique tuple and the primary key. Insert and
-- update churn is one row per changed tuple per instance plus one watermark
-- row per instance.
CREATE TABLE IF NOT EXISTS owned_target_replan (
    id BIGINT GENERATED ALWAYS AS IDENTITY,
    collector_instance_id TEXT NOT NULL,
    advisory_id TEXT NOT NULL,
    ecosystem TEXT NOT NULL,
    package_name TEXT NOT NULL,
    withdrawn BOOLEAN NOT NULL DEFAULT FALSE,
    feed_timestamp TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    enqueued_at TIMESTAMPTZ,
    PRIMARY KEY (id),
    CONSTRAINT owned_target_replan_tuple_unique UNIQUE (collector_instance_id, advisory_id, ecosystem, package_name),
    CONSTRAINT owned_target_replan_status_check CHECK (status IN ('pending', 'enqueued'))
);
CREATE INDEX IF NOT EXISTS owned_target_replan_pending_idx
    ON owned_target_replan (collector_instance_id, status, feed_timestamp, id);

CREATE TABLE IF NOT EXISTS owned_target_replan_watermarks (
    collector_instance_id TEXT PRIMARY KEY,
    committed_feed_timestamp TIMESTAMPTZ,
    last_poll_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL
);
