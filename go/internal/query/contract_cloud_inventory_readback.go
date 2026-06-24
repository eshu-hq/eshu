// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This init registers the capability support row for the canonical multi-cloud
// resource inventory readback surface. It MUST stay in sync with the matrix
// fragment specs/capability-matrix/cloud-inventory-readback.v1.yaml; the
// TestCapabilityMatrixMatchesYAMLContract gate fails if the Go row and the YAML
// row diverge. Lightweight local runtime cannot materialize the reducer-owned
// reducer_cloud_resource_identity rows the readback lists, so that profile is
// unsupported and the serving route returns unsupported_capability rather than a
// downgraded answer.
func init() {
	capabilityMatrix[cloudInventoryReadbackCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
