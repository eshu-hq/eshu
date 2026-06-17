CREATE TABLE IF NOT EXISTS graph_endpoint_presence (
    keyspace TEXT NOT NULL,
    uid TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    repo_id TEXT NOT NULL DEFAULT '',
    source_generation TEXT NOT NULL DEFAULT '',
    committed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (keyspace, uid)
);
ALTER TABLE graph_endpoint_presence
    ADD COLUMN IF NOT EXISTS repo_id TEXT NOT NULL DEFAULT '';
ALTER TABLE graph_endpoint_presence
    ADD COLUMN IF NOT EXISTS source_generation TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_scope_idx
    ON graph_endpoint_presence (scope_id);
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_updated_idx
    ON graph_endpoint_presence (updated_at DESC);
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_stale_idx
    ON graph_endpoint_presence (keyspace, scope_id, repo_id);
