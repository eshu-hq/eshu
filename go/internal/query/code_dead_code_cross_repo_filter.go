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
// The shape is a loose index scan -- a skip scan -- over
// code_reachability_entity_repository_scope_generation_idx (migration 101), one
// walk per producer entity, and it steps on two different keys depending on
// what it just found. From a GRANTED repository it seeks the entity's next
// repository outright (repository_id > the current one), because no scope of a
// granted repository can ever be hidden and reading them would be work spent on
// an answer already known. From an UNGRANTED one it seeks the next
// (repository_id, scope_id) PAIR, because that repository's remaining scopes
// are exactly where a hidden consumer could still be -- and, once they are
// exhausted, the next repository's pairs one at a time, for the same reason.
// Either way it stops as soon as a pair is both outside the grant and live.
//
// Pairs at all, rather than repositories throughout, because "is this consumer
// live" means "does a row exist under the active generation of the scope that
// wrote it", and only the scope carries which generation that is. A repository
// ingested by two scopes has two active generations, and a walk keyed on the
// repository alone would test one and miss the other.
//
// Cost per entity: at most G + S + 1 steps. G is the granted consumer
// repositories the walk passes, at most min(d, N) for d distinct consumer
// repositories and a grant of N, because a repository the caller can see costs
// ONE step however many ingestion scopes cover it -- the difference from the
// pair-stepping shape this replaces, where fifty scopes on one granted
// repository cost fifty steps. S is the ungranted (repository, scope) pairs it
// passes that hold no live consumer row: a repository outside the grant whose
// active generation no longer names this entity, while the retention runner
// still keeps the rows that say it once did. Such a pair is not hidden, so the
// walk steps past it rather than stopping. S is bounded by the entity's
// distinct (repository, scope) pairs that held a row inside the retention
// window, not by d or N -- the window sets how far back "used to call this"
// reaches, it is not itself the bound. It is the axis to watch where consumers
// churn.
//
// Each step is two index seeks, the pair seek and the four-column liveness
// seek. Nothing in the cost grows with the entity's row fan-in, with N alone,
// or -- this is what migration 101 bought over the two-column index it
// supersedes -- with how many superseded generations ONE pair retains. Measured
// in [#5167 hidden-consumer walk]: 5.13/8.14/9.78 ms and 3,270/3,268/3,263
// buffers at 0, 20 and 200 retained generations, against 22.3/89.2/630.4 ms and
// 39,403/154,603/1,150,489 buffers for the shape that scanned the group.
//
// [#5167 hidden-consumer walk]: ../../../docs/internal/evidence/5167-cross-repo-hidden-consumer-walk.md
//
// Five details are load-bearing:
//
//   - each step's lookup is ORDER BY row.repository_id, row.scope_id LIMIT 1
//     against an index that already returns that order under an entity_id
//     equality, so the ordering is free and the LIMIT stops the scan at its
//     first row. It is not a rank: nothing sorts a group.
//   - the two seeks are separate UNION ALL branches gated on walk.is_granted,
//     not one seek with a CASE bound. A CASE cannot become an index condition,
//     so a bound built that way would leave each step scanning; two branches
//     each keep a plain index condition, and the branch whose gate is false
//     returns nothing, so a step still performs one seek.
//   - the liveness test is a full equality -- entity, repository, scope, and
//     the generation ingestion_scopes says is active for that scope -- so it
//     seeks the active row instead of scanning the pair's rows for it, and the
//     generation arrives as a SCALAR SUBQUERY rather than as a join on the
//     outer row. That distinction is the whole of it. Joined, the planner may
//     reorder, and it does: before migration 101 it drove the walk from
//     ingestion_scopes and probed the primary key once per scope, 264 ms and
//     292,615 buffers for a page this shape answers in 5 ms; with the index in
//     place but the join still written, a corpus with one ingestion scope per
//     consumer repository made it drop generation_id out of the Index Cond,
//     seek three columns and probe scope_generations once per retained row --
//     365,181 buffers and 295 ms for ONE entity with 300 stale ungranted
//     consumer pairs, against 3,897 and 5.3 ms for the subquery form. The
//     liveness lookup took three different plans across the corpora measured,
//     so the shape has to remove the planner's choice rather than survive it.
//   - the liveness test sits behind AND after the grant test, so it runs only
//     for a repository outside the grant. A granted repository continues the
//     walk whether it is live or not, so its answer is never needed, and paying
//     for it on every step cost 8.1 ms where this costs 4.1.
//   - the walk's continue-condition is "the pair we just found is not hidden".
//     Dropping it does not change any answer -- the final filter still selects
//     the same entities -- it turns a bounded walk into a full enumeration of
//     every distinct consumer pair the entity has. Only a guard that measures
//     work can see that, which is why one exists.
//
// An empty $3 makes every pair ungranted and the probe answer "everything
// hidden". That happens to fail safe, but it is not an answer a grantless
// caller should get from a read at all: crossRepoDeadCodeConsumerReadPlan
// refuses that caller before any read, and crossRepoDeadCodeUngrantedConsumers
// refuses an empty grant again.
const crossRepoDeadCodeUngrantedConsumerProbeQuery = `
WITH RECURSIVE page AS (
  SELECT DISTINCT id AS entity_id FROM unnest($2::text[]) AS id
), granted AS (
  SELECT DISTINCT id AS repository_id FROM unnest($3::text[]) AS id
), walk AS (
  SELECT page.entity_id, seed.repository_id, seed.scope_id, seed.is_granted, seed.hidden
  FROM page
  CROSS JOIN LATERAL (
    SELECT pair.repository_id, pair.scope_id, pair.is_granted,
           NOT pair.is_granted
           AND EXISTS (
             SELECT 1
             FROM code_reachability_rows AS live_row
             WHERE live_row.entity_id = page.entity_id
               AND live_row.repository_id = pair.repository_id
               AND live_row.scope_id = pair.scope_id
               AND live_row.generation_id = (
                 SELECT scope.active_generation_id
                 FROM ingestion_scopes AS scope
                 JOIN scope_generations AS generation
                   ON generation.generation_id = scope.active_generation_id
                  AND generation.status = 'active'
                 WHERE scope.scope_id = pair.scope_id)
               AND live_row.depth > 0) AS hidden
    FROM (
      SELECT first_pair.repository_id, first_pair.scope_id,
             EXISTS (
               SELECT 1 FROM granted
               WHERE granted.repository_id = first_pair.repository_id) AS is_granted
      FROM (
        SELECT row.repository_id, row.scope_id
        FROM code_reachability_rows AS row
        WHERE row.entity_id = page.entity_id
          AND row.repository_id <> $1
        ORDER BY row.repository_id, row.scope_id
        LIMIT 1) AS first_pair) AS pair) AS seed
  UNION ALL
  SELECT walk.entity_id, step.repository_id, step.scope_id, step.is_granted, step.hidden
  FROM walk
  CROSS JOIN LATERAL (
    SELECT pair.repository_id, pair.scope_id, pair.is_granted,
           NOT pair.is_granted
           AND EXISTS (
             SELECT 1
             FROM code_reachability_rows AS live_row
             WHERE live_row.entity_id = walk.entity_id
               AND live_row.repository_id = pair.repository_id
               AND live_row.scope_id = pair.scope_id
               AND live_row.generation_id = (
                 SELECT scope.active_generation_id
                 FROM ingestion_scopes AS scope
                 JOIN scope_generations AS generation
                   ON generation.generation_id = scope.active_generation_id
                  AND generation.status = 'active'
                 WHERE scope.scope_id = pair.scope_id)
               AND live_row.depth > 0) AS hidden
    FROM (
      SELECT next_pair.repository_id, next_pair.scope_id,
             EXISTS (
               SELECT 1 FROM granted
               WHERE granted.repository_id = next_pair.repository_id) AS is_granted
      FROM (
        (SELECT row.repository_id, row.scope_id
         FROM code_reachability_rows AS row
         WHERE row.entity_id = walk.entity_id
           AND row.repository_id <> $1
           AND walk.is_granted
           AND row.repository_id > walk.repository_id
         ORDER BY row.repository_id, row.scope_id
         LIMIT 1)
        UNION ALL
        (SELECT row.repository_id, row.scope_id
         FROM code_reachability_rows AS row
         WHERE row.entity_id = walk.entity_id
           AND row.repository_id <> $1
           AND NOT walk.is_granted
           AND (row.repository_id, row.scope_id) > (walk.repository_id, walk.scope_id)
         ORDER BY row.repository_id, row.scope_id
         LIMIT 1)
        LIMIT 1) AS next_pair) AS pair) AS step
  WHERE NOT walk.hidden
)
SELECT DISTINCT walk.entity_id
FROM walk
WHERE walk.hidden
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
