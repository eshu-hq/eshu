// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Status reasons the slot-progress path puts on its warning logs (#9776). They
// match the wording the freshness signals use, so one alert query covers both.
const (
	ownedTargetSlotAgeBreachReason     = "owned_target_slot_age_exceeds_max_staleness"
	ownedTargetProgressFallbackReason  = "owned_target_progress_clock_fallback"
	ownedTargetSlotProgressNotSupplied = "workflow coordinator store cannot advance owned target slot progress"
)

// SlotProgressStore is the durable surface the owned-package slot-progress
// path needs beyond [Store] (#9776). It is a separate interface so a store that
// predates the feature still satisfies [Store]; an instance that turns the
// slot_progress flag on against such a store fails loudly instead of silently
// planning by the clock.
type SlotProgressStore interface {
	// CreateRunWithWorkItemsAdvancingSlotProgress is the guarded admission of
	// [Store.CreateRunWithWorkItemsIfNoOpenTargets] plus the compare-and-set
	// advance of the picked slots, in the same transaction.
	CreateRunWithWorkItemsAdvancingSlotProgress(
		context.Context,
		workflow.Run,
		[]workflow.WorkItem,
		workflow.OwnedTargetSlotAdvance,
	) (workflow.RunAdmission, workflow.SlotAdvanceResult, error)
	// AdvanceOwnedTargetSlotProgress advances the picked slots when no work item
	// is admitted, under the same per-instance advisory lock.
	AdvanceOwnedTargetSlotProgress(context.Context, workflow.OwnedTargetSlotAdvance) (workflow.SlotAdvanceResult, error)
}

// workAdmission is what one admission step reports to its caller: the work
// items the store inserted, the slot-progress movement, and whether no work
// item reached the store (so the advance ran on its own).
type workAdmission struct {
	inserted int
	advance  workflow.SlotAdvanceResult
	noWork   bool
}

// ownedSlotSelector returns the selector that turns on progress-ordered slot
// selection for one in-tree vulnerability-intelligence instance, or the zero
// value (the clock slot) when the flag is off, the derivation is off, the
// instance is a bootstrap instance, or the instance plans a single pass. The
// last two never rotate: their bucket is pinned and their plan key is fixed.
func ownedSlotSelector(
	instance workflow.CollectorInstance,
	derivation vulnerability.DerivationConfiguration,
	interval time.Duration,
) workflow.OwnedTargetSlotSelector {
	if !derivation.Enabled || !derivation.SlotProgress || instance.Bootstrap ||
		schedule.NormalizePlanningMode(derivation.PlanningMode) == schedule.PlanningModeSinglePass {
		return workflow.OwnedTargetSlotSelector{}
	}
	return workflow.OwnedTargetSlotSelector{
		CollectorInstanceID: instance.InstanceID,
		DerivationKind:      workflow.SlotDerivationKindVulnerability,
		Interval:            interval,
	}
}

// ownedSlotAdvance builds the progress movement for one read, or nil when the
// read was not progress-ordered, fell back to the clock slot, or covered no
// slot in full. The clock fallback never advances: its window is not a set of
// whole virtual slots.
func ownedSlotAdvance(
	instance workflow.CollectorInstance,
	page workflow.OwnedPackageDependencyTargetPage,
	bucket int64,
	plannedAt time.Time,
) *workflow.OwnedTargetSlotAdvance {
	selection := page.Selection
	if selection.Mode == "" || selection.Mode == workflow.SlotSelectionClockFallback || len(selection.AdvanceSlots) == 0 {
		return nil
	}
	return &workflow.OwnedTargetSlotAdvance{
		CollectorKind:       instance.CollectorKind,
		CollectorInstanceID: instance.InstanceID,
		DerivationKind:      workflow.SlotDerivationKindVulnerability,
		Bucket:              bucket,
		PlannedAt:           plannedAt.UTC(),
		Slots:               selection.AdvanceSlots,
	}
}

// admitWorkflowWork admits the planned work, moving slot progress in the same
// transaction when advance is set. A store that cannot advance progress is an
// error, not a silent clock fallback.
func (s Service) admitWorkflowWork(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
	advance *workflow.OwnedTargetSlotAdvance,
) (workflow.RunAdmission, workflow.SlotAdvanceResult, error) {
	if advance == nil {
		admission, err := s.Store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
		return admission, workflow.SlotAdvanceResult{}, err
	}
	store, ok := s.Store.(SlotProgressStore)
	if !ok {
		return workflow.RunAdmission{}, workflow.SlotAdvanceResult{}, fmt.Errorf("%s", ownedTargetSlotProgressNotSupplied)
	}
	return store.CreateRunWithWorkItemsAdvancingSlotProgress(ctx, run, items, *advance)
}

