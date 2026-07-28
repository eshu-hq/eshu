// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestEvaluateSupplyChainSuppressionEnvironmentOnlyScopeHidesEveryFindingInThatEnvironment
// pins the #5466 P3-2 blast radius the owner is deciding whether to keep: a
// scope of {"environment":"prod"} ALONE is no longer suppressionScopeIsEmpty
// (Environment is one of the nine scope keys that struct checks), so every
// OTHER identity anchor -- CVEID, AdvisoryID, PackageID, PURL, RepositoryID,
// SubjectDigest, WorkloadID, ServiceID -- is a wildcard
// (scopeAnchorMatches/scopeListAnchorMatches on an empty scoped value always
// matches). The suppression therefore hides EVERY finding in "prod" across
// every ingestion scope, not just the finding(s) it was authored against.
// Before #5466 this exact payload decoded to an empty scope
// (suppressionScopeIsEmpty was true with only Environment/WorkloadID/
// ServiceID populated, since those fields did not exist) and hid nothing.
// This test intentionally does NOT narrow or widen that behavior -- it only
// proves the current matcher contract so a future change cannot silently
// alter the blast radius without this test failing first.
func TestEvaluateSupplyChainSuppressionEnvironmentOnlyScopeHidesEveryFindingInThatEnvironment(t *testing.T) {
	t.Parallel()

	envOnlySuppression := vulnerabilitySuppression{
		SuppressionID: "suppression-env-only-prod-blast-radius",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			Environment: "prod",
			// Deliberately every other scope key left unset: this is the
			// exact {"environment":"prod"} payload shape from the issue.
		},
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	// Three findings that share NOTHING with each other or with the
	// suppression's authoring intent -- different CVE, different package,
	// different repository, different subject digest, different ingestion
	// scope -- with the only common thread being Environments contains
	// "prod". All three must be hidden.
	unrelatedFindings := []SupplyChainImpactFinding{
		{CVEID: "CVE-2026-1000", PackageID: "pkg:npm/left-pad", RepositoryID: "repo://acme/frontend", SubjectDigest: "sha256:unrelated-a", Environments: []string{"prod"}},
		{CVEID: "CVE-2026-2000", PackageID: "pkg:pypi/requests", RepositoryID: "repo://acme/data-pipeline", SubjectDigest: "sha256:unrelated-b", Environments: []string{"prod"}},
		{CVEID: "CVE-2026-3000", PackageID: "pkg:golang/example.com/lib", RepositoryID: "repo://acme/gateway", SubjectDigest: "sha256:unrelated-c", Environments: []string{"prod", "stage"}},
	}
	for _, finding := range unrelatedFindings {
		decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{envOnlySuppression}, now)
		if decision.State != SupplyChainSuppressionStateNotAffected {
			t.Fatalf("finding %#v: State = %q, want %q (env-only \"prod\" suppression must hide every unrelated finding evidenced in prod -- blast radius)", finding, decision.State, SupplyChainSuppressionStateNotAffected)
		}
		if decision.SuppressionID != envOnlySuppression.SuppressionID {
			t.Fatalf("finding %#v: SuppressionID = %q, want %q", finding, decision.SuppressionID, envOnlySuppression.SuppressionID)
		}
	}

	// A finding with no "prod" evidence at all must NOT be hidden -- the
	// blast radius is bounded to the scoped environment, not unconditional.
	// suppressionAdjacent (supply_chain_suppression_scope_match.go) gates
	// entry to the evaluator's buckets on at least one anchor matching, so a
	// suppression whose only populated anchor (Environment="prod") does not
	// match this finding's Environments (["stage"]) is not adjacent at all
	// and the finding stays "active", not even "scope_mismatch".
	stageOnlyFinding := SupplyChainImpactFinding{
		CVEID: "CVE-2026-4000", PackageID: "pkg:npm/left-pad", RepositoryID: "repo://acme/frontend",
		SubjectDigest: "sha256:unrelated-d", Environments: []string{"stage"},
	}
	decision := EvaluateSupplyChainSuppression(stageOnlyFinding, []vulnerabilitySuppression{envOnlySuppression}, now)
	if decision.State != SupplyChainSuppressionStateActive {
		t.Fatalf("stage-only finding: State = %q, want %q (env-only \"prod\" suppression must not reach a finding with no prod evidence)", decision.State, SupplyChainSuppressionStateActive)
	}
}

// TestEvaluateSupplyChainSuppressionRecentEnvironmentOnlySuppressionOutranksOlderSpecificSuppression
// pins the #5466 P3-2 selection-order consequence of the blast radius above:
// pickPreferredSuppression (supply_chain_suppression.go) sorts activeMatches
// by AuthoredAt descending, with no notion of scope specificity. When a
// broad, recently-authored environment-only suppression and an older,
// narrowly-scoped (CVE+digest) suppression BOTH match the same finding, the
// broad one wins the audit trail even though the specific one is arguably
// the "more correct" explanation. This is current, intentional-per-review
// behavior -- the owner is deciding whether specificity should ever
// outrank recency -- and this test only pins it so a future change to
// pickPreferredSuppression cannot silently flip which suppression a finding
// reports without this test failing first.
func TestEvaluateSupplyChainSuppressionRecentEnvironmentOnlySuppressionOutranksOlderSpecificSuppression(t *testing.T) {
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
		RepositoryID:  "repo://acme/api",
		Environments:  []string{"prod"},
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{specificOlder, broadNewer}, now)
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q", decision.State, SupplyChainSuppressionStateNotAffected)
	}
	if decision.SuppressionID != broadNewer.SuppressionID {
		t.Fatalf("SuppressionID = %q, want %q (the more recently authored, broader env-only suppression outranks the older, specific one -- pickPreferredSuppression sorts by AuthoredAt only, not scope specificity)", decision.SuppressionID, broadNewer.SuppressionID)
	}

	// Order in the input slice must not matter: pickPreferredSuppression
	// sorts internally.
	reordered := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{broadNewer, specificOlder}, now)
	if reordered.SuppressionID != broadNewer.SuppressionID {
		t.Fatalf("input-order-reversed: SuppressionID = %q, want %q", reordered.SuppressionID, broadNewer.SuppressionID)
	}
}
