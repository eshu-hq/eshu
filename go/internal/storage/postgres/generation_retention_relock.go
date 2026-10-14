// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds the retention selection re-lock
// (relockRetentionSelection, #7334), split out of
// generation_retention_events.go when the #7903 coverage re-check pushed
// that file over the 500-line cap. The directory's dirgate row is re-pinned
// for the split (scripts/lib/dirgate-grandfather.tsv). Since #7981 it also
// holds the same-identity witness (dropNewestSameIdentityPerBatch) and the
// bounded over-limit chunk prune, which both run under the same narrow
// targeted lock the re-lock takes.

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// sameSHANewestSkippedReason tallies prune-batch members the #7981 witness
// retains: the newest copy per same-identity group in the batch. Once older
// copies clear, a later batch has no group to protect and prunes it.
const sameSHANewestSkippedReason = "same_sha_newest"

// generationRetentionChunkedProgressSkippedReason records one over-limit
// generation drained by one bounded fact chunk this cycle (#7981): progress,
// not a block. The next cycle re-selects the remainder until it fits.
const generationRetentionChunkedProgressSkippedReason = "row_limit_chunked"

// sameIdentityGroup reports whether the candidate carries a #7981 identity
// key (scope, non-empty SHA, is_delta, COALESCE freshness_hint). A NULL or
// empty SHA never matches, so generations without a recorded commit stay on
// the rank and age rule only.
func (c generationRetentionCandidate) sameIdentityGroup() (string, bool) {
	if c.sourceCommitSHA == "" {
		return "", false
	}
	delta := "full"
	if c.isDelta {
		delta = "delta"
	}
	return c.scopeID + "\x00" + c.sourceCommitSHA + "\x00" + delta + "\x00" + c.freshnessHint, true
}

// dropNewestSameIdentityPerBatch enforces the #7981 witness rule on a
// covered prune batch: per identity group it returns the prunable rest
// first and the retained newest copy second (max superseded_at,
// generation_id tie-break, the SQL witness's order). Singletons, empty
// SHAs and uncovered members pass through on the prunable side; callers
// count the retained ones under sameSHANewestSkippedReason.
func dropNewestSameIdentityPerBatch(
	candidates []generationRetentionCandidate,
) (prunable, retained []generationRetentionCandidate) {
	newestByGroup := make(map[string]int, len(candidates))
	groupSize := make(map[string]int, len(candidates))
	for i, candidate := range candidates {
		group, ok := candidate.sameIdentityGroup()
		if !ok {
			continue
		}
		groupSize[group]++
		if j, seen := newestByGroup[group]; !seen || newerSameIdentityCopy(candidates[j], candidate) {
			newestByGroup[group] = i
		}
	}
	retainedIndex := make(map[int]bool, len(newestByGroup))
	for group, i := range newestByGroup {
		if groupSize[group] > 1 {
			retainedIndex[i] = true
		}
	}
	for i, candidate := range candidates {
		if retainedIndex[i] {
			retained = append(retained, candidate)
		} else {
			prunable = append(prunable, candidate)
		}
	}
	return prunable, retained
}

// newerSameIdentityCopy reports whether b is a newer copy than a within one
// identity group: later superseded_at wins, generation_id breaks ties, the
// same total order the SQL witness compares as a row tuple.
func newerSameIdentityCopy(a, b generationRetentionCandidate) bool {
	if c := b.supersededAt.Compare(a.supersededAt); c != 0 {
		return c > 0
	}
	return b.generationID > a.generationID
}

