// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "time"

// ageSourceStates reevaluates stored success at read time. A historical
// collection window or an old success cannot remain fresh indefinitely.
func ageSourceStates(states []SourceState, cutoff time.Time) []SourceState {
	for index := range states {
		state := &states[index]
		if state.FreshnessState != "fresh" {
			continue
		}
		success, err := time.Parse(time.RFC3339, state.LastSuccessAt)
		if err != nil || !success.After(cutoff) {
			state.FreshnessState = "stale"
			continue
		}
		if state.CollectionWindow.End == "" {
			continue
		}
		windowEnd, err := time.Parse(time.RFC3339, state.CollectionWindow.End)
		if err != nil || !windowEnd.After(cutoff) {
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
