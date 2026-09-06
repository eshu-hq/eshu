// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func classifySupplyChainImpactProduct(
	cve supplychainmodel.ImpactCVE,
	product supplychainmodel.AffectedProduct,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := baseSupplyChainImpactProductFinding(cve, product, index)
	component, attachment, image, hasComponentPath, imagePathMissing := firstSBOMProductImpactPath(product, index)
	if hasComponentPath {
		finding.ObservedVersion = payloadcore.FirstNonBlank(component.Version, versionFromCPE23Criteria(product.Criteria))
		finding.SubjectDigest = attachment.SubjectDigest
		finding.ImageRef = image.imageRef
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, component.FactID, attachment.FactID, image.factID)
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
	finding.ObservedVersion = versionFromCPE23Criteria(product.Criteria)
	finding.Status = SupplyChainImpactPossiblyAffected
	finding.Confidence = "weak_product"
	finding.RuntimeReachability = "unknown"
	finding.CanonicalWrites = 1
	finalizeSupplyChainImpactFinding(&finding, index, imagePathMissing)
	return finding
}

func baseSupplyChainImpactProductFinding(
	cve supplychainmodel.ImpactCVE,
	product supplychainmodel.AffectedProduct,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := SupplyChainImpactFinding{
		CVEID:               cve.CVEID,
		AdvisoryID:          payloadcore.FirstNonBlank(cve.AdvisoryID, cve.CVEID),
		ProductCriteria:     product.Criteria,
		MatchCriteriaID:     product.MatchCriteriaID,
		CVSSScore:           cve.CVSSScore,
		AdvisoryPublishedAt: cve.PublishedAt,
		AdvisoryUpdatedAt:   cve.SourceUpdatedAt,
		EvidencePath:        []string{facts.VulnerabilityCVEFactKind, facts.VulnerabilityAffectedProductFactKind},
		EvidenceFactIDs:     []string{cve.FactID, product.FactID},
	}
	applyRiskSignals(&finding, index.riskSignals[cve.CVEID])
	return finding
}

func baseSupplyChainImpactFinding(
	cves supplyChainCVEGroup,
	pkgs []supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	pkg := representativeAffectedPackage(pkgs)
	observations := buildAdvisoryProvenanceObservations(cves.observations, pkgs)
	provenance := selectAdvisoryProvenance(pkg.Ecosystem, observations)
	advisoryID := provenanceAdvisoryID(provenance, cves)

	finding := SupplyChainImpactFinding{
		CVEID:                cves.cveID,
		AdvisoryID:           advisoryID,
		PackageID:            pkg.PackageID,
		Ecosystem:            pkg.Ecosystem,
		PackageName:          pkg.Name,
		PURL:                 pkg.PURL,
		FixedVersion:         provenance.FixedVersion,
		CVSSScore:            provenance.SeverityScore,
		SeveritySource:       provenance.SeveritySource,
		SeverityVector:       provenance.SeverityVector,
		SeverityLabel:        provenance.SeverityLabel,
		AdvisoryPublishedAt:  cves.representative().PublishedAt,
		AdvisoryUpdatedAt:    cves.representative().SourceUpdatedAt,
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
	return payloadcore.FirstNonBlank(rep.AdvisoryID, rep.CVEID)
}

func applyRiskSignals(finding *SupplyChainImpactFinding, signals supplychainmodel.RiskSignals) {
	finding.EPSSProbability = signals.EPSSProbability
	finding.EPSSPercentile = signals.EPSSPercentile
	finding.KnownExploited = signals.KnownExploited
	finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, signals.EPSSFactID, signals.KEVFactID)
	var reasons []string
	if finding.CVSSScore > 0 {
		reasons = append(reasons, fmt.Sprintf("cvss=%.1f", finding.CVSSScore))
	}
	if signals.EPSSProbability != "" {
		reasons = append(reasons, "epss="+signals.EPSSProbability)
	}
	if signals.KnownExploited {
		reasons = append(reasons, "kev=true")
	}
	if len(reasons) > 0 {
		finding.PriorityReason = strings.Join(reasons, "; ") + "; signals do not prove reachability"
	}
	finding.EvidenceFactIDs = uniqueSortedStrings(finding.EvidenceFactIDs)
}
