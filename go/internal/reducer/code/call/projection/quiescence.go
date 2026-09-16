// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"fmt"
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
// possible callee repository has committed canonical nodes.
func (r *Runner) projectionLaneBlocked(ctx context.Context) (bool, error) {
	if r.ReducerGraphDrain != nil {
		active, err := r.ReducerGraphDrain.HasActiveReducerGraphWork(ctx)
		if err != nil {
			return false, fmt.Errorf("check reducer graph drain: %w", err)
		}
		if active {
			return true, nil
		}
		uncommitted, err := r.ReducerGraphDrain.HasUncommittedCanonicalCodeScopes(ctx)
		if err != nil {
			return false, fmt.Errorf("check canonical code quiescence: %w", err)
		}
		return uncommitted, nil
	}
	if r.CanonicalQuiescence != nil {
		uncommitted, err := r.CanonicalQuiescence.HasUncommittedCanonicalCodeScopes(ctx)
		if err != nil {
			return false, fmt.Errorf("check canonical code quiescence: %w", err)
		}
		return uncommitted, nil
	}
	return false, nil
}
