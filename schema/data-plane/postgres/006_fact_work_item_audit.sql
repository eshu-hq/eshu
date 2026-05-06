CREATE TABLE IF NOT EXISTS fact_replay_events (
    replay_event_id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    failure_class TEXT NULL,
    operator_note TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS fact_replay_events_work_item_idx
    ON fact_replay_events (work_item_id, created_at DESC);

CREATE TABLE IF NOT EXISTS fact_backfill_requests (
    backfill_request_id TEXT PRIMARY KEY,
    scope_id TEXT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE SET NULL,
    generation_id TEXT NULL REFERENCES scope_generations(generation_id) ON DELETE SET NULL,
    operator_note TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS fact_backfill_requests_scope_idx
    ON fact_backfill_requests (scope_id, generation_id, created_at DESC);
