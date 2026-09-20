-- #6785: readiness-wait ledger for the cross-scope CAN_PERFORM and USES
-- edge handlers, keyed by (scope_id, domain).
--
-- The bound on a cross-scope wait used to be measured from the reducer row's
-- own created_at. A newer generation supersedes a deferred row, and the new
-- row starts a new bound, so a scope whose generation cadence is shorter than
-- the bound never commits (review finding R2-F1). This row outlives the
-- per-generation queue row: first_deferred_at is the surviving anchor,
-- settled_at marks a missing set whose bound already expired, and the
-- committed_* columns record the last partial commit so a poll with an
-- unchanged missing set writes nothing.
--
-- anchor_epoch fences lease-expired stragglers (review P3-1). An anchor reset
-- or a clear moves the row to the next epoch, and an upsert from a lower epoch
-- is dropped, so a writer that read the row before the reset cannot restore
-- the older anchor. A clear keeps the row as a tombstone (cleared_at set, no
-- missing set) so its epoch still fences a straggler that would otherwise
-- re-insert a stale wait. A settle also moves the row to the next epoch, so a
-- straggler cannot un-settle it (review P3-a). row_version counts applied
-- writes; a clear is a compare-and-set on the version it read, so a straggler
-- clear cannot tombstone a wait a live worker rewrote at the same epoch
-- (review P3-b). Rows are bounded by two per scope (one per domain).
--
-- A new table instead of a fact_work_items column keeps the hot queue table
-- and its claim query unchanged. Writes are single-statement primary-key
-- upserts from the worker holding the (scope, domain) claim, so they add no
-- lock-order edge. CREATE TABLE takes no lock on an existing table.
CREATE TABLE IF NOT EXISTS reducer_readiness_waits (
    scope_id TEXT NOT NULL,
    domain TEXT NOT NULL,
    first_deferred_at TIMESTAMPTZ NOT NULL,
    missing_keys JSONB NOT NULL DEFAULT '[]'::jsonb,
    missing_count INTEGER NOT NULL DEFAULT 0,
    missing_fingerprint TEXT NOT NULL,
    committed_generation_id TEXT NOT NULL DEFAULT '',
    committed_cycle_started_at TIMESTAMPTZ NULL,
    committed_fingerprint TEXT NOT NULL DEFAULT '',
    settled_at TIMESTAMPTZ NULL,
    anchor_epoch BIGINT NOT NULL DEFAULT 0,
    cleared_at TIMESTAMPTZ NULL,
    row_version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT reducer_readiness_waits_pkey PRIMARY KEY (scope_id, domain),
    CONSTRAINT reducer_readiness_waits_missing_count_check
        CHECK (missing_count >= 0),
    CONSTRAINT reducer_readiness_waits_anchor_epoch_check
        CHECK (anchor_epoch >= 0),
    CONSTRAINT reducer_readiness_waits_row_version_check
        CHECK (row_version >= 0)
);
