CREATE TABLE IF NOT EXISTS cicd_run_watermarks (
    scope_id TEXT NOT NULL,
    repository TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    last_run_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, repository)
);
