// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
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
	publishedAt     string
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
	factID                    string
	evidenceKind              string
	packageID                 string
	repositoryID              string
	dependencyRange           string
	observedVersion           string
	requestedRange            string
	installedVersion          string
	dependencyPath            []string
	dependencyDepth           int
	directDependency          *bool
	dependencyScope           string
	versionEvidence           string
	unresolvedMSBuildProperty string
	ambiguousMSBuildProperty  string
	packageAPIPackages        []string
	packageAPIIdentitySource  string
	dependencyResolutionState string
	sourceSet                 string
	generatedCode             *bool
	partialEvidence           bool
	lockfile                  bool
}

type supplyChainSBOMComponent struct {
	factID     string
	documentID string
	purl       string
	cpe        string
	packageID  string
	version    string
}

type supplyChainOSPackage struct {
	factID               string
	scopeID              string
	packageID            string
	purl                 string
	distro               string
	distroVersion        string
	packageManager       string
	name                 string
	arch                 string
	installedVersion     string
	repositoryClass      string
	vendorAdvisorySource string
}

type supplyChainAttachment struct {
	factID        string
	documentID    string
	subjectDigest string
	status        string
}

type supplyChainImageIdentity struct {
	factID          string
	digest          string
	imageRef        string
	repositoryID    string
	outcome         string
	canonicalWrites int
}

type supplyChainDeploymentContext struct {
	factID         string
	artifactDigest string
	imageRef       string
	repositoryID   string
	environment    string
	outcome        string
	provenanceOnly bool
}

type supplyChainDeploymentLaneContext struct {
	factID        string
	repositoryID  string
	deploymentIDs []string
}

type supplyChainWorkloadContext struct {
	factID       string
	repositoryID string
	workloadID   string
}

type supplyChainServiceContext struct {
	factID         string
	repositoryID   string
	serviceID      string
	workloadID     string
	entityRef      string
	ownerRef       string
	outcome        string
	driftStatus    string
	provenanceOnly bool
}

type supplyChainRiskSignals struct {
	epssFactID      string
	epssProbability string
	epssPercentile  string
	kevFactID       string
	knownExploited  bool
}

type supplyChainImpactIndex struct {
	cves                    []supplyChainImpactCVE
	affectedPackages        map[string][]supplyChainAffectedPackage
	affectedProducts        map[string][]supplyChainAffectedProduct
	consumption             map[string][]supplyChainPackageConsumption
	osPackages              map[string][]supplyChainOSPackage
	components              []supplyChainSBOMComponent
	attachments             map[string]supplyChainAttachment
	images                  map[string]supplyChainImageIdentity
	deployments             []supplyChainDeploymentContext
	deploymentLanes         []supplyChainDeploymentLaneContext
	workloads               []supplyChainWorkloadContext
	services                []supplyChainServiceContext
	riskSignals             map[string]supplyChainRiskSignals
	goReachability          map[string]GoVulnerabilityFinding
	jsTSPackageReachability jsTSPackageReachabilityIndex
	pythonReachability      map[string]pythonReachabilityRepositoryEvidence
	jvmReachability         jvmReachabilityIndex
}

// buildSupplyChainImpactIndex and buildSupplyChainImpactIndexWithQuarantine
// live in supply_chain_impact_index_build.go (split out to keep this file
// under the repo's 500-line cap).

