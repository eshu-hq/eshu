CREATE TABLE IF NOT EXISTS reducer_input_invalid_facts (
    fact_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL,
    missing_field TEXT NOT NULL,
    failure_class TEXT NOT NULL,
    domain TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    decided_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, fact_id, missing_field, domain)
);

CREATE INDEX IF NOT EXISTS reducer_input_invalid_facts_scope_generation_domain_idx
    ON reducer_input_invalid_facts (scope_id, generation_id, domain, fact_kind, decided_at DESC);
