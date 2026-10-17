// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// OCIRegistryHandleBinding declares this package's verified reducer consumption of
// oci_registry kinds (issue #6629). Build-time metadata only.
var OCIRegistryHandleBinding = contract.Bind(contract.Binding{
	ID: "core/core_handle_oci_registry", FamilyID: "oci_registry",
	ReducerDir: "go/internal/reducer/supplychain/core", Symbol: "Handle",
	Kinds: []string{"oci_registry.image_index", "oci_registry.image_manifest", "oci_registry.image_referrer", "oci_registry.image_tag_observation"},
})

// PackageRegistryHandleBinding declares this package's verified reducer consumption of
// package_registry kinds (issue #6629). Build-time metadata only.
var PackageRegistryHandleBinding = contract.Bind(contract.Binding{
	ID: "core/core_handle_package_registry", FamilyID: "package_registry",
	ReducerDir: "go/internal/reducer/supplychain/core", Symbol: "Handle",
	Kinds: []string{"package_registry.package"},
})

// ReducerDerivedSupplyChainConsumptionFromEnvelopeBinding declares this package's verified reducer consumption of
// reducer_derived kinds (issue #6629). Build-time metadata only.
var ReducerDerivedSupplyChainConsumptionFromEnvelopeBinding = contract.Bind(contract.Binding{
	ID: "core/core_supply_chain_consumption_from_envelope", FamilyID: "reducer_derived",
	ReducerDir: "go/internal/reducer/supplychain/core", Symbol: "supplyChainConsumptionFromEnvelope",
	Kinds: []string{"reducer_package_consumption_correlation"},
})

// SBOMAttestationHandleBinding declares this package's verified reducer consumption of
// sbom_attestation kinds (issue #6629). Build-time metadata only.
var SBOMAttestationHandleBinding = contract.Bind(contract.Binding{
	ID: "core/core_handle_sbom_attestation", FamilyID: "sbom_attestation",
	ReducerDir: "go/internal/reducer/supplychain/core", Symbol: "Handle",
	Kinds: []string{"sbom.component"},
})

// ScannerWorkerHandleBinding declares this package's verified reducer consumption of
// scanner_worker kinds (issue #6629). Build-time metadata only.
var ScannerWorkerHandleBinding = contract.Bind(contract.Binding{
	ID: "core/core_handle_scanner_worker", FamilyID: "scanner_worker",
	ReducerDir: "go/internal/reducer/supplychain/core", Symbol: "Handle",
	Kinds: []string{"scanner_worker.analysis"},
})

// SecurityAlertHandleBinding declares this package's verified reducer consumption of
// security_alert kinds (issue #6629). Build-time metadata only.
var SecurityAlertHandleBinding = contract.Bind(contract.Binding{
	ID: "core/core_handle_security_alert", FamilyID: "security_alert",
	ReducerDir: "go/internal/reducer/supplychain/core", Symbol: "Handle",
	Kinds: []string{"security_alert.repository_alert"},
})

// VulnerabilityIntelligenceHandleBinding declares this package's verified reducer consumption of
// vulnerability_intelligence kinds (issue #6629). Build-time metadata only.
var VulnerabilityIntelligenceHandleBinding = contract.Bind(contract.Binding{
	ID: "core/core_handle_vulnerability_intelligence", FamilyID: "vulnerability_intelligence",
	ReducerDir: "go/internal/reducer/supplychain/core", Symbol: "Handle",
	Kinds: []string{"vulnerability.affected_package", "vulnerability.affected_product", "vulnerability.cve", "vulnerability.epss_score", "vulnerability.go_call_reachability", "vulnerability.go_module_evidence", "vulnerability.known_exploited", "vulnerability.os_package"},
})

// VulnerabilitySuppressionBuildVulnerabilitySuppressionsBinding declares this package's verified reducer consumption of
// vulnerability_suppression kinds (issue #6629). Build-time metadata only.
var VulnerabilitySuppressionBuildVulnerabilitySuppressionsBinding = contract.Bind(contract.Binding{
	ID: "core/core_build_vulnerability_suppressions", FamilyID: "vulnerability_suppression",
	ReducerDir: "go/internal/reducer/supplychain/core", Symbol: "BuildVulnerabilitySuppressions",
	Kinds: []string{"vulnerability.suppression"},
})
