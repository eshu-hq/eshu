// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This init registers the capability support row for the provider-neutral
// multi-cloud runtime drift readback surface (issues #1997, #1998). It MUST stay
// in sync with the matrix fragment
// specs/capability-matrix/cloud-runtime-drift-readback.v1.yaml; the
// TestCapabilityMatrixMatchesYAMLContract gate fails if the Go row and the YAML
// row diverge. Lightweight local runtime cannot materialize the reducer-owned
// reducer_multi_cloud_runtime_drift_finding rows the readback lists, so that
// profile is unsupported and the serving route returns unsupported_capability
// rather than a downgraded answer.
func init() {
	capabilityMatrix[cloudRuntimeDriftReadbackCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
