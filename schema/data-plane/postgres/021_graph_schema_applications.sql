CREATE TABLE IF NOT EXISTS graph_schema_applications (
    backend TEXT NOT NULL,
    schema_fingerprint TEXT NOT NULL,
    statement_count INTEGER NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (backend, schema_fingerprint)
);

CREATE INDEX IF NOT EXISTS graph_schema_applications_backend_idx
    ON graph_schema_applications (backend, applied_at DESC);