// advanceSlotsWithoutWork advances the picked slots when no work item reached
// the store: an empty virtual slot, or every item denied by tenant grant. Both
// would otherwise leave the same slots oldest at every tick (#9776).
func (s Service) advanceSlotsWithoutWork(
	ctx context.Context,
	advance *workflow.OwnedTargetSlotAdvance,
) (workflow.SlotAdvanceResult, error) {
	if advance == nil {
		return workflow.SlotAdvanceResult{}, nil
	}
	store, ok := s.Store.(SlotProgressStore)
	if !ok {
		return workflow.SlotAdvanceResult{}, fmt.Errorf("%s", ownedTargetSlotProgressNotSupplied)
	}
	return store.AdvanceOwnedTargetSlotProgress(ctx, *advance)
}

// reportOwnedTargetSlots publishes the slot-progress signals of one planning
// pass (#9776): the closed outcome set on the counter, the oldest planned slot
// age on the gauge, and the warning logs an operator alerts on. A read that was
// not progress-ordered reports nothing.
func (s Service) reportOwnedTargetSlots(
	ctx context.Context,
	instance workflow.CollectorInstance,
	derivation vulnerability.DerivationConfiguration,
	page workflow.OwnedPackageDependencyTargetPage,
	advance *workflow.OwnedTargetSlotAdvance,
	admitted workAdmission,
) {
	selection := page.Selection
	if selection.Mode == "" {
		return
	}
	outcomes := []string{selection.Mode}
	if selection.Mode != workflow.SlotSelectionClockFallback && (selection.HeadTruncated || selection.Truncated || page.Window.Truncated()) {
		outcomes = append(outcomes, workflow.SlotSelectionTruncated)
	}
	// A repeat tick in the same bucket reads same_bucket_reuse and moves zero
	// rows, and the ticker runs many times per bucket. Only a fresh pick can be
	// an empty slot or lose a race; a repeat is already labelled by its mode.
	fresh := selection.Mode == workflow.SlotSelectionOldest
	if fresh && advance != nil && admitted.noWork {
		outcomes = append(outcomes, workflow.SlotSelectionEmptySlot)
	}
	if admitted.advance.Requested > 0 {
		if admitted.advance.Moved > 0 {
			outcomes = append(outcomes, workflow.SlotSelectionAdvanced)
		} else if fresh {
			outcomes = append(outcomes, workflow.SlotSelectionLostRace)
		}
	}
	ageKnown := selection.Mode != workflow.SlotSelectionClockFallback && selection.Warm()
	if s.Metrics != nil {
		s.Metrics.RecordOwnedTargetSlotSelection(ctx, OwnedTargetSlotObservation{
			CollectorInstanceID: instance.InstanceID,
			Outcomes:            outcomes,
			OldestSlotAge:       selection.OldestSlotAge,
			AgeKnown:            ageKnown,
		})
	}
	if s.Logger == nil {
		return
	}
	s.Logger.Info(
		"workflow coordinator advanced owned package slot progress",
		"collector_kind", instance.CollectorKind,
		"collector_instance_id", instance.InstanceID,
		"selection_mode", selection.Mode,
		"virtual_slots", len(selection.PickedSlots),
		"slots_requested", admitted.advance.Requested,
		"slots_advanced", admitted.advance.Moved,
		"outcomes", outcomes,
	)
	if selection.Mode == workflow.SlotSelectionClockFallback {
		s.Logger.Warn(
			"workflow coordinator owned package read kept the clock slot because the slot count exceeds the virtual slots",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"reason", ownedTargetProgressFallbackReason,
			"slot_count", page.Window.SlotCount,
			"virtual_slots", workflow.OwnedPackageVirtualSlotCount,
			"total_targets", page.Window.TotalTargets,
		)
		return
	}
	if bound := derivation.MaxStalenessBound(); ageKnown && selection.OldestSlotAge > bound {
		s.Logger.Warn(
			"workflow coordinator oldest owned package slot is older than its freshness bound",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"reason", ownedTargetSlotAgeBreachReason,
			"oldest_slot_age_seconds", selection.OldestSlotAge.Seconds(),
			"max_staleness_seconds", bound.Seconds(),
		)
	}
}
