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
// CONSUMER side (code_reachability_rows.repository_id). A scoped caller gets
// two reads of the same statement shape, each stopping at the same
// maxCrossRepoDeadCodeConsumerEvidenceRows+1 sentinel:
//
//   - the evidence page, with the grant bound ahead of the LIMIT. Binding it in
//     SQL rather than filtering in Go is what keeps the page honest: the row cap
//     falls on the granted consumers, so another tenant's rows can no longer
//     crowd a granted consumer off the page.
//   - the signal read, the same statement with no grant bound -- byte for byte
//     the statement this route shipped before the grant landed. It carries the
//     "this symbol has a consumer you cannot see" answer, which filtering in SQL
//     alone would lose, and losing it would mark a live symbol dead.
//
// The second return value is that signal read's rows. The handler applies the
// caller's consumer selector to them and counts the ones outside the grant; no
// ungranted consumer's id, name, or citation is ever projected into an answer.
// An unscoped caller runs the page read only, and its statement text is
// unchanged.
func (cr *ContentReader) CrossRepoDeadCodeConsumerEvidence(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (map[string][]crossRepoDeadCodeEvidence, map[string][]crossRepoDeadCodeEvidence, error) {
	producerRepoID = strings.TrimSpace(producerRepoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || producerRepoID == "" || len(entityIDs) == 0 {
		return map[string][]crossRepoDeadCodeEvidence{}, map[string][]crossRepoDeadCodeEvidence{}, nil
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

	result, truncated, err := cr.crossRepoDeadCodeConsumerRows(ctx, producerRepoID, entityIDs, allowedRepositoryIDs)
	if err != nil {
		span.RecordError(err)
		return nil, nil, err
	}
	signal := map[string][]crossRepoDeadCodeEvidence{}
	if len(allowedRepositoryIDs) > 0 {
		rows, signalTruncated, err := cr.crossRepoDeadCodeConsumerRows(ctx, producerRepoID, entityIDs, nil)
		if err != nil {
			span.RecordError(err)
			return nil, nil, err
		}
		signal = rows
		truncated = truncated || signalTruncated
	}
	// Either read reaching the sentinel means an entity may be missing rows it
	// has, so an entity left with nothing is marked unknown rather than read as
	// having no consumer at all.
	if truncated {
		markCrossRepoDeadCodeConsumerEvidenceTruncated(result, entityIDs)
	}
	span.SetAttributes(attribute.Int("db.rows.consumer_signal_entities", len(signal)))
	return result, signal, nil
}

// crossRepoDeadCodeConsumerRows runs one consumer-evidence statement and groups
// its rows by producer entity. It reports whether the read reached the
// maxCrossRepoDeadCodeConsumerEvidenceRows sentinel, which is the caller's cue
// that an entity left with no rows may still have consumers.
func (cr *ContentReader) crossRepoDeadCodeConsumerRows(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (map[string][]crossRepoDeadCodeEvidence, bool, error) {
	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery(producerRepoID, entityIDs, allowedRepositoryIDs)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("cross-repo dead code consumer evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]crossRepoDeadCodeEvidence, len(entityIDs))
	rowCount := 0
	truncated := false
	for rows.Next() {
		entityID, evidence, err := scanCrossRepoDeadCodeEvidence(rows)
		if err != nil {
			return nil, false, err
		}
		rowCount++
		if rowCount > maxCrossRepoDeadCodeConsumerEvidenceRows {
			truncated = true
			continue
		}
		result[entityID] = append(result[entityID], evidence)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return result, truncated, nil
}

// crossRepoDeadCodeGrantFilter appends the caller's grant array to args and
// renders the membership test the consumer-evidence statement binds. It renders
// nothing for an empty list, which is what lets one builder produce both reads
// this route makes: pass the grant for the page, pass nothing for the signal
// read and for an unscoped caller, and that statement's text is exactly the one
// this route shipped before the grant landed.
func crossRepoDeadCodeGrantFilter(args []any, allowedRepositoryIDs []string) ([]any, string) {
	if len(allowedRepositoryIDs) == 0 {
		return args, ""
	}
	args = append(args, pgarray.Array(allowedRepositoryIDs))
	return args, fmt.Sprintf("\n  AND row.repository_id = ANY($%d)", len(args))
}

func buildCrossRepoDeadCodeConsumerEvidenceQuery(
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (string, []any) {
	args := make([]any, 0, len(entityIDs)+2)
	args = append(args, producerRepoID)
	placeholders := make([]string, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		args = append(args, entityID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	args, grant := crossRepoDeadCodeGrantFilter(args, allowedRepositoryIDs)
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
  AND row.depth > 0%s
ORDER BY row.entity_id ASC, row.confidence DESC, row.depth ASC,
         row.repository_id ASC, row.root_entity_id ASC
LIMIT %d
`, grant, maxCrossRepoDeadCodeConsumerEvidenceRows+1)
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
