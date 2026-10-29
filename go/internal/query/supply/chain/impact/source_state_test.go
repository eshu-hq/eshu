// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"
	"time"
)

func TestBuildSupplyChainImpactReadinessDistinguishesSourceStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		state      SourceState
		wantState  ReadinessState
		wantFresh  string
		wantReason string
	}{
		{
			name: "fresh empty success",
			state: SourceState{
				ScopeID:          "vuln-intel://nvd/CVE-2026-0001",
				Source:           "nvd",
				FreshnessState:   "fresh",
				TerminalStatus:   "succeeded",
				ResultCount:      0,
				LastSuccessAt:    "2026-05-24T19:00:00Z",
				LastAttemptAt:    "2026-05-24T19:00:00Z",
				CollectionWindow: SourceStateWindow{Start: "2026-05-23T19:00:00Z", End: "2026-05-24T19:00:00Z"},
				coverageEligible: true,
				coverageZero:     true,
			},
			wantState: ReadinessStateReadyZeroFindings,
			wantFresh: "fresh",
		},
		{
			name: "pending source",
			state: SourceState{
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
			state: SourceState{
				ScopeID:          "vuln-intel://nvd/modified",
				Source:           "nvd",
				FreshnessState:   "rate_limited",
				TerminalStatus:   "failed_retryable",
				LastAttemptAt:    "2026-05-24T19:00:00Z",
				NextRetryAt:      "2026-05-24T19:05:00Z",
				LastErrorClass:   "rate_limited",
				CollectionWindow: SourceStateWindow{Start: "2026-05-23T19:00:00Z", End: "2026-05-24T19:00:00Z"},
			},
			wantState:  ReadinessStateTargetIncomplete,
			wantFresh:  "rate_limited",
			wantReason: "nvd:rate_limited",
		},
		{
			name: "partial source",
			state: SourceState{
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
			state: SourceState{
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
			state: SourceState{
				ScopeID:          "vuln-intel://nvd/modified",
				Source:           "nvd",
				FreshnessState:   "failed",
				TerminalStatus:   "failed_retryable",
				LastAttemptAt:    "2026-05-24T19:00:00Z",
				NextRetryAt:      "2026-05-24T19:05:00Z",
				LastErrorClass:   "retryable",
				CollectionWindow: SourceStateWindow{Start: "2026-05-23T19:00:00Z", End: "2026-05-24T19:00:00Z"},
			},
			wantState:  ReadinessStateTargetIncomplete,
			wantFresh:  "failed",
			wantReason: "nvd:failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envelope := BuildReadiness(
				TargetScope{CVEID: "CVE-2026-0001"},
				nil,
				false,
				ReadinessSnapshot{
					SourceStates: []SourceState{tt.state},
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
			if tt.wantReason != "" && !ReadinessMissingContains(envelope.IncompleteReasons, tt.wantReason) {
				t.Fatalf("incomplete_reasons = %#v, want %q", envelope.IncompleteReasons, tt.wantReason)
			}
		})
	}
}

func TestRelevantSourcePendingWithAdvisoryFactsStaysIncomplete(t *testing.T) {
	t.Parallel()
	envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-0001"}, nil, false, ReadinessSnapshot{
		EvidenceSources: []EvidenceFamily{{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh}},
		SourceStates:    []SourceState{{CollectorInstanceID: "instance", ScopeID: "matching", Source: "nvd", FreshnessState: "pending", TerminalStatus: "pending"}},
	})
	if envelope.State != ReadinessStateTargetIncomplete {
		t.Fatalf("state = %q, want target_incomplete for relevant pending source", envelope.State)
	}
}

func TestRelevantIncompleteSnapshotWithAdvisoryFactsStaysIncomplete(t *testing.T) {
	t.Parallel()
	envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-0001"}, nil, false, ReadinessSnapshot{
		EvidenceSources:  []EvidenceFamily{{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh}},
		SourceSnapshots:  []SourceSnapshot{{Source: "nvd", Complete: false}},
		TargetIncomplete: true,
	})
	if envelope.State != ReadinessStateTargetIncomplete {
		t.Fatalf("state = %q, want target_incomplete for relevant incomplete snapshot", envelope.State)
	}
}

func TestStaleSnapshotWithAdvisoryFactsStaysIncomplete(t *testing.T) {
	t.Parallel()
	envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-0001"}, nil, false, ReadinessSnapshot{
		EvidenceSources: []EvidenceFamily{{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh}},
		SourceSnapshots: []SourceSnapshot{{Source: "nvd", Complete: true, Freshness: FreshnessLabelStale}},
	})
	if envelope.State != ReadinessStateTargetIncomplete {
		t.Fatalf("state = %q, want target_incomplete for stale source snapshot", envelope.State)
	}
}

