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
)

const maxCrossRepoDeadCodeConsumerEvidenceRows = 1000

// CrossRepoDeadCodeConsumerEvidence returns active-generation consumer evidence
// for producer candidates using a bounded entity-id lookup. It never performs a
// graph traversal; ambiguous or stale coverage must remain unknown at the
// handler layer rather than becoming dead-code truth.
func (cr *ContentReader) CrossRepoDeadCodeConsumerEvidence(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
) (map[string][]crossRepoDeadCodeEvidence, error) {
	producerRepoID = strings.TrimSpace(producerRepoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || producerRepoID == "" || len(entityIDs) == 0 {
		return map[string][]crossRepoDeadCodeEvidence{}, nil
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

	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery(producerRepoID, entityIDs)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("cross-repo dead code consumer evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]crossRepoDeadCodeEvidence, len(entityIDs))
	for rows.Next() {
		entityID, evidence, err := scanCrossRepoDeadCodeEvidence(rows)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		result[entityID] = append(result[entityID], evidence)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return nil, err
	}
	return result, nil
}

func buildCrossRepoDeadCodeConsumerEvidenceQuery(producerRepoID string, entityIDs []string) (string, []any) {
	args := make([]any, 0, len(entityIDs)+1)
	args = append(args, producerRepoID)
	placeholders := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
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
       row.updated_at
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.repository_id <> $1
  AND row.entity_id IN (`+strings.Join(placeholders, ", ")+`)
  AND row.depth > 0
ORDER BY row.entity_id ASC, row.confidence DESC, row.depth ASC,
         row.repository_id ASC, row.root_entity_id ASC
LIMIT %d
`, maxCrossRepoDeadCodeConsumerEvidenceRows)
	return query, args
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
