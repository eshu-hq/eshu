// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

const maxCrossRepoDeadCodeConsumerEvidenceRows = 1000

// CrossRepoDeadCodeConsumerEvidence returns active-generation consumer evidence
// for producer candidates using a bounded entity-id lookup. It never performs a
// graph traversal; ambiguous or stale coverage must remain unknown at the
// handler layer rather than becoming dead-code truth.
//
// allowedRepositoryIDs is the caller's repository grant, applied to the
// CONSUMER side (code_reachability_rows.repository_id). Binding it here rather
// than filtering in Go is what keeps the page honest: the row cap applies to
// the granted consumers, so another tenant's rows can no longer crowd a granted
// consumer off the page. A grantless list is unscoped and restricts nothing.
//
// Filtering in SQL would lose the "this symbol has a consumer you cannot see"
// signal, and losing it would mark a live symbol dead. The second return value
// carries it: for each entity, how many active consumers the grant excluded.
// The count is the only thing that crosses the boundary -- no id, name, or
// citation from an ungranted consumer is read.
func (cr *ContentReader) CrossRepoDeadCodeConsumerEvidence(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (map[string][]crossRepoDeadCodeEvidence, map[string]int, error) {
	producerRepoID = strings.TrimSpace(producerRepoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || producerRepoID == "" || len(entityIDs) == 0 {
		return map[string][]crossRepoDeadCodeEvidence{}, map[string]int{}, nil
	}

	ctx, span := cr.tracer.Start(
		ctx,
		"postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "cross_repo_dead_code_consumer_evidence"),
			attribute.String("db.sql.table", "code_reachability_rows"),
		),
	)
	defer span.End()

	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery(producerRepoID, entityIDs, allowedRepositoryIDs)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, nil, fmt.Errorf("cross-repo dead code consumer evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]crossRepoDeadCodeEvidence, len(entityIDs))
	rowCount := 0
	truncated := false
	for rows.Next() {
		entityID, evidence, err := scanCrossRepoDeadCodeEvidence(rows)
		if err != nil {
			span.RecordError(err)
			return nil, nil, err
		}
		rowCount++
		if rowCount > maxCrossRepoDeadCodeConsumerEvidenceRows {
			truncated = true
			continue
		}
		result[entityID] = append(result[entityID], evidence)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, nil, err
	}
	if truncated {
		markCrossRepoDeadCodeConsumerEvidenceTruncated(result, entityIDs)
	}
	hidden, err := cr.crossRepoDeadCodeHiddenConsumerCounts(ctx, producerRepoID, entityIDs, allowedRepositoryIDs)
	if err != nil {
		span.RecordError(err)
		return nil, nil, err
	}
	span.SetAttributes(attribute.Int("db.rows.hidden_consumer_entities", len(hidden)))
	return result, hidden, nil
}

// crossRepoDeadCodeHiddenConsumerCounts counts, per producer entity, the active
// consumers the caller's grant excludes. It runs only for a scoped caller, and
// it returns counts only -- the identities stay in the database.
func (cr *ContentReader) crossRepoDeadCodeHiddenConsumerCounts(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (map[string]int, error) {
	hidden := make(map[string]int, len(entityIDs))
	if len(allowedRepositoryIDs) == 0 {
		return hidden, nil
	}
	query, args := buildCrossRepoDeadCodeHiddenConsumerQuery(producerRepoID, entityIDs, allowedRepositoryIDs)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("cross-repo dead code hidden consumer count: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			entityID string
			count    int
		)
		if err := rows.Scan(&entityID, &count); err != nil {
			return nil, fmt.Errorf("scan cross-repo dead code hidden consumer count: %w", err)
		}
		hidden[entityID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hidden, nil
}

// crossRepoDeadCodeConsumerScan renders the shared FROM/JOIN/WHERE both
// consumer-evidence statements run over, plus the argument list they bind. The
// grant clause is empty for an unscoped caller, which keeps that caller's
// statement text unchanged.
func crossRepoDeadCodeConsumerScan(
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
	grantMatches bool,
) (string, []any) {
	args := make([]any, 0, len(entityIDs)+2)
	args = append(args, producerRepoID)
	placeholders := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	grant := ""
	if len(allowedRepositoryIDs) > 0 {
		args = append(args, pgarray.Array(allowedRepositoryIDs))
		if grantMatches {
			grant = fmt.Sprintf("\n  AND row.repository_id = ANY($%d)", len(args))
		} else {
			grant = fmt.Sprintf("\n  AND NOT (row.repository_id = ANY($%d))", len(args))
		}
	}
	scan := `
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.repository_id <> $1
  AND row.entity_id IN (` + strings.Join(placeholders, ", ") + `)
  AND row.depth > 0` + grant
	return scan, args
}

// buildCrossRepoDeadCodeHiddenConsumerQuery counts the active consumers the
// grant excludes, one row per producer entity. It reads no consumer identity.
func buildCrossRepoDeadCodeHiddenConsumerQuery(
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (string, []any) {
	scan, args := crossRepoDeadCodeConsumerScan(producerRepoID, entityIDs, allowedRepositoryIDs, false)
	// #nosec G201 -- interpolates only the fixed scan text above, whose only
	// variable parts are integer argument indices.
	return "SELECT row.entity_id, count(*) AS hidden_count" + scan +
		"\nGROUP BY row.entity_id\nORDER BY row.entity_id ASC\n", args
}

func buildCrossRepoDeadCodeConsumerEvidenceQuery(
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (string, []any) {
	scan, args := crossRepoDeadCodeConsumerScan(producerRepoID, entityIDs, allowedRepositoryIDs, true)
	query := fmt.Sprintf(`
SELECT row.entity_id,
       row.repository_id,
       '' AS consumer_repo_name,
       row.root_entity_id,
       row.depth,
       row.state,
       row.confidence,
       row.min_resolution_method,
       row.evidence,
       row.root_kinds,
       row.generation_id,
       generation.status AS generation_status,
       row.observed_at,
       row.updated_at%s
ORDER BY row.entity_id ASC, row.confidence DESC, row.depth ASC,
         row.repository_id ASC, row.root_entity_id ASC
LIMIT %d
`, scan, maxCrossRepoDeadCodeConsumerEvidenceRows+1)
	return query, args
}

func markCrossRepoDeadCodeConsumerEvidenceTruncated(
	result map[string][]crossRepoDeadCodeEvidence,
	entityIDs []string,
) {
	for _, entityID := range entityIDs {
		if len(result[entityID]) > 0 {
			continue
		}
		result[entityID] = []crossRepoDeadCodeEvidence{{
			EvidenceFamily:   "code_reachability",
			Citation:         "code_reachability_rows:truncated",
			ConfidenceLabel:  "unknown",
			GenerationStatus: "active",
			NeedsEvidence:    true,
			Reason:           "consumer_evidence_truncated",
			RelationshipType: "REACHES",
			ResolutionMethod: "bounded_lookup",
			ConsumerRepoID:   "",
			ConsumerRepoName: "",
			ConsumerEntityID: "",
			Confidence:       0,
			Depth:            0,
			GenerationID:     "",
			Ambiguous:        false,
		}}
	}
}

type crossRepoDeadCodeRowScanner interface {
	Scan(dest ...any) error
}

func scanCrossRepoDeadCodeEvidence(rows crossRepoDeadCodeRowScanner) (string, crossRepoDeadCodeEvidence, error) {
	var (
		entityID         string
		consumerRepoID   string
		consumerRepoName string
		rootEntityID     string
		depth            int
		state            string
		confidence       float64
		resolutionMethod string
		rawEvidence      []byte
		rawRootKinds     []byte
		generationID     string
		generationStatus string
		observedAt       time.Time
		updatedAt        time.Time
	)
	if err := rows.Scan(
		&entityID,
		&consumerRepoID,
		&consumerRepoName,
		&rootEntityID,
		&depth,
		&state,
		&confidence,
		&resolutionMethod,
		&rawEvidence,
		&rawRootKinds,
		&generationID,
		&generationStatus,
		&observedAt,
		&updatedAt,
	); err != nil {
		return "", crossRepoDeadCodeEvidence{}, fmt.Errorf("scan cross-repo dead code consumer evidence: %w", err)
	}
	var evidence []string
	if err := json.Unmarshal(rawEvidence, &evidence); err != nil {
		return "", crossRepoDeadCodeEvidence{}, fmt.Errorf("unmarshal cross-repo dead code evidence: %w", err)
	}
	var rootKinds []string
	if err := json.Unmarshal(rawRootKinds, &rootKinds); err != nil {
		return "", crossRepoDeadCodeEvidence{}, fmt.Errorf("unmarshal cross-repo dead code root kinds: %w", err)
	}
	item := crossRepoDeadCodeEvidence{
		ConsumerRepoID:   consumerRepoID,
		ConsumerRepoName: consumerRepoName,
		ConsumerEntityID: rootEntityID,
		RelationshipType: crossRepoDeadCodeRelationshipType(evidence),
		EvidenceFamily:   "direct_code",
		Citation:         crossRepoDeadCodeCitation(generationID, consumerRepoID, rootEntityID, entityID),
		Confidence:       confidence,
		ConfidenceLabel:  crossRepoDeadCodeConfidenceLabel(confidence),
		ResolutionMethod: resolutionMethod,
		Depth:            depth,
		GenerationID:     generationID,
		GenerationStatus: generationStatus,
		ObservedAt:       observedAt,
		Ambiguous:        strings.EqualFold(state, "ambiguous"),
	}
	if !strings.EqualFold(generationStatus, "active") {
		item.NeedsEvidence = true
		item.Reason = "stale_generation"
	}
	if item.Ambiguous {
		item.NeedsEvidence = true
		item.Reason = "ambiguous_consumer_ownership"
	}
	return entityID, item, nil
}

func crossRepoDeadCodeRelationshipType(evidence []string) string {
	for _, value := range evidence {
		for _, relationship := range []string{"CALLS", "REFERENCES", "INHERITS", "IMPORTS"} {
			if strings.Contains(strings.ToUpper(value), relationship) {
				return relationship
			}
		}
	}
	return "REACHES"
}

func crossRepoDeadCodeCitation(generationID string, consumerRepoID string, rootEntityID string, entityID string) string {
	return "code_reachability_rows:" + generationID + "/" + consumerRepoID + "/" + rootEntityID + "/" + entityID
}
