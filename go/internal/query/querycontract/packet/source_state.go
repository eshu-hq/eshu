// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packet

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

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

// SourceStatesSkipped reports whether any source state carries planner skip
// evidence for requested packages: the system already knows those packages
// will never be planned, so the envelope reports a skip verdict instead of
// pending. A skipped aggregate reads with freshness skipped, which no
// incompleteness check below treats as in flight.
// SourceStatesSkipped reports whether any source state carries planner skip evidence.
func SourceStatesSkipped(states []SourceState) bool {
	for _, state := range states {
		if state.SkippedPackageCount > 0 {
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

// DecodeSourceStates decodes the source-state snapshot wire payload.
// It lives here (not with the other snapshot decoders) because it sets the
// unexported coverage-certification bits the leaf owns.
func DecodeSourceStates(raw sql.NullString) ([]SourceState, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var decoded []struct {
		SourceState
		CoverageEligible bool `json:"coverage_eligible"`
		CoverageZero     bool `json:"coverage_zero"`
	}
	if err := json.Unmarshal([]byte(raw.String), &decoded); err != nil {
		return nil, fmt.Errorf("decode vulnerability source state metadata: %w", err)
	}
	states := make([]SourceState, 0, len(decoded))
	for _, entry := range decoded {
		entry.coverageEligible = entry.CoverageEligible
		entry.coverageZero = entry.CoverageZero
		states = append(states, entry.SourceState)
	}
	return states, nil
}
