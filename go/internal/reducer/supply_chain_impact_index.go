package reducer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type supplyChainImpactCVE struct {
	factID          string
	cveID           string
	advisoryID      string
	source          string
	cvssScore       float64
	cvssVector      string
	severityLabel   string
	sourceUpdatedAt string
	withdrawnAt     string
}

type supplyChainAffectedPackage struct {
	factID           string
	cveID            string
	source           string
	advisoryID       string
	packageID        string
	ecosystem        string
	name             string
	purl             string
	affectedVersions []string
	affectedRanges   []supplyChainAffectedRange
	affectedRangeRaw string
	fixedVersions    []string
}

type supplyChainAffectedProduct struct {
	factID          string
	cveID           string
	criteria        string
	matchCriteriaID string
	vulnerable      bool
}

type supplyChainPackageConsumption struct {
	factID           string
	packageID        string
	repositoryID     string
	dependencyRange  string
	dependencyPath   []string
	dependencyDepth  int
	directDependency *bool
}

type supplyChainSBOMComponent struct {
	factID     string
	documentID string
	purl       string
	cpe        string
	packageID  string
	version    string
}

type supplyChainAttachment struct {
	factID        string
	documentID    string
	subjectDigest string
	status        string
}

type supplyChainImageIdentity struct {
	factID       string
	digest       string
	repositoryID string
}

type supplyChainRiskSignals struct {
	epssFactID      string
	epssProbability string
	epssPercentile  string
	kevFactID       string
	knownExploited  bool
}

type supplyChainImpactIndex struct {
	cves             []supplyChainImpactCVE
	affectedPackages map[string][]supplyChainAffectedPackage
	affectedProducts map[string][]supplyChainAffectedProduct
	consumption      map[string][]supplyChainPackageConsumption
	components       []supplyChainSBOMComponent
	attachments      map[string]supplyChainAttachment
	images           map[string]supplyChainImageIdentity
	riskSignals      map[string]supplyChainRiskSignals
}

func buildSupplyChainImpactIndex(envelopes []facts.Envelope) supplyChainImpactIndex {
	index := supplyChainImpactIndex{
		affectedPackages: map[string][]supplyChainAffectedPackage{},
		affectedProducts: map[string][]supplyChainAffectedProduct{},
		consumption:      map[string][]supplyChainPackageConsumption{},
		attachments:      map[string]supplyChainAttachment{},
		images:           map[string]supplyChainImageIdentity{},
		riskSignals:      map[string]supplyChainRiskSignals{},
	}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.VulnerabilityCVEFactKind:
			cve := supplyChainCVEFromEnvelope(envelope)
			if cve.cveID != "" {
				index.cves = append(index.cves, cve)
			}
		case facts.VulnerabilityAffectedPackageFactKind:
			pkg := supplyChainAffectedPackageFromEnvelope(envelope)
			if pkg.cveID != "" {
				index.affectedPackages[pkg.cveID] = append(index.affectedPackages[pkg.cveID], pkg)
			}
		case facts.VulnerabilityAffectedProductFactKind:
			product := supplyChainAffectedProductFromEnvelope(envelope)
			if product.cveID != "" && product.criteria != "" && product.vulnerable {
				index.affectedProducts[product.cveID] = append(index.affectedProducts[product.cveID], product)
			}
		case packageConsumptionCorrelationFactKind:
			consumption := supplyChainConsumptionFromEnvelope(envelope)
			if consumption.packageID != "" {
				index.consumption[consumption.packageID] = append(index.consumption[consumption.packageID], consumption)
			}
		case facts.SBOMComponentFactKind:
			component := supplyChainSBOMComponentFromEnvelope(envelope)
			if component.purl != "" || component.packageID != "" || component.cpe != "" {
				index.components = append(index.components, component)
			}
		case sbomAttestationAttachmentFactKind:
			attachment := supplyChainAttachmentFromEnvelope(envelope)
			if attachment.documentID != "" {
				index.attachments[attachment.documentID] = attachment
			}
		case containerImageIdentityFactKind:
			image := supplyChainImageIdentityFromEnvelope(envelope)
			if image.digest != "" {
				index.images[image.digest] = image
			}
		case facts.VulnerabilityEPSSScoreFactKind:
			signals := index.riskSignals[supplyChainCVEID(envelope.Payload)]
			signals.epssFactID = envelope.FactID
			signals.epssProbability = payloadStr(envelope.Payload, "probability")
			signals.epssPercentile = payloadStr(envelope.Payload, "percentile")
			index.riskSignals[supplyChainCVEID(envelope.Payload)] = signals
		case facts.VulnerabilityKnownExploitedFactKind:
			signals := index.riskSignals[supplyChainCVEID(envelope.Payload)]
			signals.kevFactID = envelope.FactID
			signals.knownExploited = true
			index.riskSignals[supplyChainCVEID(envelope.Payload)] = signals
		}
	}
	sort.SliceStable(index.cves, func(i, j int) bool {
		return index.cves[i].cveID < index.cves[j].cveID
	})
	return index
}

