// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadinstance

import (
	"context"
	"fmt"
	"sort"
)

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
// has not materialized yet (#6785). The handler returns it only after
// committing every USES row it has (missing anchors are MATCH no-ops). It is
// enrolled in the reducer queue's nonCountingReducerRetryFailureClasses and
// bounded by the readiness-wait ledger's first-defer anchor (see Wait).
const NotReadyFailureClass = "workload_cloud_relationship_instances_not_ready"

// NotReadyError re-offers the intent, after its rows committed, until its
// missing WorkloadInstance endpoints exist or the wait settles.
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

// Decision is the existence answer for one evaluation's anchors.
type Decision struct {
	// Anchors is the sorted distinct anchor set that was checked.
	Anchors []Anchor
	// Existing holds the anchors whose WorkloadInstance exists.
	Existing map[Anchor]struct{}
	// Missing holds the anchors still without a WorkloadInstance, sorted.
	Missing []Anchor
}

// Check asks lookup which of anchors have a WorkloadInstance. It does not
// decide whether to wait; Wait does, through crossscope.DecideWait. A nil
// lookup or an empty anchor set issues no read and reports nothing missing.
func Check(ctx context.Context, lookup ExistenceLookup, anchors []Anchor) (Decision, error) {
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
