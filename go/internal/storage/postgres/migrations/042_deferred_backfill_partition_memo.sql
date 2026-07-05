CREATE TABLE IF NOT EXISTS deferred_backfill_partition_memo (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    catalog_fingerprint TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
CREATE INDEX IF NOT EXISTS deferred_backfill_partition_memo_committed_idx
    ON deferred_backfill_partition_memo (committed_at DESC);