// relockRetentionSelection re-locks the provisionally selected set after the
// selection's savepoint rollback (#7334 fix 1) and recounts it once under
// the new locks; the recount's counts replace the unlocked planning counts
// for the prune decision and the pruning events. The targeted query holds
// only the selected rows (scope, generation) in deterministic order, so two
// overlapping passes lock the same rows in the same sequence. A candidate
// the re-lock misses was taken, re-activated or given work after the
// rollback: the pass silently drops it and prunes the locked subset. A
// candidate the re-lock reports as uncovered flipped coverage after the
// selection (#7903): it was never locked, and the pass counts it as
// skipped so it surfaces in the cycle log/counter instead of vanishing.
// held reports whether the re-lock locked at least one row, so the caller
// can scope its lock-hold measurement to the narrow window.
func (s GenerationRetentionStore) relockRetentionSelection(
	ctx context.Context,
	tx db.Transaction,
	now time.Time,
	policy GenerationRetentionPolicy,
	candidates []generationRetentionCandidate,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64, bool, error) {
	if len(candidates) == 0 {
		return nil, nil, nil, false, nil
	}
	if s.beforeTargetedLock != nil {
		s.beforeTargetedLock()
	}
	phaseStart := time.Now()
	rows, err := tx.QueryContext(ctx, generationRetentionTargetedCandidateQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations, []string{},
		retentionScopeIDs(candidates), retentionGenerationIDs(candidates),
		now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("generation retention: lock candidates: %w", err)
	}
	locked, err := scanRetentionCandidates(rows)
	result.PhaseDurations[GenerationRetentionPhaseSelectCandidates] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("generation retention: lock candidates: %w", err)
	}
	// Members the re-lock reports as uncovered flipped coverage after the
	// selection; they were never locked, so they leave the batch here with
	// a skip tally instead of flowing into the recount and the prune.
	// Members it reports as delta-protected flipped the same way (#7802):
	// their scope turned delta-active after the selection.
	covered := locked[:0]
	for _, candidate := range locked {
		if candidate.uncovered {
			result.Skipped["uncovered_writer"]++
			continue
		}
		if candidate.deltaProtected {
			result.Skipped["delta_base"]++
			continue
		}
		covered = append(covered, candidate)
	}
	locked = covered
	if len(locked) == 0 {
		return nil, nil, nil, false, nil
	}
	// The unlocked planning counts were taken under locks that no longer
	// exist; the recount under the new locks is the authoritative one. One
	// recount settles the batch: it counts exactly the locked set, so key
	// attribution already reflects any member the re-lock dropped.
	sortRetentionCandidatesOldestFirst(locked)
	phaseStart = time.Now()
	_, eventRowCounts, _, err := s.countRows(ctx, tx, retentionScopeIDs(locked), retentionGenerationIDs(locked))
	result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, false, err
	}
	// No second #7981 witness here: planning already retained the newest
	// copy per identity group, and retained copies never enter the lock
	// set, so pruning any locked subset cannot remove every copy. Regrouping
	// the locked subset would retain its newest again and drain one copy per
	// cycle.
	if len(locked) == 0 {
		return nil, nil, nil, true, nil
	}
	selected, rowCounts, selectedEventRowCounts, skipped := selectCandidatesWithinRowLimit(
		locked, eventRowCounts, policy.BatchRowLimit)
	for _, generationID := range skipped {
		result.Skipped[rowLimitSkipReason(eventRowCounts[generationID], policy.BatchRowLimit)]++
	}
	if len(selected) == 0 {
		return nil, nil, nil, true, nil
	}
	return selected, rowCounts, selectedEventRowCounts, true, nil
}

