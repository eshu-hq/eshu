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

// maxCrossRepoDeadCodeHiddenConsumerRowsPerEntity caps how many out-of-grant
// consumer rows the hidden-consumer count reads for ONE producer entity, so the
// whole statement reads at most len(entityIDs) times this number -- 50,100 rows
// at the largest page deadCodeMaxLimit allows.
//
// The cap is per entity rather than per statement on purpose. A single
// statement-wide LIMIT ordered by entity id would be spent on the first entity
// ids of the page, leaving a later producer with a hidden count of zero and
// flipping its candidate from unknown_needs_evidence to dead. Saturating each
// entity's own count costs the magnitude beyond the cap and nothing else: the
// handler branches on "is this greater than zero", which stays exact.
const maxCrossRepoDeadCodeHiddenConsumerRowsPerEntity = 100

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
//
// The count saturates at maxCrossRepoDeadCodeHiddenConsumerRowsPerEntity per
// entity, which is what bounds the read. Only the reported magnitude saturates:
// the caller uses this to decide whether a producer has any consumer it cannot
// see, and that answer is unaffected by where the count stops.
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

// crossRepoDeadCodeConsumerFromJoin is the active-generation join both
// consumer-evidence statements read through: reachability rows narrowed to the
// scope's active generation. Sharing the text keeps the two statements from
// drifting into disagreeing about which generation is current.
const crossRepoDeadCodeConsumerFromJoin = `
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'`

// crossRepoDeadCodeGrantFilter appends the caller's grant array to args and
// renders the single membership test both consumer statements bind, so there is
// one place where the grant reaches this route's SQL. grantMatches selects the
// granted set (the evidence page) or its complement (the hidden count). It
// renders nothing for an unscoped caller, which keeps that caller's statement
// text unchanged.
func crossRepoDeadCodeGrantFilter(
	args []any,
	allowedRepositoryIDs []string,
	grantMatches bool,
) ([]any, string) {
	if len(allowedRepositoryIDs) == 0 {
		return args, ""
	}
	args = append(args, pgarray.Array(allowedRepositoryIDs))
	if grantMatches {
		return args, fmt.Sprintf("\n  AND row.repository_id = ANY($%d)", len(args))
	}
	return args, fmt.Sprintf("\n  AND NOT (row.repository_id = ANY($%d))", len(args))
}

// crossRepoDeadCodeConsumerScan renders the FROM/JOIN/WHERE the evidence page
// runs over, plus the argument list it binds: the producer anchor, one
// placeholder per producer entity, and the grant.
func crossRepoDeadCodeConsumerScan(
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
	args, grant := crossRepoDeadCodeGrantFilter(args, allowedRepositoryIDs, true)
	scan := crossRepoDeadCodeConsumerFromJoin + `
WHERE row.repository_id <> $1
  AND row.entity_id IN (` + strings.Join(placeholders, ", ") + `)
  AND row.depth > 0` + grant
	return scan, args
}

// buildCrossRepoDeadCodeHiddenConsumerQuery counts, per producer entity, the
// active consumers the caller's grant EXCLUDED -- the complement of what the
// evidence page reads, not the same rows. It reads no consumer identity: the
// entity id and an integer are the only columns projected.
//
// Its bound is one LATERAL arm per producer entity, each stopping at
// maxCrossRepoDeadCodeHiddenConsumerRowsPerEntity rows, so the statement reads
// at most len(entityIDs) * that cap. Each arm seeks
// code_reachability_entity_lookup_idx (entity_id, state, confidence DESC) on
// its own entity id and stops at the cap, so the cap bounds rows read and not
// just rows returned. A producer with more excluded consumers than the cap
// reports the cap.
func buildCrossRepoDeadCodeHiddenConsumerQuery(
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (string, []any) {
	args := []any{producerRepoID, pgarray.Array(entityIDs)}
	args, grant := crossRepoDeadCodeGrantFilter(args, allowedRepositoryIDs, false)
	query := fmt.Sprintf(`
SELECT ids.entity_id, capped.hidden_count
FROM unnest($2::text[]) AS ids(entity_id)
CROSS JOIN LATERAL (
  SELECT count(*) AS hidden_count
  FROM (
    SELECT 1%s
WHERE row.repository_id <> $1
  AND row.entity_id = ids.entity_id
  AND row.depth > 0%s
    LIMIT %d
  ) AS capped_rows
) AS capped
WHERE capped.hidden_count > 0
ORDER BY ids.entity_id ASC
`, crossRepoDeadCodeConsumerFromJoin, grant, maxCrossRepoDeadCodeHiddenConsumerRowsPerEntity)
	return query, args
}

func buildCrossRepoDeadCodeConsumerEvidenceQuery(
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (string, []any) {
	scan, args := crossRepoDeadCodeConsumerScan(producerRepoID, entityIDs, allowedRepositoryIDs)
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
