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

// crossRepoDeadCodeConsumerReads says how one cross-repo consumer-evidence
// lookup is bounded. The handler builds it; the reader does what it says.
//
// PageRepositoryIDs is the consumer-repository list the evidence page binds in
// SQL ahead of its LIMIT: the request's own consumer selector when it named
// one, otherwise the caller's grant, and empty only for an unscoped caller who
// named neither -- the single case where an unbounded page is the right answer.
// Which list goes here decides where the row cap falls. Binding the grant while
// the request named one consumer let a thousand rows from another granted
// repository fill the page and push the requested consumer off it.
//
// SignalGrant is the caller's grant the ungranted-consumer probe takes the
// complement of, and empty means no probe runs. The probe answers, per producer
// entity, whether a consumer outside that grant exists; it is the half of the
// question the grant-bound page cannot see, and losing it would mark a live
// symbol dead. A request that named a consumer selector leaves this empty,
// because the only consumers the probe could report are ones that request
// excluded.
type crossRepoDeadCodeConsumerReads struct {
	PageRepositoryIDs []string
	SignalGrant       []string
}

// crossRepoDeadCodeHiddenConsumers is the set of producer entity ids the
// ungranted-consumer probe proved have at least one active-generation consumer
// in a repository outside the caller's grant.
//
// It is a set of PRODUCER entity ids -- every one of which the caller is
// already reading -- and carries nothing about the consumer: not its
// repository, not its entity, not a count. The route only ever needed the
// yes/no, and answering only the yes/no is what lets the probe stop at the
// first ungranted row instead of enumerating the group.
type crossRepoDeadCodeHiddenConsumers map[string]struct{}

// has reports whether the probe found an out-of-grant consumer for this
// producer entity.
func (h crossRepoDeadCodeHiddenConsumers) has(entityID string) bool {
	_, ok := h[entityID]
	return ok
}