func classifySupplyChainImpactPackage(
	cves supplyChainCVEGroup,
	pkgs []supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := baseSupplyChainImpactFinding(cves, pkgs, index)
	pkg := representativeAffectedPackage(pkgs)
	component, attachment, image, hasComponentPath, imagePathMissing := firstSBOMImpactPath(pkg, index)
	consumption := firstConsumption(pkg.packageID, index.consumption)
	osPackage, hasOSPackage := firstOSPackageImpactPath(pkg, index)
	if consumption.factID != "" {
		finding.RepositoryID = consumption.repositoryID
		finding.RequestedRange = firstNonBlank(
			strings.TrimSpace(consumption.requestedRange),
			strings.TrimSpace(consumption.dependencyRange),
		)
		finding.DependencyScope = strings.TrimSpace(consumption.dependencyScope)
		finding.DependencyPath = append([]string(nil), consumption.dependencyPath...)
		finding.DependencyDepth = consumption.dependencyDepth
		if consumption.directDependency != nil {
			value := *consumption.directDependency
			finding.DirectDependency = &value
		}
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, consumption.factID)
		finding.EvidencePath = append(finding.EvidencePath, firstNonBlank(consumption.evidenceKind, packageConsumptionCorrelationFactKind))
		finding.ObservedVersion = strings.TrimSpace(consumption.observedVersion)
		if finding.ObservedVersion == "" {
			if manifestVersion, ok := exactConsumptionDependencyVersion(finding.Ecosystem, consumption); ok {
				finding.ObservedVersion = manifestVersion
			}
		}
	}
	if hasComponentPath {
		finding.PURL = firstNonBlank(component.purl, finding.PURL)
		finding.ObservedVersion = firstNonBlank(component.version, finding.ObservedVersion)
		finding.SubjectDigest = attachment.subjectDigest
		finding.ImageRef = image.imageRef
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, component.factID, attachment.factID, image.factID)
		finding.EvidencePath = append(finding.EvidencePath, facts.SBOMComponentFactKind, sbomAttestationAttachmentFactKind, containerImageIdentityFactKind)
		if image.repositoryID != "" {
			finding.RepositoryID = image.repositoryID
		}
	}
	if hasOSPackage {
		finding.PURL = firstNonBlank(osPackage.purl, finding.PURL)
		finding.ObservedVersion = firstNonBlank(osPackage.installedVersion, finding.ObservedVersion)
		finding.SubjectDigest = firstNonBlank(osPackage.scopeID, finding.SubjectDigest)
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, osPackage.factID)
		finding.EvidencePath = append(finding.EvidencePath, facts.VulnerabilityOSPackageFactKind)
	}
	versionDecision := evaluateSupplyChainVersionMatch(
		finding.Ecosystem,
		finding.ObservedVersion,
		finding.RequestedRange,
		finding.FixedVersion,
		pkgs,
	)
	consumptionMissing := supplyChainConsumptionMissingEvidence(consumption)
	if consumption.factID != "" && versionDecision.Status == SupplyChainImpactAffectedExact {
		applySupplyChainVersionDecision(&finding, versionDecision)
		reachabilityMissing := applyPackageSupplyChainReachability(&finding, consumption, pkgs, index)
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, consumptionMissing, reachabilityMissing)
		return finding
	}
	if versionDecision.Status == SupplyChainImpactNotAffectedKnownFixed {
		applySupplyChainVersionDecision(&finding, versionDecision)
		reachabilityMissing := applyPackageSupplyChainReachability(&finding, consumption, pkgs, index)
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, consumptionMissing, reachabilityMissing)
		return finding
	}
	if versionDecision.FailClosed {
		applySupplyChainVersionDecision(&finding, versionDecision)
		reachabilityMissing := applyPackageSupplyChainReachability(&finding, consumption, pkgs, index)
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, consumptionMissing, reachabilityMissing)
		return finding
	}
	if hasOSPackage && versionDecision.Status == SupplyChainImpactAffectedExact {
		applySupplyChainVersionDecision(&finding, versionDecision)
		finding.RuntimeReachability = "image_os_package"
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing)
		return finding
	}
	if hasComponentPath {
		finding.Status = SupplyChainImpactAffectedDerived
		finding.Confidence = "derived"
		finding.MatchReason = "sbom_component_path"
		finding.RuntimeReachability = "image_sbom"
		finding.CanonicalWrites = 1
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, consumptionMissing)
		return finding
	}
	finding.Status = SupplyChainImpactPossiblyAffected
	finding.Confidence = "partial"
	finding.MatchReason = versionDecision.Reason
	finding.RuntimeReachability = "unknown"
	finding.CanonicalWrites = 1
	reachabilityMissing := applyPackageSupplyChainReachability(&finding, consumption, pkgs, index)
	finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, consumptionMissing, reachabilityMissing)
	return finding
}

func applyPackageSupplyChainReachability(
	finding *SupplyChainImpactFinding,
	consumption supplyChainPackageConsumption,
	pkgs []supplyChainAffectedPackage,
	index supplyChainImpactIndex,
) []string {
	missing := applyGoSupplyChainReachability(finding, pkgs, index)
	missing = append(missing, applyJSTSPackageReachability(finding, index)...)
	missing = append(missing, applyPythonSupplyChainReachability(finding, pkgs, index)...)
	missing = append(missing, applyJVMSupplyChainReachability(finding, consumption, index)...)
	return uniqueSortedStrings(missing)
}

func applySupplyChainVersionDecision(
	finding *SupplyChainImpactFinding,
	decision supplyChainVersionMatchDecision,
) {
	finding.Status = decision.Status
	finding.Confidence = decision.Confidence
	finding.MatchReason = decision.Reason
	finding.RuntimeReachability = decision.RuntimeReachability
	if finding.RuntimeReachability == "" {
		finding.RuntimeReachability = "unknown"
	}
	finding.CanonicalWrites = 1
	finding.MissingEvidence = combinedMissingImpactEvidence(*finding, decision.MissingEvidence)
}

func supplyChainConsumptionMissingEvidence(consumption supplyChainPackageConsumption) []string {
	if consumption.factID == "" || !consumption.partialEvidence {
		return nil
	}
	var missing []string
	if property := strings.TrimSpace(consumption.unresolvedMSBuildProperty); property != "" {
		missing = append(missing, "msbuild property unresolved: "+property)
	}
	if property := strings.TrimSpace(consumption.ambiguousMSBuildProperty); property != "" {
		missing = append(missing, "msbuild property ambiguous: "+property)
	}
	return uniqueSortedStrings(missing)
}

func combinedMissingImpactEvidence(finding SupplyChainImpactFinding, extra []string) []string {
	missing := missingImpactEvidence(finding)
	missing = append(missing, extra...)
	missing = suppressGenericServiceMissingEvidence(missing)
	return uniqueSortedStrings(missing)
}

func suppressGenericServiceMissingEvidence(missing []string) []string {
	if !hasSpecificServiceCatalogMissingEvidence(missing) {
		return missing
	}
	out := make([]string, 0, len(missing))
	for _, value := range missing {
		switch value {
		case "service evidence missing", "service catalog correlation evidence missing":
			continue
		default:
			out = append(out, value)
		}
	}
	return out
}

func hasSpecificServiceCatalogMissingEvidence(missing []string) bool {
	for _, value := range missing {
		switch value {
		case "service/workload catalog anchor missing",
			"service catalog evidence provenance-only",
			"service catalog evidence stale",
			"service catalog evidence ambiguous",
			"service catalog evidence rejected",
			"service catalog evidence unresolved",
			"service catalog evidence unsupported":
			return true
		}
	}
	return false
}
