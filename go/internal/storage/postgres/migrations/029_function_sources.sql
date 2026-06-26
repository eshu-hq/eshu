CREATE TABLE IF NOT EXISTS function_sources (
    function_id TEXT NOT NULL,
    param_index INTEGER NOT NULL,
    kind TEXT NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (function_id, param_index)
);
CREATE INDEX IF NOT EXISTS function_sources_repo_idx
    ON function_sources (repo, function_id);
