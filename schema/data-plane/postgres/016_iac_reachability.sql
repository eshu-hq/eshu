CREATE TABLE IF NOT EXISTS iac_reachability_rows (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    repo_id TEXT NOT NULL,
    family TEXT NOT NULL,
    artifact_path TEXT NOT NULL,
    artifact_name TEXT NOT NULL,
    reachability TEXT NOT NULL,
    finding TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    evidence JSONB NOT NULL,
    limitations JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, repo_id, family, artifact_path)
);

CREATE INDEX IF NOT EXISTS iac_reachability_cleanup_idx
    ON iac_reachability_rows (scope_id, generation_id, reachability, family, repo_id);

CREATE INDEX IF NOT EXISTS iac_reachability_artifact_idx
    ON iac_reachability_rows (repo_id, family, artifact_path);
