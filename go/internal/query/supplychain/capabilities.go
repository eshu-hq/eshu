// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file declares the capability support rows for the hub's routes. It
// does NOT register them: registration stays in root package query
// (contract_supply_chain.go), which owns the router and always links into
// the production binary. Both root's init and this package's TestMain call
// these constructors, so production and the hub test binary exercise the
// same gate from a single declaration (packagereg/semanticsearch
// precedent, #6060).

// LightweightExactSupport is the support row for the vulnerability-scanner
// read contract: exact truth on every profile, servable from the lightest
// local profile up.
func LightweightExactSupport() querycontract.CapabilitySupport {
	exact := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &exact,
		LocalAuthoritativeMax: &exact,
		LocalFullStackMax:     &exact,
		ProductionMax:         &exact,
		RequiredProfile:       querycontract.ProfileLocalLightweight,
	}
}

// AuthoritativeExactSupport is the support row for every other hub route:
// exact truth from the authoritative local profile up. The list routes
// (SBOM attachments, impact findings and explanation, container image
// identities, security alert reconciliations) and the four aggregate routes
// share it because they all read reducer-owned Postgres truth that does not
// exist on the lightweight profile.
func AuthoritativeExactSupport() querycontract.CapabilitySupport {
	exact := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &exact,
		LocalFullStackMax:     &exact,
		ProductionMax:         &exact,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}
