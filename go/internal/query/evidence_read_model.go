package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// relationshipEvidenceByResolvedID hydrates a compact graph evidence pointer
// from the durable resolved_relationships Postgres read model.
func (cr *ContentReader) relationshipEvidenceByResolvedID(ctx context.Context, resolvedID string) (relationshipEvidenceReadModel, error) {
	if cr == nil || cr.db == nil || resolvedID == "" {
		return relationshipEvidenceReadModel{}, nil
	}
	rows, err := cr.db.QueryContext(ctx, relationshipEvidenceByResolvedIDSQL, resolvedID)
	if err != nil {
		return relationshipEvidenceReadModel{}, fmt.Errorf("query relationship evidence drilldown: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return relationshipEvidenceReadModel{}, fmt.Errorf("iterate relationship evidence drilldown: %w", err)
		}
		return relationshipEvidenceReadModel{}, nil
	}
	row, err := scanRelationshipEvidenceByResolvedID(rows)
	if err != nil {
		return relationshipEvidenceReadModel{}, err
	}
	if rows.Next() {
		return relationshipEvidenceReadModel{}, fmt.Errorf("relationship evidence drilldown returned multiple rows for %q", resolvedID)
	}
	if err := rows.Err(); err != nil {
		return relationshipEvidenceReadModel{}, fmt.Errorf("iterate relationship evidence drilldown: %w", err)
	}
	return relationshipEvidenceReadModel{Available: true, Row: row}, nil
}

const relationshipEvidenceByResolvedIDSQL = `
SELECT r.resolved_id,
       r.generation_id,
       COALESCE(r.source_repo_id, '') AS source_repo_id,
       COALESCE(source_scope.name, r.source_repo_id, '') AS source_name,
       COALESCE(r.source_entity_id, '') AS source_entity_id,
       COALESCE(r.target_repo_id, '') AS target_repo_id,
       COALESCE(target_scope.name, r.target_repo_id, '') AS target_name,
       COALESCE(r.target_entity_id, '') AS target_entity_id,
       r.relationship_type,
       r.confidence,
       r.evidence_count,
       r.rationale,
       r.resolution_source,
       r.details,
       COALESCE(g.scope, '') AS generation_scope,
       COALESCE(g.run_id, '') AS generation_run_id,
       COALESCE(g.status, '') AS generation_status
FROM resolved_relationships AS r
JOIN relationship_generations AS g
  ON g.generation_id = r.generation_id
LEFT JOIN LATERAL (
	SELECT COALESCE(
		payload->>'name',
		payload->>'repo_name',
		payload->>'repo_slug',
		source_key,
		scope_id
	) AS name
	FROM ingestion_scopes
	WHERE scope_kind = 'repository'
	  AND (
		scope_id = r.source_repo_id OR
		source_key = r.source_repo_id OR
		payload->>'repo_id' = r.source_repo_id OR
		payload->>'id' = r.source_repo_id
	  )
	ORDER BY scope_id
	LIMIT 1
) AS source_scope ON true
LEFT JOIN LATERAL (
	SELECT COALESCE(
		payload->>'name',
		payload->>'repo_name',
		payload->>'repo_slug',
		source_key,
		scope_id
	) AS name
	FROM ingestion_scopes
	WHERE scope_kind = 'repository'
	  AND (
		scope_id = r.target_repo_id OR
		source_key = r.target_repo_id OR
		payload->>'repo_id' = r.target_repo_id OR
		payload->>'id' = r.target_repo_id
	  )
	ORDER BY scope_id
	LIMIT 1
) AS target_scope ON true
WHERE r.resolved_id = $1
LIMIT 1
`

func scanRelationshipEvidenceByResolvedID(rows *sql.Rows) (map[string]any, error) {
	var (
		resolvedID       string
		generationID     string
		sourceRepoID     string
		sourceRepoName   string
		sourceEntityID   string
		targetRepoID     string
		targetRepoName   string
		targetEntityID   string
		relationshipType string
		confidence       float64
		evidenceCount    int64
		rationale        string
		resolutionSource string
		detailsRaw       []byte
		generationScope  string
		generationRunID  string
		generationStatus string
	)
	if err := rows.Scan(
		&resolvedID,
		&generationID,
		&sourceRepoID,
		&sourceRepoName,
		&sourceEntityID,
		&targetRepoID,
		&targetRepoName,
		&targetEntityID,
		&relationshipType,
		&confidence,
		&evidenceCount,
		&rationale,
		&resolutionSource,
		&detailsRaw,
		&generationScope,
		&generationRunID,
		&generationStatus,
	); err != nil {
		return nil, fmt.Errorf("scan relationship evidence drilldown: %w", err)
	}

	details := map[string]any{}
	if len(detailsRaw) > 0 {
		if err := json.Unmarshal(detailsRaw, &details); err != nil {
			return nil, fmt.Errorf("decode relationship evidence details: %w", err)
		}
	}
	row := map[string]any{
		"lookup_basis":       "resolved_id",
		"resolved_id":        resolvedID,
		"generation_id":      generationID,
		"relationship_type":  relationshipType,
		"confidence":         confidence,
		"evidence_count":     int(evidenceCount),
		"rationale":          rationale,
		"resolution_source":  resolutionSource,
		"source":             relationshipEvidenceEndpoint(sourceRepoID, sourceRepoName, sourceEntityID),
		"target":             relationshipEvidenceEndpoint(targetRepoID, targetRepoName, targetEntityID),
		"generation":         relationshipEvidenceGeneration(generationID, generationScope, generationRunID, generationStatus),
		"details":            details,
		"evidence_preview":   details["evidence_preview"],
		"postgres_lookup_id": resolvedID,
	}
	if kinds := repositoryRelationshipEvidenceKinds(details); len(kinds) > 0 {
		row["evidence_kinds"] = kinds
	}
	if evidenceType := repositoryRelationshipEvidenceType(details, repositoryRelationshipEvidenceKinds(details)); evidenceType != "" {
		row["evidence_type"] = evidenceType
	}
	return row, nil
}

func relationshipEvidenceEndpoint(repoID, repoName, entityID string) map[string]any {
	endpoint := map[string]any{
		"repo_id":   repoID,
		"repo_name": repoName,
	}
	if entityID != "" {
		endpoint["entity_id"] = entityID
	}
	return endpoint
}

func relationshipEvidenceGeneration(id, scope, runID, status string) map[string]any {
	generation := map[string]any{
		"id":     id,
		"scope":  scope,
		"run_id": runID,
		"status": status,
	}
	return generation
}
