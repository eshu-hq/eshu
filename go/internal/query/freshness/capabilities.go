// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file declares the capability support rows for this family's three
// routes. It does NOT register them: registration stays in root package
// query (contract_changed_since.go, contract_freshness.go,
// contract_service_changed_since.go), which owns the router and always links
// into the production binary. This package's TestMain calls these
// constructors so the family test binary exercises the same gate production
// does from a single declaration (supplychain/codeowners precedent, #6060).
//
// Root's three rows are still literal `init()` registrations today
// (contract_* is owned by another #6642 lane): a follow-up should point them
// at these constructors the way root's hardcoded-secret registration calls
// querycontract.HardcodedSecretSupport, so the two cannot drift. Until then,
// both spell the same contract -- exact truth at every profile, because all
// three reads are bounded local-host Postgres reads that never require the
// graph backend -- and any change to either must change the other in the
// same PR. capability_lockstep_freshness_test.go (root, package query) pins
// this equality field by field.

// ChangedSinceCapability is the capability key for the bounded changed-since
// delta summary. It diffs a prior generation's fact set against the current
// active generation's fact set in local-host Postgres (fact_records joined
// with ingestion_scopes and scope_generations) and does not require the graph
// backend, so it is exact at every profile. Root's copy is
// freshnessChangedSinceCapability (contract_changed_since.go).
const ChangedSinceCapability = "freshness.changed_since"

// GenerationLifecycleCapability is the capability key for the bounded
// generation lifecycle drilldown. It reads durable scope_generations and
// fact_work_items rows from local-host Postgres and does not require the
// graph backend, so it is exact at every profile. Root's copy is
// freshnessGenerationLifecycleCapability (contract_freshness.go).
const GenerationLifecycleCapability = "freshness.generation_lifecycle"

// ServiceChangedSinceCapability is the capability key for the bounded
// service-scope changed-since delta summary (#1943). It diffs a prior
// service materialization generation's evidence snapshot set against the
// current active generation's set in local-host Postgres
// (service_evidence_snapshots joined with
// service_materialization_generations) and does not require the graph
// backend, so it is exact at every profile. It reports the ownership
// (#1943), deployment (#1985), runtime (#1986), and dependencies (#1987)
// evidence families. Root's copy is freshnessServiceChangedSinceCapability
// (contract_service_changed_since.go).
const ServiceChangedSinceCapability = "freshness.service_changed_since"

// ChangedSinceSupport returns this family's capability contract for
// ChangedSinceCapability: exact truth at every local and production profile.
//
// The four ceilings get separate variables on purpose. Returning four
// pointers to one local would leave them aliased inside the returned struct,
// so writing through any one of them would silently move the other three.
func ChangedSinceSupport() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelExact
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

// GenerationLifecycleSupport returns this family's capability contract for
// GenerationLifecycleCapability: exact truth at every local and production
// profile. See ChangedSinceSupport for why the four ceilings use separate
// local variables.
func GenerationLifecycleSupport() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelExact
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

// ServiceChangedSinceSupport returns this family's capability contract for
// ServiceChangedSinceCapability: exact truth at every local and production
// profile. See ChangedSinceSupport for why the four ceilings use separate
// local variables.
func ServiceChangedSinceSupport() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelExact
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
