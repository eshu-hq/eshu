// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

const maxCrossRepoDeadCodeConsumerEvidenceRows = 1000

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
//     not ask about can crowd a wanted consumer off the page. That cap bounds
//     what comes back, and -- only because migration 103 carries the
//     statement's ORDER BY -- how far the scan goes to produce it: up to the
//     cap times the retained generations per position, not the cap alone; see
//     buildCrossRepoDeadCodeConsumerEvidenceQuery.
//   - the ungranted-consumer probe, when reads.SignalGrant is set. It carries
//     the "this symbol has a consumer you cannot see" answer, which filtering in
//     SQL alone would lose, and losing it would mark a live symbol dead. It
//     returns producer entity ids and nothing else --
//     deadcode.CrossRepoDeadCodeUngrantedConsumerProbeQuery walks each producer entity's
//     distinct (repository_id, scope_id) pairs in index order and stops as soon
//     as one of them is both outside the grant and live, a loose index scan
//     rather than the page statement re-run with no grant bound. Pairs rather
//     than repositories, and live rather than merely present, because only the
//     scope carries which generation is active: a stale-only row in an
//     ungranted repository is not a consumer the caller cannot see, and does
//     not stop the walk.
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
	reads code.CrossRepoDeadCodeConsumerReads,
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, code.CrossRepoDeadCodeHiddenConsumers, error) {
	producerRepoID = strings.TrimSpace(producerRepoID)
	entityIDs = cleanDeadCodeIncomingEntityIDs(entityIDs)
	if cr == nil || cr.db == nil || producerRepoID == "" || len(entityIDs) == 0 {
		return map[string][]deadcode.CrossRepoDeadCodeEvidence{}, code.CrossRepoDeadCodeHiddenConsumers{}, nil
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
	hidden := code.CrossRepoDeadCodeHiddenConsumers{}
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
) (map[string][]deadcode.CrossRepoDeadCodeEvidence, crossRepoDeadCodeConsumerCoverage, error) {
	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery(producerRepoID, entityIDs, allowedRepositoryIDs)
	coverage := crossRepoDeadCodeConsumerCoverage{}
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, coverage, fmt.Errorf("cross-repo dead code consumer evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]deadcode.CrossRepoDeadCodeEvidence, len(entityIDs))
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

// crossRepoDeadCodeUngrantedConsumers runs the ungranted-consumer probe for one
// candidate page and returns the producer entities that have a consumer the
// caller may not see. The probe query itself
// (deadcode.CrossRepoDeadCodeUngrantedConsumerProbeQuery) stays in
// code_dead_code_cross_repo_filter.go; this method is relocated here (#6060)
// because its receiver, ContentReader, is declared in content_reader.go,
// which stays in root when code_dead_code_cross_repo_filter.go's family
// moves to its own subpackage -- Go requires a type's methods to live in the
// same package as their declaration.
//
// Every entity on the page is probed, so the answer covers all of them: unlike
// the row-returning read it replaces, the probe has no shared row budget one
// busy entity can spend, and therefore never leaves a later entity unproven.
// The result is bounded by the page's own entity count, which the statement
// binds as its LIMIT.
//
// An empty grant returns no entities and runs nothing. The statement would
// answer "nothing hidden" for a caller who may see nothing, so the guard is
// here as well as in crossRepoDeadCodeConsumerReadPlan.
func (cr *ContentReader) crossRepoDeadCodeUngrantedConsumers(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) (code.CrossRepoDeadCodeHiddenConsumers, error) {
	hidden := code.CrossRepoDeadCodeHiddenConsumers{}
	if len(entityIDs) == 0 || len(grantRepositoryIDs) == 0 {
		return hidden, nil
	}
	rows, err := cr.db.QueryContext(
		ctx,
		deadcode.CrossRepoDeadCodeUngrantedConsumerProbeQuery,
		producerRepoID,
		array.Of(entityIDs),
		array.Of(grantRepositoryIDs),
		len(entityIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("cross-repo dead code ungranted consumer probe: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			return nil, fmt.Errorf("scan cross-repo dead code ungranted consumer probe: %w", err)
		}
		hidden[entityID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hidden, nil
}

// crossRepoDeadCodeGrantFilter appends a consumer-repository array to args and
// renders the membership test the grant-bound evidence statement binds ahead of
// its LIMIT, so the cap falls on consumers the caller may see. It renders
// nothing for an empty list.
func crossRepoDeadCodeGrantFilter(args []any, allowedRepositoryIDs []string) ([]any, string) {
	if len(allowedRepositoryIDs) == 0 {
		return args, ""
	}
	args = append(args, array.Of(allowedRepositoryIDs))
	return args, fmt.Sprintf("\n  AND row.repository_id = ANY($%d)", len(args))
}

// buildCrossRepoDeadCodeConsumerEvidenceQuery renders the evidence page: the
// active-generation consumer rows for these producer entities, ranked strongest
// first within each entity, capped at the sentinel. Both shapes return the same
// rows in the same order, (entity_id, confidence DESC, depth, repository_id,
// root_entity_id, scope_id, generation_id) -- migration 103's
// code_reachability_entity_confidence_rank_idx key. The last two columns are a
// tiebreak: without them an index scan and a top-N heapsort disagree about
// which of two rows equal on the first five lands at the cap. depth > 0 stays a
// predicate: depth 0 is the root's own row, not a consumer edge.
//
// An unscoped read (no consumer list) takes the per-entity lateral (#7249). A
// grant-bound read keeps the flat statement, unchanged, because neither shape
// bounds it: with the grant bound the planner leaves the rank index for another
// index plus a sort in both, and reads an entity's whole fan-in from about 50
// granted repositories up. The lateral also costs more buffers there, through
// its per-row ingestion_scopes probe. That is a documented pre-existing limit,
// see docs/internal/evidence/7249-dead-code-reachability.md.
func buildCrossRepoDeadCodeConsumerEvidenceQuery(
	producerRepoID string,
	entityIDs []string,
	allowedRepositoryIDs []string,
) (string, []any) {
	if len(allowedRepositoryIDs) > 0 {
		return buildCrossRepoDeadCodeConsumerEvidenceGrantQuery(producerRepoID, entityIDs, allowedRepositoryIDs)
	}
	return crossRepoDeadCodeConsumerEvidenceLateralQuery, []any{producerRepoID, array.Of(entityIDs)}
}

// buildCrossRepoDeadCodeConsumerEvidenceGrantQuery renders the grant-bound page:
// one placeholder per entity, the grant ahead of the LIMIT, and the liveness
// test as joins above one ordered scan. The active-generation join discards
// superseded entries after the scan reads them, so the read is the cap times
// the retained generations per position at best.
func buildCrossRepoDeadCodeConsumerEvidenceGrantQuery(
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
         row.repository_id ASC, row.root_entity_id ASC,
         row.scope_id ASC, row.generation_id ASC
LIMIT %d
`, grant, maxCrossRepoDeadCodeConsumerEvidenceRows+1)
	return query, args
}

// crossRepoDeadCodeConsumerEvidenceLateralQuery is the unscoped page. $1 is the
// producer repository and $2 the page's entity ids as one text[], so the text
// is the same for every page. Each entity's consumers are ranked by an Index
// Only Scan of the rank index in key order under a per-entity LIMIT; the cap is
// lossless because the page is ordered entity first. The liveness tests are
// per-row subqueries, not joins: the join form flipped, at 250 page entities on
// the QA replica's statistics, to probing the primary key once per active
// ingestion scope, and a subquery cannot drive the plan. A lateral is planned
// from the AVERAGE fan-in, so "another index plus a sort" is always a
// candidate; the ordered walk wins because it needs no heap. That holds while
// the table's pages are marked all-visible, which the planner prices into an
// Index Only Scan (97.9% of pages on the QA replica; the guards run on a
// vacuumed fixture): pages a fresh snapshot rewrite has not yet had vacuumed
// need heap reads and can tip the plan the other way. The walk is the cap times
// (1 + retained generations) per entity, and an entity that crosses the cap is
// read in full to its boundary.
//
// The columns the index lacks are fetched per returned row on the five
// primary-key columns plus depth, which identify one row. The writer bounds the
// index entries that fetch reads when the planner serves it from an index whose
// key lacks root_entity_id: the only writer,
// CodeReachabilityStore.ReplaceRepositoryRows, replaces a (scope, generation,
// repository) snapshot that codeintel.BuildCodeReachabilityRowsWithStats builds
// with one row per entity (codeReachabilityKeepBest; pinned by
// TestBuildCodeReachabilityRowsEmitsOneRowPerEntityPerSnapshot).
// generation_status is the literal 'active': the liveness test admits nothing
// else.
var crossRepoDeadCodeConsumerEvidenceLateralQuery = fmt.Sprintf(`
SELECT hit.entity_id,
       detail.repository_id,
       '' AS consumer_repo_name,
       detail.root_entity_id,
       detail.depth,
       detail.state,
       detail.confidence,
       detail.min_resolution_method,
       detail.evidence,
       detail.root_kinds,
       detail.generation_id,
       'active'::text AS generation_status,
       detail.observed_at,
       detail.updated_at
FROM (SELECT DISTINCT id FROM unnest($2::text[]) AS id ORDER BY id) AS page
CROSS JOIN LATERAL (
  SELECT row.entity_id, row.confidence, row.depth, row.repository_id,
         row.root_entity_id, row.scope_id, row.generation_id
  FROM code_reachability_rows AS row
  WHERE row.entity_id = page.id
    AND row.repository_id <> $1
    AND row.depth > 0
    AND row.generation_id = (
      SELECT scope.active_generation_id
      FROM ingestion_scopes AS scope
      WHERE scope.scope_id = row.scope_id)
    AND (
      SELECT generation.status
      FROM scope_generations AS generation
      WHERE generation.generation_id = row.generation_id) = 'active'
  ORDER BY row.confidence DESC, row.depth ASC,
           row.repository_id ASC, row.root_entity_id ASC,
           row.scope_id ASC, row.generation_id ASC
  LIMIT %[1]d
) AS hit
JOIN code_reachability_rows AS detail
  ON detail.scope_id = hit.scope_id
 AND detail.generation_id = hit.generation_id
 AND detail.repository_id = hit.repository_id
 AND detail.root_entity_id = hit.root_entity_id
 AND detail.entity_id = hit.entity_id
 AND detail.depth = hit.depth
ORDER BY page.id ASC, hit.confidence DESC, hit.depth ASC,
         hit.repository_id ASC, hit.root_entity_id ASC,
         hit.scope_id ASC, hit.generation_id ASC
LIMIT %[1]d
`, maxCrossRepoDeadCodeConsumerEvidenceRows+1)

// markCrossRepoDeadCodeConsumerEvidenceTruncated adds the truncation marker to
// every entity the evidence page is not proven to have finished. The marker
// carries NeedsEvidence, so the handler answers unknown_needs_evidence for that
// entity rather than reading a partial page as a complete one.
func markCrossRepoDeadCodeConsumerEvidenceTruncated(
	result map[string][]deadcode.CrossRepoDeadCodeEvidence,
	entityIDs []string,
	page crossRepoDeadCodeConsumerCoverage,
) {
	for _, entityID := range entityIDs {
		if page.covers(entityID) {
			continue
		}
		result[entityID] = append(result[entityID], deadcode.CrossRepoDeadCodeEvidence{
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
