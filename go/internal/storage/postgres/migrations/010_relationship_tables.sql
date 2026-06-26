
CREATE TABLE IF NOT EXISTS relationship_assertions (
    assertion_id TEXT PRIMARY KEY,
    source_repo_id TEXT NOT NULL,
    target_repo_id TEXT NOT NULL,
    source_entity_id TEXT,
    target_entity_id TEXT,
    relationship_type TEXT NOT NULL,
    decision TEXT NOT NULL,
    reason TEXT NOT NULL,
    actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS relationship_assertions_lookup_idx
    ON relationship_assertions (relationship_type, source_repo_id, target_repo_id);

CREATE TABLE IF NOT EXISTS relationship_generations (
    generation_id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    run_id TEXT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    activated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS relationship_generations_scope_idx
    ON relationship_generations (scope, status, created_at DESC);

CREATE TABLE IF NOT EXISTS relationship_evidence_facts (
    evidence_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    evidence_kind TEXT NOT NULL,
    relationship_type TEXT NOT NULL,
    source_repo_id TEXT,
    target_repo_id TEXT,
    source_entity_id TEXT,
    target_entity_id TEXT,
    confidence DOUBLE PRECISION NOT NULL,
    rationale TEXT NOT NULL,
    details JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS relationship_evidence_generation_idx
    ON relationship_evidence_facts (generation_id, relationship_type);

CREATE TABLE IF NOT EXISTS relationship_candidates (
    candidate_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    source_repo_id TEXT,
    target_repo_id TEXT,
    source_entity_id TEXT,
    target_entity_id TEXT,
    relationship_type TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    evidence_count INTEGER NOT NULL,
    rationale TEXT NOT NULL,
    details JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS relationship_candidates_generation_idx
    ON relationship_candidates (generation_id, relationship_type);

CREATE TABLE IF NOT EXISTS resolved_relationships (
    resolved_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    source_repo_id TEXT,
    target_repo_id TEXT,
    source_entity_id TEXT,
    target_entity_id TEXT,
    relationship_type TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    evidence_count INTEGER NOT NULL,
    rationale TEXT NOT NULL,
    resolution_source TEXT NOT NULL,
    details JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS resolved_relationships_generation_idx
    ON resolved_relationships (generation_id, relationship_type);

CREATE INDEX IF NOT EXISTS resolved_relationships_source_repo_idx
    ON resolved_relationships (source_repo_id, generation_id, relationship_type)
    WHERE source_repo_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS resolved_relationships_target_repo_idx
    ON resolved_relationships (target_repo_id, generation_id, relationship_type)
    WHERE target_repo_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS resolved_relationships_identity_idx
    ON resolved_relationships (
        generation_id,
        COALESCE(source_entity_id, source_repo_id),
        COALESCE(target_entity_id, target_repo_id),
        relationship_type
    );
