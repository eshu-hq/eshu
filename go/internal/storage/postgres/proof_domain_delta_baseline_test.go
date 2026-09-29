// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "github.com/eshu-hq/eshu/go/internal/scope"

// proofDeltaBaselineFenceRows answers deltaBaselineFenceQuery (#7319) from the
// in-memory proof state: the target generation row and the scope's active
// generation, with NULL baseline and active columns where the state has none.
func proofDeltaBaselineFenceRows(state proofState, args []any) [][]any {
	scopeID, _ := args[0].(string)
	generationID, _ := args[1].(string)
	target, ok := state.generations[generationID]
	if !ok || target.ScopeID != scopeID {
		return nil
	}
	var baseline, activeID, activeCommit any
	if target.DeltaBaselineCommitSHA != "" {
		baseline = target.DeltaBaselineCommitSHA
	}
	if id := state.activeGenerations[scopeID]; id != "" {
		activeID = id
		if active, found := state.generations[id]; found && active.SourceCommitSHA != "" {
			activeCommit = active.SourceCommitSHA
		}
	}
	return [][]any{{string(target.Status), target.IsDelta, baseline, activeID, activeCommit}}
}

// proofMarkProjectionWriteStartedRows answers markProjectionWriteStartedQuery
// (#7389) from the in-memory proof state: one row when the generation is
// pending, active or failed and this attempt still owns its claimed or running
// projector work item, none otherwise. The proof state keeps no write-start
// column, so the latest-write-start update itself is not modeled.
func proofMarkProjectionWriteStartedRows(state proofState, args []any) [][]any {
	scopeID, _ := args[0].(string)
	generationID, _ := args[1].(string)
	leaseOwner, _ := args[2].(string)
	attempt, _ := args[3].(int)
	target, ok := state.generations[generationID]
	if !ok || target.ScopeID != scopeID ||
		(target.Status != scope.GenerationStatusPending && target.Status != scope.GenerationStatusActive &&
			target.Status != scope.GenerationStatusFailed) {
		return nil
	}
	for _, item := range state.workItems {
		if item.stage == "projector" && item.scopeID == scopeID && item.generationID == generationID &&
			item.leaseOwner == leaseOwner && item.attemptCount == attempt &&
			(item.status == "claimed" || item.status == "running") {
			return [][]any{{generationID}}
		}
	}
	return nil
}
