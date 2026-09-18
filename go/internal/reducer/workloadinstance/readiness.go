// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadinstance

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// MaxWait bounds the WorkloadInstance defer by ELAPSED TIME since the current
// repair cycle began (crossscope.ReadinessCycleAnchor). It cannot be an attempt
// bound: NotReadyFailureClass is a non-counting readiness class, which freezes
// attempt_count, so an attempt comparison would never fire. The 30 minutes
// match the reducer's other cross-scope waits.
const MaxWait = crossscope.ProducerReadinessMaxWait

// Anchor is one (workload id, environment) pair a USES row needs a
// WorkloadInstance for.
type Anchor struct {
	WorkloadID  string
	Environment string
}

// ExistenceLookup reports which anchors already have a materialized
// WorkloadInstance the USES writer's MATCH can bind. It returns an error, never
// an empty set, when it cannot answer.
type ExistenceLookup interface {
	ExistingAnchors(ctx context.Context, anchors []Anchor) (map[Anchor]struct{}, error)
}

// NotReadyFailureClass classifies a workload-cloud USES intent deferred
// because a resource names a (workload, environment) whose WorkloadInstance
// has not materialized yet (#6785). It is enrolled in the reducer queue's
// nonCountingReducerRetryFailureClasses and bounded by MaxWait elapsed time.
const NotReadyFailureClass = "workload_cloud_relationship_instances_not_ready"

// NotReadyError defers the intent until its WorkloadInstance endpoints exist.
type NotReadyError struct {
	ScopeID      string
	GenerationID string
	Missing      int
}

// Error describes the deferral.
func (e NotReadyError) Error() string {
	return fmt.Sprintf(
		"%d workload instance anchor(s) not materialized for workload cloud relationship scope %s generation %s",
		e.Missing, e.ScopeID, e.GenerationID,
	)
}

// Retryable reports that the queue should re-offer the intent.
func (NotReadyError) Retryable() bool { return true }

// FailureClass returns the non-counting readiness class.
func (NotReadyError) FailureClass() string { return NotReadyFailureClass }

// Decision is the gate's answer for one evaluation.
type Decision struct {
	// Anchors is the sorted distinct anchor set that was checked.
	Anchors []Anchor
	// Existing holds the anchors whose WorkloadInstance exists.
	Existing map[Anchor]struct{}
	// Missing holds the anchors still without a WorkloadInstance, sorted.
	Missing []Anchor
	// Defer is true when Missing is non-empty and the bound has not expired,
	// or the cycle anchor is unknown (zero), which keeps deferring rather than
	// reading as infinitely elapsed.
	Defer bool
}

// Evaluate looks up anchors and decides whether the intent must defer.
// cycleStartedAt is crossscope.ReadinessCycleAnchor(intent); now is the
// caller's clock reading.
func Evaluate(
	ctx context.Context,
	lookup ExistenceLookup,
	anchors []Anchor,
	cycleStartedAt time.Time,
	now time.Time,
) (Decision, error) {
	distinct := sortedDistinct(anchors)
	decision := Decision{Anchors: distinct}
	if lookup == nil || len(distinct) == 0 {
		return decision, nil
	}
	existing, err := lookup.ExistingAnchors(ctx, distinct)
	if err != nil {
		return Decision{}, fmt.Errorf("check workload instance readiness: %w", err)
	}
	decision.Existing = existing
	for _, anchor := range distinct {
		if _, ok := existing[anchor]; !ok {
			decision.Missing = append(decision.Missing, anchor)
		}
	}
	if len(decision.Missing) > 0 {
		decision.Defer = cycleStartedAt.IsZero() || now.Sub(cycleStartedAt) < MaxWait
	}
	return decision, nil
}

func sortedDistinct(anchors []Anchor) []Anchor {
	seen := make(map[Anchor]struct{}, len(anchors))
	out := make([]Anchor, 0, len(anchors))
	for _, anchor := range anchors {
		if _, dup := seen[anchor]; dup {
			continue
		}
		seen[anchor] = struct{}{}
		out = append(out, anchor)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].WorkloadID != out[b].WorkloadID {
			return out[a].WorkloadID < out[b].WorkloadID
		}
		return out[a].Environment < out[b].Environment
	})
	return out
}
