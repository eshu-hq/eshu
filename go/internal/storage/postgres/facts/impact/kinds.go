// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

// Scanned fact kinds. The set and its order are the pre-#8880 statement's
// fact_kind IN list; KindsInRetiredOrder pins it.
const (
	KindCVE                    = "vulnerability.cve"
	KindAffectedPackage        = "vulnerability.affected_package"
	KindAffectedProduct        = "vulnerability.affected_product"
	KindSuppression            = "vulnerability.suppression"
	KindSecurityAlert          = "security_alert.repository_alert"
	KindPackageVersion         = "package_registry.package_version"
	KindVulnerabilityHint      = "package_registry.vulnerability_hint"
	KindPackageConsumption     = "reducer_package_consumption_correlation"
	KindSBOMComponent          = "sbom.component"
	KindSBOMAttachment         = "reducer_sbom_attestation_attachment"
	KindCICDRunCorrelation     = "reducer_ci_cd_run_correlation"
	KindPlatformMaterialized   = "reducer_platform_materialization"
	KindServiceCatalog         = "reducer_service_catalog_correlation"
	KindWorkloadIdentity       = "reducer_workload_identity"
	KindOCIImageManifest       = "oci_registry.image_manifest"
	KindOCIImageIndex          = "oci_registry.image_index"
	KindOCIImageTagObservation = "oci_registry.image_tag_observation"
	KindOCIImageReferrer       = "oci_registry.image_referrer"
	KindFile                   = "file"
	KindEPSSScore              = "vulnerability.epss_score"
	KindKnownExploited         = "vulnerability.known_exploited"
)

// Class is how a kind's rows are found (#8880 ruling Addendum A).
type Class int

const (
	// ClassGrowth kinds scale with external catalogs, registries, SBOMs,
	// runs, or repositories x advisories. Every contract-carried arm is a
	// key-leading index probe; cells the contract never carries go to the
	// foreign-key guard.
	ClassGrowth Class = iota + 1
	// ClassBounded kinds are bounded by topology or operator authorship.
	// One statement per kind evaluates the full disjunction over a
	// kind-partial anchor, so no cell is omitted.
	ClassBounded
	// ClassFile is the parser file kind: the JS/TS repo_id probe and the
	// resolved-scope probe; every other file cell goes to the guard.
	ClassFile
)

// KindInfo records a kind's class and the census that justified it. A kind
// changes class only with a new census.
type KindInfo struct {
	Kind  string
	Class Class
	// HostedActiveRows is the active non-tombstone row count from the #8880
	// hosted census (2026-10-23, docs/internal/evidence/8880-supply-chain-impact-id-resolution.md).
	HostedActiveRows int
	// Reason states why the class holds beyond the one census.
	Reason string
}

// Kinds returns every scanned kind in the retired statement's order.
func Kinds() []KindInfo {
	return []KindInfo{
		{KindCVE, ClassGrowth, 223, "one row per advisory in the NVD/OSV catalogs"},
		{KindAffectedPackage, ClassGrowth, 239, "one row per advisory x package in OSV feeds"},
		{KindAffectedProduct, ClassGrowth, 0, "one row per NVD CPE configuration"},
		{KindSuppression, ClassBounded, 0, "operator-authored VEX and suppression documents"},
		{KindSecurityAlert, ClassGrowth, 80, "repositories x vulnerable dependencies"},
		{KindPackageVersion, ClassGrowth, 50, "one row per registry package version"},
		{KindVulnerabilityHint, ClassGrowth, 0, "one row per registry advisory hint"},
		{KindPackageConsumption, ClassGrowth, 9, "repositories x consumed packages"},
		{KindSBOMComponent, ClassGrowth, 0, "one row per SBOM component"},
		{KindSBOMAttachment, ClassGrowth, 0, "one row per attached SBOM or attestation document"},
		{KindCICDRunCorrelation, ClassGrowth, 0, "one row per CI/CD run"},
		{KindPlatformMaterialized, ClassBounded, 1462, "one row per materialized workload platform"},
		{KindServiceCatalog, ClassBounded, 2, "one row per catalog entity"},
		{KindWorkloadIdentity, ClassBounded, 1462, "one row per workload"},
		{KindOCIImageManifest, ClassGrowth, 0, "one row per registry manifest"},
		{KindOCIImageIndex, ClassGrowth, 1, "one row per registry index"},
		{KindOCIImageTagObservation, ClassGrowth, 1, "one row per registry tag observation"},
		{KindOCIImageReferrer, ClassGrowth, 0, "one row per registry referrer"},
		{KindFile, ClassFile, 131710, "one row per parsed repository file"},
		{KindEPSSScore, ClassGrowth, 0, "one row per scored CVE"},
		{KindKnownExploited, ClassGrowth, 1755, "the CISA KEV catalog"},
	}
}

// KindsInRetiredOrder returns the scanned kind names in the retired order.
func KindsInRetiredOrder() []string {
	infos := Kinds()
	kinds := make([]string, 0, len(infos))
	for _, info := range infos {
		kinds = append(kinds, info.Kind)
	}
	return kinds
}

// RepositoryKinds are the kinds the repository arms apply to, in the
// retired statement's order.
func RepositoryKinds() []string {
	return []string{
		KindSuppression, KindPackageConsumption, KindCICDRunCorrelation,
		KindPlatformMaterialized, KindServiceCatalog, KindWorkloadIdentity,
	}
}
