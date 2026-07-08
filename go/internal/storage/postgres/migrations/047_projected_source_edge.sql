CREATE TABLE IF NOT EXISTS projected_source_edge (
    evidence_source TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    source_uid TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (evidence_source, scope_id, generation_id, source_uid)
);
CREATE INDEX IF NOT EXISTS projected_source_edge_source_scope_idx
    ON projected_source_edge (evidence_source, scope_id);
CREATE INDEX IF NOT EXISTS projected_source_edge_source_idx
    ON projected_source_edge (evidence_source);
CREATE INDEX IF NOT EXISTS projected_source_edge_stale_idx
    ON projected_source_edge (evidence_source, scope_id, generation_id);
