
CREATE TABLE IF NOT EXISTS projection_decisions (
    decision_id TEXT PRIMARY KEY,
    decision_type TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    work_item_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    confidence_score DOUBLE PRECISION NOT NULL,
    confidence_reason TEXT NOT NULL,
    provenance_summary JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS projection_decisions_repo_run_idx
    ON projection_decisions (repository_id, source_run_id, created_at);

CREATE TABLE IF NOT EXISTS projection_decision_evidence (
    evidence_id TEXT PRIMARY KEY,
    decision_id TEXT NOT NULL,
    fact_id TEXT NULL,
    evidence_kind TEXT NOT NULL,
    detail JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS projection_decision_evidence_decision_idx
    ON projection_decision_evidence (decision_id, created_at);