// relockChunkedBatch re-locks one pass carrying an over-limit chunk target
// beside its planned normal batch (#7981). One targeted statement locks
// every selected row in deterministic order; the locked set splits into the
// chunk target and the normal subset. The chunk drains first so the recount
// already reflects the removed page. A missed chunk target drops silently; a
// flipped-uncovered one counts as an uncovered skip, a flipped delta-base
// one as a delta_base skip (#7802). A short page (fewer
// rows than the bound, including empty) folds back into the normal subset
// and prunes whole. It returns the fitted batch with its counts, the drained
// chunk, whether a chunk drained, and whether anything locked.
func (s GenerationRetentionStore) relockChunkedBatch(
	ctx context.Context,
	tx db.Transaction,
	now time.Time,
	policy GenerationRetentionPolicy,
	candidates []generationRetentionCandidate,
	chunkTarget generationRetentionCandidate,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, map[string]int64, map[string]map[string]int64,
	generationRetentionChunk, bool, bool, error,
) {
	if s.beforeTargetedLock != nil {
		s.beforeTargetedLock()
	}
	all := make([]generationRetentionCandidate, 0, len(candidates)+1)
	all = append(all, candidates...)
	all = append(all, chunkTarget)
	phaseStart := time.Now()
	rows, err := tx.QueryContext(ctx, generationRetentionTargetedCandidateQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations, []string{},
		retentionScopeIDs(all), retentionGenerationIDs(all),
		now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		return nil, nil, nil, generationRetentionChunk{}, false, false,
			fmt.Errorf("generation retention: lock chunked batch: %w", err)
	}
	locked, err := scanRetentionCandidates(rows)
	result.PhaseDurations[GenerationRetentionPhaseSelectCandidates] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, generationRetentionChunk{}, false, false,
			fmt.Errorf("generation retention: lock chunked batch: %w", err)
	}
	var normal []generationRetentionCandidate
	var lockedChunk *generationRetentionCandidate
	for _, candidate := range locked {
		switch {
		case candidate.uncovered:
			result.Skipped["uncovered_writer"]++
		case candidate.deltaProtected:
			// A scope that turned delta-active after selection (#7802):
			// the member, chunk target included, is a protected base or
			// chain row, never locked, so the skip tally stands in for it.
			result.Skipped["delta_base"]++
		case candidate.generationID == chunkTarget.generationID:
			target := candidate
			lockedChunk = &target
		default:
			normal = append(normal, candidate)
		}
	}
	if lockedChunk == nil && len(normal) == 0 {
		return nil, nil, nil, generationRetentionChunk{}, false, false, nil
	}
	if lockedChunk != nil && s.afterChunkTargetLocked != nil {
		s.afterChunkTargetLocked()
	}
	// No second #7981 witness on the normal subset either: planning retained
	// each group's newest outside the lock set, so this subset is safe to
	// prune whole; regrouping it would drain one copy per cycle.
	sortRetentionCandidatesOldestFirst(normal)
	var chunk generationRetentionChunk
	chunked := false
	if lockedChunk != nil {
		// A raised BatchRowLimit must not turn the bounded per-cycle page
		// into an unbounded one: the page never exceeds the default row
		// limit, the bound the normal path's single-transaction prune was
		// designed around.
		chunkRows := min(policy.BatchRowLimit, defaultGenerationRetentionRowLimit)
		drained, full, err := pruneChunkedCandidate(ctx, tx, *lockedChunk, chunkRows, result)
		if err != nil {
			return nil, nil, nil, generationRetentionChunk{}, false, true, err
		}
		if full {
			normal = append(normal, *lockedChunk)
			sortRetentionCandidatesOldestFirst(normal)
		} else {
			chunk, chunked = drained, true
		}
	}
	if len(normal) == 0 {
		return nil, nil, nil, chunk, chunked, true, nil
	}
	// The recount under the new locks is authoritative: it counts exactly
	// the locked normal subset after the chunk delete, so key attribution
	// already reflects any member the re-lock dropped and any key the chunk
	// pruned.
	phaseStart = time.Now()
	_, eventRowCounts, _, err := s.countRows(ctx, tx, retentionScopeIDs(normal), retentionGenerationIDs(normal))
	result.PhaseDurations[GenerationRetentionPhaseCountRows] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, nil, generationRetentionChunk{}, false, true, err
	}
	selected, rowCounts, selectedEventRowCounts, skipped := selectCandidatesWithinRowLimit(
		normal, eventRowCounts, policy.BatchRowLimit)
	for _, generationID := range skipped {
		result.Skipped[rowLimitSkipReason(eventRowCounts[generationID], policy.BatchRowLimit)]++
	}
	return selected, rowCounts, selectedEventRowCounts, chunk, chunked, true, nil
}

