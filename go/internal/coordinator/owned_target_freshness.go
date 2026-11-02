// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ownedTargetRotationBreachReason names the breach on the warning log and
// matches the status reason wording the freshness issues use (#9775).
const ownedTargetRotationBreachReason = "owned_target_rotation_exceeds_max_staleness"

// reportOwnedTargetFreshness derives the owned-package rotation period from the
// window one read returned, publishes it on the freshness gauges, and warns
// when it is longer than the freshness bound (#9775). It returns the value so
// the caller can put the same two numbers on the per-read window log line.
//
// The period is P x interval, with P taken from the read itself, so nothing is
// persisted and a coordinator restart changes nothing. A disabled derivation, a
// single_pass instance (it plans slot 0 under a stable key and never rotates),
// and a read that returned no window have no rotation to report: the gauges
// drop the instance's series and the returned value is not Known.
func (s Service) reportOwnedTargetFreshness(
	ctx context.Context,
	instance workflow.CollectorInstance,
	derivation vulnerability.DerivationConfiguration,
	page workflow.OwnedPackageDependencyTargetPage,
	interval time.Duration,
) schedule.OwnedTargetFreshness {
	var freshness schedule.OwnedTargetFreshness
	if derivation.Enabled && schedule.NormalizePlanningMode(derivation.PlanningMode) != schedule.PlanningModeSinglePass {
		freshness = schedule.OwnedTargetRotationFreshness(page.Window.SlotCount, interval, derivation.MaxStalenessBound())
	}
	if s.Metrics != nil {
		s.Metrics.RecordOwnedTargetRotation(ctx, OwnedTargetRotationObservation{
			CollectorInstanceID: instance.InstanceID,
			Freshness:           freshness,
		})
	}
	if s.Logger != nil && freshness.Exceeds() {
		s.Logger.Warn(
			"workflow coordinator owned package rotation is longer than its freshness bound",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"reason", ownedTargetRotationBreachReason,
			"slot_count", freshness.SlotCount,
			"interval_seconds", freshness.Interval.Seconds(),
			"rotation_period_seconds", freshness.Period.Seconds(),
			"max_staleness_seconds", freshness.MaxStaleness.Seconds(),
			"total_targets", page.Window.TotalTargets,
		)
	}
	return freshness
}