func TestExactSourceResultWithoutAdvisoryFactDoesNotCertifyZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		results  int
		warnings int
	}{
		{name: "reported advisory", results: 1},
		{name: "warning", warnings: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-9227"}, nil, false, ReadinessSnapshot{
				SourceStates: []SourceState{{
					CollectorInstanceID: "instance", ScopeID: "exact", Source: "nvd",
					FreshnessState: "fresh", TerminalStatus: "succeeded", ResultCount: tc.results,
					WarningCount: tc.warnings, coverageEligible: true,
				}},
			})
			if envelope.State == ReadinessStateReadyZeroFindings {
				t.Fatalf("reported results=%d warnings=%d certified clean zero", tc.results, tc.warnings)
			}
		})
	}
}

func TestMixedExactSourceResultsDoNotCertifyZero(t *testing.T) {
	t.Parallel()
	envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-9227"}, nil, false, ReadinessSnapshot{
		SourceStates: []SourceState{
			{CollectorInstanceID: "first", ScopeID: "clean", Source: "nvd", FreshnessState: "fresh", TerminalStatus: "succeeded", coverageEligible: true},
			{CollectorInstanceID: "second", ScopeID: "reported", Source: "nvd", FreshnessState: "fresh", TerminalStatus: "succeeded", ResultCount: 1, coverageEligible: true},
		},
	})
	if envelope.State == ReadinessStateReadyZeroFindings {
		t.Fatal("one clean exact target hid another exact target with a reported advisory")
	}
}

func TestSourceHealthDoesNotCertifyAdvisoryAbsence(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"nvd", "cisa_kev", "first_epss", "osv", "glad"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-9227"}, nil, false,
				ReadinessSnapshot{SourceStates: []SourceState{{
					CollectorInstanceID: "instance", ScopeID: "arbitrary-scope", Source: source,
					FreshnessState: "fresh", TerminalStatus: "succeeded",
				}}})
			if envelope.State == ReadinessStateReadyZeroFindings {
				t.Fatalf("%s source health certified advisory absence without exact CVE coverage", source)
			}
		})
	}
}

func TestAgeSourceStatesAtReadTime(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		state SourceState
		want  string
	}{
		{"recent exact", SourceState{FreshnessState: "fresh", LastSuccessAt: "2026-10-20T00:00:00Z"}, "fresh"},
		{"old success", SourceState{FreshnessState: "fresh", LastSuccessAt: "2026-09-20T00:00:00Z"}, "stale"},
		// The collection window says what was asked for, not when it was
		// fetched; only success time ages a source (#9227 F2 ruling).
		{"old window", SourceState{FreshnessState: "fresh", LastSuccessAt: "2026-10-20T00:00:00Z", CollectionWindow: SourceStateWindow{End: "2026-09-20T00:00:00Z"}}, "fresh"},
		{"old window, old success", SourceState{FreshnessState: "fresh", LastSuccessAt: "2026-09-20T00:00:00Z", CollectionWindow: SourceStateWindow{End: "2026-09-20T00:00:00Z"}}, "stale"},
		{"unparseable success", SourceState{FreshnessState: "fresh", LastSuccessAt: "yesterday"}, "stale"},
		{"missing success", SourceState{FreshnessState: "fresh"}, "stale"},
		{"failure precedence", SourceState{FreshnessState: "failed", LastSuccessAt: "2026-09-20T00:00:00Z"}, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ageSourceStates([]SourceState{tc.state}, cutoff)
			if len(got) != 1 || got[0].FreshnessState != tc.want {
				t.Fatalf("ageSourceStates() = %+v, want %s", got, tc.want)
			}
		})
	}
}

func TestAgeSourceSnapshotsAtReadTime(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got := ageSourceSnapshots([]SourceSnapshot{
		{Source: "nvd", Freshness: "fresh", LastUpdatedAt: "2026-10-20T00:00:00Z"},
		{Source: "first_epss", Freshness: "fresh", LastUpdatedAt: "2026-09-20T00:00:00Z"},
		{Source: "glad", Freshness: "fresh"},
	}, cutoff)
	if got[0].Freshness != FreshnessLabelFresh ||
		got[1].Freshness != FreshnessLabelStale ||
		got[2].Freshness != FreshnessLabelStale {
		t.Fatalf("ageSourceSnapshots() = %+v", got)
	}
}
