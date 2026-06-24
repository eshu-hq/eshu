// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// Per-ecosystem reachability capabilities (issue #3021). These mirror the
// specs/capability-matrix/reachability.v1.yaml rows so the runtime conformance
// map stays in lockstep with the YAML matrix (TestCapabilityMatrixMatchesYAMLContract).
//
// Reachability is surfaced as the reachability envelope on supply-chain impact
// findings, not as a dedicated handler, so truth ceilings are derived
// (computed/bounded), never exact. The value_flow ecosystems are unsupported in
// every profile because they are gated by ESHU_EMIT_DATAFLOW (off by default);
// the overlay records their gated maturity. JVM reachability is bounded and
// local graph-backed only, so its production profile is unsupported.
func init() {
	capabilityMatrix["reachability.go.govulncheck"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
	capabilityMatrix["reachability.python.value_flow"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: nil,
		LocalFullStackMax:     nil,
		ProductionMax:         nil,
	}
	capabilityMatrix["reachability.typescript.value_flow"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: nil,
		LocalFullStackMax:     nil,
		ProductionMax:         nil,
	}
	capabilityMatrix["reachability.javascript.value_flow"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: nil,
		LocalFullStackMax:     nil,
		ProductionMax:         nil,
	}
	capabilityMatrix["reachability.jvm.bounded"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         nil,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
