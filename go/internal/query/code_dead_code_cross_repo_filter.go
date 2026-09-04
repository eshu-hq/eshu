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
// code_reachability_entity_repository_idx (migration 100), one walk per
// producer entity. The seed takes that entity's smallest consumer repository;
// each recursive step seeks the smallest one strictly greater than the last;
// and the walk stops as soon as it reaches a repository the grant does not
// contain. So a walk visits each of the entity's DISTINCT consumer
// repositories at most once, stops at the first ungranted one, and never looks
// at a second row of any repository however many rows that repository has.
//
// That bound is the point. Cost per entity is one index probe per distinct
// consumer repository the grant contains, plus one -- at most min(d, N) + 1
// probes, where d is the entity's distinct consumer repositories and N the
// grant size, because the walk cannot pass more granted repositories than
// either number allows. It does not grow with the entity's row fan-in, and it
// does not grow with N alone.
//
// The shape this replaced expressed the same question as repository_id ranges
// around the sorted grant, one range per gap. That was correct but cost one
// index probe per granted repository per entity, so it grew linearly with the
// caller's grant: measured on the same data, a 250-entity page went from 6.8 ms
// at a 5-repository grant to 633 ms at 500, while this walk stays at 4.6-5.2 ms
// across the same range and 7.8 ms even for a producer entity consumed by 300
// distinct repositories. It also needed the grant ordered in the database's
// collation to be correct at all; membership here is an equality test against
// the granted CTE, which Postgres hashes, so nothing depends on sort order and
// no bound has to be rendered per granted repository.
//
// Two details are load-bearing:
//
//   - each lookup is ORDER BY row.repository_id LIMIT 1 against an index that
//     already returns that order under an entity_id equality, so the ordering
//     is free and the LIMIT stops the scan at its first row. It is not a rank:
//     nothing sorts a group, which is the whole difference from the read this
//     family started as.
//   - the walk's continue-condition is "the value we just found IS granted".
//     Dropping it does not change any answer -- the final NOT EXISTS still
//     selects the same entities -- it turns a bounded walk into a full
//     enumeration of every distinct consumer repository the entity has. Only a
//     guard that measures work can see that, which is why one exists.
//
// An empty $3 makes the seed's value ungranted for every entity and the probe
// answer "everything hidden". That happens to fail safe, but it is not an
// answer a grantless caller should get from a read at all:
// crossRepoDeadCodeConsumerReadPlan refuses that caller before any read, and
// crossRepoDeadCodeUngrantedConsumers refuses an empty grant again.
const crossRepoDeadCodeUngrantedConsumerProbeQuery = `
WITH RECURSIVE page AS (
  SELECT DISTINCT id AS entity_id FROM unnest($2::text[]) AS id
), granted AS (
  SELECT DISTINCT id AS repository_id FROM unnest($3::text[]) AS id
), walk AS (
  SELECT page.entity_id, first_consumer.repository_id
  FROM page
  CROSS JOIN LATERAL (
    SELECT row.repository_id
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
    ORDER BY row.repository_id
    LIMIT 1) AS first_consumer
  UNION ALL
  SELECT walk.entity_id,
         (SELECT row.repository_id
          FROM code_reachability_rows AS row
          JOIN ingestion_scopes AS scope
            ON scope.scope_id = row.scope_id
           AND scope.active_generation_id = row.generation_id
          JOIN scope_generations AS generation
            ON generation.generation_id = row.generation_id
           AND generation.status = 'active'
          WHERE row.entity_id = walk.entity_id
            AND row.repository_id <> $1
            AND row.depth > 0
            AND row.repository_id > walk.repository_id
          ORDER BY row.repository_id
          LIMIT 1)
  FROM walk
  WHERE walk.repository_id IS NOT NULL
    AND EXISTS (SELECT 1 FROM granted WHERE granted.repository_id = walk.repository_id)
)
SELECT DISTINCT walk.entity_id
FROM walk
WHERE walk.repository_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM granted WHERE granted.repository_id = walk.repository_id)
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
