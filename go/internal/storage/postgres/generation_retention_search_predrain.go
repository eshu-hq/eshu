// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// This file holds the search index pre-delete glue the retention policy
// bounds (#7427): the weakened candidate read, the drained-set admission the
// prune transaction runs before its re-lock, and the event and row
// attribution. The pre-delete loop itself lives in the
// storage/postgres/search/index/retention leaf; only the glue that needs
// root-private retention state stays here.

// generationRetentionSearchIndexReadMarker tags the pre-delete's candidate
// read and its work_mem setting, so pg_stat_statements and the store's test
// doubles tell it apart from the prune transaction's own selection.
const generationRetentionSearchIndexReadMarker = "-- retention: search index pre-delete candidate read"

// generationRetentionSearchIndexReadQuery is the shipped selection with its
// two FOR UPDATE ... SKIP LOCKED clauses weakened to FOR NO KEY UPDATE ...
// SKIP LOCKED, run in its own short transaction that rolls back before the
// first pre-delete batch (#7427). Like the selection it skips, never waits
// for, a scope or generation another retention pass or an Ack holds FOR
// UPDATE. Unlike the selection it does not conflict with the FOR KEY SHARE a
// concurrent fact insert's foreign-key check takes, so it never blocks
// ingestion. That also means it is not guaranteed to pick the set the
// prune's selection picks: while inserts are in flight the read can spend
// BatchGenerationLimit slots on scopes the prune's FOR UPDATE then skips,
// and the prune's replacement scopes are then undrained and skipped as
// search_index_undrained until a later pass (a throughput cost; a generation
// is never pruned undrained). Its row locks end at the rollback, so no
// retention row lock spans a batch. A test pins the derivation.
var generationRetentionSearchIndexReadQuery = searchIndexCandidateReadQuery(generationRetentionCandidateQuery)

// generationRetentionSearchIndexReadWorkMemStatement is the selection's
// work_mem setting, tagged so it is told apart from the prune transaction's.
const generationRetentionSearchIndexReadWorkMemStatement = generationRetentionWorkMemStatement + " " + generationRetentionSearchIndexReadMarker

// searchIndexCandidateReadQuery derives the pre-delete's candidate read from
// the shipped selection: the marker comment, then the query with each
// FOR UPDATE row lock replaced by FOR NO KEY UPDATE.
func searchIndexCandidateReadQuery(selection string) string {
	return weakenedCandidateReadQuery(generationRetentionSearchIndexReadMarker, selection)
}

// generationRetentionSearchIndex carries one pass's pre-delete into the prune
// transaction: the drained set, and the generations the prune admitted on
// it, so the pass can log any it then left unpruned.
type generationRetentionSearchIndex struct {
	pass     *indexretention.Pass
	admitted []generationRetentionCandidate
}

// predeleteSearchIndex runs the pass's search index pre-delete before the
// prune transaction opens (#7427): it reads the eligible candidates in a
// transaction it rolls back first, drops uncovered writers, delta-protected
// generations (#7802) and each identity group's newest copy as the prune's
// planning does, and drains the rest oldest first in committed
// batches under the policy's row cap and deadline. It checks the retention
// key indexes first, so a cycle the prune would refuse changes nothing.
func (s GenerationRetentionStore) predeleteSearchIndex(
	ctx context.Context,
	beginner db.Beginner,
	now time.Time,
	policy GenerationRetentionPolicy,
) (*generationRetentionSearchIndex, error) {
	if err := checkGenerationRetentionKeyIndexes(ctx, s.database); err != nil {
		return nil, err
	}
	candidates, err := s.readSearchIndexCandidates(ctx, beginner, now, policy)
	if err != nil {
		return nil, err
	}
	covered := candidates[:0]
	for _, candidate := range candidates {
		if !candidate.uncovered && !candidate.deltaProtected { // never admitted to the prune (#8139)
			covered = append(covered, candidate)
		}
	}
	prunable, _ := dropNewestSameIdentityPerBatch(covered)
	generations := make([]indexretention.Generation, 0, len(prunable))
	for _, candidate := range prunable {
		generations = append(generations, indexretention.Generation{
			ScopeID: candidate.scopeID, GenerationID: candidate.generationID,
		})
	}
	pass := indexretention.NewPass(policy.SearchIndexPredeleteRowCap,
		time.Now().Add(policy.SearchIndexPredeleteDeadline), s.logger)
	if err := pass.Run(ctx, beginner, generations); err != nil {
		return nil, fmt.Errorf("generation retention: %w", err)
	}
	return &generationRetentionSearchIndex{pass: pass}, nil
}

// readSearchIndexCandidates runs the candidate read in its own transaction
// and rolls it back before returning, releasing the selection's row locks.
func (s GenerationRetentionStore) readSearchIndexCandidates(
	ctx context.Context,
	beginner db.Beginner,
	now time.Time,
	policy GenerationRetentionPolicy,
) ([]generationRetentionCandidate, error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("generation retention: begin search index candidate read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, generationRetentionSearchIndexReadWorkMemStatement); err != nil {
		return nil, fmt.Errorf("generation retention: set search index candidate read work_mem: %w", err)
	}
	rows, err := tx.QueryContext(ctx, generationRetentionSearchIndexReadQuery,
		now.Add(-policy.MaxSupersededAge), policy.MinSupersededGenerations,
		policy.BatchGenerationLimit, now.Add(-policy.HardMaxSupersededAge))
	if err != nil {
		return nil, fmt.Errorf("generation retention: read search index candidates: %w", err)
	}
	candidates, err := scanRetentionCandidates(rows)
	if err != nil {
		return nil, fmt.Errorf("generation retention: read search index candidates: %w", err)
	}
	sortRetentionCandidatesOldestFirst(candidates)
	return candidates, nil
}

