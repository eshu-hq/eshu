CREATE TABLE IF NOT EXISTS graph_endpoint_presence (
    keyspace TEXT NOT NULL,
    uid TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    committed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (keyspace, uid)
);
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_scope_idx
    ON graph_endpoint_presence (scope_id);
CREATE INDEX IF NOT EXISTS graph_endpoint_presence_updated_idx
    ON graph_endpoint_presence (updated_at DESC);
