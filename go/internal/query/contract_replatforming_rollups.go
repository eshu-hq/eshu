// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This init registers the capability support row for the replatforming drift
// and readiness rollup surface. It MUST stay in sync with the matrix fragment
// specs/capability-matrix/replatforming-rollups.v1.yaml; the
// TestCapabilityMatrixMatchesYAMLContract gate fails if the Go row and the YAML
// row diverge. Lightweight local runtime cannot materialize the reducer-owned
// drift and IaC evidence the rollup aggregates, so that profile is unsupported
// and the serving route returns unsupported_capability.
func init() {
	capabilityMatrix[replatformingRollupsCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
