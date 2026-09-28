// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fencePrior holds the state generation X, the prior of the link about to be
// written, for the rest of the link transaction (arbiter ruling arb-7127-3d,
// C1). Every ledger writer must hold FOR KEY SHARE on every generation it
// names until it commits: then a row naming X commits before generation
// retention can lock X, and retention's delete sees it. Without the fence a
// link written after X was pruned names a generation that no longer exists.
//
// It runs after the activating generation's lock and before the slot, so
// the lock order is cursor, activating generation, prior, slot. It runs under
// the generation lock timeout the caller set before the first generation
// lock: 55P03 is the non-counting generation_lock_timeout. A plain existence
// read comes first:
//   - X present: lock it FOR KEY SHARE SKIP LOCKED. No row back means
//     retention holds it: a non-counting generation_locked *RetryError.
//   - X absent: retention pruned it. present is false and the caller rebases.
func fencePrior(ctx context.Context, tx db.Transaction, scopeID, priorID string) (present bool, err error) {
	_, exists, err := generationIsDelta(ctx, tx, scopeID, priorID)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	if err := lockGeneration(ctx, tx, scopeID, priorID); err != nil {
		return false, err
	}
	return true, nil
}

// rebase moves the state from the pruned generation priorID to generationID
// with RebaseLinkSQL: only changed keys are written, the link row is a root
// with an empty prior, and no link delta or bucket row is written (arbiter
// ruling arb-7127-3d, C2). The result carries the prior_pruned break.
func (w *LinkWriter) rebase(
	ctx context.Context, tx db.Transaction, scopeID, generationID, priorID string, result *LinkResult,
) error {
	var files, entities, facts, deleted, upserted, expectedDeletes, expectedUpserts, foreignDeletes int64
	if err := queryOne(ctx, tx, RebaseLinkSQL,
		[]any{scopeID, generationID, DigestVersion, w.now()},
		&result.DeltaRows, &files, &entities, &facts, &deleted, &upserted,
		&expectedDeletes, &expectedUpserts, &foreignDeletes); err != nil {
		return fmt.Errorf("changed-since link: rebase: %w", err)
	}
	// The same row-count invariant as incremental(): the ctid delete is exact
	// only under the per-scope fence.
	if deleted != expectedDeletes || upserted != expectedUpserts || foreignDeletes != 0 {
		return fmt.Errorf("changed-since link: rebase: row-count invariant broken: deleted %d of %d, upserted %d of %d, foreign deletes %d",
			deleted, expectedDeletes, upserted, expectedUpserts, foreignDeletes)
	}
	result.Kind = LinkKindRoot
	result.Break = BreakPriorPruned
	result.RebasedFrom = priorID
	result.Keys = files + entities + facts
	return nil
}
