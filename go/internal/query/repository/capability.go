// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// ContextOverviewCapability is the capability every repository
// context/story/list/tree/content handler serves its truth envelope under.
const ContextOverviewCapability = "platform_impact.context_overview"

// CatalogCapability is the capability the workload catalog route serves its
// truth envelope under.
const CatalogCapability = "platform_impact.catalog"

// ContextOverviewSupport returns this family's context-overview capability
// contract: the per-profile truth ceiling and the minimum profile the routes
// require.
//
// LocalLightweightMax is nil, which means unsupported rather than degraded:
// repository context routes require authoritative platform context truth, so
// the handlers' profile gate must be able to refuse the route outright instead
// of serving a lower-truth answer from a corpus that does not exist.
//
// This is the ONLY declaration of these values. Root package query's
// baseCapabilityMatrix registers the result for production, and this package's
// main_test.go registers it again for tests that cannot link root (#6060).
// See semanticsearch/capability.go, the template this file copies.
//
// It is a function, not an exported var, and every call allocates its own
// truth levels rather than pointing at package-level ones. CapabilitySupport
// carries its ceilings as pointers, so a shared var would hand every caller —
// including root's production registration — write access to the same three
// ints. No writer exists today; the point is that this file is a copy of the
// template every family move in #6053 follows, so the shape has to match
// before it is cloned again.
//
// The three ceilings get separate variables on purpose. Returning three
// pointers to one local would leave them aliased inside the returned struct,
// so writing through any one of them would silently move the other two.
//
// The values themselves are pinned independently by a root test
// (repository_capability_support_test.go), so editing them here without
// intending to reddens that test rather than silently changing what the
// routes serve.
func ContextOverviewSupport() querycontract.CapabilitySupport {
	localAuthoritativeMax := querycontract.TruthLevelExact
	localFullStackMax := querycontract.TruthLevelExact
	productionMax := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}

// CatalogSupport returns this family's catalog capability contract. Unlike
// the context routes, the catalog serves a derived ceiling to local
// lightweight callers instead of refusing them, and it names no required
// profile. The function-not-var and separate-ceiling-variables rules from
// ContextOverviewSupport apply unchanged.
func CatalogSupport() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelDerived
	localAuthoritativeMax := querycontract.TruthLevelExact
	localFullStackMax := querycontract.TruthLevelExact
	productionMax := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &localLightweightMax,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
	}
}
