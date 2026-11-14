// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds one chunk pre-delete batch (#10020): the advisory
// try-lock, the eligibility re-check with the row lock, one bounded page
// delete with its content prunes, and the commit, plus the page statements
// the batch runs. The pass that drives batches lives in
// generation_retention_chunk_predrain.go.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	factkeys "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/keys"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/statements"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
)

// generationRetentionChunkPredrainLockMarker tags the predrain batch's
// eligibility re-check and row lock.
const generationRetentionChunkPredrainLockMarker = "-- retention: chunk predrain batch lock"

// generationRetentionChunkPredrainLockQuery re-checks one generation's full
// prune eligibility (window, rank, live work, uncovered writer, delta base,
// same-identity witness) and locks its scope and generation rows FOR NO KEY
// UPDATE ... SKIP LOCKED. It is the shipped targeted candidate lock with
// weakened locks: the predicates are identical, so a member eligible at
// selection is still eligible here unless its row changed under it, and a
// concurrent activation or work claim that commits between the batch's
// snapshot and its row lock fails the re-check (EvalPlanQual) instead of
// being deleted under. $1 soft cutoff, $2 minimum newer generations, $3
// excluded ids, $4 scope ids, $5 generation ids, $6 hard-ceiling cutoff.
var generationRetentionChunkPredrainLockQuery = generationRetentionChunkPredrainLockMarker + "\n" +
	strings.ReplaceAll(statements.TargetedCandidateLock, "FOR UPDATE OF ", "FOR NO KEY UPDATE OF ")

// generationRetentionChunkPredrainLockTimeout bounds how long one predrain
// batch waits for a row lock another session holds. It sits below the
// server's default deadlock_timeout (1 s), so the batch, not a writer,
// gives way: the generation stays undrained for this pass and the next
// pass retries it.
const generationRetentionChunkPredrainLockTimeout = "500ms"

// generationRetentionChunkPredrainSetLockTimeout is SET LOCAL lock_timeout:
// it ends with the batch transaction and never leaks onto the pooled
// connection.
const generationRetentionChunkPredrainSetLockTimeout = `SELECT set_config('lock_timeout', $1, true)`

// chunkPredrainStopReason names why a generation's drain ended without a
// short page. The empty reason means the batch committed and the drain
// continues.
type chunkPredrainStopReason string

// Chunk predrain stop reasons.
const (
	chunkPredrainStopBusy       chunkPredrainStopReason = "advisory_lock_busy"
	chunkPredrainStopRowBusy    chunkPredrainStopReason = "row_lock_busy"
	chunkPredrainStopIneligible chunkPredrainStopReason = "ineligible"
	// chunkPredrainStopLockTimeout is a lock wait that outlasted the
	// batch's lock_timeout (SQLSTATE 55P03): the timed-out statement
	// applied nothing and the batch transaction rolls back, so the
	// generation stays undrained and the next pass retries it.
	chunkPredrainStopLockTimeout chunkPredrainStopReason = "lock_timeout"
)

// chunkPredrainBatchResult is one committed (or refused) predrain batch.
type chunkPredrainBatchResult struct {
	committed bool
	drained   bool
	stopped   chunkPredrainStopReason
	chunk     generationRetentionChunk
	elapsed   time.Duration
}

