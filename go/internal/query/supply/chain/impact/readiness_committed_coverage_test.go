// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "testing"

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