func classifySupplyChainImpactPackage(
	cves supplyChainCVEGroup,
	pkgs []supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := baseSupplyChainImpactFinding(cves, pkgs, index)
	pkg := representativeAffectedPackage(pkgs)
	component, attachment, image, hasComponentPath := firstSBOMImpactPath(pkg, index)
	consumption := firstConsumption(pkg.packageID, index.consumption)
	if consumption.factID != "" {
		finding.RepositoryID = consumption.repositoryID
		finding.DependencyPath = append([]string(nil), consumption.dependencyPath...)
		finding.DependencyDepth = consumption.dependencyDepth
		if consumption.directDependency != nil {
			value := *consumption.directDependency
			finding.DirectDependency = &value
		}
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, consumption.factID)
		finding.EvidencePath = append(finding.EvidencePath, packageConsumptionCorrelationFactKind)
		if manifestVersion, ok := exactManifestDependencyVersion(consumption.dependencyRange); ok {
			finding.ObservedVersion = manifestVersion
		}
	}
	if hasComponentPath {
		finding.PURL = firstNonBlank(component.purl, finding.PURL)
		finding.ObservedVersion = firstNonBlank(component.version, finding.ObservedVersion)
		finding.SubjectDigest = attachment.subjectDigest
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, component.factID, attachment.factID, image.factID)
		finding.EvidencePath = append(finding.EvidencePath, facts.SBOMComponentFactKind, sbomAttestationAttachmentFactKind, containerImageIdentityFactKind)
		if image.repositoryID != "" {
			finding.RepositoryID = image.repositoryID
		}
	}
	if consumption.factID != "" && anyPackageVersionAffected(finding.ObservedVersion, pkgs) {
		finding.Status = SupplyChainImpactAffectedExact
		finding.Confidence = "exact"
		finding.RuntimeReachability = "package_manifest"
		finding.CanonicalWrites = 1
		finding.MissingEvidence = missingImpactEvidence(finding)
		return finding
	}
	if isKnownFixed(finding.ObservedVersion, finding.FixedVersion) {
		finding.Status = SupplyChainImpactNotAffectedKnownFixed
		finding.Confidence = "exact"
		finding.RuntimeReachability = "known_fixed"
		finding.CanonicalWrites = 1
		finding.MissingEvidence = missingImpactEvidence(finding)
		return finding
	}
	if hasComponentPath {
		finding.Status = SupplyChainImpactAffectedDerived
		finding.Confidence = "derived"
		finding.RuntimeReachability = "image_sbom"
		finding.CanonicalWrites = 1
		finding.MissingEvidence = missingImpactEvidence(finding)
		return finding
	}
	finding.Status = SupplyChainImpactPossiblyAffected
	finding.Confidence = "partial"
	finding.RuntimeReachability = "unknown"
	finding.CanonicalWrites = 1
	finding.MissingEvidence = missingImpactEvidence(finding)
	return finding
}

func classifySupplyChainImpactProduct(
	cve supplyChainImpactCVE,
	product supplyChainAffectedProduct,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := baseSupplyChainImpactProductFinding(cve, product, index)
	component, attachment, image, hasComponentPath := firstSBOMProductImpactPath(product, index)
	if hasComponentPath {
		finding.ObservedVersion = firstNonBlank(component.version, versionFromCPE23Criteria(product.criteria))
		finding.SubjectDigest = attachment.subjectDigest
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, component.factID, attachment.factID, image.factID)
		finding.EvidencePath = append(finding.EvidencePath, facts.SBOMComponentFactKind, sbomAttestationAttachmentFactKind, containerImageIdentityFactKind)
		if image.repositoryID != "" {
			finding.RepositoryID = image.repositoryID
		}
		finding.Status = SupplyChainImpactAffectedDerived
		finding.Confidence = "derived_product"
		finding.RuntimeReachability = "image_sbom"
		finding.CanonicalWrites = 1
		finding.MissingEvidence = missingImpactEvidence(finding)
		return finding
	}
	finding.ObservedVersion = versionFromCPE23Criteria(product.criteria)
	finding.Status = SupplyChainImpactPossiblyAffected
	finding.Confidence = "weak_product"
	finding.RuntimeReachability = "unknown"
	finding.CanonicalWrites = 1
	finding.MissingEvidence = missingImpactEvidence(finding)
	return finding
}

func baseSupplyChainImpactProductFinding(
	cve supplyChainImpactCVE,
	product supplyChainAffectedProduct,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := SupplyChainImpactFinding{
		CVEID:           cve.cveID,
		AdvisoryID:      firstNonBlank(cve.advisoryID, cve.cveID),
		ProductCriteria: product.criteria,
		MatchCriteriaID: product.matchCriteriaID,
		CVSSScore:       cve.cvssScore,
		EvidencePath:    []string{facts.VulnerabilityCVEFactKind, facts.VulnerabilityAffectedProductFactKind},
		EvidenceFactIDs: []string{cve.factID, product.factID},
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
		AlternateSeverities:  provenance.AlternateSeverities,
		FixedVersionSource:   provenance.FixedVersionSource,
		FixedVersionBranches: provenance.FixedVersionBranches,
		RangeSource:          provenance.RangeSource,
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
