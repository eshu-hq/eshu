// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
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
SELECT entity_id, metadata->'dead_code_root_kinds' AS root_kinds,
       metadata->>'class_context' AS class_context
FROM content_entities
WHERE repo_id = $1
  AND jsonb_typeof(metadata->'dead_code_root_kinds') = 'array'
  AND jsonb_array_length(metadata->'dead_code_root_kinds') > 0
ORDER BY entity_id ASC
`

// listCodeReachabilityRubyClassesSQL loads the repo-wide Ruby class ancestry the
// #5376 controller verdict builder walks: every Ruby class entity's simple name
// plus its declared qualified_bases. It loads ALL Ruby classes (including
// base-less ones) so the walk can resolve an intermediate hop to an in-corpus
// class that itself declares no base. Index-backed on repo_id/entity_type
// (evidence-5376-code-root-verdicts.md, Q1 = 0.46 ms at 505 classes).
const listCodeReachabilityRubyClassesSQL = `
SELECT entity_name, metadata->'qualified_bases' AS qualified_bases
FROM content_entities
WHERE repo_id = $1
  AND entity_type = 'Class'
  AND language = 'ruby'
ORDER BY entity_name ASC
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
		// Only pay for the repo-wide Ruby class-registry load when this
		// repository actually has a controller-action root to evaluate; a
		// non-Ruby or controller-free repository loads no classes.
		var rubyClasses []reducer.RubyClassEntity
		if codeReachabilityRootsHaveRailsController(roots) {
			rubyClasses, err = s.loadCodeReachabilityRubyClasses(ctx, candidate.repositoryID)
			if err != nil {
				return nil, err
			}
		}
		inputs = append(inputs, reducer.CodeReachabilityProjectionInput{
			ScopeID:      candidate.scopeID,
			GenerationID: candidate.generationID,
			RepositoryID: candidate.repositoryID,
			Roots:        roots,
			Edges:        edges,
			RubyClasses:  rubyClasses,
			UpdatedAt:    candidate.completedAt,
		})
	}
	return inputs, nil
}

// codeReachabilityRootsHaveRailsController reports whether any loaded root
// carries the ruby.rails_controller_action kind, gating the repo-wide Ruby
// class-registry load to Ruby repositories with controller roots.
func codeReachabilityRootsHaveRailsController(roots []reducer.CodeReachabilityRoot) bool {
	for _, root := range roots {
		for _, kind := range root.RootKinds {
			if kind == reducer.CodeRootKindRubyRailsControllerAction {
				return true
			}
		}
	}
	return false
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
		var classContext sql.NullString
		if err := rows.Scan(&root.EntityID, &rootKindsRaw, &classContext); err != nil {
			return nil, fmt.Errorf("scan code reachability root: %w", err)
		}
		if err := json.Unmarshal(rootKindsRaw, &root.RootKinds); err != nil {
			return nil, fmt.Errorf("unmarshal code reachability root kinds: %w", err)
		}
		root.ClassContext = strings.TrimSpace(classContext.String)
		roots = append(roots, root)
	}
	return roots, rows.Err()
}

// loadCodeReachabilityRubyClasses loads the repo-wide Ruby class ancestry
// (name + declared qualified_bases). A class with no declared superclass carries
// a nil/absent qualified_bases and is still returned so it is a known class the
// verdict walk can resolve an intermediate hop to.
func (s *CodeReachabilityStore) loadCodeReachabilityRubyClasses(
	ctx context.Context,
	repositoryID string,
) ([]reducer.RubyClassEntity, error) {
	rows, err := s.db.QueryContext(ctx, listCodeReachabilityRubyClassesSQL, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("query code reachability ruby classes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	classes := make([]reducer.RubyClassEntity, 0)
	for rows.Next() {
		var class reducer.RubyClassEntity
		var qualifiedBasesRaw []byte
		if err := rows.Scan(&class.Name, &qualifiedBasesRaw); err != nil {
			return nil, fmt.Errorf("scan code reachability ruby class: %w", err)
		}
		if len(qualifiedBasesRaw) > 0 {
			if err := json.Unmarshal(qualifiedBasesRaw, &class.QualifiedBases); err != nil {
				return nil, fmt.Errorf("unmarshal code reachability ruby class qualified bases: %w", err)
			}
		}
		classes = append(classes, class)
	}
	return classes, rows.Err()
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
