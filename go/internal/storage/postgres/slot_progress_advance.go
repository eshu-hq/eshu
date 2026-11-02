// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// workflowSlotProgressAdvanceQuery moves the picked virtual slots to the
// bucket they were planned in (#9776). It is an upsert compare-and-set: a row
// moves only when the stored bucket is lower, so a coordinator whose clock is
// one bucket behind moves zero rows and can never regress a slot. RowsAffected
// counts only inserted or raised rows, which makes zero the lost-race signal.
// Binds: $1 collector instance, $2 derivation kind, $3 bucket, $4 planned-at,
// $5 virtual slots.
const workflowSlotProgressAdvanceQuery = `
INSERT INTO owned_target_slot_progress (
    collector_instance_id,
    derivation_kind,
    virtual_slot,
    last_planned_bucket,
    last_planned_at
)
SELECT $1::text, $2::text, picked.virtual_slot::integer, $3::bigint, $4::timestamptz
FROM unnest($5::bigint[]) AS picked(virtual_slot)
ON CONFLICT (collector_instance_id, derivation_kind, virtual_slot) DO UPDATE
SET last_planned_bucket = EXCLUDED.last_planned_bucket,
    last_planned_at = EXCLUDED.last_planned_at
WHERE owned_target_slot_progress.last_planned_bucket < EXCLUDED.last_planned_bucket
`

// CreateRunWithWorkItemsAdvancingSlotProgress is
// [WorkflowControlStore.CreateRunWithWorkItemsIfNoOpenTargets] plus the slot
// progress move of the owned-package derivation (#9776). The compare-and-set
// runs inside the admission transaction, after the per-instance advisory lock
// and before commit, on every path that commits: the terminal-run skip, the
// zero-eligible skip, and the insert. A rolled-back pass moves nothing. With no
// work items the call is the advance-only path of [WorkflowControlStore.AdvanceOwnedTargetSlotProgress].
// The result reports how many progress rows moved; zero for a requested move is
// a lost race, not an error.
func (s *WorkflowControlStore) CreateRunWithWorkItemsAdvancingSlotProgress(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
	advance workflow.OwnedTargetSlotAdvance,
) (workflow.RunAdmission, workflow.SlotAdvanceResult, error) {
	return s.createRunWithWorkItemsIfNoOpenTargets(ctx, run, items, &advance)
}

// AdvanceOwnedTargetSlotProgress moves the picked slots when the planner has no
// work item to admit. A virtual slot with no targets (a small instance, or a
// hash hole) yields no items, so without this call nothing advances, the same
// empty slot stays oldest at every tick, and the rotation stalls (#9776). It
// takes the same per-instance advisory lock the admission path takes, so an
// empty pick and a work-bearing pick of two coordinators serialize.
func (s *WorkflowControlStore) AdvanceOwnedTargetSlotProgress(
	ctx context.Context,
	advance workflow.OwnedTargetSlotAdvance,
) (workflow.SlotAdvanceResult, error) {
	if s.database == nil {
		return workflow.SlotAdvanceResult{}, fmt.Errorf("workflow control store database is required")
	}
	if s.beginner == nil {
		return workflow.SlotAdvanceResult{}, fmt.Errorf("workflow control store transaction support is required")
	}
	if err := validateSlotAdvance(&advance); err != nil {
		return workflow.SlotAdvanceResult{}, err
	}
	return s.advanceSlotsOnly(ctx, &advance)
}

// advanceSlotsOnly runs the advance-only transaction with the same retry
// budget as the admission path. A nil advance or one with no slots writes
// nothing and opens no transaction.
func (s *WorkflowControlStore) advanceSlotsOnly(
	ctx context.Context,
	advance *workflow.OwnedTargetSlotAdvance,
) (workflow.SlotAdvanceResult, error) {
	if advance == nil || len(advance.Slots) == 0 {
		return workflow.SlotAdvanceResult{}, nil
	}
	var lastErr error
	for attempt := 1; attempt <= workflowGuardedRunCreateMaxAttempts; attempt++ {
		result, err := s.advanceSlotsOnlyOnce(ctx, advance)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isRetryableWorkflowReconciliationError(err) || attempt >= workflowGuardedRunCreateMaxAttempts {
			break
		}
	}
	return workflow.SlotAdvanceResult{}, lastErr
}