// predrainChunkedTarget drains one over-limit generation until a short page
// (it fits now), the budget runs out, a lock is busy, or the generation
// flips ineligible. Rows deleted by committed batches stay deleted; a
// generation that stops mid-drain is retried by the next pass.
func (s GenerationRetentionStore) predrainChunkedTarget(
	ctx context.Context,
	beginner db.Beginner,
	now time.Time,
	policy GenerationRetentionPolicy,
	target generationRetentionCandidate,
	chunkRows int,
	budget *generationRetentionChunkBudget,
	predrain *generationRetentionChunkPredrain,
) error {
	predrain.targets = append(predrain.targets, target)
	aggregate := predrain.chunks[target.generationID]
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("generation retention: chunk predrain: %w", err)
		}
		if budget.exhausted(time.Now()) {
			break
		}
		result, err := s.predrainChunkBatch(ctx, beginner, now, policy, target, chunkRows)
		if err != nil {
			return err
		}
		predrain.duration += result.elapsed
		if !result.committed {
			break
		}
		// Touched means the pass deleted rows here: an empty page commits
		// to release its locks but deletes nothing, so it neither merges
		// progress at finish nor suppresses the exclusion skip.
		if result.chunk.rows() > 0 {
			predrain.touched[target.generationID] = true
		}
		predrain.batches++
		budget.batches++
		budget.rows += result.chunk.rows()
		aggregate = aggregate.merged(result.chunk)
		predrain.chunks[target.generationID] = aggregate
		if result.drained {
			// A short or empty page means nothing remains to drain: the
			// prune transaction may admit the generation this pass.
			predrain.drained[target.generationID] = true
			break
		}
	}
	return nil
}

// predrainChunkBatch runs one chunk predrain batch transaction: the
// advisory try-lock, the eligibility re-check with the row lock, one
// bounded page delete with its content prunes, and the commit. A busy lock
// or an ineligible generation rolls the batch back with nothing applied.
// A lock wait that outlasts the batch's SET LOCAL lock_timeout (SQLSTATE
// 55P03, e.g. a repo advisory lock or a page delete queued behind a writer)
// is a graceful stop with the same shape: the timed-out statement applied
// nothing and the rollback leaves the generation undrained for the next
// pass, so it must not fail the pass.
func (s GenerationRetentionStore) predrainChunkBatch(
	ctx context.Context,
	beginner db.Beginner,
	now time.Time,
	policy GenerationRetentionPolicy,
	target generationRetentionCandidate,
	chunkRows int,
) (result chunkPredrainBatchResult, err error) {
	start := time.Now()
	finish := func(result chunkPredrainBatchResult) chunkPredrainBatchResult {
		result.elapsed = time.Since(start)
		return result
	}
	defer func() {
		if err != nil && coordination.IsLockNotAvailable(err) {
			result = finish(chunkPredrainBatchResult{stopped: chunkPredrainStopLockTimeout})
			err = nil
		}
	}()
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: begin chunk predrain batch: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, generationRetentionChunkPredrainSetLockTimeout, generationRetentionChunkPredrainLockTimeout); err != nil {
		return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: set chunk predrain lock_timeout: %w", err)
	}
	locked, err := indexretention.TryLockGenerations(ctx, tx, []string{target.generationID})
	if err != nil {
		return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: %w", err)
	}
	if !locked[target.generationID] {
		return finish(chunkPredrainBatchResult{stopped: chunkPredrainStopBusy}), nil
	}
	rows, err := tx.QueryContext(ctx, generationRetentionChunkPredrainLockQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations, []string{},
		[]string{target.scopeID}, []string{target.generationID},
		now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: lock chunk predrain batch: %w", err)
	}
	members, err := scanRetentionCandidates(rows)
	if err != nil {
		return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: lock chunk predrain batch: %w", err)
	}
	eligible := false
	for _, member := range members {
		switch {
		case member.uncovered || member.deltaProtected:
			return finish(chunkPredrainBatchResult{stopped: chunkPredrainStopIneligible}), nil
		case member.generationID == target.generationID:
			eligible = true
		}
	}
	if !eligible {
		// The row lock missed: another pass holds the generation and
		// SKIP LOCKED moved past it, or it flipped out of the batch.
		// Either way this pass leaves it alone.
		return finish(chunkPredrainBatchResult{stopped: chunkPredrainStopRowBusy}), nil
	}
	page, err := queryChunkPage(ctx, tx, target, chunkRows)
	if err != nil {
		return chunkPredrainBatchResult{}, err
	}
	if len(page.ids) == 0 {
		if err := tx.Commit(); err != nil {
			return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: commit empty chunk predrain batch: %w", err)
		}
		committed = true
		return finish(chunkPredrainBatchResult{committed: true, drained: true}), nil
	}
	if s.beforeChunkPredrainPage != nil {
		s.beforeChunkPredrainPage()
	}
	var chunk generationRetentionChunk
	// The infra repository locks derive from the generation's facts, so
	// they are taken before the page delete removes them; the orphan
	// delete after the entity prune takes the page's repos explicitly.
	if len(page.entityRepos) > 0 {
		if err := inventory.LockRepositoriesForGenerations(ctx, tx, []string{target.generationID}); err != nil {
			return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: lock chunk predrain repositories: %w", err)
		}
	}
	if chunk.factKeysPruned, err = factkeys.DeletePage(ctx, tx, target.scopeID, target.generationID, page.ids); err != nil {
		return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: %w", err)
	}
	deleted, err := execRowsAffected(ctx, tx, generationRetentionChunkDeleteFactsQuery, page.ids)
	if err != nil {
		return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: delete chunk predrain facts: %w", err)
	}
	chunk.factsDeleted = deleted
	if len(page.fileRepos) > 0 {
		var refs, files int64
		if err := queryChunkPruneCounts(ctx, tx, generationRetentionChunkPruneFilesQuery,
			page.fileRepos, page.filePaths, &refs, &files); err != nil {
			return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: prune chunk predrain files: %w", err)
		}
		chunk.referencesPruned = refs
		chunk.filesPruned = files
	}
	if len(page.entityRepos) > 0 {
		var entities int64
		if err := queryChunkPruneCounts(ctx, tx, generationRetentionChunkPruneEntitiesQuery,
			page.entityRepos, page.entityIDs, &entities); err != nil {
			return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: prune chunk predrain entities: %w", err)
		}
		chunk.entitiesPruned = entities
		mirror, err := execRowsAffected(ctx, tx,
			generationRetentionChunkDeleteMirrorOrphansQuery, page.entityRepos)
		if err != nil {
			return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: delete chunk predrain mirror orphans: %w", err)
		}
		chunk.mirrorPruned = mirror
	}
	if err := tx.Commit(); err != nil {
		return chunkPredrainBatchResult{}, fmt.Errorf("generation retention: commit chunk predrain batch: %w", err)
	}
	committed = true
	return finish(chunkPredrainBatchResult{committed: true, drained: len(page.ids) < chunkRows, chunk: chunk}), nil
}

