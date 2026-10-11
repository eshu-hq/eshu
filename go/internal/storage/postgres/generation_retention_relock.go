// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds the retention selection re-lock
// (relockRetentionSelection, #7334), split out of
// generation_retention_events.go when the #7903 coverage re-check pushed
// that file over the 500-line cap. The directory's dirgate row is re-pinned
// for the split (scripts/lib/dirgate-grandfather.tsv).

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

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
	covered := locked[:0]
	for _, candidate := range locked {
		if candidate.uncovered {
			result.Skipped["uncovered_writer"]++
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
