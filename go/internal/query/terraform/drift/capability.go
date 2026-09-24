// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drift

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Capability is the capability id POST
// /api/v0/terraform/config-state-drift/findings serves.
const Capability = "terraform_config_state_drift.findings.list"

// Support returns this route's capability contract: no local-lightweight
// support, and a derived truth ceiling on every profile that runs the
// reducer, since the findings are reducer-materialized drift facts rather
// than a direct read of config or state.
//
// This is the single declaration of the row.
// contract/capability_matrix_terraform.go registers it for production and this
// package's main_test.go registers it for tests that cannot link root (#6642),
// so there is no second copy to drift. It returns fresh truth-level pointers on
// every call, one per ceiling, so no caller can mutate another's ceiling.
func Support() querycontract.CapabilitySupport {
	localAuthoritativeMax := querycontract.TruthLevelDerived
	localFullStackMax := querycontract.TruthLevelDerived
	productionMax := querycontract.TruthLevelDerived
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}
