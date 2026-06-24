// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

func missingImpactEvidence(finding SupplyChainImpactFinding) []string {
	var missing []string
	if finding.PackageID == "" || finding.ObservedVersion == "" {
		missing = append(missing, "package version evidence missing")
	}
	if finding.RepositoryID == "" {
		missing = append(missing, "repository dependency evidence missing")
	}
	if finding.SubjectDigest == "" && finding.RuntimeReachability != "known_fixed" &&
		!supplyChainReachabilityHasPackageAnchor(finding.RuntimeReachability) {
		missing = append(missing, "image or SBOM attachment evidence missing")
	}
	hasDeployment := len(finding.DeploymentIDs) > 0
	hasWorkloadOrService := len(finding.WorkloadIDs) > 0 || len(finding.ServiceIDs) > 0
	if finding.RuntimeReachability != "known_fixed" && len(finding.Environments) == 0 {
		if hasDeployment {
			missing = append(missing, "environment evidence missing")
		} else if hasWorkloadOrService {
			missing = append(missing, "runtime deployment evidence not linked to vulnerable package")
		} else {
			missing = append(missing, "deployment exposure evidence missing")
		}
	}
	if finding.RuntimeReachability != "known_fixed" && len(finding.WorkloadIDs) == 0 {
		missing = append(missing, "workload evidence missing")
	}
	if finding.RuntimeReachability != "known_fixed" && len(finding.ServiceIDs) == 0 {
		if hasSupplyChainServiceCatalogEvidence(finding) {
			if !hasResolvedSupplyChainServiceCatalogAnchor(finding) {
				missing = append(missing, "service/workload catalog anchor missing")
			}
		} else if hasDeployment || hasWorkloadOrService {
			missing = append(missing, "service catalog correlation evidence missing")
		} else {
			missing = append(missing, "service evidence missing")
		}
	}
	return uniqueSortedStrings(missing)
}

func hasSupplyChainServiceCatalogEvidence(finding SupplyChainImpactFinding) bool {
	for _, hop := range finding.EvidencePath {
		if hop == serviceCatalogCorrelationFactKind {
			return true
		}
	}
	return false
}

func hasResolvedSupplyChainServiceCatalogAnchor(finding SupplyChainImpactFinding) bool {
	if len(finding.ServiceIDs) > 0 {
		return true
	}
	return len(finding.WorkloadIDs) > 0 && len(finding.CatalogEntityRefs) > 0
}

func supplyChainReachabilityHasPackageAnchor(runtimeReachability string) bool {
	switch runtimeReachability {
	case "package_manifest",
		jsTSPackageAPICallEvidence,
		jsTSPackageAPIImportEvidence,
		jsTSPackageAPIReExportEvidence,
		jsTSPackageAPISCIPCallEvidence,
		jsTSPackageAPIUnknownEvidence,
		jsTSPackageAPIAmbiguousEvidence,
		jsTSPackageAPIMissingEvidence:
		return true
	default:
		return false
	}
}
