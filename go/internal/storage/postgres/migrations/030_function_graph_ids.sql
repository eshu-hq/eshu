CREATE TABLE IF NOT EXISTS function_graph_ids (
    function_id TEXT PRIMARY KEY,
    uid TEXT NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS function_graph_ids_repo_idx
    ON function_graph_ids (repo, function_id);

CREATE INDEX IF NOT EXISTS function_graph_ids_uid_idx
    ON function_graph_ids (uid);
