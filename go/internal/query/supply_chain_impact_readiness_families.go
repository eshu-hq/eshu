// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// supplyChainImpactReadinessFamilies orders the evidence-family identifiers
// emitted by the readiness store. Iteration order is fixed so JSON output and
// regression tests stay deterministic regardless of map walk order.
var supplyChainImpactReadinessFamilies = []string{
	EvidenceFamilyContainerImageIdentity,
	EvidenceFamilyPackageConsumption,
	EvidenceFamilyPackageRegistry,
	EvidenceFamilySBOMAttestation,
	EvidenceFamilySBOMComponent,
	EvidenceFamilyVulnerabilityAdvisory,
	EvidenceFamilyVulnerabilityExploitability,
}

var (
	vulnerabilityAdvisoryFactKinds = []string{
		"vulnerability.cve",
		"vulnerability.affected_package",
		"vulnerability.affected_product",
	}
	vulnerabilityExploitabilityFactKinds = []string{
		"vulnerability.epss_score",
		"vulnerability.known_exploited",
	}
	packageConsumptionCorrelationFactKinds = []string{
		"reducer_package_consumption_correlation",
	}
	packageRegistryFactKinds = []string{
		"package_registry.package",
		"package_registry.package_version",
	}
	sbomComponentFactKinds               = []string{"sbom.component"}
	sbomAttestationFactKinds             = []string{"reducer_sbom_attestation_attachment"}
	containerImageIdentityFactKinds      = []string{"reducer_container_image_identity"}
	vulnerabilitySourceSnapshotFactKinds = []string{"vulnerability.source_snapshot"}
)
