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
}
