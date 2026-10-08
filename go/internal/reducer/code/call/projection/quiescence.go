// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"fmt"
	"time"
)

// ReducerGraphDrain reports whether reducer graph-writing domains are still
// active and whether active code generations have committed canonical nodes.
type ReducerGraphDrain interface {
	HasActiveReducerGraphWork(ctx context.Context) (bool, error)
	HasUncommittedCanonicalCodeScopes(ctx context.Context) (bool, error)
}

// CanonicalCodeQuiescenceChecker reports whether an active code generation is
// still missing its canonical-nodes phase without imposing a backend profile.
type CanonicalCodeQuiescenceChecker interface {
	HasUncommittedCanonicalCodeScopes(ctx context.Context) (bool, error)
}

// projectionLaneBlocked checks the profile-gated graph drain when present and
// otherwise checks canonical-code quiescence directly. Exactly one dependency
// runs per cycle. This keeps the cross-repository CALLS lane parked until every
// possible callee repository has committed canonical nodes. It returns the
// bounded reason the lane is held (BlockedReasonReducerGraphWork or
// BlockedReasonCanonicalCodeQuiescence), or an empty string when it is open.
func (r *Runner) projectionLaneBlocked(ctx context.Context) (string, error) {
	if r.ReducerGraphDrain != nil {
		active, err := r.ReducerGraphDrain.HasActiveReducerGraphWork(ctx)
		if err != nil {
			return "", fmt.Errorf("check reducer graph drain: %w", err)
		}
		if active {
			return BlockedReasonReducerGraphWork, nil
		}
		uncommitted, err := r.timedQuiescenceProbe(ctx, r.ReducerGraphDrain)
		if err != nil {
			return "", fmt.Errorf("check canonical code quiescence: %w", err)
		}
		return blockedReasonIf(uncommitted), nil
	}
	if r.CanonicalQuiescence != nil {
		uncommitted, err := r.timedQuiescenceProbe(ctx, r.CanonicalQuiescence)
		if err != nil {
			return "", fmt.Errorf("check canonical code quiescence: %w", err)
		}
		return blockedReasonIf(uncommitted), nil
	}
	return "", nil
}

// timedQuiescenceProbe consults the canonical-code gate and records the
// probe's latency (#7166) on every successful consultation, held or open.
// Cycles short-circuited by active reducer-graph work return before the
// probe and emit nothing, as do failed consultations.
func (r *Runner) timedQuiescenceProbe(ctx context.Context, checker CanonicalCodeQuiescenceChecker) (bool, error) {
	start := time.Now()
	uncommitted, err := checker.HasUncommittedCanonicalCodeScopes(ctx)
	if err == nil && r.Instruments != nil && r.Instruments.SharedProjectionLaneGateDuration != nil {
		r.Instruments.SharedProjectionLaneGateDuration.Record(
			ctx, time.Since(start).Seconds(), laneAttributes(BlockedReasonCanonicalCodeQuiescence),
		)
	}
	return uncommitted, err
}

func blockedReasonIf(uncommitted bool) string {
	if uncommitted {
		return BlockedReasonCanonicalCodeQuiescence
	}
	return ""
}