func (s *WorkflowControlStore) advanceSlotsOnlyOnce(
	ctx context.Context,
	advance *workflow.OwnedTargetSlotAdvance,
) (workflow.SlotAdvanceResult, error) {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return workflow.SlotAdvanceResult{}, fmt.Errorf("advance owned target slot progress: begin transaction: %w", err)
	}
	rollback := tx.Rollback
	defer func() { _ = rollback() }()

	// Same key as the admission path: (collector_kind, collector_instance_id).
	key := workflowPlanningLockKey(workflow.WorkItem{
		CollectorKind:       advance.CollectorKind,
		CollectorInstanceID: advance.CollectorInstanceID,
	})
	if _, err := tx.ExecContext(ctx, workflowAdvisoryTargetLockQuery, key); err != nil {
		return workflow.SlotAdvanceResult{}, fmt.Errorf("advance owned target slot progress: lock instance: %w", err)
	}
	result, err := advanceOwnedTargetSlots(ctx, tx, advance)
	if err != nil {
		return workflow.SlotAdvanceResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return workflow.SlotAdvanceResult{}, fmt.Errorf("advance owned target slot progress: commit: %w", err)
	}
	rollback = func() error { return nil }
	return result, nil
}

// advanceOwnedTargetSlots runs the compare-and-set on the caller's transaction.
// A nil advance, or one with no slots, writes nothing.
func advanceOwnedTargetSlots(
	ctx context.Context,
	executor db.Executor,
	advance *workflow.OwnedTargetSlotAdvance,
) (workflow.SlotAdvanceResult, error) {
	if advance == nil || len(advance.Slots) == 0 {
		return workflow.SlotAdvanceResult{}, nil
	}
	slots := make([]int64, len(advance.Slots))
	for i, slot := range advance.Slots {
		slots[i] = int64(slot)
	}
	result, err := executor.ExecContext(ctx, workflowSlotProgressAdvanceQuery,
		advance.CollectorInstanceID, advance.DerivationKind, advance.Bucket, advance.PlannedAt.UTC(), slots)
	if err != nil {
		return workflow.SlotAdvanceResult{}, fmt.Errorf("advance owned target slot progress: write progress: %w", err)
	}
	moved, err := result.RowsAffected()
	if err != nil {
		return workflow.SlotAdvanceResult{}, fmt.Errorf("advance owned target slot progress: read rows moved: %w", err)
	}
	return workflow.SlotAdvanceResult{Requested: len(slots), Moved: int(moved)}, nil
}

// validateSlotAdvance rejects an advance that names no instance, kind, or
// collector, a negative bucket, or a slot outside [0, V).
func validateSlotAdvance(advance *workflow.OwnedTargetSlotAdvance) error {
	if advance == nil {
		return nil
	}
	switch {
	case strings.TrimSpace(string(advance.CollectorKind)) == "":
		return fmt.Errorf("advance owned target slot progress: collector kind is required")
	case strings.TrimSpace(advance.CollectorInstanceID) == "":
		return fmt.Errorf("advance owned target slot progress: collector instance id is required")
	case strings.TrimSpace(advance.DerivationKind) == "":
		return fmt.Errorf("advance owned target slot progress: derivation kind is required")
	case advance.Bucket < 0:
		return fmt.Errorf("advance owned target slot progress: bucket %d is negative", advance.Bucket)
	}
	seen := make(map[int]struct{}, len(advance.Slots))
	for _, slot := range advance.Slots {
		if slot < 0 || slot >= workflow.OwnedPackageVirtualSlotCount {
			return fmt.Errorf("advance owned target slot progress: virtual slot %d is outside [0, %d)",
				slot, workflow.OwnedPackageVirtualSlotCount)
		}
		// A repeated slot makes the upsert fail with "cannot affect row a
		// second time", so refuse it before any statement runs.
		if _, dup := seen[slot]; dup {
			return fmt.Errorf("advance owned target slot progress: duplicate virtual slot %d", slot)
		}
		seen[slot] = struct{}{}
	}
	return nil
}

// validateSlotAdvanceMatchesItems refuses an advance whose collector kind or
// instance differs from the admitted items. The admission transaction holds the
// advisory lock of the items' (kind, instance); progress written under another
// pair would move without the lock that serializes it.
func validateSlotAdvanceMatchesItems(advance *workflow.OwnedTargetSlotAdvance, items []workflow.WorkItem) error {
	if advance == nil {
		return nil
	}
	want := workflowPlanningLockKeyValue(workflow.WorkItem{
		CollectorKind:       advance.CollectorKind,
		CollectorInstanceID: advance.CollectorInstanceID,
	})
	for _, item := range items {
		if workflowPlanningLockKeyValue(item) != want {
			return fmt.Errorf("advance owned target slot progress: advance for %q/%q does not match work item %q of %q/%q",
				advance.CollectorKind, advance.CollectorInstanceID, item.WorkItemID, item.CollectorKind, item.CollectorInstanceID)
		}
	}
	return nil
}
