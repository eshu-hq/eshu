// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

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
