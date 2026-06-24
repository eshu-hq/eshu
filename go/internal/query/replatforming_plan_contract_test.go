// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func validReplatformingPlan() ReplatformingPlan {
	plan := NewReplatformingPlan(ReplatformingPlanScope{
		Kind:    ReplatformingScopeService,
		Account: "111122223333",
		Service: "checkout",
	})
	plan.Items = []MigrationPacketItem{{
		ItemID:           "item-1",
		Provider:         "aws",
		ResourceType:     "aws_s3_bucket",
		StableID:         "arn:aws:s3:::checkout-assets",
		SourceState:      ReplatformingSourceStateDerived,
		ManagementStatus: managementStatusCloudOnly,
		SafetyGate:       IaCManagementSafetyGate{Outcome: "read_only", ReadOnly: true},
		ImportCandidate: &ReplatformingImportCandidate{
			Status:       ReplatformingImportStatusReady,
			ResourceType: "aws_s3_bucket",
			ImportBlock:  "import {\n  to = aws_s3_bucket.checkout_assets\n  id = \"checkout-assets\"\n}",
		},
	}}
	return plan
}

func TestNewReplatformingPlanPinsVersionAndNonGoals(t *testing.T) {
	plan := NewReplatformingPlan(ReplatformingPlanScope{Kind: ReplatformingScopeAccount})
	if plan.ContractVersion != ReplatformingPlanContractVersion {
		t.Fatalf("contract_version = %q, want %q", plan.ContractVersion, ReplatformingPlanContractVersion)
	}
	if len(plan.NonGoals) == 0 {
		t.Fatal("new plan must carry non-goals")
	}
}

func TestReplatformingPlanValidateAcceptsWellFormed(t *testing.T) {
	plan := validReplatformingPlan()
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestReplatformingPlanValidateRejectsContractViolations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ReplatformingPlan)
	}{
		{"bad version", func(p *ReplatformingPlan) { p.ContractVersion = "v0" }},
		{"missing non-goals", func(p *ReplatformingPlan) { p.NonGoals = nil }},
		{"missing item id", func(p *ReplatformingPlan) { p.Items[0].ItemID = "" }},
		{"missing provider", func(p *ReplatformingPlan) { p.Items[0].Provider = "" }},
		{"missing stable id", func(p *ReplatformingPlan) { p.Items[0].StableID = "" }},
		{"invalid source state", func(p *ReplatformingPlan) { p.Items[0].SourceState = "made_up" }},
		{"ready candidate without block", func(p *ReplatformingPlan) { p.Items[0].ImportCandidate.ImportBlock = "" }},
		{"refused candidate without reasons", func(p *ReplatformingPlan) {
			p.Items[0].ImportCandidate.Status = ReplatformingImportStatusRefused
			p.Items[0].ImportCandidate.ImportBlock = ""
			p.Items[0].ImportCandidate.RefusalReasons = nil
		}},
		{"refused candidate with block", func(p *ReplatformingPlan) {
			p.Items[0].ImportCandidate.Status = ReplatformingImportStatusRefused
			p.Items[0].ImportCandidate.RefusalReasons = []string{"security_review_required"}
		}},
		{"competing owners without reasons", func(p *ReplatformingPlan) {
			p.Items[0].OwnerCandidates = []ReplatformingOwnerCandidate{
				{Kind: "service", Value: "checkout"},
				{Kind: "service", Value: "billing"},
			}
		}},
		{"ambiguous item with reason-free owner", func(p *ReplatformingPlan) {
			p.Items[0].SourceState = ReplatformingSourceStateAmbiguous
			p.Items[0].OwnerCandidates = []ReplatformingOwnerCandidate{{Kind: "service", Value: "checkout"}}
		}},
		{"duplicate item id", func(p *ReplatformingPlan) {
			dup := p.Items[0]
			p.Items = append(p.Items, dup)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := validReplatformingPlan()
			tc.mutate(&plan)
			if err := plan.Validate(); err == nil {
				t.Fatalf("Validate() = nil, want error for %s", tc.name)
			}
		})
	}
}

