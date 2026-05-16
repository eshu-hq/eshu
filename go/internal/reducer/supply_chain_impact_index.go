package reducer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type supplyChainImpactCVE struct {
	factID     string
	cveID      string
	advisoryID string
	cvssScore  float64
}

type supplyChainAffectedPackage struct {
	factID           string
	cveID            string
	packageID        string
	ecosystem        string
	name             string
	purl             string
	affectedVersions []string
	fixedVersions    []string
}

type supplyChainPackageVersion struct {
	factID    string
	packageID string
	purl      string
	version   string
}

type supplyChainPackageConsumption struct {
	factID       string
	packageID    string
	repositoryID string
}

type supplyChainSBOMComponent struct {
	factID     string
	documentID string
	purl       string
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
	packageVersions  map[string][]supplyChainPackageVersion
	consumption      map[string][]supplyChainPackageConsumption
	components       []supplyChainSBOMComponent
	attachments      map[string]supplyChainAttachment
	images           map[string]supplyChainImageIdentity
	riskSignals      map[string]supplyChainRiskSignals
}

func buildSupplyChainImpactIndex(envelopes []facts.Envelope) supplyChainImpactIndex {
	index := supplyChainImpactIndex{
		affectedPackages: map[string][]supplyChainAffectedPackage{},
		packageVersions:  map[string][]supplyChainPackageVersion{},
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
		case facts.PackageRegistryPackageVersionFactKind:
			version := supplyChainPackageVersionFromEnvelope(envelope)
			if version.packageID != "" {
				index.packageVersions[version.packageID] = append(index.packageVersions[version.packageID], version)
			}
		case packageConsumptionCorrelationFactKind:
			consumption := supplyChainConsumptionFromEnvelope(envelope)
			if consumption.packageID != "" {
				index.consumption[consumption.packageID] = append(index.consumption[consumption.packageID], consumption)
			}
		case facts.SBOMComponentFactKind:
			component := supplyChainSBOMComponentFromEnvelope(envelope)
			if component.purl != "" || component.packageID != "" {
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
	cve supplyChainImpactCVE,
	pkg supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := baseSupplyChainImpactFinding(cve, pkg, index)
	version, hasVersion := firstPackageVersion(pkg, index.packageVersions[pkg.packageID])
	component, attachment, image, hasComponentPath := firstSBOMImpactPath(pkg, index)
	if hasVersion {
		finding.PURL = firstNonBlank(version.purl, finding.PURL)
		finding.ObservedVersion = version.version
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, version.factID)
		finding.EvidencePath = append(finding.EvidencePath, facts.PackageRegistryPackageVersionFactKind)
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
	if isKnownFixed(finding.ObservedVersion, finding.FixedVersion) {
		finding.Status = SupplyChainImpactNotAffectedKnownFixed
		finding.Confidence = "exact"
		finding.RuntimeReachability = "known_fixed"
		finding.CanonicalWrites = 1
		finding.MissingEvidence = missingImpactEvidence(finding)
		return finding
	}
	if consumption := firstConsumption(pkg.packageID, index.consumption); hasVersion && consumption.factID != "" && versionAffected(finding.ObservedVersion, pkg.affectedVersions) {
		finding.Status = SupplyChainImpactAffectedExact
		finding.Confidence = "exact"
		finding.RepositoryID = consumption.repositoryID
		finding.RuntimeReachability = "package_manifest"
		finding.CanonicalWrites = 1
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, consumption.factID)
		finding.EvidencePath = append(finding.EvidencePath, packageConsumptionCorrelationFactKind)
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

func unknownSupplyChainImpactFinding(
	cve supplyChainImpactCVE,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := SupplyChainImpactFinding{
		CVEID:           cve.cveID,
		AdvisoryID:      firstNonBlank(cve.advisoryID, cve.cveID),
		Status:          SupplyChainImpactUnknown,
		Confidence:      "unknown",
		CVSSScore:       cve.cvssScore,
		MissingEvidence: []string{"affected package evidence missing", "package or SBOM evidence missing", "runtime reachability evidence missing"},
		EvidencePath:    []string{facts.VulnerabilityCVEFactKind},
		EvidenceFactIDs: []string{cve.factID},
	}
	applyRiskSignals(&finding, index.riskSignals[cve.cveID])
	return finding
}

func baseSupplyChainImpactFinding(
	cve supplyChainImpactCVE,
	pkg supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	fixed := ""
	if len(pkg.fixedVersions) > 0 {
		fixed = pkg.fixedVersions[0]
	}
	finding := SupplyChainImpactFinding{
		CVEID:           cve.cveID,
		AdvisoryID:      firstNonBlank(cve.advisoryID, cve.cveID),
		PackageID:       pkg.packageID,
		Ecosystem:       pkg.ecosystem,
		PackageName:     pkg.name,
		PURL:            pkg.purl,
		FixedVersion:    fixed,
		CVSSScore:       cve.cvssScore,
		EvidencePath:    []string{facts.VulnerabilityCVEFactKind, facts.VulnerabilityAffectedPackageFactKind},
		EvidenceFactIDs: []string{cve.factID, pkg.factID},
	}
	applyRiskSignals(&finding, index.riskSignals[cve.cveID])
	return finding
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