// admit keeps, inside the prune transaction and before the re-lock, only the
// candidates (and chunk target) the pre-delete drained and whose
// per-generation advisory lock this transaction takes; the rest count as
// skipped and stay for a later pass. It also folds the pre-delete's phase
// times and batch count into the result.
func (si *generationRetentionSearchIndex) admit(
	ctx context.Context,
	tx db.Transaction,
	candidates []generationRetentionCandidate,
	chunkTarget *generationRetentionCandidate,
	result *GenerationRetentionResult,
) ([]generationRetentionCandidate, *generationRetentionCandidate, error) {
	_, _, batches, keyset, sweep := si.pass.Totals()
	if si.pass.Ran() {
		result.PhaseDurations[GenerationRetentionPhasePredeleteSearchIndex] += keyset
		result.PhaseDurations[GenerationRetentionPhaseSweepSearchIndex] += sweep
	}
	result.SearchIndexBatches = batches
	drained := func(candidate generationRetentionCandidate) bool {
		if si.pass.Drained(candidate.generationID) {
			return true
		}
		result.Skipped[searchIndexUndrainedSkippedReason]++
		return false
	}
	var ids []string
	kept := make([]generationRetentionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if drained(candidate) {
			kept = append(kept, candidate)
			ids = append(ids, candidate.generationID)
		}
	}
	if chunkTarget != nil {
		if drained(*chunkTarget) {
			ids = append(ids, chunkTarget.generationID)
		} else {
			chunkTarget = nil
		}
	}
	phaseStart := time.Now()
	locked, err := indexretention.TryLockGenerations(ctx, tx, ids)
	result.PhaseDurations[GenerationRetentionPhaseSelectCandidates] += time.Since(phaseStart)
	if err != nil {
		return nil, nil, fmt.Errorf("generation retention: %w", err)
	}
	admitted := kept[:0]
	for _, candidate := range kept {
		if locked[candidate.generationID] {
			admitted = append(admitted, candidate)
		} else {
			result.Skipped[searchIndexLockBusySkippedReason]++
		}
	}
	if chunkTarget != nil && !locked[chunkTarget.generationID] {
		result.Skipped[searchIndexLockBusySkippedReason]++
		chunkTarget = nil
	}
	si.admitted = append(si.admitted[:0], admitted...)
	if chunkTarget != nil {
		si.admitted = append(si.admitted, *chunkTarget)
	}
	return admitted, chunkTarget, nil
}

// eventCounts returns the per-generation event row counts with each
// generation's pre-deleted search index rows added: the recount under the
// lock runs after the pre-delete and sees those tables empty, so without
// this a pruned generation's retention event would record 0 rows the
// counter (finish) still adds. It copies every inner map it changes; the
// batch's row-limit totals were already computed from the recount.
func (si *generationRetentionSearchIndex) eventCounts(perGeneration map[string]map[string]int64) map[string]map[string]int64 {
	out := make(map[string]map[string]int64, len(perGeneration))
	for generationID, tables := range perGeneration {
		out[generationID] = tables
	}
	for _, candidate := range si.admitted {
		report, ok := si.pass.Report(candidate.generationID)
		if !ok || report.Rows() == 0 {
			continue
		}
		tables := make(map[string]int64, len(out[candidate.generationID])+2)
		for table, rows := range out[candidate.generationID] {
			tables[table] = rows
		}
		tables["eshu_search_index_terms"] += report.TermsDeleted
		tables["eshu_search_index_documents"] += report.DocumentsDeleted
		out[candidate.generationID] = tables
	}
	return out
}

// finish adds the pre-delete's rows to RowsPruned once pruneCandidates has
// rebuilt it from the batch counts (the recount sees the drained tables
// empty). Rows of generations the prune removed also sit in their events
// (eventCounts); rows pre-deleted for a generation this pass did not prune
// appear only on the counter, because no event is written for it. It logs every admitted generation the prune did not then remove:
// a re-lock miss, a row-limit refit or a chunk drain. Its search index rows
// are already gone, which is harmless for a terminal superseded generation;
// a later pass prunes the rest.
func (si *generationRetentionSearchIndex) finish(
	ctx context.Context,
	logger *slog.Logger,
	result *GenerationRetentionResult,
	pruned []generationRetentionCandidate,
) {
	terms, documents, _, _, _ := si.pass.Totals()
	for table, rows := range map[string]int64{"eshu_search_index_terms": terms, "eshu_search_index_documents": documents} {
		if rows > 0 {
			result.RowsPruned[table] += rows
		}
	}
	if logger == nil {
		return
	}
	prunedIDs := make(map[string]bool, len(pruned))
	for _, candidate := range pruned {
		prunedIDs[candidate.generationID] = true
	}
	for _, candidate := range si.admitted {
		if prunedIDs[candidate.generationID] {
			continue
		}
		logger.InfoContext(ctx, "generation retention search index pre-deleted generation left for a later pass",
			slog.String(telemetry.LogKeyScopeID, candidate.scopeID),
			slog.String(telemetry.LogKeyGenerationID, candidate.generationID),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}
