// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// This file owns the entity family's capability rows. The rows live here —
// declared by the family that implements the resolve routes, following the
// repository/capability.go and semanticsearch/capability.go precedent — so
// the family's own test binary observes the same profile gates production
// does without importing package query (which would cycle through the root
// alias shim). The query root's matrix no longer repeats these rows: two
// copies drift silently. Registration is last-write-wins and idempotent for
// identical rows; the contract suite rejects duplicate initialization, so
// keep each capability in exactly one home. See #6060.
//
// code_search.exact_symbol and code_search.fuzzy_symbol also gate the code
// family's search routes, which have not moved yet (lane A owns the code_*
// families). Lane A takes these rows over by referencing these declarations,
// the way root references them today — not by copying the values.

// ExactSymbolCapability is the capability the entity resolve routes serve
// their exact-match truth envelope under.
const ExactSymbolCapability = "code_search.exact_symbol"

// FuzzySymbolCapability is the capability the entity resolve routes serve
// their fuzzy-match truth envelope under.
const FuzzySymbolCapability = "code_search.fuzzy_symbol"

// ExactSymbolSupport returns this family's exact-symbol capability contract.
// It is a function, not an exported var, and every call allocates its own
// truth levels rather than pointing at package-level ones. CapabilitySupport
// carries its ceilings as pointers, so a shared var would hand every caller —
// including root's production registration — write access to the same three
// ints. See repository/capability.go, the template this file copies.
func ExactSymbolSupport() querycontract.CapabilitySupport {
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

// FuzzySymbolSupport returns this family's fuzzy-symbol capability contract.
// Same allocation discipline as ExactSymbolSupport: fresh levels per call.
func FuzzySymbolSupport() querycontract.CapabilitySupport {
	localLightweightMax := querycontract.TruthLevelDerived
	localAuthoritativeMax := querycontract.TruthLevelDerived
	localFullStackMax := querycontract.TruthLevelDerived
	productionMax := querycontract.TruthLevelDerived
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &localLightweightMax,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
	}
}