// generationRetentionChunkPageQuery pages one over-limit generation's own
// facts oldest-first through the keyset index (#7981), returning the page's
// ids plus the content keys its doomed-key prunes need. $1 scope id,
// $2 generation id, $3 page bound.
const generationRetentionChunkPageQuery = `-- retention: over-limit chunk page
SELECT fact.fact_id, fact.fact_kind, fact.is_tombstone,
    COALESCE(fact.payload->>'repo_id', '') AS repo_id,
    COALESCE(fact.payload->>'relative_path', '') AS relative_path,
    COALESCE(fact.payload->>'entity_id', '') AS entity_id
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
ORDER BY fact.observed_at, fact.fact_id
LIMIT $3
`

// generationRetentionChunkDeleteFactsQuery deletes exactly the paged facts by
// primary key (#7981): one PK seek per fact, never a scan, and only rows of
// the chunk's own (scope, generation). $1 fact ids.
const generationRetentionChunkDeleteFactsQuery = `-- retention: over-limit chunk fact delete
DELETE FROM fact_records
WHERE fact_id = ANY($1::text[])
`

// generationRetentionChunkPruneFilesQuery prunes content file rows whose
// only live fact was in the deleted page (#7981): page keys replace the
// batch prune's generation facts, and the doomed probe keeps a key any live
// fact still names. References go in the same statement; secret lines follow
// by FK cascade. $1 repo ids, $2 paths, parallel page-key arrays.
const generationRetentionChunkPruneFilesQuery = `-- retention: over-limit chunk file prune
WITH page_keys AS (
    SELECT DISTINCT unnest($1::text[]) AS repo_id, unnest($2::text[]) AS relative_path
),
doomed AS (
    SELECT page_key.repo_id, page_key.relative_path
    FROM page_keys AS page_key
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.fact_kind = 'file' AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = page_key.repo_id
          AND retained.payload->>'relative_path' = page_key.relative_path
    )
),
deleted_references AS (
    DELETE FROM content_file_references AS ref
    USING doomed AS d
    WHERE ref.repo_id = d.repo_id AND ref.relative_path = d.relative_path
    RETURNING 1
),
deleted_files AS (
    DELETE FROM content_files AS content
    USING doomed AS d
    WHERE content.repo_id = d.repo_id AND content.relative_path = d.relative_path
    RETURNING 1
)
SELECT (SELECT count(*) FROM deleted_references), (SELECT count(*) FROM deleted_files)
`

// generationRetentionChunkPruneEntitiesQuery prunes content entity rows whose
// only live fact was in the deleted page (#7981), same page-key/doomed shape
// as files. The caller locks repositories first and deletes mirror orphans
// after, the batch order. $1 repo ids, $2 entity ids, parallel arrays.
const generationRetentionChunkPruneEntitiesQuery = `-- retention: over-limit chunk entity prune
WITH page_keys AS (
    SELECT DISTINCT unnest($1::text[]) AS repo_id, unnest($2::text[]) AS entity_id
),
doomed AS (
    SELECT page_key.repo_id, page_key.entity_id
    FROM page_keys AS page_key
    WHERE NOT EXISTS (
        SELECT 1
        FROM fact_records AS retained
        WHERE retained.fact_kind = 'content_entity' AND retained.is_tombstone = FALSE
          AND retained.payload->>'repo_id' = page_key.repo_id
          AND retained.payload->>'entity_id' = page_key.entity_id
    )
),
deleted_entities AS (
    DELETE FROM content_entities AS entity
    USING doomed AS d
    WHERE entity.repo_id = d.repo_id AND entity.entity_id = d.entity_id
    RETURNING 1
)
SELECT count(*) FROM deleted_entities
`

// generationRetentionChunkDeleteMirrorOrphansQuery drops infra mirror rows
// whose content row the chunk just deleted (#7981), scoped to the page's
// repository ids explicitly: fact-derived scoping would miss repositories
// whose entity facts drained entirely in this chunk. $1 repo ids.
const generationRetentionChunkDeleteMirrorOrphansQuery = `-- retention: over-limit chunk mirror orphans
DELETE FROM infra_resource_entities AS mirror
WHERE mirror.repo_id = ANY($1::text[])
  AND NOT EXISTS (
      SELECT 1 FROM content_entities AS entity
      WHERE entity.entity_id = mirror.entity_id
  )
`

