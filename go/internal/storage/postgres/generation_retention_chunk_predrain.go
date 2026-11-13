// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file holds the over-limit chunk pre-delete (#10020): generations too
// large to prune in one bounded transaction drain their fact and content
// pages in committed batches before the prune transaction opens, following
// the #7427 search index pre-delete precedent. The narrow scope lock then
// covers only the re-lock, the locked recount, the events, and the final
// whole-generation delete; no page ever drains under it.
//
// Every batch is its own transaction: it takes the generation's advisory
// lock with pg_try_advisory_xact_lock(7427, hashtext(generation_id)) — the
// same lock the search pre-delete batches and the prune's admission take —
// sets a lock_timeout below deadlock_timeout, and runs one statement that
// re-checks eligibility and locks the generation row FOR NO KEY UPDATE ...
// SKIP LOCKED in its own snapshot. A busy lock, a missed row lock, or an
// ineligible generation ends that generation's drain with no row deleted;
// the next pass retries it. Rows deleted by earlier committed batches stay
// deleted, and the generation row is never touched, so a crash resumes on
// the still-superseded row with no progress table: the drain is idempotent.

// generationRetentionChunkPredrainReadMarker tags the predrain's candidate
// read, so pg_stat_statements and the store's test doubles tell it apart
// from the prune transaction's own selection.
const generationRetentionChunkPredrainReadMarker = "-- retention: chunk predrain candidate read"

// generationRetentionChunkPredrainReadQuery is the shipped selection with
// its FOR UPDATE ... SKIP LOCKED clauses weakened to FOR NO KEY UPDATE ...
// SKIP LOCKED, run in its own short transaction that rolls back before the
// first pre-delete batch. Like the search pre-delete's read it never blocks
// ingestion and never holds a lock across a batch.
var generationRetentionChunkPredrainReadQuery = weakenedCandidateReadQuery(
	generationRetentionChunkPredrainReadMarker, generationRetentionCandidateQuery)

// generationRetentionChunkPredrainReadWorkMemStatement is the read's work_mem
// setting, tagged so it is told apart from the prune transaction's: the
// work_mem probe observes the prune transaction only.
const generationRetentionChunkPredrainReadWorkMemStatement = generationRetentionWorkMemStatement + " " + generationRetentionChunkPredrainReadMarker

// generationRetentionChunkRows bounds one predrain page, and the whole
// under-lock chunk mechanism it retires: a raised BatchRowLimit must not
// turn the bounded per-cycle page into an unbounded one.
func generationRetentionChunkRows(policy GenerationRetentionPolicy) int {
	return min(policy.BatchRowLimit, defaultGenerationRetentionRowLimit)
}

// generationRetentionChunkBudget bounds one pass's chunk pre-delete: the
// rows it may delete and the wall-clock deadline. The first batch of a pass
// always runs, so every pass makes progress on its oldest over-limit
// generation. A Budget is not safe for concurrent use; one pass drains its
// generations one after another.
type generationRetentionChunkBudget struct {
	rowCap   int64
	deadline time.Time
	rows     int64
	batches  int
}

// exhausted reports whether the pass may start no further batch.
func (b *generationRetentionChunkBudget) exhausted(now time.Time) bool {
	if b.batches == 0 {
		return false
	}
	return b.rows >= b.rowCap || (!b.deadline.IsZero() && !now.Before(b.deadline))
}

// generationRetentionChunkPredrain carries one pass's chunk pre-delete into
// the prune transaction: the per-generation drained rows, which targets saw
// deletions, and the batches the pass committed, so the pass can merge them
// into its result and log any generation it then left unpruned.
type generationRetentionChunkPredrain struct {
	targets  []generationRetentionCandidate
	chunks   map[string]generationRetentionChunk
	touched  map[string]bool
	batches  int
	duration time.Duration
}

