// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/rubycontroller"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// DeadCodeIncomingEntityIDs returns, per candidate entity, the strongest
// completed reducer code-call, metaclass, or inheritance incoming edge in the
// relational read model. Confidence is derived from the per-edge
// resolution_method (ADR #2222) via codeprovenance.Confidence; an edge whose
// payload carries no resolution_method is treated as strong (LegacyConfidence)
// so a candidate is only demoted when every incoming edge is known to be weak.
func (cr *ContentReader) DeadCodeIncomingEntityIDs(
	ctx context.Context,
	repoID string,
	entityIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	repoID = strings.TrimSpace(repoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || repoID == "" || len(entityIDs) == 0 {
		return map[string]deadCodeIncomingEdge{}, nil
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
	// DISTINCT collapses duplicate (entity, method) pairs so row volume is bounded
	// by distinct resolution methods per candidate, not the raw incoming-edge
	// count; the strongest-edge selection by confidence still happens in Go.
	// #nosec G202 -- concatenates only $N parameter placeholders (generated from loop indices) into IN lists; entity ID values are bound args, not SQL text
	query := `
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
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("dead code incoming entity ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	incoming := make(map[string]deadCodeIncomingEdge)
	for rows.Next() {
		var (
			entityID string
			method   sql.NullString
		)
		if err := rows.Scan(&entityID, &method); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan dead code incoming entity id: %w", err)
		}
		mergeDeadCodeIncomingEdge(incoming, entityID, method.String)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return incoming, nil
}

// CodeReachabilityIncomingEntityIDs returns dead-code reachability evidence
// from reducer-materialized code_reachability_rows. The lookup is entity-scoped
// across active generations so a library scan can honor rows materialized from
// a service repository that reaches the library symbol; DeadCodeIncomingEntityIDs
// remains a compatibility fallback for stores without materialized reachability.
func (cr *ContentReader) CodeReachabilityIncomingEntityIDs(
	ctx context.Context,
	repoID string,
	entityIDs []string,
) (map[string]deadCodeIncomingEdge, error) {
	repoID = strings.TrimSpace(repoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || repoID == "" || len(entityIDs) == 0 {
		return map[string]deadCodeIncomingEdge{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "code_reachability_incoming_entity_ids"),
			attribute.String("db.sql.table", "code_reachability_rows"),
		),
	)
	defer span.End()

	placeholders := make([]string, 0, len(entityIDs))
	args := make([]any, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	// #nosec G202 -- concatenates only $N parameter placeholders (generated from len(args)) into the IN list; entity ID values are bound args, not SQL text
	query := `
		SELECT DISTINCT row.entity_id, row.min_resolution_method
		FROM code_reachability_rows AS row
		JOIN ingestion_scopes AS scope
		  ON scope.scope_id = row.scope_id
		 AND scope.active_generation_id = row.generation_id
		JOIN scope_generations AS generation
		  ON generation.generation_id = row.generation_id
		 AND generation.status = 'active'
		WHERE row.entity_id IN (` + strings.Join(placeholders, ", ") + `)
		  AND row.depth > 0
	`
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("code reachability incoming entity ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	incoming := make(map[string]deadCodeIncomingEdge)
	for rows.Next() {
		var entityID string
		var method sql.NullString
		if err := rows.Scan(&entityID, &method); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan code reachability incoming entity id: %w", err)
		}
		mergeDeadCodeIncomingEdge(incoming, entityID, method.String)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return incoming, nil
}

// DowngradedCodeRootKinds returns, per candidate entity, the set of guess-based
// dead-code root kinds the reducer's repo-wide #5376 verdict positively
// downgraded in the active generation. Only 'downgraded' rows are read; a
// confirmed verdict never appears here. A missing/lagging/non-active-generation
// verdict yields no row for that entity, so the caller keeps the parser root
// (lag-safety). Any error is returned; the caller fail-opens to KEEP.
func (cr *ContentReader) DowngradedCodeRootKinds(
	ctx context.Context,
	repoID string,
	entityIDs []string,
) (map[string]map[string]struct{}, error) {
	repoID = strings.TrimSpace(repoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || repoID == "" || len(entityIDs) == 0 {
		return map[string]map[string]struct{}{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "downgraded_code_root_kinds"),
			attribute.String("db.sql.table", "code_root_verdicts"),
		),
	)
	defer span.End()

	placeholders := make([]string, 0, len(entityIDs))
	// $1 = repoID, $2 = the downgraded verdict value bound from the shared
	// rubycontroller constant the reducer writes, so a rename of the verdict
	// value cannot silently desync this predicate (no bare 'downgraded' literal).
	args := make([]any, 0, len(entityIDs)+2)
	args = append(args, repoID, rubycontroller.VerdictDowngraded)
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	// #nosec G202 -- concatenates only $N parameter placeholders (generated from len(args)) into the IN list; entity ID values are bound args, not SQL text
	query := `
		SELECT verdict.entity_id, verdict.root_kind
		FROM code_root_verdicts AS verdict
		JOIN ingestion_scopes AS scope
		  ON scope.scope_id = verdict.scope_id
		 AND scope.active_generation_id = verdict.generation_id
		JOIN scope_generations AS generation
		  ON generation.generation_id = verdict.generation_id
		 AND generation.status = 'active'
		WHERE verdict.repository_id = $1
		  AND verdict.verdict = $2
		  AND verdict.entity_id IN (` + strings.Join(placeholders, ", ") + `)
	`
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("downgraded code root kinds: %w", err)
	}
	defer func() { _ = rows.Close() }()

	downgraded := make(map[string]map[string]struct{})
	for rows.Next() {
		var entityID, rootKind string
		if err := rows.Scan(&entityID, &rootKind); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan downgraded code root kind: %w", err)
		}
		if downgraded[entityID] == nil {
			downgraded[entityID] = make(map[string]struct{})
		}
		downgraded[entityID][rootKind] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return downgraded, nil
}

// CodeReachabilityCoverage reports whether the active generation has a
// materialized reachability snapshot for repoID, and whether that snapshot hit
// the traversal bound. Complete snapshots make absent entities authoritative
// dead-code candidates; truncated or unavailable snapshots keep the legacy
// incoming-edge fallback conservative.
func (cr *ContentReader) CodeReachabilityCoverage(
	ctx context.Context,
	repoID string,
) (codeReachabilityCoverage, error) {
	repoID = strings.TrimSpace(repoID)
	if cr == nil || cr.db == nil || repoID == "" {
		return codeReachabilityCoverage{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "code_reachability_coverage"),
			attribute.String("db.sql.table", "code_reachability_repository_watermarks"),
		),
	)
	defer span.End()

	const query = `
		WITH active_watermarks AS (
			SELECT watermark.truncated
			FROM code_reachability_repository_watermarks AS watermark
			JOIN ingestion_scopes AS scope
			  ON scope.scope_id = watermark.scope_id
			 AND scope.active_generation_id = watermark.generation_id
			JOIN scope_generations AS generation
			  ON generation.generation_id = watermark.generation_id
			 AND generation.status = 'active'
			WHERE watermark.repository_id = $1
		)
		SELECT count(*) > 0 AS available,
		       coalesce(bool_or(truncated), false) AS truncated
		FROM active_watermarks
	`
	var coverage codeReachabilityCoverage
	if err := cr.db.QueryRowContext(ctx, query, repoID).Scan(&coverage.Available, &coverage.Truncated); err != nil {
		span.RecordError(err)
		return codeReachabilityCoverage{}, fmt.Errorf("code reachability coverage: %w", err)
	}
	return coverage, nil
}

// mergeDeadCodeIncomingEdge records the strongest incoming edge seen for
// entityID. Confidence is derived from method via codeprovenance.Confidence, so
// a missing or unrecorded method yields LegacyConfidence (strong) rather than a
// silent demotion.
func mergeDeadCodeIncomingEdge(incoming map[string]deadCodeIncomingEdge, entityID, method string) {
	confidence := codeprovenance.Confidence(method)
	if existing, ok := incoming[entityID]; !ok || confidence > existing.MaxConfidence {
		incoming[entityID] = deadCodeIncomingEdge{MaxConfidence: confidence, Method: method}
	}
}

func cleanDeadCodeIncomingEntityIDs(entityIDs []string) []string {
	if len(entityIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(entityIDs))
	cleaned := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		cleaned = append(cleaned, entityID)
	}
	return cleaned
}
