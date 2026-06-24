// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const listPendingCodeReachabilityInputsSQL = `
WITH candidate AS (
    SELECT acceptance.scope_id,
           acceptance.acceptance_unit_id AS repository_id,
           acceptance.source_run_id,
           acceptance.generation_id,
           max(intent.completed_at) AS completed_at,
           max(watermark.updated_at) AS reach_updated_at
    FROM shared_projection_acceptance AS acceptance
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = acceptance.scope_id
     AND scope.active_generation_id = acceptance.generation_id
    JOIN scope_generations AS generation
      ON generation.generation_id = acceptance.generation_id
     AND generation.status = 'active'
    JOIN shared_projection_intents AS intent
      ON intent.scope_id = acceptance.scope_id
     AND intent.acceptance_unit_id = acceptance.acceptance_unit_id
     AND intent.source_run_id = acceptance.source_run_id
     AND intent.generation_id = acceptance.generation_id
     AND intent.projection_domain IN ('code_calls', 'inheritance_edges')
     AND intent.completed_at IS NOT NULL
    LEFT JOIN code_reachability_repository_watermarks AS watermark
      ON watermark.scope_id = acceptance.scope_id
     AND watermark.generation_id = acceptance.generation_id
     AND watermark.repository_id = acceptance.acceptance_unit_id
    GROUP BY acceptance.scope_id, acceptance.acceptance_unit_id,
             acceptance.source_run_id, acceptance.generation_id
)
SELECT scope_id, repository_id, source_run_id, generation_id, completed_at
FROM candidate
WHERE reach_updated_at IS NULL OR completed_at > reach_updated_at
ORDER BY completed_at ASC, repository_id ASC
LIMIT $1
`

const listCodeReachabilityRootsSQL = `
SELECT entity_id, metadata->'dead_code_root_kinds' AS root_kinds
FROM content_entities
WHERE repo_id = $1
  AND jsonb_typeof(metadata->'dead_code_root_kinds') = 'array'
  AND jsonb_array_length(metadata->'dead_code_root_kinds') > 0
ORDER BY entity_id ASC
`

const listCodeReachabilityEdgesSQL = `
SELECT payload->>'caller_entity_id' AS source_entity_id,
       payload->>'callee_entity_id' AS target_entity_id,
       coalesce(nullif(payload->>'relationship_type', ''), 'CALLS') AS relationship_type,
       coalesce(nullif(payload->>'resolution_method', ''), 'unspecified') AS resolution_method
FROM shared_projection_intents
WHERE scope_id = $1
  AND acceptance_unit_id = $2
  AND source_run_id = $3
  AND generation_id = $4
  AND projection_domain = 'code_calls'
  AND completed_at IS NOT NULL
  AND payload->>'caller_entity_id' <> ''
  AND payload->>'callee_entity_id' <> ''
UNION ALL
SELECT payload->>'child_entity_id' AS source_entity_id,
       payload->>'parent_entity_id' AS target_entity_id,
       coalesce(nullif(payload->>'relationship_type', ''), 'INHERITS') AS relationship_type,
       coalesce(nullif(payload->>'resolution_method', ''), 'unspecified') AS resolution_method
FROM shared_projection_intents
WHERE scope_id = $1
  AND acceptance_unit_id = $2
  AND source_run_id = $3
  AND generation_id = $4
  AND projection_domain = 'inheritance_edges'
  AND completed_at IS NOT NULL
  AND payload->>'child_entity_id' <> ''
  AND payload->>'parent_entity_id' <> ''
ORDER BY source_entity_id ASC, target_entity_id ASC, relationship_type ASC
`

// LoadPendingCodeReachabilityInputs loads bounded active-generation snapshots
// whose completed traversed code-edge intents are newer than materialized
// reachability.
func (s *CodeReachabilityStore) LoadPendingCodeReachabilityInputs(
	ctx context.Context,
	limit int,
) ([]reducer.CodeReachabilityProjectionInput, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, listPendingCodeReachabilityInputsSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending code reachability inputs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type candidate struct {
		scopeID      string
		repositoryID string
		sourceRunID  string
		generationID string
		completedAt  time.Time
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var candidate candidate
		if err := rows.Scan(
			&candidate.scopeID,
			&candidate.repositoryID,
			&candidate.sourceRunID,
			&candidate.generationID,
			&candidate.completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending code reachability input: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	inputs := make([]reducer.CodeReachabilityProjectionInput, 0, len(candidates))
	for _, candidate := range candidates {
		roots, err := s.loadCodeReachabilityRoots(ctx, candidate.repositoryID)
		if err != nil {
			return nil, err
		}
		edges, err := s.loadCodeReachabilityEdges(ctx, candidate.scopeID, candidate.repositoryID, candidate.sourceRunID, candidate.generationID)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, reducer.CodeReachabilityProjectionInput{
			ScopeID:      candidate.scopeID,
			GenerationID: candidate.generationID,
			RepositoryID: candidate.repositoryID,
			Roots:        roots,
			Edges:        edges,
			UpdatedAt:    candidate.completedAt,
		})
	}
	return inputs, nil
}

func (s *CodeReachabilityStore) loadCodeReachabilityRoots(
	ctx context.Context,
	repositoryID string,
) ([]reducer.CodeReachabilityRoot, error) {
	rows, err := s.db.QueryContext(ctx, listCodeReachabilityRootsSQL, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("query code reachability roots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	roots := make([]reducer.CodeReachabilityRoot, 0)
	for rows.Next() {
		var root reducer.CodeReachabilityRoot
		var rootKindsRaw []byte
		if err := rows.Scan(&root.EntityID, &rootKindsRaw); err != nil {
			return nil, fmt.Errorf("scan code reachability root: %w", err)
		}
		if err := json.Unmarshal(rootKindsRaw, &root.RootKinds); err != nil {
			return nil, fmt.Errorf("unmarshal code reachability root kinds: %w", err)
		}
		roots = append(roots, root)
	}
	return roots, rows.Err()
}

func (s *CodeReachabilityStore) loadCodeReachabilityEdges(
	ctx context.Context,
	scopeID string,
	repositoryID string,
	sourceRunID string,
	generationID string,
) ([]reducer.CodeReachabilityEdge, error) {
	rows, err := s.db.QueryContext(ctx, listCodeReachabilityEdgesSQL, scopeID, repositoryID, sourceRunID, generationID)
	if err != nil {
		return nil, fmt.Errorf("query code reachability edges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	edges := make([]reducer.CodeReachabilityEdge, 0)
	for rows.Next() {
		var edge reducer.CodeReachabilityEdge
		if err := rows.Scan(
			&edge.SourceEntityID,
			&edge.TargetEntityID,
			&edge.RelationshipType,
			&edge.ResolutionMethod,
		); err != nil {
			return nil, fmt.Errorf("scan code reachability edge: %w", err)
		}
		edge.RelationshipType = strings.ToUpper(strings.TrimSpace(edge.RelationshipType))
		edges = append(edges, edge)
	}
	return edges, rows.Err()
}
