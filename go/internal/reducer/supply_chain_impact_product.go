// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func classifySupplyChainImpactProduct(
	cve supplyChainImpactCVE,
	product supplyChainAffectedProduct,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := baseSupplyChainImpactProductFinding(cve, product, index)
	component, attachment, image, hasComponentPath, imagePathMissing := firstSBOMProductImpactPath(product, index)
	if hasComponentPath {
		finding.ObservedVersion = firstNonBlank(component.version, versionFromCPE23Criteria(product.criteria))
		finding.SubjectDigest = attachment.subjectDigest
		finding.ImageRef = image.imageRef
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, component.factID, attachment.factID, image.factID)
		finding.EvidencePath = append(finding.EvidencePath, facts.SBOMComponentFactKind, sbomAttestationAttachmentFactKind, containerImageIdentityFactKind)
		if image.repositoryID != "" {
			finding.RepositoryID = image.repositoryID
		}
		finding.Status = SupplyChainImpactAffectedDerived
		finding.Confidence = "derived_product"
		finding.RuntimeReachability = "image_sbom"
		finding.CanonicalWrites = 1
		finalizeSupplyChainImpactFinding(&finding, index, imagePathMissing)
		return finding
	}
	finding.ObservedVersion = versionFromCPE23Criteria(product.criteria)
	finding.Status = SupplyChainImpactPossiblyAffected
	finding.Confidence = "weak_product"
	finding.RuntimeReachability = "unknown"
	finding.CanonicalWrites = 1
	finalizeSupplyChainImpactFinding(&finding, index, imagePathMissing)
	return finding
}

func baseSupplyChainImpactProductFinding(
	cve supplyChainImpactCVE,
	product supplyChainAffectedProduct,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := SupplyChainImpactFinding{
		CVEID:               cve.cveID,
		AdvisoryID:          firstNonBlank(cve.advisoryID, cve.cveID),
		ProductCriteria:     product.criteria,
		MatchCriteriaID:     product.matchCriteriaID,
		CVSSScore:           cve.cvssScore,
		AdvisoryPublishedAt: cve.publishedAt,
		AdvisoryUpdatedAt:   cve.sourceUpdatedAt,
		EvidencePath:        []string{facts.VulnerabilityCVEFactKind, facts.VulnerabilityAffectedProductFactKind},
		EvidenceFactIDs:     []string{cve.factID, product.factID},
	}
	applyRiskSignals(&finding, index.riskSignals[cve.cveID])
	return finding
}

func baseSupplyChainImpactFinding(
	cves supplyChainCVEGroup,
	pkgs []supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	pkg := representativeAffectedPackage(pkgs)
	observations := buildAdvisoryProvenanceObservations(cves.observations, pkgs)
	provenance := selectAdvisoryProvenance(pkg.ecosystem, observations)
	advisoryID := provenanceAdvisoryID(provenance, cves)

	finding := SupplyChainImpactFinding{
		CVEID:                cves.cveID,
		AdvisoryID:           advisoryID,
		PackageID:            pkg.packageID,
		Ecosystem:            pkg.ecosystem,
		PackageName:          pkg.name,
		PURL:                 pkg.purl,
		FixedVersion:         provenance.FixedVersion,
		CVSSScore:            provenance.SeverityScore,
		SeveritySource:       provenance.SeveritySource,
		SeverityVector:       provenance.SeverityVector,
		SeverityLabel:        provenance.SeverityLabel,
		AdvisoryPublishedAt:  cves.representative().publishedAt,
		AdvisoryUpdatedAt:    cves.representative().sourceUpdatedAt,
		AlternateSeverities:  provenance.AlternateSeverities,
		FixedVersionSource:   provenance.FixedVersionSource,
		FixedVersionBranches: provenance.FixedVersionBranches,
		RangeSource:          provenance.RangeSource,
		VulnerableRange:      provenance.VulnerableRange,
		AdvisorySources:      provenance.AdvisorySources,
		EvidencePath:         []string{facts.VulnerabilityCVEFactKind, facts.VulnerabilityAffectedPackageFactKind},
		EvidenceFactIDs:      provenance.EvidenceFactIDs,
	}
	applyRiskSignals(&finding, index.riskSignals[cves.cveID])
	return finding
}

// provenanceAdvisoryID picks the advisory identifier surfaced on the finding
// row. The selected severity source's advisory identifier wins so the row
// matches the source the operator sees as the primary signal; falling back
// to the first advisory observation keeps the row populated when no source
// published a CVSS score.
func provenanceAdvisoryID(provenance advisoryProvenanceSelection, cves supplyChainCVEGroup) string {
	if provenance.SeveritySource != "" {
		for _, advisory := range provenance.AdvisorySources {
			if advisory.Source == provenance.SeveritySource {
				return advisory.AdvisoryID
			}
		}
	}
	if len(provenance.AdvisorySources) > 0 {
		return provenance.AdvisorySources[0].AdvisoryID
	}
	rep := cves.representative()
	return firstNonBlank(rep.advisoryID, rep.cveID)
}

func applyRiskSignals(finding *SupplyChainImpactFinding, signals supplyChainRiskSignals) {
	finding.EPSSProbability = signals.epssProbability
	finding.EPSSPercentile = signals.epssPercentile
	finding.KnownExploited = signals.knownExploited
	finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, signals.epssFactID, signals.kevFactID)
	var reasons []string
	if finding.CVSSScore > 0 {
		reasons = append(reasons, fmt.Sprintf("cvss=%.1f", finding.CVSSScore))
	}
	if signals.epssProbability != "" {
		reasons = append(reasons, "epss="+signals.epssProbability)
	}
	if signals.knownExploited {
		reasons = append(reasons, "kev=true")
	}
	if len(reasons) > 0 {
		finding.PriorityReason = strings.Join(reasons, "; ") + "; signals do not prove reachability"
	}
	finding.EvidenceFactIDs = uniqueSortedStrings(finding.EvidenceFactIDs)
}
