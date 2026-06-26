CREATE TABLE IF NOT EXISTS fact_work_items (
    work_item_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    stage TEXT NOT NULL,
    domain TEXT NOT NULL,
    conflict_domain TEXT NOT NULL DEFAULT 'scope',
    conflict_key TEXT NULL,
    status TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NULL,
    claim_until TIMESTAMPTZ NULL,
    visible_at TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS conflict_domain TEXT NOT NULL DEFAULT 'scope';

ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS conflict_key TEXT NULL;

CREATE INDEX IF NOT EXISTS fact_work_items_scope_generation_idx
    ON fact_work_items (scope_id, generation_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_status_idx
    ON fact_work_items (status, visible_at, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_stage_domain_status_idx
    ON fact_work_items (stage, domain, status, visible_at, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_claim_until_idx
    ON fact_work_items (claim_until)
    WHERE claim_until IS NOT NULL;

CREATE INDEX IF NOT EXISTS fact_work_items_reducer_conflict_claim_idx
    ON fact_work_items (stage, conflict_domain, COALESCE(conflict_key, scope_id), status, claim_until, updated_at DESC)
    WHERE stage = 'reducer';