// generationRetentionChunk reports one bounded over-limit chunk prune: the
// fact rows removed and the content rows that died with the page's keys, by
// RowsPruned table name.
type generationRetentionChunk struct {
	factsDeleted     int64
	filesPruned      int64
	referencesPruned int64
	entitiesPruned   int64
	mirrorPruned     int64
	factKeysPruned   map[string]int64 // #7420 key sidecar rows, by table
}

// rows reports every fact and content row one drained chunk removed, so one
// budget number bounds the pass's pre-delete work.
func (c generationRetentionChunk) rows() int64 {
	total := c.factsDeleted + c.filesPruned + c.referencesPruned + c.entitiesPruned + c.mirrorPruned
	for _, rows := range c.factKeysPruned {
		total += rows
	}
	return total
}

// merged folds one batch's drained rows into the generation's aggregate.
func (c generationRetentionChunk) merged(other generationRetentionChunk) generationRetentionChunk {
	out := c
	out.factsDeleted += other.factsDeleted
	out.filesPruned += other.filesPruned
	out.referencesPruned += other.referencesPruned
	out.entitiesPruned += other.entitiesPruned
	out.mirrorPruned += other.mirrorPruned
	if other.factKeysPruned != nil {
		if out.factKeysPruned == nil {
			out.factKeysPruned = make(map[string]int64, len(other.factKeysPruned))
		}
		for table, rows := range other.factKeysPruned {
			out.factKeysPruned[table] += rows
		}
	}
	return out
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
