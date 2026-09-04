// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

func (h *CodeHandler) filterCrossRepoDeadCodeResultsWithoutProducerLocalIncomingEdges(
	ctx context.Context,
	results []map[string]any,
	label string,
) ([]map[string]any, error) {
	if len(results) == 0 {
		return results, nil
	}
	incoming, err := h.legacyDeadCodeIncomingEntityIDs(ctx, results)
	if err != nil {
		return nil, err
	}
	graphIncoming, err := h.deadCodeResultsWithGraphIncomingEdges(
		ctx,
		deadCodeResultsNeedingGraphIncomingProbe(results, label),
		label,
	)
	if err != nil {
		return nil, err
	}
	return applyDeadCodeIncomingEdges(results, incoming, graphIncoming), nil
}

// crossRepoDeadCodeConsumerReadPlan decides how one request's consumer-evidence
// lookup is bounded, and reports false when it cannot be bounded at all.
//
// A request that names consumers binds those, because that is where its row cap
// belongs: with a grant of several repositories and a selector naming one, the
// page used to be cut from the whole grant and the requested consumer could
// fall off the end of it. consumerRepoIDs is already resolved through the grant
// by applyRepositorySelectorForCapability; intersecting again is the belt to
// that braces, and a scoped caller left with nothing gets no read rather than
// the unbounded statement an empty list renders.
//
// Such a request also skips the ungranted-consumer probe. That probe reports
// consumers outside the grant, and every selector entry the grant admits is, by
// definition, inside the grant -- so for a request that named consumers its
// answer could only be about repositories the request excluded, which
// filterCrossRepoDeadCodeEvidence drops before anything is counted. Leaving
// SignalGrant empty is what keeps that structural: the probe cannot report a
// consumer the caller did not ask about because it never runs.
func crossRepoDeadCodeConsumerReadPlan(
	access repositoryAccessFilter,
	consumerRepoIDs []string,
) (crossRepoDeadCodeConsumerReads, bool) {
	if len(consumerRepoIDs) > 0 {
		page := consumerRepoIDs
		if access.Scoped() {
			page = grantedCrossRepoDeadCodeConsumerIDs(access, consumerRepoIDs)
			if len(page) == 0 {
				return crossRepoDeadCodeConsumerReads{}, false
			}
		}
		return crossRepoDeadCodeConsumerReads{PageRepositoryIDs: page}, true
	}
	if !access.Scoped() {
		return crossRepoDeadCodeConsumerReads{}, true
	}
	grant := access.RepositorySearchIDs()
	if len(grant) == 0 {
		return crossRepoDeadCodeConsumerReads{}, false
	}
	return crossRepoDeadCodeConsumerReads{PageRepositoryIDs: grant, SignalGrant: grant}, true
}

// grantedCrossRepoDeadCodeConsumerIDs keeps the requested consumers the grant
// admits, preserving the request's order so the bound array stays deterministic.
func grantedCrossRepoDeadCodeConsumerIDs(
	access repositoryAccessFilter,
	consumerRepoIDs []string,
) []string {
	granted := make([]string, 0, len(consumerRepoIDs))
	for _, repoID := range consumerRepoIDs {
		if access.AllowsRepositoryID(repoID) {
			granted = append(granted, repoID)
		}
	}
	return granted
}

func filterCrossRepoDeadCodeEvidence(
	evidence []crossRepoDeadCodeEvidence,
	allowedConsumers map[string]struct{},
	access repositoryAccessFilter,
) ([]crossRepoDeadCodeEvidence, []crossRepoDeadCodeEvidence) {
	visible := make([]crossRepoDeadCodeEvidence, 0, len(evidence))
	hidden := make([]crossRepoDeadCodeEvidence, 0)
	for _, row := range evidence {
		if row.NeedsEvidence && row.ConsumerRepoID == "" {
			visible = append(visible, row)
			continue
		}
		if len(allowedConsumers) > 0 {
			if _, ok := allowedConsumers[row.ConsumerRepoID]; !ok {
				continue
			}
		}
		if !access.AllowsRepositoryID(row.ConsumerRepoID) {
			hidden = append(hidden, row)
			continue
		}
		visible = append(visible, row)
	}
	return visible, hidden
}

func crossRepoDeadCodeUnknownReasons(
	row map[string]any,
	evidence []crossRepoDeadCodeEvidence,
	hiddenCount int,
	evidenceAvailable bool,
) []string {
	reasons := make([]string, 0)
	if !evidenceAvailable {
		reasons = append(reasons, "cross_repo_evidence_unavailable")
	}
	if hiddenCount > 0 {
		reasons = append(reasons, "permission_hidden_consumer")
	}
	if row["classification"] == deadCodeClassificationAmbiguous {
		reasons = append(reasons, "candidate_ambiguous")
	}
	for _, item := range evidence {
		if item.NeedsEvidence || item.Ambiguous || !strings.EqualFold(item.GenerationStatus, "active") {
			reason := strings.TrimSpace(item.Reason)
			if reason == "" && !strings.EqualFold(item.GenerationStatus, "active") {
				reason = "stale_generation"
			}
			if reason == "" {
				reason = "needs_evidence"
			}
			reasons = append(reasons, reason)
			continue
		}
		if item.Confidence <= codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName) {
			reasons = append(reasons, "ambiguous_consumer_ownership")
		}
	}
	slices.Sort(reasons)
	return slices.Compact(reasons)
}