// generationRetentionChunk reports one bounded over-limit chunk prune: the
// fact rows removed and the content rows that died with the page's keys, by
// RowsPruned table name.
type generationRetentionChunk struct {
	factsDeleted     int64
	filesPruned      int64
	referencesPruned int64
	entitiesPruned   int64
	mirrorPruned     int64
}

// retentionChunkPage is one fact page: the ids to delete and the live
// content keys (non-tombstone file and content_entity facts only, the batch
// prune's key shape) their doomed-key prunes need.
type retentionChunkPage struct {
	ids         []string
	fileRepos   []string
	filePaths   []string
	entityRepos []string
	entityIDs   []string
}

// pruneChunkedCandidate drains one bounded page of an over-limit
// generation's own facts under the narrow targeted lock (#7981): page the
// oldest facts through the keyset index, delete exactly that page by primary
// key, prune content rows whose only live fact was in the page, and clean
// their new mirror orphans. The generation row is never touched, so a crash
// resumes on the still-superseded row with no progress table. Per-fact
// children follow by FK cascade; generation-scoped rows (work items, ledger,
// search state) wait for the final whole-generation prune with its
// ledger-before-generations order. It returns full when the page holds fewer
// rows than the bound: the remainder fits, so the caller folds it back into
// the normal batch.
func pruneChunkedCandidate(
	ctx context.Context,
	tx db.Transaction,
	candidate generationRetentionCandidate,
	chunkRows int,
	result *GenerationRetentionResult,
) (chunk generationRetentionChunk, full bool, err error) {
	phaseStart := time.Now()
	defer func() {
		result.PhaseDurations[GenerationRetentionPhasePruneChunkedFacts] += time.Since(phaseStart)
	}()
	page, err := queryChunkPage(ctx, tx, candidate, chunkRows)
	if err != nil {
		return generationRetentionChunk{}, false, err
	}
	if len(page.ids) == 0 {
		return generationRetentionChunk{}, true, nil
	}
	// The infra repository locks derive from the generation's facts, so
	// they are taken before the page delete removes them; the orphan
	// delete after the entity prune takes the page's repos explicitly.
	if len(page.entityRepos) > 0 {
		if err := inventory.LockRepositoriesForGenerations(
			ctx, tx, []string{candidate.generationID}); err != nil {
			return generationRetentionChunk{}, false, fmt.Errorf(
				"generation retention: lock chunk repositories: %w", err)
		}
	}
	deleted, err := execRowsAffected(ctx, tx, generationRetentionChunkDeleteFactsQuery, page.ids)
	if err != nil {
		return generationRetentionChunk{}, false, fmt.Errorf(
			"generation retention: delete chunk facts: %w", err)
	}
	chunk.factsDeleted = deleted
	if len(page.fileRepos) > 0 {
		var refs, files int64
		if err := queryChunkPruneCounts(ctx, tx, generationRetentionChunkPruneFilesQuery,
			page.fileRepos, page.filePaths, &refs, &files); err != nil {
			return generationRetentionChunk{}, false, fmt.Errorf(
				"generation retention: prune chunk files: %w", err)
		}
		chunk.referencesPruned = refs
		chunk.filesPruned = files
	}
	if len(page.entityRepos) > 0 {
		var entities int64
		if err := queryChunkPruneCounts(ctx, tx, generationRetentionChunkPruneEntitiesQuery,
			page.entityRepos, page.entityIDs, &entities); err != nil {
			return generationRetentionChunk{}, false, fmt.Errorf(
				"generation retention: prune chunk entities: %w", err)
		}
		chunk.entitiesPruned = entities
		mirror, err := execRowsAffected(ctx, tx,
			generationRetentionChunkDeleteMirrorOrphansQuery, page.entityRepos)
		if err != nil {
			return generationRetentionChunk{}, false, fmt.Errorf(
				"generation retention: delete chunk mirror orphans: %w", err)
		}
		chunk.mirrorPruned = mirror
	}
	return chunk, len(page.ids) < chunkRows, nil
}
