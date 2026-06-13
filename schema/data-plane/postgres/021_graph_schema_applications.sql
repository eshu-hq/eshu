CREATE TABLE IF NOT EXISTS graph_schema_applications (
    backend TEXT NOT NULL,
    schema_fingerprint TEXT NOT NULL,
    statement_count INTEGER NOT NULL,
    compatible_fingerprints JSONB NOT NULL DEFAULT '[]'::jsonb,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (backend, schema_fingerprint)
);

ALTER TABLE graph_schema_applications
    ADD COLUMN IF NOT EXISTS compatible_fingerprints JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS graph_schema_applications_backend_idx
    ON graph_schema_applications (backend, applied_at DESC);
