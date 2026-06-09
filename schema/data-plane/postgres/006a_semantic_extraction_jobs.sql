CREATE TABLE IF NOT EXISTS semantic_extraction_jobs (
    job_id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    source_class TEXT NOT NULL,
    source_id_hash TEXT NOT NULL,
    chunk_id_hash TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    chunk_hash TEXT NOT NULL,
    source_version TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    redaction_version TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    extraction_mode TEXT NOT NULL,
    provider_kind TEXT NULL,
    provider_profile_id TEXT NULL,
    provider_profile_class TEXT NULL,
    policy_id TEXT NULL,
    rule_id TEXT NULL,
    policy_state TEXT NULL,
    policy_reason TEXT NULL,
    guard_state TEXT NULL,
    guard_reason TEXT NULL,
    actor_class TEXT NULL,
    acl_state TEXT NULL,
    classifier_version TEXT NULL,
    status TEXT NOT NULL,
    provider_job BOOLEAN NOT NULL DEFAULT false,
    retryable BOOLEAN NOT NULL DEFAULT false,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NULL,
    claim_until TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    stale_reason TEXT NULL,
    stale_at TIMESTAMPTZ NULL,
    response_hash TEXT NULL,
    budget_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS semantic_extraction_jobs_work_item_idx
    ON semantic_extraction_jobs (work_item_id);

CREATE INDEX IF NOT EXISTS semantic_extraction_jobs_scope_generation_status_idx
    ON semantic_extraction_jobs (scope_id, generation_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS semantic_extraction_jobs_fingerprint_idx
    ON semantic_extraction_jobs (scope_id, source_id_hash, chunk_id_hash, fingerprint);

CREATE INDEX IF NOT EXISTS semantic_extraction_jobs_claim_idx
    ON semantic_extraction_jobs (status, claim_until, updated_at ASC)
    WHERE status IN ('pending', 'retrying');

CREATE INDEX IF NOT EXISTS semantic_extraction_jobs_provider_claim_idx
    ON semantic_extraction_jobs (scope_id, status, next_attempt_at, claim_until, updated_at ASC, job_id)
    WHERE status IN ('pending', 'retrying') AND provider_job = true;