// CrossRepoDeadCodeConsumerEvidence returns active-generation consumer evidence
// for producer candidates using a bounded entity-id lookup. It never performs a
// graph traversal; ambiguous or stale coverage must remain unknown at the
// handler layer rather than becoming dead-code truth.
//
// reads says how the lookup is bounded on the CONSUMER side
// (code_reachability_rows.repository_id). It produces up to two statements:
//
//   - the evidence page, with reads.PageRepositoryIDs bound ahead of the LIMIT,
//     stopping at the maxCrossRepoDeadCodeConsumerEvidenceRows+1 sentinel.
//     Binding the list in SQL rather than filtering in Go is what keeps the page
//     honest: the row cap falls on the consumers this answer is about, so
//     neither another tenant's rows nor a granted repository the request did
//     not ask about can crowd a wanted consumer off the page.
//   - the ungranted-consumer probe, when reads.SignalGrant is set. It carries
//     the "this symbol has a consumer you cannot see" answer, which filtering in
//     SQL alone would lose, and losing it would mark a live symbol dead. It
//     returns producer entity ids and nothing else -- see
//     crossRepoDeadCodeUngrantedConsumerProbeQuery for why it is expressed as
//     grant-complement ranges rather than as the page statement with no grant
//     bound.
//
// The second return value is that probe's answer. A caller that asked for no
// probe gets an empty set, and the page statement is the only one sent.
//
// Only the page can stop short, and the entities it did not finish are marked
// consumer_evidence_truncated in the first return value -- per entity, not per
// request. The probe examines every entity it is given, so it never leaves one
// unproven: one producer entity with a huge fan-in cannot cost a later entity
// its answer, which is exactly what the row-returning read it replaced did.
func (cr *ContentReader) CrossRepoDeadCodeConsumerEvidence(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	reads crossRepoDeadCodeConsumerReads,
) (map[string][]crossRepoDeadCodeEvidence, crossRepoDeadCodeHiddenConsumers, error) {
	producerRepoID = strings.TrimSpace(producerRepoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || producerRepoID == "" || len(entityIDs) == 0 {
		return map[string][]crossRepoDeadCodeEvidence{}, crossRepoDeadCodeHiddenConsumers{}, nil
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

	result, pageCoverage, err := cr.crossRepoDeadCodeConsumerRows(ctx, producerRepoID, entityIDs, reads.PageRepositoryIDs)
	if err != nil {
		span.RecordError(err)
		return nil, nil, err
	}
	hidden := crossRepoDeadCodeHiddenConsumers{}
	if len(reads.SignalGrant) > 0 {
		hidden, err = cr.crossRepoDeadCodeUngrantedConsumers(ctx, producerRepoID, entityIDs, reads.SignalGrant)
		if err != nil {
			span.RecordError(err)
			return nil, nil, err
		}
	}
	// Coverage is per entity, not per request: the page reaching its sentinel
	// leaves the entities it never finished unproven. The probe contributes no
	// coverage gap of its own because it answers for every entity it is given.
	markCrossRepoDeadCodeConsumerEvidenceTruncated(result, entityIDs, pageCoverage)
	span.SetAttributes(attribute.Int("db.rows.consumer_signal_entities", len(hidden)))
	return result, hidden, nil
}

// crossRepoDeadCodeConsumerCoverage says which producer entities one bounded
// consumer read is proven to have read in full.
//
// A read that stops at the sentinel proves nothing about the entities it never
// reached, and the statement's ORDER BY is not a boundary this process can
// compare against: it orders entity ids in the database's collation, which is
// not Go's byte order, so an entity id ranked against the last one returned can
// land on the wrong side. Coverage is therefore taken from the rows the read
// actually returned. An entity is proven complete when the read returned rows
// for it and it is not the last entity the read returned -- the read moved past
// it before the cap. Every other entity, including one with no rows at all, is
// unproven and takes the truncation marker.
type crossRepoDeadCodeConsumerCoverage struct {
	truncated bool
	complete  map[string]struct{}
}

// covers reports whether this read is proven to have returned every row the
// entity has. A read that never hit the sentinel covers every entity.
func (c crossRepoDeadCodeConsumerCoverage) covers(entityID string) bool {
	if !c.truncated {
		return true
	}
	_, ok := c.complete[entityID]
	return ok
}

// crossRepoDeadCodeConsumerRows runs one consumer-evidence statement, groups its
// rows by producer entity, and reports which entities the read finished. Rows
// past the maxCrossRepoDeadCodeConsumerEvidenceRows sentinel are dropped, and
// the entity they belong to stays unproven.
func (cr *ContentReader) crossRepoDeadCodeConsumerRows(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (map[string][]crossRepoDeadCodeEvidence, crossRepoDeadCodeConsumerCoverage, error) {
	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery(producerRepoID, entityIDs, allowedRepositoryIDs)
	coverage := crossRepoDeadCodeConsumerCoverage{}
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, coverage, fmt.Errorf("cross-repo dead code consumer evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]crossRepoDeadCodeEvidence, len(entityIDs))
	rowCount := 0
	lastEntityID := ""
	sentinelEntityID := ""
	for rows.Next() {
		entityID, evidence, err := scanCrossRepoDeadCodeEvidence(rows)
		if err != nil {
			return nil, coverage, err
		}
		rowCount++
		if rowCount > maxCrossRepoDeadCodeConsumerEvidenceRows {
			coverage.truncated = true
			// The sentinel row is dropped, but its entity id is already
			// scanned and it is the one boundary this process can compare
			// against without guessing the database's collation: the statement
			// orders by entity id, so a sentinel belonging to a different
			// entity proves the read moved past the last entity it returned.
			sentinelEntityID = entityID
			continue
		}
		lastEntityID = entityID
		result[entityID] = append(result[entityID], evidence)
	}
	if err := rows.Err(); err != nil {
		return nil, coverage, err
	}
	if coverage.truncated {
		coverage.complete = make(map[string]struct{}, len(result))
		unproven := lastEntityID
		if sentinelEntityID != "" && sentinelEntityID != lastEntityID {
			// The read stopped between two entities, not inside one, so every
			// entity it returned rows for was returned in full.
			unproven = ""
		}
		for entityID := range result {
			if entityID == unproven {
				continue
			}
			coverage.complete[entityID] = struct{}{}
		}
	}
	return result, coverage, nil
}

// crossRepoDeadCodeGrantFilter appends a consumer-repository array to args and
// renders the membership test the consumer-evidence statement binds. It renders
// nothing for an empty list, so an unscoped caller who named no consumers -- the
// one case where an unbounded page is the right answer -- executes exactly the
// statement this route shipped before the grant landed.
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

// markCrossRepoDeadCodeConsumerEvidenceTruncated adds the truncation marker to
// every entity the evidence page is not proven to have finished. The marker
// carries NeedsEvidence, so the handler answers unknown_needs_evidence for that
// entity rather than reading a partial page as a complete one.
func markCrossRepoDeadCodeConsumerEvidenceTruncated(
	result map[string][]crossRepoDeadCodeEvidence,
	entityIDs []string,
	page crossRepoDeadCodeConsumerCoverage,
) {
	for _, entityID := range entityIDs {
		if page.covers(entityID) {
			continue
		}
		result[entityID] = append(result[entityID], crossRepoDeadCodeEvidence{
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
		})
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
