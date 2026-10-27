// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func TestPositiveFindingStillWinsWhenCollectionCoverageIsIncomplete(t *testing.T) {
	envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-9227"}, []FindingResult{{}}, false, ReadinessSnapshot{
		TargetIncomplete:  true,
		IncompleteReasons: []string{"nvd:committed_snapshot_missing"},
		SourceStates: []SourceState{{
			CollectorInstanceID: "instance", ScopeID: "scope", Source: "nvd",
			FreshnessState: "fresh", TerminalStatus: "succeeded",
		}},
	})
	if envelope.State != ReadinessStateReadyWithFindings {
		t.Fatalf("positive finding readiness = %q, want ready_with_findings", envelope.State)
	}
}

// TestCoveredNonzeroSnapshotDoesNotCertifyAbsence pins the #9227 F1 ruling: a
// covered NVD target certifies advisory absence only when its committed
// snapshot also reports zero results. Source-state counts are health only.
func TestCoveredNonzeroSnapshotDoesNotCertifyAbsence(t *testing.T) {
	t.Parallel()
	covered := SourceState{
		CollectorInstanceID: "instance", ScopeID: "exact", Source: "nvd",
		FreshnessState: "fresh", TerminalStatus: "succeeded", coverageEligible: true,
	}
	for _, tc := range []struct {
		name       string
		zero       bool
		advisories int
		want       ReadinessState
	}{
		{name: "zero snapshot certifies absence", zero: true, want: ReadinessStateReadyZeroFindings},
		{name: "nonzero snapshot without advisory", want: ReadinessStateEvidenceIncomplete},
		{name: "nonzero snapshot with advisory", advisories: 1, want: ReadinessStateReadyZeroFindings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := covered
			state.coverageZero = tc.zero
			snapshot := ReadinessSnapshot{SourceStates: []SourceState{state}}
			if tc.advisories > 0 {
				snapshot.EvidenceSources = []EvidenceFamily{{
					Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: tc.advisories,
					LatestObservedAt: "2026-10-27T00:00:00Z", Freshness: FreshnessLabelFresh,
				}}
			}
			envelope := BuildReadiness(TargetScope{CVEID: "CVE-2026-9227"}, nil, false, snapshot)
			if envelope.State != tc.want {
				t.Fatalf("state = %q, want %q; envelope = %+v", envelope.State, tc.want, envelope)
			}
			if tc.want == ReadinessStateEvidenceIncomplete &&
				(len(envelope.MissingEvidence) != 1 || envelope.MissingEvidence[0] != MissingEvidenceAdvisorySources) {
				t.Fatalf("missing evidence = %v, want only advisory_sources", envelope.MissingEvidence)
			}
		})
	}
}

// TestDecodeSourceStatesCarriesCoverageZero proves the snapshot-derived zero
// bit survives the SQL-to-Go boundary without leaking into the API shape.
func TestDecodeSourceStatesCarriesCoverageZero(t *testing.T) {
	t.Parallel()
	states, err := decodeSourceStates(sql.NullString{Valid: true, String: `[
		{"scope_id":"zero","source":"nvd","freshness_state":"fresh","coverage_eligible":true,"coverage_zero":true},
		{"scope_id":"known","source":"nvd","freshness_state":"fresh","coverage_eligible":true,"coverage_zero":false}]`})
	if err != nil {
		t.Fatalf("decodeSourceStates() error = %v", err)
	}
	if len(states) != 2 || !states[0].coverageZero || states[1].coverageZero {
		t.Fatalf("decoded coverage_zero = %+v, want [true false]", states)
	}
	encoded, err := json.Marshal(states[0])
	if err != nil {
		t.Fatalf("marshal source state: %v", err)
	}
	if strings.Contains(string(encoded), "coverage_zero") {
		t.Fatalf("API source state leaked internal coverage_zero: %s", encoded)
	}
}
