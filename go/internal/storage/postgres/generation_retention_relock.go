// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds the retention selection re-lock
// (relockRetentionSelection, #7334), split out of
// generation_retention_events.go when the #7903 coverage re-check pushed
// that file over the 500-line cap. The split added one file to a
// directory that is over the dirgate cap. Since #7981 it also
// holds the same-identity witness (dropNewestSameIdentityPerBatch). The
// over-limit chunk drain it once ran under the lock moved to committed
// pre-delete batches before the prune transaction opens (#10020), so no
// page ever drains under the narrow targeted lock the re-lock takes.

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
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
	result.PhaseDurations[GenerationRetentionPhaseLockedRecount] += time.Since(phaseStart)
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
