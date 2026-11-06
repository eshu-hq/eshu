// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

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
			// A package natively keyed to this group skips its products. Packages
			// that are all upstream copies do not: a distro record naming the CVE
			// must not hide an NVD product match (#9914).
			if !allUpstreamCopies(affected) {
				continue
			}
		}
		products := index.affectedProducts[cveID]
		if len(products) == 0 {
			continue
		}
		native := group.nativeObservations()
		if len(native.observations) == 0 {
			continue
		}
		representative, retraction := native.representative(), native.retraction(index)
		for _, product := range products {
			finding := classifySupplyChainImpactProduct(representative, product, index)
			finding.AdvisoryRetraction = retraction
			findings = appendSupplyChainImpactFinding(findings, finding)
		}
	}
	return findings
}

// allUpstreamCopies reports whether every package reached this group only
// through the OS advisory grouping pass.
func allUpstreamCopies(pkgs []supplychainmodel.AffectedPackage) bool {
	for _, pkg := range pkgs {
		if !pkg.UpstreamCopy {
			return false
		}
	}
	return true
}

// nativeObservations returns the group without its upstream copies. It returns
// the group unchanged, without allocating, when it holds no copy.
func (g supplyChainCVEGroup) nativeObservations() supplyChainCVEGroup {
	isCopy := func(cve supplychainmodel.ImpactCVE) bool { return cve.UpstreamCopy }
	if !slices.ContainsFunc(g.observations, isCopy) {
		return g
	}
	g.observations = slices.DeleteFunc(slices.Clone(g.observations), isCopy)
	return g
}
