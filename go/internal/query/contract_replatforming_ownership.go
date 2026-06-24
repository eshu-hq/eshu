// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This init registers the capability support row for the replatforming
// unmanaged-resource ownership packet surface. It MUST stay in sync with the
// matrix fragment specs/capability-matrix/replatforming-ownership.v1.yaml; the
// TestCapabilityMatrixMatchesYAMLContract gate fails if the Go row and the YAML
// row diverge. Lightweight local runtime cannot materialize the reducer-owned
// drift, IaC, service, and environment evidence the ownership packet composes,
// so that profile is unsupported and the serving route returns
// unsupported_capability rather than a downgraded answer.
func init() {
	capabilityMatrix[replatformingOwnershipCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
