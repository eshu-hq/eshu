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
	generationID         string
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

// supplyChainScannerAnalysis is the reducer's internal projection of one
// scanner_worker.analysis envelope: the sibling fact that carries the real,
// content-addressed image digest/reference for the image a scanner_worker
// analyzer (including the OS-package analyzer) inspected. os_package facts
// only carry an opaque ScopeID (a scan-target locator, never a sha256), so
// classifySupplyChainImpactPackage joins an os_package to its sibling
// analysis by ScopeID+GenerationID (supplyChainScopeGenerationKey) to anchor
// SubjectDigest on a real image digest instead of the scope_id.
type supplyChainScannerAnalysis struct {
	factID         string
	scopeID        string
	generationID   string
	imageDigest    string
	imageReference string
}

// supplyChainScopeGenerationKey returns the composite key
// (ScopeID+GenerationID) supplyChainImpactIndex.scannerAnalyses is keyed by,
// and the same key classifySupplyChainImpactPackage looks an os_package's
// sibling scanner_worker.analysis up by. It defers to facts.Envelope's own
// ScopeGenerationKey formatting so the reducer's join key stays byte-identical
// to the durable scope-generation boundary the rest of the platform uses,
// rather than re-deriving an equivalent format locally.
func supplyChainScopeGenerationKey(scopeID, generationID string) string {
	return facts.Envelope{ScopeID: scopeID, GenerationID: generationID}.ScopeGenerationKey()
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
	imageRef     string
	repositoryID string
	// sourceRepositoryIDs is the full, deliberately broader set of git source
	// repository ids the reducer_container_image_identity decision attributed
	// this image to -- build evidence AND weaker scope/deploy references alike
	// (container_image_identity.go ContainerImageIdentityDecision.SourceRepositoryIDs).
	sourceRepositoryIDs []string
	// buildProvenanceRepositoryIDs is the strong-evidence-only subset: an OCI
	// config source label, a CI run, or verified SLSA provenance (never a mere
	// deploy/scope reference). singleSupplyChainImageSourceRepositoryID ranks
	// this ahead of sourceRepositoryIDs (#5801) so a label-derived repository is
	// not blanked out as ambiguous merely because a weaker scope anchor also
	// names a different repository for the same image.
	buildProvenanceRepositoryIDs []string
	outcome                      string
	canonicalWrites              int
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
	scannerAnalyses         map[string]supplyChainScannerAnalysis
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
	// repoFromConsumption is the single provenance signal both image-evidence
	// branches below gate on: the SBOM branch must not overwrite a
	// consumption-derived anchor with the image's OCI registry path (#5780),
	// and the os_package branch must not overwrite it with the image-identity
	// source anchor (#5779). It is hoisted here (rather than computed inside
	// the os_package branch) so the SBOM branch, which runs first, can see it.
	repoFromConsumption := consumption.factID != "" && strings.TrimSpace(consumption.repositoryID) != ""
	var reconciliationMissing []string
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
		// image.repositoryID is the OCI/container registry's OWN repository
		// identifier ("oci-registry://..."), a namespace disjoint from every git
		// "repository:..." entity id that matchingSupplyChainWorkloads/Services/
		// DeploymentLanes join on by exact equality — so it is a dead anchor,
		// unreachable from runtime context (#5463). Overwrite only a replaceable
		// anchor (blank, or an OCI path this branch itself set on an earlier
		// package): a consumption-derived git repository ("github.com/...", set
		// by the manifest-dependency path above) is the more precise per-package
		// anchor and MUST survive. Without this guard a finding carrying both a
		// consumption anchor and SBOM evidence, but no os_package evidence to
		// reach the repair below, shipped the dead OCI path (#5780) — the same
		// precedence the os_package branch enforces via #5779.
		if image.repositoryID != "" && !repoFromConsumption {
			finding.RepositoryID = image.repositoryID
		}
	}
	if hasOSPackage {
		finding.PURL = firstNonBlank(osPackage.purl, finding.PURL)
		finding.ObservedVersion = firstNonBlank(osPackage.installedVersion, finding.ObservedVersion)
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, osPackage.factID)
		finding.EvidencePath = append(finding.EvidencePath, facts.VulnerabilityOSPackageFactKind)
		// An os_package fact's ScopeID is an opaque scan-target locator, never a
		// sha256 digest — it MUST NOT stand in for SubjectDigest. The real digest
		// only exists on the sibling scanner_worker.analysis fact the same
		// scanner_worker analyzer emitted for the same ScopeID+GenerationID. When
		// no sibling analysis is indexed (or it decoded with a blank digest),
		// SubjectDigest stays whatever it already was (empty, or an SBOM-derived
		// digest set earlier) rather than falling back to the scope_id.
		analysisKey := supplyChainScopeGenerationKey(osPackage.scopeID, osPackage.generationID)
		if analysis, ok := index.scannerAnalyses[analysisKey]; ok && analysis.imageDigest != "" {
			finding.SubjectDigest = analysis.imageDigest
			finding.ImageRef = firstNonBlank(analysis.imageReference, finding.ImageRef)
			finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, analysis.factID)
			finding.EvidencePath = append(finding.EvidencePath, facts.ScannerWorkerAnalysisFactKind)
		}
		// Anchor RepositoryID from the scanned image's own identity fact
		// (issue #5464) — loaded cross-scope by
		// loadSupplyChainImpactResolvedDigestEvidenceFacts, seeded with the
		// digest just stamped above — when nothing has claimed RepositoryID
		// yet. This MUST NOT overwrite a RepositoryID the consumption path
		// (package manifest evidence, higher up in this function) has already
		// set: a per-package manifest anchor is more precise than an
		// image-level identity, which resolves to whichever repository the
		// image's identity evidence agrees on -- a source-repository
		// consensus first, falling back to the image's own build provenance
		// only when that broader consensus is ambiguous (the tier A > tier B
		// rule singleSupplyChainImageSourceRepositoryID and
		// preferSupplyChainImageIdentity implement, #5813) -- and can be
		// shared across many unrelated packages.
		//
		// Deliberately reads image.sourceRepositoryIDs, NOT image.repositoryID
		// (unlike the SBOM path above): repositoryID is the OCI/container
		// registry's OWN repository identifier (e.g.
		// "oci-registry://ghcr.io/org/repo", see ociRepositoryID in
		// container_image_identity_registry.go) — a namespace disjoint from
		// every git-source Repository entity id. matchingSupplyChainWorkloads/
		// DeploymentLanes/Services (supply_chain_impact_runtime.go) compare
		// finding.RepositoryID by exact equality against workload/service/
		// deployment-lane records, which are always the git "repository:..." id
		// (supplyChainWorkloadRepositoryID/repositoryIDFromReducerScope); an OCI
		// registry path can never match one, so joining on repositoryID here
		// would produce a non-blank RepositoryID that is permanently unable to
		// reach workload/service/environment context — unit-green, dead in
		// production, exactly what #5463 exists to prevent (#5464 STEP 1
		// finding). sourceRepositoryIDs carries the git repository the identity
		// decision attributed the image to (CI-run/SLSA/source-label evidence).
		// Only used when unambiguous — singleSupplyChainImageSourceRepositoryID
		// returns "" for zero or multiple distinct repositories, matching the
		// #5463 "never invent an anchor" discipline: an image attributable to
		// more than one repository is not attributed to any single one. Do not
		// "simplify" this back to image.repositoryID.
		// #5464: prefer the git source anchor. Skip when consumption
		// already set RepositoryID (consumption always wins). Otherwise,
		// apply the anchor unless the current value IS already a git
		// repo ID (prefix "repository:") — this replaces OCI registry
		// paths set by the SBOM branch while preserving consumption-
		// derived IDs regardless of their format (consumption IDs come
		// from manifest evidence and use un-prefixed formats like
		// "github.com/org/repo").
		if finding.SubjectDigest != "" && !repoFromConsumption &&
			!strings.HasPrefix(finding.RepositoryID, "repository:") {
			if image, ok := index.images[finding.SubjectDigest]; ok {
				if repositoryID := singleSupplyChainImageSourceRepositoryID(image); repositoryID != "" {
					finding.RepositoryID = repositoryID
					finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, image.factID)
					finding.EvidencePath = append(finding.EvidencePath, containerImageIdentityFactKind)
					// #5468: cross-check scanner digest against every other
					// identity for the same repository — if CI declared a
					// different digest for this repo, surface the disagreement
					// as explicit missing_evidence. Collected into a local so
					// the value survives finalizeSupplyChainImpactFinding's
					// recomputation of MissingEvidence (which overwrites
					// whatever was set here — finalize passes its variadic
					// arguments through to combinedMissingImpactEvidence).
					reconciliationMissing = reconcileSupplyChainScannerIdentityDigest(
						finding.SubjectDigest, repositoryID, index.images,
					)
				}
			}
		}
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
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, consumptionMissing, reachabilityMissing, reconciliationMissing)
		return finding
	}
	if versionDecision.Status == SupplyChainImpactNotAffectedKnownFixed {
		applySupplyChainVersionDecision(&finding, versionDecision)
		reachabilityMissing := applyPackageSupplyChainReachability(&finding, consumption, pkgs, index)
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, consumptionMissing, reachabilityMissing, reconciliationMissing)
		return finding
	}
	if versionDecision.FailClosed {
		applySupplyChainVersionDecision(&finding, versionDecision)
		reachabilityMissing := applyPackageSupplyChainReachability(&finding, consumption, pkgs, index)
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, consumptionMissing, reachabilityMissing, reconciliationMissing)
		return finding
	}
	if hasOSPackage && versionDecision.Status == SupplyChainImpactAffectedExact {
		applySupplyChainVersionDecision(&finding, versionDecision)
		finding.RuntimeReachability = "image_os_package"
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, reconciliationMissing)
		return finding
	}
	if hasComponentPath {
		finding.Status = SupplyChainImpactAffectedDerived
		finding.Confidence = "derived"
		finding.MatchReason = "sbom_component_path"
		finding.RuntimeReachability = "image_sbom"
		finding.CanonicalWrites = 1
		finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, consumptionMissing, reconciliationMissing)
		return finding
	}
	finding.Status = SupplyChainImpactPossiblyAffected
	finding.Confidence = "partial"
	finding.MatchReason = versionDecision.Reason
	finding.RuntimeReachability = "unknown"
	finding.CanonicalWrites = 1
	reachabilityMissing := applyPackageSupplyChainReachability(&finding, consumption, pkgs, index)
	finalizeSupplyChainImpactFinding(&finding, index, versionDecision.MissingEvidence, imagePathMissing, consumptionMissing, reachabilityMissing, reconciliationMissing)
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
