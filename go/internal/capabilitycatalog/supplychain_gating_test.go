// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"strings"
	"testing"
)

// TestSupplyChainListCapabilitiesAreCollectorGated locks in the issue #3496
// contract: the supply-chain, package-registry, CI/CD, and container-image
// list capabilities are served by external collectors that are off in a
// default deploy, so the catalog must advertise them as gated (not GA) with a
// maturity_reason that names the opt-in collector gate. The matrix still
// records production support, so DerivedMaturity stays general_availability and
// the runtime tool remains available once the operator enables the collector;
// only the advertised default-deploy maturity is downgraded.
func TestSupplyChainListCapabilitiesAreCollectorGated(t *testing.T) {
	t.Parallel()

	catalog, findings, err := BuildFromSpecs(repoSpecsDir(t), Signals{MCPTools: map[string]bool{
		"list_sbom_attestation_attachments":   true,
		"list_package_registry_packages":      true,
		"list_package_registry_versions":      true,
		"list_package_registry_dependencies":  true,
		"list_package_registry_correlations":  true,
		"list_container_image_identities":     true,
		"list_supply_chain_impact_findings":   true,
		"list_security_alert_reconciliations": true,
		"list_ci_cd_run_correlations":         true,
		"list_service_catalog_correlations":   true,
	}})
	if err != nil {
		t.Fatalf("BuildFromSpecs: %v", err)
	}
	for _, finding := range findings {
		switch finding.Kind {
		case FindingInvalidOverlayMaturity, FindingMissingMaturityReason, FindingStaleOverlayCapability:
			if isSupplyChainGatedCapability(finding.Subject) {
				t.Fatalf("supply-chain overlay finding: %+v", finding)
			}
		}
	}

	byID := map[string]Entry{}
	for _, entry := range catalog.Entries {
		byID[entry.Capability] = entry
	}

	// Each gated list capability: effective maturity gated, matrix still
	// supports it (derived GA), and the reason names the external-collector gate.
	for _, id := range supplyChainGatedCapabilityIDs {
		entry, ok := byID[id]
		if !ok {
			t.Fatalf("%s missing from catalog", id)
		}
		if entry.Maturity != MaturityGated {
			t.Errorf("%s maturity = %q, want gated (collector off in default deploy)", id, entry.Maturity)
		}
		if entry.DerivedMaturity != MaturityGeneralAvailability {
			t.Errorf("%s derived_maturity = %q, want general_availability (matrix still supports it)", id, entry.DerivedMaturity)
		}
		reason := strings.ToUpper(entry.MaturityReason)
		if !strings.Contains(reason, "ESHU_COLLECTOR_INSTANCES_JSON") {
			t.Errorf("%s maturity_reason must name the collector instance gate, got %q", id, entry.MaturityReason)
		}
	}

	// service_catalog.correlations.list populates from the default git collector
	// (committed catalog-info.yaml/opslevel.yml/cortex.yaml), so it is genuinely
	// GA in a default deploy and must NOT be gated. An empty result is a true
	// fresh-zero, not an unconfigured-collector state.
	svc, ok := byID["service_catalog.correlations.list"]
	if !ok {
		t.Fatalf("service_catalog.correlations.list missing from catalog")
	}
	if svc.Maturity != MaturityGeneralAvailability {
		t.Errorf("service_catalog.correlations.list maturity = %q, want general_availability (git collector populates it)", svc.Maturity)
	}
}

// supplyChainGatedCapabilityIDs are the list capabilities whose feeding
// collectors require ESHU_COLLECTOR_INSTANCES_JSON plus external credentials and
// are off in a default deploy.
var supplyChainGatedCapabilityIDs = []string{
	"supply_chain.sbom_attestation_attachments.list",
	"package_registry.packages.list",
	"package_registry.versions.list",
	"package_registry.dependencies.list",
	"package_registry.correlations.list",
	"supply_chain.container_image_identities.list",
	"supply_chain.impact_findings.list",
	"supply_chain.security_alert_reconciliations.list",
	"ci_cd.run_correlations.list",
}

func isSupplyChainGatedCapability(id string) bool {
	for _, gated := range supplyChainGatedCapabilityIDs {
		if gated == id {
			return true
		}
	}
	return false
}
