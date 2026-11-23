// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"
	"testing"
)

// TestPreciseImpliesStatusAndObservedVersion pins the writer invariant the route V
// status prune relies on (#10880 PR-B2a, arbiter ruling): a finding classified
// precise has status affected_exact or not_affected_known_fixed and a non-empty
// observed version. Both are parts of the SQL canonical key, so every support of a
// key shares them with its winner, and a winner whose status or observed version
// rules precise out can be skipped when the supplement looks for a precise support.
// The read prunes with `impact_status IN (...) AND observed_version <> ”`; this
// test fails if the classifier ever calls a finding precise outside that set.
func TestPreciseImpliesStatusAndObservedVersion(t *testing.T) {
	t.Parallel()
	statuses := []SupplyChainImpactStatus{
		SupplyChainImpactAffectedExact, SupplyChainImpactAffectedDerived, SupplyChainImpactPossiblyAffected,
		SupplyChainImpactNotAffectedKnownFixed, "", "not_affected", "unknown_status",
	}
	versions := []string{"", " ", "\t", "1.0.0", " 2.1 ", "3.0.11-1"}
	reasons := []string{
		"", "unknown_reason", "dpkg_affected_range",
		supplyChainVersionReasonNPMSemverAffectedRange, supplyChainVersionReasonNPMSemverKnownFixed,
		supplyChainVersionReasonMavenRangeMatch, supplyChainVersionReasonDPKGExactAffected,
		supplyChainVersionReasonRPMExactAffected, supplyChainVersionReasonAPKExactKnownFixed,
		supplyChainVersionReasonRubyGemsAffectedRange, supplyChainVersionReasonSwiftSemverKnownFixed,
	}
	precise := 0
	for _, status := range statuses {
		for _, version := range versions {
			for _, reason := range reasons {
				finding := SupplyChainImpactFinding{Status: status, ObservedVersion: version, MatchReason: reason}
				if classifySupplyChainImpactDetectionProfile(finding) != DetectionProfilePrecise {
					continue
				}
				precise++
				if status != SupplyChainImpactAffectedExact && status != SupplyChainImpactNotAffectedKnownFixed {
					t.Fatalf("status %q classified precise: the route V status prune would skip a precise support", status)
				}
				if strings.TrimSpace(version) == "" || version == "" {
					t.Fatalf("observed version %q classified precise: the prune requires observed_version <> ''", version)
				}
			}
		}
	}
	if precise == 0 {
		t.Fatal("no combination classified precise: the invariant test lost its signal")
	}
}