// crossRepoDeadCodeUngrantedConsumerProbeQuery answers, per producer entity on
// one candidate page, whether that entity has at least one active-generation
// consumer row in a repository the caller was not granted. It answers whether,
// never which: no ungranted repository id, consumer entity id, or citation is
// in its result, so nothing about a repository the caller cannot see can reach
// an answer even by accident.
//
// $1 is the producer repository, $2 the page's producer entity ids, $3 the
// caller's grant, $4 the page's entity count. The text does not change with the
// page or grant size, so every request plans as the same statement.
//
// The out-of-grant test is three repository_id ranges around the sorted grant
// -- below the first granted id, between two consecutive ones, above the last
// -- because a range is something an index can seek to and stop at. The
// complement of the grant is exactly the union of those ranges, and
// code_reachability_entity_repository_idx (migration 100) makes each one an
// Index Cond under an equality on entity_id, so proving a producer entity has
// NO ungranted consumer costs one seek per range rather than a scan of its
// whole fan-in group. The predicate this replaces --
// NOT (repository_id = ANY($3)) -- is not seekable: measured on the same data
// it takes a Parallel Seq Scan.
//
// Two details are load-bearing and neither is cosmetic:
//
//   - the grant is ordered by Postgres, in the `gap` CTE, not by the caller.
//     The ranges only partition the domain when their bounds are sorted in the
//     COLLATION the index and the comparisons use, and Go's byte order is not
//     that collation. Bounds sorted in Go can put a granted repository inside a
//     range the probe treats as ungranted, which reports a hidden consumer that
//     is not hidden.
//   - every range is probed through CROSS JOIN LATERAL ... LIMIT 1, including
//     the two that need only one bound. The LIMIT stops the subquery being
//     flattened, which is what keeps the per-entity equality correlated and the
//     range bound a run-time constant. Both failures were observed rather than
//     guessed. Written as a plain correlated EXISTS, the interior ranges plan
//     identically under a custom plan and then lose both bounds from the Index
//     Cond under a generic plan -- which is where pgx's statement cache puts
//     them. The outer two are worse: on a short candidate page Postgres turns a
//     plain EXISTS into a hashed subplan, which drops row.entity_id =
//     page.entity_id and reads the whole table once instead of seeking per
//     entity.
//
// An empty $3 makes every range empty and the probe answer "nothing hidden" for
// every entity, which is the opposite of the truth for a caller granted
// nothing. crossRepoDeadCodeConsumerReadPlan refuses that caller before any
// read, and crossRepoDeadCodeUngrantedConsumers refuses an empty grant again.
const crossRepoDeadCodeUngrantedConsumerProbeQuery = `
WITH page AS (
  SELECT DISTINCT id AS entity_id FROM unnest($2::text[]) AS id
), granted AS (
  SELECT DISTINCT id AS repository_id FROM unnest($3::text[]) AS id
), grant_bounds AS (
  SELECT min(repository_id) AS lowest, max(repository_id) AS highest FROM granted
), gap AS (
  SELECT lo, hi
  FROM (
    SELECT lag(repository_id) OVER (ORDER BY repository_id) AS lo,
           repository_id AS hi
    FROM granted
  ) AS ordered
  WHERE lo IS NOT NULL
)
SELECT page.entity_id
FROM page
WHERE EXISTS (
        SELECT 1
        FROM grant_bounds
        CROSS JOIN LATERAL (
          SELECT 1
          FROM code_reachability_rows AS row
          JOIN ingestion_scopes AS scope
            ON scope.scope_id = row.scope_id
           AND scope.active_generation_id = row.generation_id
          JOIN scope_generations AS generation
            ON generation.generation_id = row.generation_id
           AND generation.status = 'active'
          WHERE row.entity_id = page.entity_id
            AND row.repository_id <> $1
            AND row.depth > 0
            AND row.repository_id < grant_bounds.lowest
          LIMIT 1) AS below)
   OR EXISTS (
        SELECT 1
        FROM grant_bounds
        CROSS JOIN LATERAL (
          SELECT 1
          FROM code_reachability_rows AS row
          JOIN ingestion_scopes AS scope
            ON scope.scope_id = row.scope_id
           AND scope.active_generation_id = row.generation_id
          JOIN scope_generations AS generation
            ON generation.generation_id = row.generation_id
           AND generation.status = 'active'
          WHERE row.entity_id = page.entity_id
            AND row.repository_id <> $1
            AND row.depth > 0
            AND row.repository_id > grant_bounds.highest
          LIMIT 1) AS above)
   OR EXISTS (
        SELECT 1
        FROM gap
        CROSS JOIN LATERAL (
          SELECT 1
          FROM code_reachability_rows AS row
          JOIN ingestion_scopes AS scope
            ON scope.scope_id = row.scope_id
           AND scope.active_generation_id = row.generation_id
          JOIN scope_generations AS generation
            ON generation.generation_id = row.generation_id
           AND generation.status = 'active'
          WHERE row.entity_id = page.entity_id
            AND row.repository_id <> $1
            AND row.depth > 0
            AND row.repository_id > gap.lo
            AND row.repository_id < gap.hi
          LIMIT 1) AS interior)
LIMIT $4
`

// crossRepoDeadCodeUngrantedConsumers runs the ungranted-consumer probe for one
// candidate page and returns the producer entities that have a consumer the
// caller may not see.
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
) (crossRepoDeadCodeHiddenConsumers, error) {
	hidden := crossRepoDeadCodeHiddenConsumers{}
	if len(entityIDs) == 0 || len(grantRepositoryIDs) == 0 {
		return hidden, nil
	}
	rows, err := cr.db.QueryContext(
		ctx,
		crossRepoDeadCodeUngrantedConsumerProbeQuery,
		producerRepoID,
		pgarray.Array(entityIDs),
		pgarray.Array(grantRepositoryIDs),
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
