// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestMain registers this family's nine owned capabilities with
// querycontract before any test runs, then runs the suite.
//
// In production these capabilities are registered by root package query's
// literal contract_capability_matrix.go rows. Root always links into the
// production binary (it owns the router), so those rows always register
// there and production is unaffected by this file.
//
// `go test ./internal/query/iac` never links root package query: this
// package cannot import it without an import cycle (root's iac_alias.go
// already imports this package for the Handler compatibility alias, #6642),
// so root's registrations never run in this test binary. Without this
// TestMain, every handler test in this package fails with the capability
// gate's unsupported_capability 501 -- not because the handler is broken,
// but because no capability was ever registered for it to check against.
//
// It registers through the Support constructors in capabilities.go -- the
// same constructors a follow-up lane should point root's rows at -- never a
// copy of their fields. See freshness/main_test.go for the identical
// rationale and precedent (#6060).
//
// The last two registrations mirror capabilities this family's routes gate
// but does not own: replatformingPlanReadinessCapability and
// replatformingSelectorInventoryCapability are registered in production by
// root's contract_replatforming.go (Part C, not editable by this move). This
// TestMain copies that row's four fields literally (not through a Support()
// constructor, since this family does not own the registration) purely so
// `go test ./internal/query/iac` can exercise handleReplatformingPlan and
// handleReplatformingSelectors without a 501 unsupported_capability -- the
// same capability-gate-needs-registration problem this file's own nine
// capabilities solve above.
//
// Do NOT delete this file as redundant: it is the only thing that makes this
// package's own tests exercise the same capability gate production does.
func TestMain(m *testing.M) {
	replatformingDerivedMax := querycontract.TruthLevelDerived
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{Capability: DeadCapability, Support: DeadSupport()},
		querycontract.CapabilityRegistration{Capability: ManagementCapability, Support: ManagementSupport()},
		querycontract.CapabilityRegistration{Capability: ManagementStatusCapability, Support: ManagementStatusSupport()},
		querycontract.CapabilityRegistration{Capability: ManagementExplainCapability, Support: ManagementExplainSupport()},
		querycontract.CapabilityRegistration{Capability: TerraformImportCapability, Support: TerraformImportSupport()},
		querycontract.CapabilityRegistration{Capability: AWSRuntimeDriftFindingsCapability, Support: AWSRuntimeDriftFindingsSupport()},
		querycontract.CapabilityRegistration{Capability: ResourcesCapability, Support: ResourcesSupport()},
		querycontract.CapabilityRegistration{Capability: ReplatformingOwnershipCapability, Support: ReplatformingOwnershipSupport()},
		querycontract.CapabilityRegistration{Capability: ReplatformingRollupsCapability, Support: ReplatformingRollupsSupport()},
		querycontract.CapabilityRegistration{Capability: ReplatformingPlanReadinessCapability, Support: querycontract.CapabilitySupport{
			LocalAuthoritativeMax: &replatformingDerivedMax,
			LocalFullStackMax:     &replatformingDerivedMax,
			ProductionMax:         &replatformingDerivedMax,
			RequiredProfile:       querycontract.ProfileLocalAuthoritative,
		}},
		querycontract.CapabilityRegistration{Capability: ReplatformingSelectorInventoryCapability, Support: querycontract.CapabilitySupport{
			LocalAuthoritativeMax: &replatformingDerivedMax,
			LocalFullStackMax:     &replatformingDerivedMax,
			ProductionMax:         &replatformingDerivedMax,
			RequiredProfile:       querycontract.ProfileLocalAuthoritative,
		}},
	)
	os.Exit(m.Run())
}
