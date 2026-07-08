CREATE TABLE IF NOT EXISTS code_taint_evidence_projected_node (
    evidence_source TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    node_uid TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (evidence_source, scope_id, generation_id, node_uid)
);
CREATE INDEX IF NOT EXISTS code_taint_evidence_projected_node_source_scope_idx
    ON code_taint_evidence_projected_node (evidence_source, scope_id);
CREATE INDEX IF NOT EXISTS code_taint_evidence_projected_node_source_idx
    ON code_taint_evidence_projected_node (evidence_source);
CREATE INDEX IF NOT EXISTS code_taint_evidence_projected_node_stale_idx
    ON code_taint_evidence_projected_node (evidence_source, scope_id, generation_id);
