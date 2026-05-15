package postgres

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

const relationshipSchemaSQL = `
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
`

// RelationshipSchemaSQL returns the DDL for relationship tables.
func RelationshipSchemaSQL() string {
	return relationshipSchemaSQL
}

const upsertAssertionSQL = `
INSERT INTO relationship_assertions (
    assertion_id, source_repo_id, target_repo_id,
    source_entity_id, target_entity_id,
    relationship_type, decision, reason, actor,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (assertion_id) DO UPDATE
SET source_entity_id = EXCLUDED.source_entity_id,
    target_entity_id = EXCLUDED.target_entity_id,
    decision = EXCLUDED.decision,
    reason = EXCLUDED.reason,
    actor = EXCLUDED.actor,
    updated_at = EXCLUDED.updated_at
`

const listAssertionsSQL = `
SELECT source_repo_id, target_repo_id,
       COALESCE(source_entity_id, source_repo_id),
       COALESCE(target_entity_id, target_repo_id),
       relationship_type, decision, reason, actor
FROM relationship_assertions
ORDER BY updated_at ASC, created_at ASC
`

const listAssertionsByTypeSQL = `
SELECT source_repo_id, target_repo_id,
       COALESCE(source_entity_id, source_repo_id),
       COALESCE(target_entity_id, target_repo_id),
       relationship_type, decision, reason, actor
FROM relationship_assertions
WHERE relationship_type = $1
ORDER BY updated_at ASC, created_at ASC
`

const createGenerationSQL = `
INSERT INTO relationship_generations (generation_id, scope, run_id, status, created_at)
VALUES ($1, $2, $3, 'pending', $4)
`

const activateGenerationSQL = `
WITH deactivate AS (
    UPDATE relationship_generations
    SET status = 'superseded'
    WHERE scope = $3
      AND generation_id <> $2
      AND status = 'active'
)
UPDATE relationship_generations
SET status = 'active', activated_at = $1
WHERE generation_id = $2 AND scope = $3
`

const activateResolutionGenerationSQL = `
WITH deactivate AS (
    UPDATE relationship_generations
    SET status = 'superseded'
    WHERE scope = $2
      AND generation_id <> $1
      AND status = 'active'
)
INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
VALUES ($1, $2, 'active', $3, $4)
ON CONFLICT (generation_id) DO UPDATE SET status = 'active', activated_at = $4
`

const insertEvidenceFactSQL = `
INSERT INTO relationship_evidence_facts (
    evidence_id, generation_id, evidence_kind, relationship_type,
    source_repo_id, target_repo_id, source_entity_id, target_entity_id,
    confidence, rationale, details, observed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (evidence_id) DO NOTHING
`

const insertCandidateSQL = `
INSERT INTO relationship_candidates (
    candidate_id, generation_id, source_repo_id, target_repo_id,
    source_entity_id, target_entity_id, relationship_type,
    confidence, evidence_count, rationale, details
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (candidate_id) DO NOTHING
`

const insertResolvedSQL = `
INSERT INTO resolved_relationships (
    resolved_id, generation_id, source_repo_id, target_repo_id,
    source_entity_id, target_entity_id, relationship_type,
    confidence, evidence_count, rationale, resolution_source, details
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (
    generation_id,
    COALESCE(source_entity_id, source_repo_id),
    COALESCE(target_entity_id, target_repo_id),
    relationship_type
) DO NOTHING
`

const listEvidenceFactsByGenerationSQL = `
SELECT evidence_kind, relationship_type,
       COALESCE(source_repo_id, ''),
       COALESCE(target_repo_id, ''),
       COALESCE(source_entity_id, ''),
       COALESCE(target_entity_id, ''),
       confidence, rationale, details
FROM relationship_evidence_facts
WHERE generation_id = $1
ORDER BY observed_at ASC, evidence_id ASC
`

const listResolvedSQL = `
SELECT r.source_repo_id, r.target_repo_id,
       COALESCE(r.source_entity_id, r.source_repo_id),
       COALESCE(r.target_entity_id, r.target_repo_id),
       r.relationship_type, r.confidence, r.evidence_count,
       r.rationale, r.resolution_source, r.details
FROM resolved_relationships AS r
JOIN relationship_generations AS g
  ON g.generation_id = r.generation_id
WHERE g.scope = $1
  AND g.status = 'active'
ORDER BY COALESCE(r.source_entity_id, r.source_repo_id),
         COALESCE(r.target_entity_id, r.target_repo_id),
         r.relationship_type
`

const listResolvedByGenerationSQL = `
SELECT r.source_repo_id, r.target_repo_id,
       COALESCE(r.source_entity_id, r.source_repo_id),
       COALESCE(r.target_entity_id, r.target_repo_id),
       r.relationship_type, r.confidence, r.evidence_count,
       r.rationale, r.resolution_source, r.details
FROM resolved_relationships AS r
JOIN relationship_generations AS g
  ON g.generation_id = r.generation_id
WHERE g.scope = $1
  AND g.generation_id = $2
ORDER BY COALESCE(r.source_entity_id, r.source_repo_id),
         COALESCE(r.target_entity_id, r.target_repo_id),
         r.relationship_type
`

const listResolvedByReposSQL = `
SELECT r.source_repo_id, r.target_repo_id,
       COALESCE(r.source_entity_id, r.source_repo_id),
       COALESCE(r.target_entity_id, r.target_repo_id),
       r.relationship_type, r.confidence, r.evidence_count,
       r.rationale, r.resolution_source, r.details
FROM resolved_relationships AS r
JOIN relationship_generations AS g
  ON g.generation_id = r.generation_id
WHERE g.status = 'active'
  AND (
    r.source_repo_id IN (%s)
    OR r.target_repo_id IN (%s)
  )
ORDER BY COALESCE(r.source_entity_id, r.source_repo_id),
         COALESCE(r.target_entity_id, r.target_repo_id),
         r.relationship_type
`

// relationshipDigest builds a stable short identifier from one or more parts.
func relationshipDigest(prefix string, parts ...string) string {
	normalized := make([]string, len(parts))
	for i, part := range parts {
		if part == "" {
			normalized[i] = "<none>"
		} else {
			normalized[i] = part
		}
	}
	h := sha1.New()
	h.Write([]byte(strings.Join(normalized, "\n")))
	digest := hex.EncodeToString(h.Sum(nil))[:16]
	return fmt.Sprintf("%s_%s", prefix, digest)
}

// coalesceNullable returns the first non-empty value, or nil for SQL NULL.
func coalesceNullable(a, b string) any {
	if a != "" {
		return a
	}
	if b != "" {
		return b
	}
	return nil
}

// relationshipBootstrapDefinition returns the schema definition for the
// bootstrap registry.
func relationshipBootstrapDefinition() Definition {
	return Definition{
		Name: "relationship_tables",
		Path: "schema/data-plane/postgres/010_relationship_tables.sql",
		SQL:  relationshipSchemaSQL,
	}
}

// init registers the relationship schema in the bootstrap definitions.
func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, relationshipBootstrapDefinition())
}
