// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This file holds capability support entries that no longer fit in
// contract_capability_matrix.go (that file sits at the repo's 500-line cap;
// golang-engineering skill). init() appends these entries to the
// package-level capabilityMatrix var; Go completes every package-level
// variable initializer (including capabilityMatrix's map literal in
// contract_capability_matrix.go) before running any init() function, so this
// merge is safe regardless of file compilation order.
func init() {
	capabilityMatrix["reachability.java.value_flow"] = capabilitySupport{
		// Java value-flow reachability is operationally gated by
		// ESHU_EMIT_DATAFLOW (off by default), so it is unsupported in every
		// profile here, mirroring the other value-flow ecosystems (#3069).
		// The gated overlay maturity lives in specs/capability-catalog.v1.yaml.
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: nil,
		LocalFullStackMax:     nil,
		ProductionMax:         nil,
	}
	capabilityMatrix["reachability.csharp.value_flow"] = capabilitySupport{
		// C# value-flow reachability is operationally gated by
		// ESHU_EMIT_DATAFLOW (off by default), so it is unsupported in every
		// profile here, mirroring the other value-flow ecosystems (#3069).
		// The gated overlay maturity lives in specs/capability-catalog.v1.yaml.
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: nil,
		LocalFullStackMax:     nil,
		ProductionMax:         nil,
	}
	capabilityMatrix["codeowners.ownership.list"] = capabilitySupport{
		// GET /api/v0/codeowners/ownership (issue #5419 Phase 4) reads the
		// Phase 3 DECLARES_CODEOWNER graph edges plus, for effective_owner,
		// the reducer's service-catalog correlation store -- both require the
		// authoritative graph/store profile.
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
	capabilityMatrix["symbol_graph.language_entities"] = capabilitySupport{
		// POST /api/v0/code/language-query (#5761) had no capability of its
		// own before this entry. Its MCP tool, execute_language_query, is
		// already bound to five symbol_graph.* facets (decorators,
		// argument_names, class_methods, imports, inheritance), but each of
		// those names one specific semantic facet, not "look up entities of
		// kind K in language L" -- the route's actual behavior. Reusing one of
		// them would gate the whole route on a facet it may not even be
		// exercising for a given call, and code_symbol.go already owns
		// code_search.symbol_lookup for a different route
		// (POST /api/v0/code/symbols/search), so sharing it here would let one
		// id gate two unrelated routes with different failure semantics. This
		// capability names the route itself. Content-only entity kinds are
		// servable without a graph sidecar (derived truth), so
		// local_lightweight is supported; graph-backed and graph-first entity
		// kinds need the graph sidecar for exact truth.
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		// #5761 F1: local_authoritative is the first tier with a graph
		// sidecar (required_runtime: local_host_plus_graph), so it is the
		// correct requiredProfile() answer for the graph-only-entity-kind
		// 501 residue at language_queries.go. Without this, requiredProfile
		// falls through to its ProfileLocalFullStack default, which
		// overstates the tier an operator needs and contradicts this row's
		// own local_lightweight: supported entry for every other entity
		// kind.
		RequiredProfile: ProfileLocalAuthoritative,
	}
}
