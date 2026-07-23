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
           max(watermark.updated_at) AS reach_updated_at,
           max(watermark.verdict_schema_epoch) AS reach_verdict_epoch
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
WHERE reach_updated_at IS NULL
   OR completed_at > reach_updated_at
   OR coalesce(reach_verdict_epoch, 0) < $2
ORDER BY completed_at ASC, repository_id ASC
LIMIT $1
`

const listCodeReachabilityRootsSQL = `
SELECT entity_id, metadata->'dead_code_root_kinds' AS root_kinds,
       metadata->>'class_context' AS class_context, entity_name
FROM content_entities
WHERE repo_id = $1
  AND jsonb_typeof(metadata->'dead_code_root_kinds') = 'array'
  AND jsonb_array_length(metadata->'dead_code_root_kinds') > 0
ORDER BY entity_id ASC
`

// listCodeReachabilityRailsRouteFactsSQL loads the repo-wide Rails route-fact
// snapshot the #5494 route-liveness verdict extension joins against. It reads
// the SAME durable fact_records rows and partial index
// (fact_records_framework_routes_repo_path_idx) internal/query's
// ListFrameworkRoutes already uses for live route-evidence display -- the
// WHERE clause repeats that index's exact predicate (fact_kind='file' AND
// framework_semantics IS NOT NULL AND jsonb_array_length(frameworks)>0) so the
// planner can use it, then adds one residual filter narrowing to files that
// actually carry a "rails" section. This carries no new ordering dependency on
// the shared_projection_intents completion gates the CALLS/INHERITS edge
// loads rely on: a stale or not-yet-materialized HANDLES_ROUTE intent can
// never make this query under-report route evidence, because it reads the
// parser's raw route facts directly rather than the downstream materialized
// edge. Any historical fact_records row for the repo counts (no
// generation/source_run filter): a route observed in an OLDER generation still
// counts as positive route evidence or ambiguity, which only ever biases the
// #5494 join toward KEEP, never toward a wrong downgrade -- and a genuinely
// removed controller action simply has no root row to evaluate in the first
// place (roots come from the current content_entities snapshot).
const listCodeReachabilityRailsRouteFactsSQL = `
SELECT
    entries.entry->>'handler' AS handler,
    coalesce((file.payload->'parsed_file_data'->'framework_semantics'->'rails'->>'has_unmodeled_routes')::boolean, FALSE) AS has_unmodeled_routes
FROM fact_records AS file
LEFT JOIN LATERAL jsonb_array_elements(
    coalesce(file.payload->'parsed_file_data'->'framework_semantics'->'rails'->'route_entries', '[]'::jsonb)
) AS entries(entry) ON TRUE
WHERE file.fact_kind = 'file'
  AND file.payload->>'repo_id' = $1
  AND file.payload->'parsed_file_data'->'framework_semantics' IS NOT NULL
  AND jsonb_array_length(
      coalesce(file.payload->'parsed_file_data'->'framework_semantics'->'frameworks', '[]'::jsonb)
  ) > 0
  AND file.payload->'parsed_file_data'->'framework_semantics'->'rails' IS NOT NULL
`

// listCodeReachabilityRubyClassesSQL loads the repo-wide Ruby class ancestry the
// #5376 controller verdict builder walks: every Ruby class entity's simple name,
// its namespace-qualified name (F3, the registry key), and its declared
// qualified_bases. It loads ALL Ruby classes (including base-less ones) so the
// walk can resolve an intermediate hop to an in-corpus class that itself
// declares no base. qualified_name is NULL for pre-upgrade rows; the reducer
// registry falls back to entity_name (simple-key + F1 floor, lag-safe).
// Index-backed on repo_id/entity_type (evidence-5376-code-root-verdicts.md,
// Q1 = 0.46 ms at 505 classes).
const listCodeReachabilityRubyClassesSQL = `
SELECT entity_name, metadata->>'qualified_name' AS qualified_name,
       metadata->'qualified_bases' AS qualified_bases
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
	rows, err := s.db.QueryContext(ctx, listPendingCodeReachabilityInputsSQL, limit, CodeReachabilityVerdictSchemaEpoch)
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
		var rubyRoutes reducer.RubyRailsRouteFacts
		if codeReachabilityRootsHaveRailsController(roots) {
			rubyClasses, err = s.loadCodeReachabilityRubyClasses(ctx, candidate.repositoryID)
			if err != nil {
				return nil, err
			}
			rubyRoutes, err = s.loadCodeReachabilityRailsRouteFacts(ctx, candidate.repositoryID)
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
			RubyRoutes:   rubyRoutes,
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
		var actionName sql.NullString
		if err := rows.Scan(&root.EntityID, &rootKindsRaw, &classContext, &actionName); err != nil {
			return nil, fmt.Errorf("scan code reachability root: %w", err)
		}
		if err := json.Unmarshal(rootKindsRaw, &root.RootKinds); err != nil {
			return nil, fmt.Errorf("unmarshal code reachability root kinds: %w", err)
		}
		root.ClassContext = strings.TrimSpace(classContext.String)
		root.ActionName = strings.TrimSpace(actionName.String)
		roots = append(roots, root)
	}
	return roots, rows.Err()
}

// loadCodeReachabilityRailsRouteFacts loads the repo-wide Rails route-fact
// snapshot the #5494 route-liveness verdict extension joins against (see
// listCodeReachabilityRailsRouteFactsSQL). HasAnyRouteEvidence is true
// whenever this query returns at least one row: every returned row comes from
// a file whose "rails" framework_semantics section was observed, whether or
// not that file itself produced a resolvable route_entries handler.
func (s *CodeReachabilityStore) loadCodeReachabilityRailsRouteFacts(
	ctx context.Context,
	repositoryID string,
) (reducer.RubyRailsRouteFacts, error) {
	rows, err := s.db.QueryContext(ctx, listCodeReachabilityRailsRouteFactsSQL, repositoryID)
	if err != nil {
		return reducer.RubyRailsRouteFacts{}, fmt.Errorf("query code reachability rails route facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := reducer.RubyRailsRouteFacts{RoutedHandlers: make(map[string]struct{})}
	for rows.Next() {
		var handler sql.NullString
		var hasUnmodeledRoutes bool
		if err := rows.Scan(&handler, &hasUnmodeledRoutes); err != nil {
			return reducer.RubyRailsRouteFacts{}, fmt.Errorf("scan code reachability rails route fact: %w", err)
		}
		facts.HasAnyRouteEvidence = true
		if hasUnmodeledRoutes {
			facts.HasUnmodeledRoutes = true
		}
		if trimmed := strings.TrimSpace(handler.String); trimmed != "" {
			facts.RoutedHandlers[trimmed] = struct{}{}
		}
	}
	return facts, rows.Err()
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
		var qualifiedName sql.NullString
		var qualifiedBasesRaw []byte
		if err := rows.Scan(&class.Name, &qualifiedName, &qualifiedBasesRaw); err != nil {
			return nil, fmt.Errorf("scan code reachability ruby class: %w", err)
		}
		class.QualifiedName = strings.TrimSpace(qualifiedName.String)
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
