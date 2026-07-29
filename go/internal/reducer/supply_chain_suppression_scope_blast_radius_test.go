// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestEvaluateSupplyChainSuppressionEnvironmentOnlyScopeFailsClosed proves
// deployment context narrows a discoverable vulnerability identity; it cannot
// be the identity by itself. A direct caller that constructs an invalid
// environment-only scope must retain the finding and surface a mismatch.
func TestEvaluateSupplyChainSuppressionEnvironmentOnlyScopeFailsClosed(t *testing.T) {
	t.Parallel()

	environmentOnly := vulnerabilitySuppression{
		SuppressionID: "suppression-environment-only",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			Environment: "prod",
		},
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	unrelatedFindings := []SupplyChainImpactFinding{
		{CVEID: "CVE-2026-1000", PackageID: "pkg:npm/example-a", RepositoryID: "repo://example/frontend", SubjectDigest: "sha256:placeholder-a", Environments: []string{"prod"}},
		{CVEID: "CVE-2026-2000", PackageID: "pkg:pypi/example-b", RepositoryID: "repo://example/data", SubjectDigest: "sha256:placeholder-b", Environments: []string{"prod"}},
		{CVEID: "CVE-2026-3000", PackageID: "pkg:golang/example.com/lib", RepositoryID: "repo://example/gateway", SubjectDigest: "sha256:placeholder-c", Environments: []string{"prod", "stage"}},
	}
	for _, finding := range unrelatedFindings {
		decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{environmentOnly}, now)
		if decision.State != SupplyChainSuppressionStateScopeMismatch {
			t.Fatalf("finding %#v: State = %q, want %q", finding, decision.State, SupplyChainSuppressionStateScopeMismatch)
		}
		if decision.SuppressionID != environmentOnly.SuppressionID {
			t.Fatalf("finding %#v: SuppressionID = %q, want %q", finding, decision.SuppressionID, environmentOnly.SuppressionID)
		}
	}
}

// TestEvaluateSupplyChainSuppressionSpecificScopeOutranksInvalidEnvironmentOnly
// proves invalid deployment-only input cannot displace a valid, anchored
// suppression even when the invalid record is newer.
func TestEvaluateSupplyChainSuppressionSpecificScopeOutranksInvalidEnvironmentOnly(t *testing.T) {
	t.Parallel()

	specificOlder := vulnerabilitySuppression{
		SuppressionID: "suppression-specific-older",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0900",
			SubjectDigest: "sha256:specific-digest",
			// Environment deliberately unset: a wildcard for environment,
			// so this suppression matches the finding below purely on
			// CVE+digest, independent of the recency race this test proves.
		},
	}
	broadNewer := vulnerabilitySuppression{
		SuppressionID: "suppression-env-only-newer",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			Environment: "prod",
		},
	}
	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0900",
		SubjectDigest: "sha256:specific-digest",
		RepositoryID:  "repo://example/api",
		Environments:  []string{"prod"},
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{specificOlder, broadNewer}, now)
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateNotAffected)
	}
	if decision.SuppressionID != specificOlder.SuppressionID {
		t.Fatalf("SuppressionID = %q, want %q", decision.SuppressionID, specificOlder.SuppressionID)
	}

	reordered := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{broadNewer, specificOlder}, now)
	if reordered.SuppressionID != specificOlder.SuppressionID {
		t.Fatalf("input-order-reversed: SuppressionID = %q, want %q", reordered.SuppressionID, specificOlder.SuppressionID)
	}
}
