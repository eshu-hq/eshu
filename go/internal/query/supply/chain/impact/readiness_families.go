// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// supplyChainImpactReadinessFamilies orders the evidence-family identifiers
// emitted by the readiness store. Iteration order is fixed so JSON output and
// regression tests stay deterministic regardless of map walk order.
var supplyChainImpactReadinessFamilies = []string{
	EvidenceFamilyContainerImageIdentity,
	EvidenceFamilyPackageConsumption,
	EvidenceFamilyPackageRegistry,
	EvidenceFamilySBOMAttestation,
	EvidenceFamilySBOMComponent,
	EvidenceFamilyScannerWorkerAnalysis,
	EvidenceFamilyVulnerabilityAdvisory,
	EvidenceFamilyVulnerabilityExploitability,
	EvidenceFamilyVulnerabilityOSPackage,
}

const (
	// EvidenceFamilyVulnerabilityOSPackage groups installed OS-package
	// evidence from the OS-package vulnerability collector (apk/dpkg/rpm),
	// counted only for the scanned image the request's subject digest or
	// image reference resolves to. os_package facts carry no image identity
	// of their own (sdk/go/factschema/vulnerability/v1/os_package.go); the
	// readiness store resolves the digest through the sibling
	// scanner_worker.analysis fact, pair-keyed first (same scope and
	// generation, the reducer's SubjectDigest key:
	// go/internal/reducer/supplychain/core/impact.go) with a scope-level
	// fallback across committed generations for provably single-digest
	// scopes (issue #8878: sibling tiers supersede the analysis generation
	// under the active os_package generation). The fallback is withheld on
	// rescanned multi-digest scopes (issue #9685), where scope-level
	// attribution would count another image's packages; the pair-keyed
	// image_reference arm is withheld there too (issue #9691), since both
	// generations of a rescan share one reference. A present-but-zero
	// count for an image that DID scan (see
	// EvidenceFamilyScannerWorkerAnalysis) is therefore ambiguous on its
	// own: on a single-digest scope it is a valid "no installed packages
	// found" observation, while on a multi-digest scope with an older
	// subject digest it is a deliberately withheld attribution, not a
	// clean scan and not missing evidence. Tell the two apart by the
	// scope's digest cardinality (scope_with_multiple_digests), never by
	// the zero alone.
	EvidenceFamilyVulnerabilityOSPackage = "vulnerability.os_package"
	// EvidenceFamilyScannerWorkerAnalysis groups scanner-worker image
	// analysis facts, which the analyzer emits only for a completed image
	// scan (sdk/go/factschema/scannerworker/v1/analysis.go). This is the
	// signal that distinguishes "this image was never scanned" (family
	// absent) from "scanned and clean" (family present, zero findings): the
	// analysis fact is recorded whether or not the scan found any installed
	// OS packages to match.
	EvidenceFamilyScannerWorkerAnalysis = "scanner_worker.analysis"
)

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
	vulnerabilityOSPackageFactKinds      = []string{"vulnerability.os_package"}
	scannerWorkerAnalysisFactKinds       = []string{"scanner_worker.analysis"}
)
