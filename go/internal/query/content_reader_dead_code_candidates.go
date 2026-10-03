// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// DeadCodeCandidateRows returns label-scoped cleanup candidates from the
// content read model, preserving the graph candidate response shape.
func (cr *ContentReader) DeadCodeCandidateRows(
	ctx context.Context,
	query codeshaping.DeadCodeCandidateQuery,
) ([]map[string]any, error) {
	repoID := strings.TrimSpace(query.RepoID)
	language := strings.ToLower(strings.TrimSpace(query.Language))
	label := query.Label
	limit, offset := query.Limit, query.Offset
	if cr == nil || cr.db == nil {
		return nil, nil
	}
	entityType, ok := code.DeadCodeCandidateEntityType(label)
	if !ok {
		return nil, fmt.Errorf("unsupported dead code candidate label %q", label)
	}
	if limit <= 0 {
		limit = codeshaping.DeadCodeCandidateQueryMin
	}
	if offset < 0 {
		offset = 0
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "dead_code_candidate_rows"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	// #5167: a corpus-wide scan (repoID == "") carries the caller's granted
	// repository ids into the WHERE, so LIMIT/OFFSET pages the granted set.
	args := []any{repoID, entityType, language}
	grant := ""
	if len(query.AllowedRepositoryIDs) > 0 {
		args = append(args, array.Of(query.AllowedRepositoryIDs))
		grant = fmt.Sprintf("\n\t\t  AND repo_id = ANY($%d)", len(args))
	}
	args = append(args, limit, offset)
	// #nosec G201 -- interpolates only integer argument indices and the fixed
	// grant clause above; no caller-supplied text is concatenated into the SQL.
	statement := fmt.Sprintf(`
		SELECT entity_id, entity_name, entity_type, repo_id, relative_path,
		       coalesce(language, ''), start_line, end_line, metadata
		FROM content_entities
		WHERE ($1 = '' OR repo_id = $1)
		  AND entity_type = $2
		  AND ($3 = '' OR lower(coalesce(language, '')) = $3)%s
		ORDER BY repo_id, relative_path, entity_name, entity_id
		LIMIT $%d OFFSET $%d
	`, grant, len(args)-1, len(args))
	rows, err := cr.db.QueryContext(ctx, statement, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("dead code candidate rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]map[string]any, 0, limit)
	for rows.Next() {
		var entityID string
		var entityName string
		var entityType string
		var rowRepoID string
		var relativePath string
		var language string
		var startLine int
		var endLine int
		var rawMetadata []byte
		if err := rows.Scan(
			&entityID,
			&entityName,
			&entityType,
			&rowRepoID,
			&relativePath,
			&language,
			&startLine,
			&endLine,
			&rawMetadata,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan dead code candidate row: %w", err)
		}
		metadata, err := decodeEntityMetadata(rawMetadata)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("dead code candidate rows: %w", err)
		}
		result := map[string]any{
			"entity_id":  entityID,
			"name":       entityName,
			"labels":     []any{label},
			"file_path":  relativePath,
			"repo_id":    rowRepoID,
			"language":   language,
			"start_line": startLine,
			"end_line":   endLine,
		}
		if len(metadata) > 0 {
			result["metadata"] = metadata
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return results, nil
}

// DeadCodeIncomingEntityIDs returns, per candidate entity, the strongest
// completed reducer code-call, metaclass, or inheritance incoming edge in the
// relational read model. Confidence is derived from the per-edge
// resolution_method (ADR #2222) via codeprovenance.Confidence; an edge whose
// payload carries no resolution_method is treated as strong (LegacyConfidence)
// so a candidate is only demoted when every incoming edge is known to be weak.
//
// The edges counted are those of the repository's active acceptance run when
// that run provably holds the complete edge set (deadCodeIncomingBoundQuery
// names the checks), so an edge kept only by a superseded generation no longer
// keeps its target alive (#7249). Otherwise -- a delta generation, a run still
// projecting or reset by a rebuild, or no active acceptance row -- it counts
// every completed edge across retained generations, unchanged from before.
// The span attribute dead_code_incoming.read_mode records which read answered.
func (cr *ContentReader) DeadCodeIncomingEntityIDs(
	ctx context.Context,
	repoID string,
	entityIDs []string,
) (map[string]code.DeadCodeIncomingEdge, error) {
	repoID = strings.TrimSpace(repoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || repoID == "" || len(entityIDs) == 0 {
		return map[string]code.DeadCodeIncomingEdge{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "dead_code_incoming_entity_ids"),
			attribute.String("db.sql.table", "shared_projection_intents"),
		),
	)
	defer span.End()

	placeholders := make([]string, 0, len(entityIDs))
	args := make([]any, 0, len(entityIDs)+1)
	args = append(args, repoID)
	for i, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+2))
	}
	entityIDSet := strings.Join(placeholders, ", ")
	// The bound statement answers from the active acceptance run when its
	// edge set is provably complete and otherwise returns one NULL sentinel
	// row; the guard and the bound read share that one statement snapshot.
	incoming, unbound, err := cr.readDeadCodeIncomingEdges(ctx, deadCodeIncomingBoundQuery(entityIDSet), args)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if !unbound {
		span.SetAttributes(attribute.String("dead_code_incoming.read_mode", "active_run"))
		return incoming, nil
	}
	span.SetAttributes(attribute.String("dead_code_incoming.read_mode", "all_generations"))
	incoming, _, err = cr.readDeadCodeIncomingEdges(ctx, deadCodeIncomingUnboundQuery(entityIDSet), args)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return incoming, nil
}

// readDeadCodeIncomingEdges runs one incoming-edge statement and merges its
// (entity, resolution_method) rows into the strongest edge per entity. A row
// whose entity id is NULL is the bound statement's fallback sentinel: it
// reports unbound=true and the caller discards the (empty) bound answer.
func (cr *ContentReader) readDeadCodeIncomingEdges(
	ctx context.Context,
	query string,
	args []any,
) (map[string]code.DeadCodeIncomingEdge, bool, error) {
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("dead code incoming entity ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	incoming := make(map[string]code.DeadCodeIncomingEdge)
	unbound := false
	for rows.Next() {
		var entityID, method sql.NullString
		if err := rows.Scan(&entityID, &method); err != nil {
			return nil, false, fmt.Errorf("scan dead code incoming entity id: %w", err)
		}
		if !entityID.Valid {
			unbound = true
			continue
		}
		mergeDeadCodeIncomingEdge(incoming, entityID.String, method.String)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return incoming, unbound, nil
}

// deadCodeIncomingUnboundQuery builds the legacy incoming-edge statement over
// every completed code_calls and inheritance_edges intent of the repository,
// across all retained generations. entityIDSet is the comma-joined $N
// placeholder list for the candidate ids ($1 is the repository id).
func deadCodeIncomingUnboundQuery(entityIDSet string) string {
	// DISTINCT collapses duplicate (entity, method) pairs so row volume is bounded
	// by distinct resolution methods per candidate, not the raw incoming-edge
	// count; the strongest-edge selection by confidence still happens in Go.
	// #nosec G202 -- concatenates only $N parameter placeholders (generated from loop indices) into IN lists; entity ID values are bound args, not SQL text
	return `
		SELECT DISTINCT incoming_entity_id, resolution_method
		FROM (
			SELECT payload->>'callee_entity_id' AS incoming_entity_id,
			       payload->>'resolution_method' AS resolution_method
			FROM shared_projection_intents
			WHERE repository_id = $1
			  AND projection_domain = 'code_calls'
			  AND completed_at IS NOT NULL
			  AND payload->>'callee_entity_id' IN (` + entityIDSet + `)
			UNION ALL
			SELECT payload->>'target_entity_id' AS incoming_entity_id,
			       payload->>'resolution_method' AS resolution_method
			FROM shared_projection_intents
			WHERE repository_id = $1
			  AND projection_domain = 'code_calls'
			  AND completed_at IS NOT NULL
			  AND payload->>'relationship_type' = 'USES_METACLASS'
			  AND payload->>'target_entity_id' IN (` + entityIDSet + `)
			UNION ALL
			SELECT payload->>'parent_entity_id' AS incoming_entity_id,
			       payload->>'resolution_method' AS resolution_method
			FROM shared_projection_intents
			WHERE repository_id = $1
			  AND projection_domain = 'inheritance_edges'
			  AND completed_at IS NOT NULL
			  AND payload->>'parent_entity_id' IN (` + entityIDSet + `)
		) incoming
		WHERE incoming_entity_id IS NOT NULL
		  AND incoming_entity_id <> ''
	`
}

// deadCodeIncomingBoundQuery builds the incoming-edge statement bound to the
// repository's active acceptance run(s), the key the reducer's reachability
// loader reads edges by. It binds only when that run provably holds the
// complete edge set, judged per active (scope, generation):
//
//   - the generation is full: a delta generation carries only changed files;
//   - both reducer materialization work items (code_call_materialization and
//     inheritance_materialization) succeeded and none is queued, running, or
//     failed: a domain that never ran has emitted no intents at all;
//   - no code_calls or inheritance_edges intent of the generation for this
//     repository is still pending (migration 108's partial index).
//
// When any check fails, or the repository has no active acceptance row, the
// statement returns exactly one row with a NULL incoming_entity_id and the
// caller runs deadCodeIncomingUnboundQuery instead. Bound, each of the three
// branches keeps a row only when its (source_run_id, generation_id) PAIR is an
// active acceptance pair; two independent IN lists would also admit a row that
// pairs one scope's run with another scope's generation. The planner still
// probes shared_projection_intents_repo_run_idx by the run key.
func deadCodeIncomingBoundQuery(entityIDSet string) string {
	// #nosec G202 -- concatenates only $N parameter placeholders (generated from loop indices) into IN lists; entity ID values are bound args, not SQL text
	return `
		WITH active_run AS MATERIALIZED (
			SELECT acceptance.scope_id, acceptance.source_run_id,
			       acceptance.generation_id, generation.is_delta
			FROM shared_projection_acceptance AS acceptance
			JOIN ingestion_scopes AS scope
			  ON scope.scope_id = acceptance.scope_id
			 AND scope.active_generation_id = acceptance.generation_id
			JOIN scope_generations AS generation
			  ON generation.generation_id = acceptance.generation_id
			 AND generation.status = 'active'
			WHERE acceptance.acceptance_unit_id = $1
		), run_gate AS MATERIALIZED (
			SELECT coalesce(bool_and(
			         NOT active.is_delta
			         AND EXISTS (
			             SELECT 1 FROM fact_work_items AS work
			             WHERE work.scope_id = active.scope_id
			               AND work.generation_id = active.generation_id
			               AND work.stage = 'reducer'
			               AND work.domain = 'code_call_materialization'
			               AND work.status = 'succeeded')
			         AND EXISTS (
			             SELECT 1 FROM fact_work_items AS work
			             WHERE work.scope_id = active.scope_id
			               AND work.generation_id = active.generation_id
			               AND work.stage = 'reducer'
			               AND work.domain = 'inheritance_materialization'
			               AND work.status = 'succeeded')
			         AND NOT EXISTS (
			             SELECT 1 FROM fact_work_items AS work
			             WHERE work.scope_id = active.scope_id
			               AND work.generation_id = active.generation_id
			               AND work.stage = 'reducer'
			               AND work.domain IN ('code_call_materialization', 'inheritance_materialization')
			               AND work.status <> 'succeeded')
			         AND NOT EXISTS (
			             SELECT 1 FROM shared_projection_intents AS pending
			             WHERE pending.generation_id = active.generation_id
			               AND pending.completed_at IS NULL
			               AND pending.repository_id = $1
			               AND pending.projection_domain IN ('code_calls', 'inheritance_edges'))
			       ), false) AS bound
			FROM active_run AS active
		)
		(
		SELECT DISTINCT incoming_entity_id, resolution_method
		FROM (
			SELECT payload->>'callee_entity_id' AS incoming_entity_id,
			       payload->>'resolution_method' AS resolution_method
			FROM shared_projection_intents
			WHERE repository_id = $1
			  AND projection_domain = 'code_calls'
			  AND completed_at IS NOT NULL
			  AND EXISTS (
			      SELECT 1 FROM active_run
			      WHERE active_run.source_run_id = shared_projection_intents.source_run_id
			        AND active_run.generation_id = shared_projection_intents.generation_id)
			  AND payload->>'callee_entity_id' IN (` + entityIDSet + `)
			UNION ALL
			SELECT payload->>'target_entity_id' AS incoming_entity_id,
			       payload->>'resolution_method' AS resolution_method
			FROM shared_projection_intents
			WHERE repository_id = $1
			  AND projection_domain = 'code_calls'
			  AND completed_at IS NOT NULL
			  AND EXISTS (
			      SELECT 1 FROM active_run
			      WHERE active_run.source_run_id = shared_projection_intents.source_run_id
			        AND active_run.generation_id = shared_projection_intents.generation_id)
			  AND payload->>'relationship_type' = 'USES_METACLASS'
			  AND payload->>'target_entity_id' IN (` + entityIDSet + `)
			UNION ALL
			SELECT payload->>'parent_entity_id' AS incoming_entity_id,
			       payload->>'resolution_method' AS resolution_method
			FROM shared_projection_intents
			WHERE repository_id = $1
			  AND projection_domain = 'inheritance_edges'
			  AND completed_at IS NOT NULL
			  AND EXISTS (
			      SELECT 1 FROM active_run
			      WHERE active_run.source_run_id = shared_projection_intents.source_run_id
			        AND active_run.generation_id = shared_projection_intents.generation_id)
			  AND payload->>'parent_entity_id' IN (` + entityIDSet + `)
		) incoming
		WHERE incoming_entity_id IS NOT NULL
		  AND incoming_entity_id <> ''
		  AND (SELECT bound FROM run_gate)
		)
		UNION ALL
		SELECT NULL::text, NULL::text
		WHERE NOT (SELECT bound FROM run_gate)
	`
}
