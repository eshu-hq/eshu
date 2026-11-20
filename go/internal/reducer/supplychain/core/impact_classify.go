// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// appendSupplyChainImpactIndexFindings classifies every group the index holds
// and returns the findings plus the products the newest-copy admission
// dropped: a product whose own scope and generation holds no admitted NVD
// copy forms no finding (#10735).
func appendSupplyChainImpactIndexFindings(findings []SupplyChainImpactFinding, index supplyChainImpactIndex) ([]SupplyChainImpactFinding, int) {
	var dropped int
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
		// Newest copy first (#10735): the elected copy alone supplies the
		// product set, the representative, and the severity, so a superseded
		// copy lends nothing. The two guards keep today's behavior: an
		// unreadable sibling may still assert the advisory, and a truncated
		// load may have cut the winning copy.
		reduced := native
		admissionActive := false
		var admitted map[advisoryCopyScope]bool
		if _, unreadable := index.unreadableCVEs[cveID]; !unreadable && !index.evidenceTruncated {
			admissionActive = true
			admitted = newestCopyAdmittedProductScopes(native.observations)
			reduced.observations = newestCopyCVEByRecord(native.observations)
		}
		representative, retraction := reduced.representative(), native.retraction(index)
		severity := reduced.severityObservation(representative)
		for _, product := range products {
			if admissionActive && !admitted[advisoryCopyScope{scope: product.ScopeID, generation: product.GenerationID}] {
				dropped++
				continue
			}
			finding := classifySupplyChainImpactProduct(representative, severity, product, index)
			finding.AdvisoryRetraction = retraction
			findings = appendSupplyChainImpactFinding(findings, finding)
		}
	}
	return findings, dropped
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
