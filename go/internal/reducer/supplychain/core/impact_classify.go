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
			for _, product := range products {
				findings = appendSupplyChainImpactFinding(findings, classifySupplyChainImpactProduct(group.representative(), product, index))
			}
			continue
		}
	}
	return findings
}
