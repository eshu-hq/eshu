// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"slices"
	"time"
)

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

// fetchConfirmedSources maps the evidence families whose facts a hint-skipped
// vulnerability collector leaves old to the source-state sources that confirm
// them. An unchanged upstream answer stores no new generation (#9694), so the
// active facts keep the observed_at of the last real change and fact age alone
// would call a fetched, current source stale after the freshness window.
var fetchConfirmedSources = map[string][]string{
	EvidenceFamilyVulnerabilityAdvisory:       {"osv", "nvd", "glad"},
	EvidenceFamilyVulnerabilityExploitability: {"cisa_kev", "first_epss"},
}

// sourcesFetchConfirmed reports whether every aged state for the named sources
// is a fresh success and at least one exists. States arrive aged by
// ageSourceStates, so fresh already means a success inside the window. One
// stale, partial, failed, pending or rate-limited state among them leaves the
// answer unconfirmed: a target that was not fetched is still missing evidence.
func sourcesFetchConfirmed(states []SourceState, sources []string) bool {
	confirmed := false
	for _, state := range states {
		if !slices.Contains(sources, state.Source) {
			continue
		}
		if state.FreshnessState != "fresh" || state.TerminalStatus != "succeeded" {
			return false
		}
		confirmed = true
	}
	return confirmed
}

// freshenFamiliesFromFetch labels a stale advisory or exploitability family
// fresh when the source states confirm a successful fetch inside the window.
// LatestObservedAt keeps the fact time: facts are not re-observed when the
// answer is unchanged.
func freshenFamiliesFromFetch(families map[string]EvidenceFamily, states []SourceState) {
	for family, sources := range fetchConfirmedSources {
		entry, ok := families[family]
		if !ok || entry.Freshness != FreshnessLabelStale {
			continue
		}
		if sourcesFetchConfirmed(states, sources) {
			entry.Freshness = FreshnessLabelFresh
			families[family] = entry
		}
	}
}

// freshenSnapshotsFromFetch labels a stale source snapshot fresh when every
// aged state of the snapshot's own source is a fresh success, and moves its
// last_updated_at to the newest such success. The snapshot's own time comes from
// the cache or the snapshot fact, and neither moves when an unchanged fetch is
// skipped: with a configured source cache the cache_updated_at stops moving for
// OSV, KEV and EPSS as much as for NVD (#9694 review G2). One source's states
// never freshen another source's snapshot.
func freshenSnapshotsFromFetch(snapshots []SourceSnapshot, states []SourceState) []SourceSnapshot {
	for index := range snapshots {
		snapshot := &snapshots[index]
		if snapshot.Freshness != FreshnessLabelStale || snapshot.Source == "" {
			continue
		}
		if !sourcesFetchConfirmed(states, []string{snapshot.Source}) {
			continue
		}
		snapshot.Freshness = FreshnessLabelFresh
		if newest := newestSuccessAt(states, snapshot.Source); newest != "" {
			snapshot.LastUpdatedAt = newest
		}
	}
	return snapshots
}

func newestSuccessAt(states []SourceState, source string) string {
	var newest time.Time
	for _, state := range states {
		if state.Source != source {
			continue
		}
		success, err := time.Parse(time.RFC3339, state.LastSuccessAt)
		if err == nil && success.After(newest) {
			newest = success
		}
	}
	if newest.IsZero() {
		return ""
	}
	return newest.UTC().Format(time.RFC3339)
}
