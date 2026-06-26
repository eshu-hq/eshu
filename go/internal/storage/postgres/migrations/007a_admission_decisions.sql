CREATE TABLE IF NOT EXISTS admission_decisions (
    decision_id TEXT PRIMARY KEY,
    domain TEXT NOT NULL,
    state TEXT NOT NULL,
    domain_state TEXT NOT NULL,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    anchor_kind TEXT NOT NULL,
    anchor_id TEXT NOT NULL,
    candidate_kind TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    confidence_score DOUBLE PRECISION NOT NULL,
    confidence_bucket TEXT NOT NULL,
    confidence_basis TEXT NOT NULL,
    freshness_state TEXT NOT NULL,
    freshness_observed_at TIMESTAMPTZ NULL,
    freshness_cause TEXT NOT NULL,
    source_handles JSONB NOT NULL DEFAULT '[]'::jsonb,
    redaction_state TEXT NOT NULL,
    redaction_reason TEXT NOT NULL,
    canonical_write JSONB NOT NULL DEFAULT '{}'::jsonb,
    recommended_action JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_version TEXT NOT NULL,
    decided_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT admission_decisions_state_check
        CHECK (state IN ('admitted', 'rejected', 'ambiguous', 'stale', 'missing_evidence', 'permission_hidden', 'unsupported', 'unsafe'))
);

CREATE INDEX IF NOT EXISTS admission_decisions_scope_generation_domain_idx
    ON admission_decisions (scope_id, generation_id, domain, state, updated_at DESC, decision_id);

CREATE INDEX IF NOT EXISTS admission_decisions_anchor_idx
    ON admission_decisions (scope_id, generation_id, anchor_kind, anchor_id, domain, updated_at DESC, decision_id);

CREATE INDEX IF NOT EXISTS admission_decisions_candidate_idx
    ON admission_decisions (candidate_kind, candidate_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS admission_decision_evidence (
    evidence_id TEXT PRIMARY KEY,
    decision_id TEXT NOT NULL REFERENCES admission_decisions(decision_id) ON DELETE CASCADE,
    source_handle TEXT NOT NULL,
    evidence_kind TEXT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS admission_decision_evidence_decision_idx
    ON admission_decision_evidence (decision_id, created_at ASC, evidence_id ASC);

CREATE INDEX IF NOT EXISTS admission_decision_evidence_source_handle_idx
    ON admission_decision_evidence (source_handle, created_at DESC);
