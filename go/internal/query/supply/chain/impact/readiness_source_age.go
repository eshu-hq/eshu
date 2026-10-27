// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "time"

// ageSourceStates reevaluates stored success at read time: a fresh state
// becomes stale when last_success_at is missing, unparseable, or not after the
// cutoff. The collection window is reported, never aged: it records what a
// target asked for (a static configured window or date), not when it last ran.
func ageSourceStates(states []SourceState, cutoff time.Time) []SourceState {
	for index := range states {
		state := &states[index]
		if state.FreshnessState != "fresh" {
			continue
		}
		success, err := time.Parse(time.RFC3339, state.LastSuccessAt)
		if err != nil || !success.After(cutoff) {
			state.FreshnessState = "stale"
		}
	}
	return states
}

// ageSourceSnapshots prevents a cached observation from keeping its old
// freshness label after the read window has passed.
func ageSourceSnapshots(snapshots []SourceSnapshot, cutoff time.Time) []SourceSnapshot {
	for index := range snapshots {
		snapshot := &snapshots[index]
		if snapshot.Freshness != FreshnessLabelFresh {
			continue
		}
		updated, err := time.Parse(time.RFC3339, snapshot.LastUpdatedAt)
		if err != nil || !updated.After(cutoff) {
			snapshot.Freshness = FreshnessLabelStale
		}
	}
	return snapshots
}
