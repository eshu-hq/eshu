// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// De-enrolled from the live-postgres-readiness plan under the drift-gate
// option A (PR #9544, steer 6530394665): the no-policy loader path stays
// proven end to end by the enrolled TestAuthorizedImpactLoaderDisengagedBuildsAdvisoryFindingLive
// handler test (which executes this same disengaged load and would fail on
// zero findings), so de-enrollment suspends only the API-level assertions
// below. Shared helpers stay in
// supply_chain_impact_authorized_loader_onboarding_live_test.go. The code
// below stays to re-enroll in the post-refit follow-up.

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestAuthorizedImpactLoaderWithoutPolicyStateLeavesPlainWriteLive boots a
// full public schema with no role-policy install (the pre-onboarding
// production shape behind the Ifa gate dead-letters) and requires the
// loader to report the static load complete but protection disengaged.
func TestAuthorizedImpactLoaderWithoutPolicyStateLeavesPlainWriteLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "onboarding_norow",
		"set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the loader onboarding proof")
	loader := NewAuthorizedSupplyChainImpactEvidenceLoader(SQLDB{DB: database})
	if loader == nil {
		t.Fatal("authorized impact loader is unavailable on a repeatable-read store")
	}
	envelopes := []facts.Envelope{{FactID: "onboarding-static-fact-1", ScopeID: "onboarding-proof-scope", GenerationID: "onboarding-proof-generation-1"}}
	loaded, err := loader.ListAuthorizedSupplyChainImpactEvidence(ctx, onboardingProofIntent(), staticOnlyExpansion(envelopes))
	if err != nil {
		t.Fatalf("loader without installed policy: %v", err)
	}
	checkPlainStaticLoad(t, loaded)
}