// predrainChunkedGenerations runs the pass's chunk pre-delete before the
// prune transaction opens (#10020): it reads the eligible candidates in a
// transaction it rolls back first, drops uncovered writers, delta-protected
// generations and each identity group's newest copy as the prune's planning
// does, and drains every over-limit generation oldest first in committed
// batches under the policy's row cap and deadline. It checks the retention
// key indexes first, so a cycle the prune would refuse changes nothing.
func (s GenerationRetentionStore) predrainChunkedGenerations(
	ctx context.Context,
	beginner db.Beginner,
	now time.Time,
	policy GenerationRetentionPolicy,
) (*generationRetentionChunkPredrain, error) {
	if err := checkGenerationRetentionKeyIndexes(ctx, s.database); err != nil {
		return nil, err
	}
	candidates, err := s.readChunkPredrainCandidates(ctx, beginner, now, policy)
	if err != nil {
		return nil, err
	}
	predrain := &generationRetentionChunkPredrain{
		chunks:  make(map[string]generationRetentionChunk),
		touched: make(map[string]bool),
	}
	chunkRows := generationRetentionChunkRows(policy)
	budget := &generationRetentionChunkBudget{
		rowCap:   policy.ChunkPredeleteRowCap,
		deadline: time.Now().Add(policy.ChunkPredeleteDeadline),
	}
	for i := range candidates {
		if budget.exhausted(time.Now()) {
			break
		}
		if err := s.predrainChunkedTarget(ctx, beginner, now, policy, candidates[i], chunkRows, budget, predrain); err != nil {
			return nil, err
		}
	}
	return predrain, nil
}

// readChunkPredrainCandidates runs the predrain's candidate read in its own
// transaction and rolls it back before returning, releasing the selection's
// row locks. It mirrors selectPrunableCandidates' unlocked planning —
// uncovered and delta-protected members never drain, the witness retains
// each identity group's newest copy, and only generations over the row
// limit on own facts alone drain — but it never tallies: the prune's own
// selection reports every skip again on the post-drain facts.
func (s GenerationRetentionStore) readChunkPredrainCandidates(
	ctx context.Context,
	beginner db.Beginner,
	now time.Time,
	policy GenerationRetentionPolicy,
) ([]generationRetentionCandidate, error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("generation retention: begin chunk predrain candidate read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, generationRetentionChunkPredrainReadWorkMemStatement); err != nil {
		return nil, fmt.Errorf("generation retention: set chunk predrain candidate read work_mem: %w", err)
	}
	rows, err := tx.QueryContext(ctx, generationRetentionChunkPredrainReadQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations,
		policy.BatchGenerationLimit, now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		return nil, fmt.Errorf("generation retention: read chunk predrain candidates: %w", err)
	}
	candidates, err := scanRetentionCandidates(rows)
	if err != nil {
		return nil, fmt.Errorf("generation retention: read chunk predrain candidates: %w", err)
	}
	sortRetentionCandidatesOldestFirst(candidates)
	prunable := candidates[:0]
	for _, candidate := range candidates {
		if !candidate.uncovered && !candidate.deltaProtected {
			prunable = append(prunable, candidate)
		}
	}
	prunable, _ = dropNewestSameIdentityPerBatch(prunable)
	ownFacts, err := s.prescreenOwnFacts(ctx, tx, prunable)
	if err != nil {
		return nil, err
	}
	rowLimit := int64(policy.BatchRowLimit)
	var overLimit []generationRetentionCandidate
	for _, candidate := range prunable {
		if ownFacts[candidate.generationID] > rowLimit {
			overLimit = append(overLimit, candidate)
		}
	}
	return overLimit, nil
}

// excludeChunkTarget drops a still-over-limit chunk target instead of
// draining a page under the scope lock (#10020): its presence after the
// predrain means the predrain did not finish it this pass, so it waits for
// the next pass rather than holding the lock for a page drain. A target the
// predrain touched reports its drained rows with the row_limit_chunked skip
// at finish; an untouched one reports the skip here.
func (cp *generationRetentionChunkPredrain) excludeChunkTarget(
	chunkTarget *generationRetentionCandidate,
	result *GenerationRetentionResult,
) {
	if chunkTarget == nil {
		return
	}
	if !cp.touched[chunkTarget.generationID] {
		result.Skipped[generationRetentionChunkedProgressSkippedReason]++
	}
}

