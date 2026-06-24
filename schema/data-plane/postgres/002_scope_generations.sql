CREATE TABLE IF NOT EXISTS scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL,
    freshness_hint TEXT NULL,
    source_commit_sha TEXT NULL,
    is_delta BOOLEAN NOT NULL DEFAULT false,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- Additive migration for installs created before the delta-correctness
-- baseline (epic #2340): source_commit_sha carries the commit a generation was
-- observed from so the next git sync can baseline its delta on the last
-- successfully projected commit instead of the local working-copy HEAD; is_delta
-- marks delta resyncs so the reconciliation sweep can find the last full
-- observation per scope.
ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS source_commit_sha TEXT NULL;

ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS is_delta BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS scope_generations_scope_idx
    ON scope_generations (scope_id, status, ingested_at DESC);

-- Backs the latest-generation DISTINCT ON (issue #3704): the relationship
-- backfill and active-generation lookups pick each scope's newest generation by
-- ORDER BY (scope_id, ingested_at DESC, generation_id DESC). The status-leading
-- scope_generations_scope_idx cannot serve that ordering without a sort because
-- status sits between scope_id and ingested_at; this index leads straight into
-- the DISTINCT ON sort key so the per-scope newest row is an index read.
CREATE INDEX IF NOT EXISTS scope_generations_scope_latest_lookup_idx
    ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);

CREATE INDEX IF NOT EXISTS scope_generations_active_pending_activity_idx
    ON scope_generations (GREATEST(observed_at, ingested_at, COALESCE(activated_at, observed_at)) DESC)
    WHERE status IN ('pending', 'active');

CREATE UNIQUE INDEX IF NOT EXISTS scope_generations_active_scope_idx
    ON scope_generations (scope_id)
    WHERE status = 'active';
