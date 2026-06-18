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

CREATE TABLE IF NOT EXISTS function_summary_generations (
    generation_id TEXT NOT NULL,
    function_id TEXT NOT NULL,
    effects JSONB NOT NULL,
    version TEXT NOT NULL,
    structural_hash TEXT NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (generation_id, function_id)
);

CREATE INDEX IF NOT EXISTS function_summary_generations_repo_generation_idx
    ON function_summary_generations (repo, generation_id, function_id);

CREATE INDEX IF NOT EXISTS function_summary_generations_updated_idx
    ON function_summary_generations (updated_at DESC, generation_id, function_id);
