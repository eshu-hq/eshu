// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

// SupplyChainImpactSourceState exposes durable vulnerability source
// checkpoint, retry, and freshness state without raw advisory payloads.
type SupplyChainImpactSourceState struct {
	CollectorInstanceID string                             `json:"collector_instance_id"`
	ScopeID             string                             `json:"scope_id"`
	Source              string                             `json:"source"`
	Ecosystem           string                             `json:"ecosystem,omitempty"`
	CollectionWindow    SupplyChainImpactSourceStateWindow `json:"collection_window,omitempty"`
	LastAttemptAt       string                             `json:"last_attempt_at,omitempty"`
	LastSuccessAt       string                             `json:"last_success_at,omitempty"`
	NextRetryAt         string                             `json:"next_retry_at,omitempty"`
	LastErrorClass      string                             `json:"last_error_class,omitempty"`
	FreshnessState      string                             `json:"freshness_state"`
	TerminalStatus      string                             `json:"terminal_status"`
	ResultCount         int                                `json:"result_count"`
	WarningCount        int                                `json:"warning_count"`
	UpdatedAt           string                             `json:"updated_at,omitempty"`
}

// SupplyChainImpactSourceStateWindow is the bounded source collection window.
type SupplyChainImpactSourceStateWindow struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func normalizeSourceStates(states []SupplyChainImpactSourceState) []SupplyChainImpactSourceState {
	if len(states) == 0 {
		return nil
	}
	out := make([]SupplyChainImpactSourceState, 0, len(states))
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
		return out[i].ScopeID < out[j].ScopeID
	})
	return out
}

func sourceStatesIncomplete(states []SupplyChainImpactSourceState) bool {
	for _, state := range states {
		switch state.FreshnessState {
		case "pending", "stale", "rate_limited", "failed", "partial":
			return true
		}
	}
	return false
}

func sourceStateIncompleteReasons(states []SupplyChainImpactSourceState) []string {
	reasons := make([]string, 0, len(states))
	for _, state := range states {
		switch state.FreshnessState {
		case "pending", "stale", "rate_limited", "failed", "partial":
			reasons = append(reasons, state.Source+":"+state.FreshnessState)
		}
	}
	return reasons
}

func sourceStatesHaveFreshSuccess(states []SupplyChainImpactSourceState) bool {
	for _, state := range states {
		if state.FreshnessState == "fresh" && state.TerminalStatus == "succeeded" {
			return true
		}
	}
	return false
}

func aggregateSourceStateFreshness(states []SupplyChainImpactSourceState) string {
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