// eventCounts returns the per-generation event row counts with each pruned
// generation's predrained chunk rows added: the recount under the lock runs
// after the predrain and sees those tables smaller, so without this a
// pruned generation's retention event would record 0 rows the counter
// (finish) still adds. It copies every inner map it changes; generations
// this pass did not prune are left to finish, which reports them without
// events.
func (cp *generationRetentionChunkPredrain) eventCounts(
	perGeneration map[string]map[string]int64,
) map[string]map[string]int64 {
	out := make(map[string]map[string]int64, len(perGeneration))
	for generationID, tables := range perGeneration {
		out[generationID] = tables
	}
	for generationID, chunk := range cp.chunks {
		tables, ok := out[generationID]
		if !ok {
			continue
		}
		changed := make(map[string]int64, len(tables)+6)
		for table, rows := range tables {
			changed[table] = rows
		}
		changed["fact_records"] += chunk.factsDeleted
		changed["content_files"] += chunk.filesPruned
		changed["content_file_references"] += chunk.referencesPruned
		changed["content_entities"] += chunk.entitiesPruned
		changed["infra_resource_entities"] += chunk.mirrorPruned
		for table, rows := range chunk.factKeysPruned {
			changed[table] += rows
		}
		out[generationID] = changed
	}
	return out
}

// finish merges the predrained rows into RowsPruned once pruneCandidates
// has rebuilt it from the batch counts. Rows of generations the prune
// removed sit in their events too (eventCounts); rows predrained for a
// generation this pass did not prune merge with the row_limit_chunked skip
// instead, because no event is written for it. It folds the pass's batch
// count and phase time, and logs every touched generation the prune did
// not then remove: its predrained rows are already gone, which is harmless
// for a terminal superseded generation, and a later pass prunes the rest.
func (cp *generationRetentionChunkPredrain) finish(
	ctx context.Context,
	logger *slog.Logger,
	result *GenerationRetentionResult,
	pruned []generationRetentionCandidate,
	now time.Time,
) {
	prunedIDs := make(map[string]bool, len(pruned))
	for _, candidate := range pruned {
		prunedIDs[candidate.generationID] = true
	}
	byID := make(map[string]generationRetentionCandidate, len(cp.targets))
	for _, target := range cp.targets {
		byID[target.generationID] = target
	}
	for generationID, chunk := range cp.chunks {
		if chunk.rows() == 0 {
			continue
		}
		if prunedIDs[generationID] {
			result.RowsPruned["fact_records"] += chunk.factsDeleted
			result.RowsPruned["content_files"] += chunk.filesPruned
			result.RowsPruned["content_file_references"] += chunk.referencesPruned
			result.RowsPruned["content_entities"] += chunk.entitiesPruned
			result.RowsPruned["infra_resource_entities"] += chunk.mirrorPruned
			for table, rows := range chunk.factKeysPruned {
				result.RowsPruned[table] += rows
			}
			continue
		}
		if target, ok := byID[generationID]; ok {
			mergeChunkProgress(result, target, chunk, now)
		}
	}
	result.ChunkPredeleteBatches = cp.batches
	if cp.batches > 0 {
		result.PhaseDurations[GenerationRetentionPhasePredrainChunk] += cp.duration
	}
	if logger == nil {
		return
	}
	for _, target := range cp.targets {
		if !cp.touched[target.generationID] || prunedIDs[target.generationID] {
			continue
		}
		logger.InfoContext(ctx, "generation retention chunk pre-deleted generation left for a later pass",
			slog.String(telemetry.LogKeyScopeID, target.scopeID),
			slog.String(telemetry.LogKeyGenerationID, target.generationID),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}
