// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/iac"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestIaCCapabilityLockstep pins root's nine literal
// contract_capability_matrix.go rows for the IaC/replatforming family
// (iac_quality.dead_iac, iac_management.find_unmanaged_resources,
// iac_management.get_status, iac_management.explain_status,
// iac_management.propose_terraform_import_plan, aws_runtime_drift.findings.list,
// iac_inventory.resources.list) and its two Part C init() rows
// (contract_replatforming_ownership.go, contract_replatforming_rollups.go)
// to package iac's own Support() constructors, field by field, so the two
// copies of the same contract can never drift apart silently.
//
// Root's contract_capability_matrix.go and contract_replatforming_ownership.go/
// contract_replatforming_rollups.go are owned by another #6642 lane (Part C)
// and keep a literal copy of these fields today -- iac/capabilities.go's file
// doc comment names the follow-up that should point root's rows at these
// constructors directly instead of a copy (the repository family's row
// already does this: repository.ContextOverviewCapability:
// repository.ContextOverviewSupport() in contract_capability_matrix.go).
// Until that follow-up lands, this test is what catches a one-sided edit.
func TestIaCCapabilityLockstep(t *testing.T) {
	// truthLevelPtrEqual compares two optional ceilings; both nil is equal, one
	// nil is not. It lives here because #6674 retired the shared root helper
	// together with the freshness lockstep test once root's contract rows
	// adopted the leaf constructors; this test keeps its own copy until the
	// same adoption lands for this family's rows.
	truthLevelPtrEqual := func(a, b *querycontract.TruthLevel) bool {
		switch {
		case a == nil && b == nil:
			return true
		case a == nil || b == nil:
			return false
		}
		return *a == *b
	}
	for _, tc := range []struct {
		name        string
		rootConst   string
		leafConst   string
		rootSupport capabilitySupport
		leafSupport capabilitySupport
	}{
		{
			name:        "dead_iac",
			rootConst:   "iac_quality.dead_iac",
			leafConst:   iac.DeadCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()["iac_quality.dead_iac"],
			leafSupport: iac.DeadSupport(),
		},
		{
			name:        "find_unmanaged_resources",
			rootConst:   "iac_management.find_unmanaged_resources",
			leafConst:   iac.ManagementCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()["iac_management.find_unmanaged_resources"],
			leafSupport: iac.ManagementSupport(),
		},
		{
			name:        "get_status",
			rootConst:   "iac_management.get_status",
			leafConst:   iac.ManagementStatusCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()["iac_management.get_status"],
			leafSupport: iac.ManagementStatusSupport(),
		},
		{
			name:        "explain_status",
			rootConst:   "iac_management.explain_status",
			leafConst:   iac.ManagementExplainCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()["iac_management.explain_status"],
			leafSupport: iac.ManagementExplainSupport(),
		},
		{
			name:        "propose_terraform_import_plan",
			rootConst:   "iac_management.propose_terraform_import_plan",
			leafConst:   iac.TerraformImportCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()["iac_management.propose_terraform_import_plan"],
			leafSupport: iac.TerraformImportSupport(),
		},
		{
			name:        "aws_runtime_drift_findings",
			rootConst:   "aws_runtime_drift.findings.list",
			leafConst:   iac.AWSRuntimeDriftFindingsCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()["aws_runtime_drift.findings.list"],
			leafSupport: iac.AWSRuntimeDriftFindingsSupport(),
		},
		{
			name:        "iac_resources",
			rootConst:   "iac_inventory.resources.list",
			leafConst:   iac.ResourcesCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()["iac_inventory.resources.list"],
			leafSupport: iac.ResourcesSupport(),
		},
		{
			name:        "replatforming_ownership",
			rootConst:   replatformingOwnershipCapability,
			leafConst:   iac.ReplatformingOwnershipCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()[replatformingOwnershipCapability],
			leafSupport: iac.ReplatformingOwnershipSupport(),
		},
		{
			name:        "replatforming_rollups",
			rootConst:   replatformingRollupsCapability,
			leafConst:   iac.ReplatformingRollupsCapability,
			rootSupport: querycontract.CompatibilityCapabilityMatrix()[replatformingRollupsCapability],
			leafSupport: iac.ReplatformingRollupsSupport(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.rootConst != tc.leafConst {
				t.Fatalf("capability string = %q (root) vs %q (iac)", tc.rootConst, tc.leafConst)
			}
			if !truthLevelPtrEqual(tc.rootSupport.LocalLightweightMax, tc.leafSupport.LocalLightweightMax) {
				t.Fatalf("%s: LocalLightweightMax = %v (root) vs %v (iac)", tc.name, tc.rootSupport.LocalLightweightMax, tc.leafSupport.LocalLightweightMax)
			}
			if !truthLevelPtrEqual(tc.rootSupport.LocalAuthoritativeMax, tc.leafSupport.LocalAuthoritativeMax) {
				t.Fatalf("%s: LocalAuthoritativeMax = %v (root) vs %v (iac)", tc.name, tc.rootSupport.LocalAuthoritativeMax, tc.leafSupport.LocalAuthoritativeMax)
			}
			if !truthLevelPtrEqual(tc.rootSupport.LocalFullStackMax, tc.leafSupport.LocalFullStackMax) {
				t.Fatalf("%s: LocalFullStackMax = %v (root) vs %v (iac)", tc.name, tc.rootSupport.LocalFullStackMax, tc.leafSupport.LocalFullStackMax)
			}
			if !truthLevelPtrEqual(tc.rootSupport.ProductionMax, tc.leafSupport.ProductionMax) {
				t.Fatalf("%s: ProductionMax = %v (root) vs %v (iac)", tc.name, tc.rootSupport.ProductionMax, tc.leafSupport.ProductionMax)
			}
			if tc.rootSupport.RequiredProfile != tc.leafSupport.RequiredProfile {
				t.Fatalf("%s: RequiredProfile = %q (root) vs %q (iac)", tc.name, tc.rootSupport.RequiredProfile, tc.leafSupport.RequiredProfile)
			}
		})
	}
}

// TestIaCReplatformingCapabilityMirrorsMatchRoot pins iac/capabilities.go's
// two Part C mirror constants (ReplatformingPlanReadinessCapability,
// ReplatformingSelectorInventoryCapability) equal to root's unexported
// contract_replatforming.go constants they copy by value, because a leaf
// package cannot see an unexported root identifier. capabilities.go's file
// doc comment carries the full rationale.
func TestIaCReplatformingCapabilityMirrorsMatchRoot(t *testing.T) {
	if iac.ReplatformingPlanReadinessCapability != replatformingPlanReadinessCapability {
		t.Fatalf("ReplatformingPlanReadinessCapability = %q (iac) vs %q (root)", iac.ReplatformingPlanReadinessCapability, replatformingPlanReadinessCapability)
	}
	if iac.ReplatformingSelectorInventoryCapability != replatformingSelectorInventoryCapability {
		t.Fatalf("ReplatformingSelectorInventoryCapability = %q (iac) vs %q (root)", iac.ReplatformingSelectorInventoryCapability, replatformingSelectorInventoryCapability)
	}
}
