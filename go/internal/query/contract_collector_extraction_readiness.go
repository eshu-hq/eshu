// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This init registers the capability support rows for the advisory collector
// extraction readiness readback. The data is static policy classification
// computed from documented repository evidence, so every profile serves the same
// exact truth and needs no graph, Postgres, or registry state. The rows MUST
// stay in sync with specs/capability-matrix/collector-extraction-readiness.v1.yaml;
// the TestCapabilityMatrixMatchesYAMLContract gate fails if the Go rows and the
// YAML rows diverge.
func init() {
	support := capabilitySupport{
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalLightweight,
	}
	capabilityMatrix[collectorExtractionReadinessListCapability] = support
	capabilityMatrix[collectorExtractionReadinessFamilyCapability] = support
}
