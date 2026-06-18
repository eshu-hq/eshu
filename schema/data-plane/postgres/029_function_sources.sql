CREATE TABLE IF NOT EXISTS function_sources (
    function_id TEXT NOT NULL,
    param_index INTEGER NOT NULL,
    source_kind TEXT NOT NULL,
    source_label TEXT NOT NULL DEFAULT '',
    lang TEXT NOT NULL DEFAULT '',
    repo TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (function_id, param_index, source_kind, source_label)
);

CREATE INDEX IF NOT EXISTS function_sources_repo_idx
    ON function_sources (repo, function_id, param_index);
