CREATE TABLE IF NOT EXISTS function_summaries (
    function_id TEXT PRIMARY KEY,
    effects JSONB NOT NULL,
    version TEXT NOT NULL,
    structural_hash TEXT NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS function_summaries_repo_idx
    ON function_summaries (repo, function_id);

CREATE INDEX IF NOT EXISTS function_summaries_updated_idx
    ON function_summaries (updated_at DESC, function_id);
