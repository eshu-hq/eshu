// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "testing"

// TestSupplyChainImpactExpansionLimitMarkerPenaltyIsTheFlatMissingEvidenceRule
// pins the contract recorded on #11150: the active-expansion truncation marker
// carries no weight of its own. It costs 5 only because any missing evidence costs one flat
// 5 (missing_evidence_present), so it moves a finding that had no missing
// evidence and moves nothing on a finding that already misses some. The cases
// run the shipped marker function, not a copy of its arithmetic.
func TestSupplyChainImpactExpansionLimitMarkerPenaltyIsTheFlatMissingEvidenceRule(t *testing.T) {
	t.Parallel()

	base := SupplyChainImpactFinding{
		CVEID:          "CVE-2026-11150",
		Status:         SupplyChainImpactAffectedExact,
		CVSSScore:      9.8,
		SeverityVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		RepositoryID:   "repo://example/marker",
	}

	for _, tc := range []struct {
		name        string
		missing     []string
		wantPenalty int
	}{
		{name: "marker alone costs the flat 5", missing: nil, wantPenalty: 5},
		{name: "marker plus other missing evidence moves nothing", missing: []string{"installed_version"}, wantPenalty: 0},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before := base
			before.MissingEvidence = append([]string(nil), tc.missing...)
			before = withSupplyChainImpactPriority(before)
			if before.PriorityScore <= 5 {
				t.Fatalf("fixture PriorityScore = %d, want above 5 so the clamp cannot hide the penalty", before.PriorityScore)
			}

			after := markSupplyChainImpactFindingsActiveExpansionTruncated(
				[]SupplyChainImpactFinding{before},
			)[0]

			assertContainsString(t, after.MissingEvidence, supplyChainMissingActiveEvidenceExpansionLimit)
			if got := before.PriorityScore - after.PriorityScore; got != tc.wantPenalty {
				t.Fatalf("marker moved PriorityScore by %d (%d -> %d), want %d", got, before.PriorityScore, after.PriorityScore, tc.wantPenalty)
			}
			if after.Status != before.Status {
				t.Fatalf("Status = %q after marker, want unchanged %q", after.Status, before.Status)
			}
			if n := countMissingEvidenceContributions(after); n != 1 {
				t.Fatalf("missing_evidence_present contributions = %d, want exactly 1 (no truncation-specific row)", n)
			}
		})
	}
}

func countMissingEvidenceContributions(finding SupplyChainImpactFinding) int {
	count := 0
	for _, contribution := range finding.PriorityContributions {
		if contribution.ReasonCode == "missing_evidence_present" {
			count++
		}
	}
	return count
}
