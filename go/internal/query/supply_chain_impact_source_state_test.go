// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildSupplyChainImpactReadinessDistinguishesSourceStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		state      SupplyChainImpactSourceState
		wantState  SupplyChainImpactReadinessState
		wantFresh  string
		wantReason string
	}{
		{
			name: "fresh empty success",
			state: SupplyChainImpactSourceState{
				ScopeID:          "vuln-intel://nvd/CVE-2026-0001",
				Source:           "nvd",
				FreshnessState:   "fresh",
				TerminalStatus:   "succeeded",
				ResultCount:      0,
				LastSuccessAt:    "2026-05-24T19:00:00Z",
				LastAttemptAt:    "2026-05-24T19:00:00Z",
				CollectionWindow: SupplyChainImpactSourceStateWindow{Start: "2026-05-23T19:00:00Z", End: "2026-05-24T19:00:00Z"},
			},
			wantState: ReadinessStateReadyZeroFindings,
			wantFresh: "fresh",
		},
		{
			name: "pending source",
			state: SupplyChainImpactSourceState{
				ScopeID:        "vuln-intel://osv/npm/vite?version=5.4.21",
				Source:         "osv",
				Ecosystem:      "npm",
				FreshnessState: "pending",
				TerminalStatus: "pending",
				LastAttemptAt:  "2026-05-24T19:00:00Z",
			},
			wantState:  ReadinessStateTargetIncomplete,
			wantFresh:  "pending",
			wantReason: "osv:pending",
		},
		{
			name: "rate limited source",
			state: SupplyChainImpactSourceState{
				ScopeID:          "vuln-intel://nvd/modified",
				Source:           "nvd",
				FreshnessState:   "rate_limited",
				TerminalStatus:   "failed_retryable",
				LastAttemptAt:    "2026-05-24T19:00:00Z",
				NextRetryAt:      "2026-05-24T19:05:00Z",
				LastErrorClass:   "rate_limited",
				CollectionWindow: SupplyChainImpactSourceStateWindow{Start: "2026-05-23T19:00:00Z", End: "2026-05-24T19:00:00Z"},
			},
			wantState:  ReadinessStateTargetIncomplete,
			wantFresh:  "rate_limited",
			wantReason: "nvd:rate_limited",
		},
		{
			name: "partial source",
			state: SupplyChainImpactSourceState{
				ScopeID:        "vuln-intel://first/epss",
				Source:         "first_epss",
				FreshnessState: "partial",
				TerminalStatus: "partial",
				ResultCount:    1,
				WarningCount:   1,
			},
			wantState:  ReadinessStateTargetIncomplete,
			wantFresh:  "partial",
			wantReason: "first_epss:partial",
		},
		{
			name: "stale source",
			state: SupplyChainImpactSourceState{
				ScopeID:        "vuln-intel://cisa/kev",
				Source:         "cisa_kev",
				FreshnessState: "stale",
				TerminalStatus: "succeeded",
				ResultCount:    0,
				LastSuccessAt:  "2026-05-20T19:00:00Z",
				LastAttemptAt:  "2026-05-24T19:00:00Z",
			},
			wantState:  ReadinessStateTargetIncomplete,
			wantFresh:  "stale",
			wantReason: "cisa_kev:stale",
		},
		{
			name: "failed source",
			state: SupplyChainImpactSourceState{
				ScopeID:          "vuln-intel://nvd/modified",
				Source:           "nvd",
				FreshnessState:   "failed",
				TerminalStatus:   "failed_retryable",
				LastAttemptAt:    "2026-05-24T19:00:00Z",
				NextRetryAt:      "2026-05-24T19:05:00Z",
				LastErrorClass:   "retryable",
				CollectionWindow: SupplyChainImpactSourceStateWindow{Start: "2026-05-23T19:00:00Z", End: "2026-05-24T19:00:00Z"},
			},
			wantState:  ReadinessStateTargetIncomplete,
			wantFresh:  "failed",
			wantReason: "nvd:failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envelope := BuildSupplyChainImpactReadiness(
				SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
				nil,
				false,
				SupplyChainImpactReadinessSnapshot{
					SourceStates: []SupplyChainImpactSourceState{tt.state},
				},
			)
			if envelope.State != tt.wantState {
				t.Fatalf("state = %q, want %q", envelope.State, tt.wantState)
			}
			if envelope.Freshness != tt.wantFresh {
				t.Fatalf("freshness = %q, want %q", envelope.Freshness, tt.wantFresh)
			}
			if len(envelope.SourceStates) != 1 {
				t.Fatalf("source_states = %#v, want one state", envelope.SourceStates)
			}
			if tt.wantReason != "" && !readinessMissingContains(envelope.IncompleteReasons, tt.wantReason) {
				t.Fatalf("incomplete_reasons = %#v, want %q", envelope.IncompleteReasons, tt.wantReason)
			}
		})
	}
}
