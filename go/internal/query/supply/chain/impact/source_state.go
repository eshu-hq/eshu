// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"sort"
	"strings"
)

// SourceState exposes durable vulnerability source
// checkpoint, retry, and freshness state without raw advisory payloads.
type SourceState struct {
	CollectorInstanceID string            `json:"collector_instance_id"`
	ScopeID             string            `json:"scope_id"`
	Source              string            `json:"source"`
	Ecosystem           string            `json:"ecosystem,omitempty"`
	CollectionWindow    SourceStateWindow `json:"collection_window,omitempty"`
	LastAttemptAt       string            `json:"last_attempt_at,omitempty"`
	LastSuccessAt       string            `json:"last_success_at,omitempty"`
	NextRetryAt         string            `json:"next_retry_at,omitempty"`
	LastErrorClass      string            `json:"last_error_class,omitempty"`
	FreshnessState      string            `json:"freshness_state"`
	TerminalStatus      string            `json:"terminal_status"`
	ResultCount         int               `json:"result_count"`
	WarningCount        int               `json:"warning_count"`
	UpdatedAt           string            `json:"updated_at,omitempty"`
	// Derived marks one aggregate row standing for every derived
	// owned-package OSV scope that covers the request's packages in one
	// (collector instance, source, ecosystem). Its scope_id is a
	// derived:// name, not a collector scope. The fields below are set only
	// on derived rows. FreshnessState is the worst aged state of the kept
	// scopes, read as pending when any requested package has no scope.
	Derived bool `json:"derived,omitempty"`
	// ScopeCount is the number of distinct derived scopes kept: the newest
	// scope per requested package, then deduplicated.
	ScopeCount int `json:"scope_count,omitempty"`
	// PackageCount is the number of requested packages a scope covered.
	PackageCount int `json:"package_count,omitempty"`
	// UnmatchedPackageCount is the number of requested packages no derived
	// scope covered. It includes packages the planner skipped and scopes
	// written before package keys were recorded.
	UnmatchedPackageCount int `json:"unmatched_package_count,omitempty"`
	// FreshCount through PendingCount count the kept scopes by aged state.
	FreshCount       int `json:"fresh_count,omitempty"`
	StaleCount       int `json:"stale_count,omitempty"`
	FailedCount      int `json:"failed_count,omitempty"`
	RateLimitedCount int `json:"rate_limited_count,omitempty"`
	PartialCount     int `json:"partial_count,omitempty"`
	PendingCount     int `json:"pending_count,omitempty"`
	// ScopeIDs lists up to 10 kept scope ids, worst state first and oldest
	// success first within a state.
	ScopeIDs []string `json:"scope_ids,omitempty"`
	// MultiCVETarget marks a configured NVD target that lists several CVEs:
	// absence stays per target, so the target certifies none of its CVEs
	// absent once NVD returned any record for it (#9322).
	MultiCVETarget   bool `json:"multi_cve_target,omitempty"`
	coverageEligible bool
	coverageZero     bool
}

// SourceStateWindow is the bounded source collection window.
type SourceStateWindow struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func normalizeSourceStates(states []SourceState) []SourceState {
	if len(states) == 0 {
		return nil
	}
	out := make([]SourceState, 0, len(states))
	seen := map[string]struct{}{}
	for _, state := range states {
		state.CollectorInstanceID = strings.TrimSpace(state.CollectorInstanceID)
		state.ScopeID = strings.TrimSpace(state.ScopeID)
		state.Source = strings.TrimSpace(state.Source)
		state.Ecosystem = strings.TrimSpace(state.Ecosystem)
		state.FreshnessState = strings.TrimSpace(state.FreshnessState)
		state.TerminalStatus = strings.TrimSpace(state.TerminalStatus)
		if state.Source == "" || state.ScopeID == "" || state.FreshnessState == "" {
			continue
		}
		key := state.CollectorInstanceID + "\x00" + state.ScopeID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, state)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		if out[i].ScopeID != out[j].ScopeID {
			return out[i].ScopeID < out[j].ScopeID
		}
		return out[i].CollectorInstanceID < out[j].CollectorInstanceID
	})
	return out
}

func sourceSnapshotsIncomplete(snapshots []SourceSnapshot) bool {
	for _, snapshot := range snapshots {
		if !snapshot.Complete || snapshot.Freshness == FreshnessLabelStale {
			return true
		}
	}
	return false
}

func sourceStatesIncomplete(states []SourceState) bool {
	for _, state := range states {
		switch state.FreshnessState {
		case "pending", "stale", "rate_limited", "failed", "partial":
			return true
		}
	}
	return false
}

func sourceStateIncompleteReasons(states []SourceState) []string {
	reasons := make([]string, 0, len(states))
	for _, state := range states {
		switch state.FreshnessState {
		case "pending", "stale", "rate_limited", "failed", "partial":
			reasons = append(reasons, state.Source+":"+state.FreshnessState)
		}
	}
	return reasons
}

// sourceStatesHaveCoveredSuccess reports whether committed NVD coverage
// certifies advisory absence: every covered exact-CVE NVD state must be fresh,
// succeeded, warning-free, and backed by a committed zero-result snapshot
// (coverageZero). The state row's own counts stay as extra health gates; the
// certifying bit comes from the snapshot, never from source-state health.
func sourceStatesHaveCoveredSuccess(states []SourceState) bool {
	covered := false
	for _, state := range states {
		if !state.coverageEligible || state.Source != "nvd" {
			continue
		}
		if state.FreshnessState != "fresh" || state.TerminalStatus != "succeeded" ||
			!state.coverageZero || state.ResultCount != 0 || state.WarningCount != 0 {
			return false
		}
		covered = true
	}
	return covered
}

func sourceStatesHaveFreshSuccess(states []SourceState) bool {
	for _, state := range states {
		if state.FreshnessState == "fresh" && state.TerminalStatus == "succeeded" {
			return true
		}
	}
	return false
}

func aggregateSourceStateFreshness(states []SourceState) string {
	for _, priority := range []string{"rate_limited", "failed", "partial", "pending", "stale"} {
		for _, state := range states {
			if state.FreshnessState == priority {
				return priority
			}
		}
	}
	if sourceStatesHaveFreshSuccess(states) {
		return "fresh"
	}
	return FreshnessLabelUnknown
}

func combineReadinessFreshness(evidence string, sourceState string) string {
	if freshnessRank(sourceState) > freshnessRank(evidence) {
		return sourceState
	}
	return evidence
}

func freshnessRank(value string) int {
	switch value {
	case "rate_limited":
		return 7
	case "failed":
		return 6
	case "partial":
		return 5
	case "pending":
		return 4
	case FreshnessLabelStale:
		return 3
	case FreshnessLabelFresh:
		return 2
	default:
		return 1
	}
}
