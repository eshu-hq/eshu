// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestEvaluateSupplyChainSuppressionMatchesDeadGroupMember pins the #10408 L3
// ladder end for the re-key carry, following the non-canonical member pattern
// in scope_test.go: after a merge retires the higher id, a suppression naming
// that dead id still hides the merged finding keyed by the survivor, because
// the dead id stays in the finding's member set. A finding from another group
// with no shared member stays active under the same scope. The loader arm
// behind this (AliasIDs) is covered in factimpact; this pins the evaluation
// half.
func TestEvaluateSupplyChainSuppressionMatchesDeadGroupMember(t *testing.T) {
	t.Parallel()

	merged := SupplyChainImpactFinding{
		CVEID:                "CVE-2026-0401",
		AdvisoryID:           "CVE-2026-0401",
		AdvisoryGroupMembers: []string{"CVE-2026-0401", "CVE-2026-0402"},
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	authored := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	for _, scope := range []vulnerabilitySuppressionScope{
		{CVEID: "CVE-2026-0402"},
		{AdvisoryID: "CVE-2026-0402"},
	} {
		suppression := vulnerabilitySuppression{
			SuppressionID: "suppression-dead-member",
			Source:        chain.VulnerabilitySuppressionSourcePolicy,
			Justification: chain.VulnerabilitySuppressionJustificationNotAffected,
			AuthoredAt:    authored,
			Scope:         scope,
		}
		decision := EvaluateSupplyChainSuppression(merged, []vulnerabilitySuppression{suppression}, now)
		if decision.State != SupplyChainSuppressionStateNotAffected {
			t.Fatalf("State = %q, want %q for scope %#v naming the dead member", decision.State, SupplyChainSuppressionStateNotAffected, scope)
		}
		if decision.SuppressionID != "suppression-dead-member" {
			t.Fatalf("SuppressionID = %q, want the dead-id suppression carried", decision.SuppressionID)
		}
	}

	outsider := SupplyChainImpactFinding{
		CVEID:                "CVE-2026-0300",
		AdvisoryID:           "CVE-2026-0300",
		AdvisoryGroupMembers: []string{"CVE-2026-0300", "GHSA-0300-aaaa"},
	}
	outsiderSuppression := vulnerabilitySuppression{
		SuppressionID: "suppression-dead-member",
		Source:        chain.VulnerabilitySuppressionSourcePolicy,
		Justification: chain.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    authored,
		Scope:         vulnerabilitySuppressionScope{AdvisoryID: "CVE-2026-0402"},
	}
	if decision := EvaluateSupplyChainSuppression(outsider, []vulnerabilitySuppression{outsiderSuppression}, now); decision.State != SupplyChainSuppressionStateActive {
		t.Fatalf("State = %q, want %q for a finding outside the merged group", decision.State, SupplyChainSuppressionStateActive)
	}
}