func TestReplatformingPlanValidateAcceptsAmbiguousWithReasons(t *testing.T) {
	plan := validReplatformingPlan()
	plan.Items[0].SourceState = ReplatformingSourceStateAmbiguous
	plan.Items[0].OwnerCandidates = []ReplatformingOwnerCandidate{
		{Kind: "service", Value: "checkout", AmbiguityReasons: []string{"two_services_tag_the_arn"}},
		{Kind: "service", Value: "billing", AmbiguityReasons: []string{"two_services_tag_the_arn"}},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil for ambiguous-with-reasons", err)
	}
}

func TestReplatformingSourceStateTruthLevel(t *testing.T) {
	cases := map[ReplatformingSourceState]TruthLevel{
		ReplatformingSourceStateExact:       TruthLevelExact,
		ReplatformingSourceStateDerived:     TruthLevelDerived,
		ReplatformingSourceStatePartial:     TruthLevelDerived,
		ReplatformingSourceStateAmbiguous:   TruthLevelFallback,
		ReplatformingSourceStateStale:       TruthLevelFallback,
		ReplatformingSourceStateUnavailable: TruthLevelFallback,
		ReplatformingSourceStateUnsupported: TruthLevelFallback,
		ReplatformingSourceStateUnknown:     TruthLevelFallback,
		ReplatformingSourceStateRejected:    TruthLevelFallback,
	}
	for state, want := range cases {
		if got := state.TruthLevel(); got != want {
			t.Fatalf("%q.TruthLevel() = %q, want %q", state, got, want)
		}
	}
}

func TestReplatformingPlanRollupIsConservative(t *testing.T) {
	empty := NewReplatformingPlan(ReplatformingPlanScope{Kind: ReplatformingScopeAccount})
	if got := empty.RollupTruthLevel(); got != TruthLevelDerived {
		t.Fatalf("empty rollup = %q, want derived", got)
	}

	exact := validReplatformingPlan()
	exact.Items[0].SourceState = ReplatformingSourceStateExact
	if got := exact.RollupTruthLevel(); got != TruthLevelExact {
		t.Fatalf("all-exact rollup = %q, want exact", got)
	}

	mixed := exact
	mixed.Items = append(mixed.Items, MigrationPacketItem{
		ItemID:       "item-2",
		Provider:     "aws",
		ResourceType: "aws_lambda_function",
		StableID:     "arn:aws:lambda:us-east-1:111122223333:function:worker",
		SourceState:  ReplatformingSourceStateAmbiguous,
	})
	if got := mixed.RollupTruthLevel(); got != TruthLevelFallback {
		t.Fatalf("mixed rollup = %q, want fallback (most conservative)", got)
	}
}

func TestReplatformingPlanRollupNeverExceedsCapabilityTruth(t *testing.T) {
	// A future serving route must not claim truth above the capability's max for
	// any profile. The declared replatforming.plan.readiness capability caps at
	// derived everywhere it is supported; the contract test fails if that ever
	// rises to exact, which would let a route over-state authority.
	for _, profile := range []QueryProfile{
		ProfileLocalAuthoritative, ProfileLocalFullStack, ProfileProduction,
	} {
		max := maxTruthLevel(replatformingPlanReadinessCapability, profile)
		if max == nil {
			t.Fatalf("profile %q unexpectedly unsupported", profile)
		}
		if *max == TruthLevelExact {
			t.Fatalf("profile %q caps at exact; a route could over-state plan authority", profile)
		}

		// Even an all-exact plan must clamp to the capability max when served.
		exact := validReplatformingPlan()
		exact.Items[0].SourceState = ReplatformingSourceStateExact
		served := minTruthLevel(exact.RollupTruthLevel(), *max)
		if served == TruthLevelExact {
			t.Fatalf("served truth for profile %q reached exact past the capability cap", profile)
		}
	}
}
