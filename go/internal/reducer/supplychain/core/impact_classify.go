// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

func appendSupplyChainImpactIndexFindings(findings []SupplyChainImpactFinding, index supplyChainImpactIndex) []SupplyChainImpactFinding {
	cveGroups := groupSupplyChainCVEsByID(index.cves)
	if findings == nil {
		findings = make([]SupplyChainImpactFinding, 0, len(index.affectedPackages)+len(index.affectedProducts))
	}
	for _, cveID := range sortedCVEKeys(cveGroups) {
		group := cveGroups[cveID]
		affected := index.affectedPackages[cveID]
		if len(affected) > 0 {
			pkgGroups := groupSupplyChainAffectedByPackage(affected)
			for _, packageID := range sortedPackageKeys(pkgGroups) {
				pkgs := pkgGroups[packageID]
				findings = appendSupplyChainImpactPackageFindings(findings, group, pkgs, index)
			}
			continue
		}
		products := index.affectedProducts[cveID]
		if len(products) > 0 {
			representative, retraction := group.representative(), group.retraction(index)
			for _, product := range products {
				finding := classifySupplyChainImpactProduct(representative, product, index)
				finding.AdvisoryRetraction = retraction
				findings = appendSupplyChainImpactFinding(findings, finding)
			}
			continue
		}
	}
	return findings
}
