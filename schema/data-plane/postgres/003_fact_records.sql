CREATE TABLE IF NOT EXISTS fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS fact_records_scope_generation_idx
    ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC);

CREATE INDEX IF NOT EXISTS fact_records_stable_key_idx
    ON fact_records (stable_fact_key, generation_id);

CREATE INDEX IF NOT EXISTS fact_records_framework_routes_repo_path_idx
    ON fact_records ((payload->>'repo_id'), (payload->>'relative_path'))
    WHERE fact_kind = 'file'
      AND payload->'parsed_file_data'->'framework_semantics' IS NOT NULL
      AND jsonb_array_length(
          COALESCE(payload->'parsed_file_data'->'framework_semantics'->'frameworks', '[]'::jsonb)
      ) > 0;
