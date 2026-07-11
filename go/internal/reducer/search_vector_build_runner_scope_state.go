// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
)

// vectorScopeStateKey joins scope_id and generation_id for a fence map key.
func vectorScopeStateKey(scopeID, generationID string) string {
	return scopeID + "\x00" + generationID
}

// finalizeVectorScopeStates checks per-scope vector completeness and
// CAS-publishes ready state for scopes that are fully built. It is called
// from both the batch fast path and the serial per-scope path.
func (r *SearchVectorBuildRunner) finalizeVectorScopeStates(
	ctx context.Context,
	scopes []SearchVectorBuildPendingScope,
	identity SearchVectorBuildIdentity,
	fences map[string]int64,
) int {
	if r.ScopeState == nil {
		return 0
	}
	finalized := 0
	for _, scope := range scopes {
		complete, err := r.ScopeState.ScopeVectorComplete(ctx, scope.ScopeID, scope.GenerationID, identity)
		if err != nil {
			slog.Warn("search vector scope complete check failed",
				"scope_id", scope.ScopeID,
				"generation_id", scope.GenerationID,
				"error", err,
			)
			continue
		}
		if !complete {
			continue
		}
		fence := fences[vectorScopeStateKey(scope.ScopeID, scope.GenerationID)]
		ok, err := r.ScopeState.FinalizeReady(ctx, scope.ScopeID, scope.GenerationID, identity, scope.ProjectionRevision, fence)
		if err != nil {
			slog.Warn("search vector scope finalize ready failed",
				"scope_id", scope.ScopeID,
				"generation_id", scope.GenerationID,
				"error", err,
			)
			continue
		}
		if !ok {
			slog.Warn("search vector scope finalize skipped",
				"scope_id", scope.ScopeID,
				"generation_id", scope.GenerationID,
				"reason", "cas_rejected",
			)
			continue
		}
		finalized++
	}
	return finalized
}
