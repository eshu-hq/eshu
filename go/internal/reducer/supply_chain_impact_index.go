// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packagecorrelation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// The 15 DTO types the supply-chain-impact family used to declare — 13 in
// this file (supplychainmodel.ImpactCVE, supplychainmodel.AffectedPackage,
// supplychainmodel.AffectedProduct, supplychainmodel.PackageConsumption,
// supplychainmodel.SBOMComponent, supplychainmodel.OSPackage,
// supplychainmodel.ScannerAnalysis, supplychainmodel.Attachment,
// supplychainmodel.DeploymentContext, supplychainmodel.DeploymentLaneContext,
// supplychainmodel.WorkloadContext, supplychainmodel.ServiceContext,
// supplychainmodel.RiskSignals) and 2 in the sibling
// supply_chain_impact_ranges.go (supplychainmodel.AffectedRange,
// supplychainmodel.AffectedRangeEvent) — plus the supplyChainScopeGenerationKey
// function, now supplychainmodel.ScopeGenerationKey, all moved to
// [supplychainmodel] (issue #6061 PR1); this file, and every other
// reducer-root file that used them, now spells the qualified
// supplychainmodel.* names directly. supplyChainImpactIndex below
// deliberately did not move — see that type's own doc comment.

// supplyChainImageIdentity, its envelope decode, and the anchor-tier ranking
// logic that resolves it to a single repository (both row-level and
// cross-row) live in supply_chain_impact_anchor_tier.go (split out to keep
// this file under the repo's 500-line cap).

// supplyChainImpactIndex aggregates every supply-chain-impact evidence
// cluster classifySupplyChainImpactPackage reads: the supplychainmodel DTOs
// (cves, affectedPackages, ..., scannerAnalyses) alongside four
// reachability-cluster indexes (goReachability, jsTSPackageReachability,
// pythonReachability, jvmReachability) and the container-image-identity
// index (images). Those five fields name types this PR's leaf package does
// not own, so the aggregate itself stays at the reducer root rather than
// dragging them into supplychainmodel; see
// supplychainmodel/README.md#ownership-boundary.
type supplyChainImpactIndex struct {
	cves                    []supplychainmodel.ImpactCVE
	affectedPackages        map[string][]supplychainmodel.AffectedPackage
	affectedProducts        map[string][]supplychainmodel.AffectedProduct
	consumption             map[string][]supplychainmodel.PackageConsumption
	osPackages              map[string][]supplychainmodel.OSPackage
	components              []supplychainmodel.SBOMComponent
	attachments             map[string]supplychainmodel.Attachment
	images                  map[string]supplyChainImageIdentity
	deployments             []supplychainmodel.DeploymentContext
	deploymentLanes         []supplychainmodel.DeploymentLaneContext
	workloads               []supplychainmodel.WorkloadContext
	services                []supplychainmodel.ServiceContext
	riskSignals             map[string]supplychainmodel.RiskSignals
	scannerAnalyses         map[string]supplychainmodel.ScannerAnalysis
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
	pkgs []supplychainmodel.AffectedPackage,
	index supplyChainImpactIndex,
) SupplyChainImpactFinding {
	finding := baseSupplyChainImpactFinding(cves, pkgs, index)
	pkg := representativeAffectedPackage(pkgs)
	component, attachment, image, hasComponentPath, imagePathMissing := firstSBOMImpactPath(pkg, index)
	consumption := firstConsumption(pkg.PackageID, index.consumption)
	osPackage, hasOSPackage := firstOSPackageImpactPath(pkg, index)
	// repoFromConsumption is the single provenance signal both image-evidence
	// branches below gate on: the SBOM branch must not overwrite a
	// consumption-derived anchor with the image's OCI registry path (#5780),
	// and the os_package branch must not overwrite it with the image-identity
	// source anchor (#5779). It is hoisted here (rather than computed inside
	// the os_package branch) so the SBOM branch, which runs first, can see it.
	repoFromConsumption := consumption.FactID != "" && strings.TrimSpace(consumption.RepositoryID) != ""
	var reconciliationMissing []string
	if consumption.FactID != "" {
		finding.RepositoryID = consumption.RepositoryID
		finding.RequestedRange = payloadcore.FirstNonBlank(
			strings.TrimSpace(consumption.RequestedRange),
			strings.TrimSpace(consumption.DependencyRange),
		)
		finding.DependencyScope = strings.TrimSpace(consumption.DependencyScope)
		finding.DependencyPath = append([]string(nil), consumption.DependencyPath...)
		finding.DependencyDepth = consumption.DependencyDepth
		if consumption.DirectDependency != nil {
			value := *consumption.DirectDependency
			finding.DirectDependency = &value
		}
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, consumption.FactID)
		finding.EvidencePath = append(finding.EvidencePath, payloadcore.FirstNonBlank(consumption.EvidenceKind, packagecorrelation.PackageConsumptionCorrelationFactKind))
		finding.ObservedVersion = strings.TrimSpace(consumption.ObservedVersion)
		if finding.ObservedVersion == "" {
			if manifestVersion, ok := exactConsumptionDependencyVersion(finding.Ecosystem, consumption); ok {
				finding.ObservedVersion = manifestVersion
			}
		}
	}
	if hasComponentPath {
		finding.PURL = payloadcore.FirstNonBlank(component.PURL, finding.PURL)
		finding.ObservedVersion = payloadcore.FirstNonBlank(component.Version, finding.ObservedVersion)
		finding.SubjectDigest = attachment.SubjectDigest
		finding.ImageRef = image.imageRef
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, component.FactID, attachment.FactID, image.factID)
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
		finding.PURL = payloadcore.FirstNonBlank(osPackage.PURL, finding.PURL)
		finding.ObservedVersion = payloadcore.FirstNonBlank(osPackage.InstalledVersion, finding.ObservedVersion)
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, osPackage.FactID)
		finding.EvidencePath = append(finding.EvidencePath, facts.VulnerabilityOSPackageFactKind)
		// An os_package fact's ScopeID is an opaque scan-target locator, never a
		// sha256 digest — it MUST NOT stand in for SubjectDigest. The real digest
		// only exists on the sibling scanner_worker.analysis fact the same
		// scanner_worker analyzer emitted for the same ScopeID+GenerationID. When
		// no sibling analysis is indexed (or it decoded with a blank digest),
		// SubjectDigest stays whatever it already was (empty, or an SBOM-derived
		// digest set earlier) rather than falling back to the scope_id.
		analysisKey := supplychainmodel.ScopeGenerationKey(osPackage.ScopeID, osPackage.GenerationID)
		if analysis, ok := index.scannerAnalyses[analysisKey]; ok && analysis.ImageDigest != "" {
			finding.SubjectDigest = analysis.ImageDigest
			finding.ImageRef = payloadcore.FirstNonBlank(analysis.ImageReference, finding.ImageRef)
			finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, analysis.FactID)
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
		// finding). sourceRepositoryIDs is deliberately broader than build
		// evidence alone: it carries every git repository the identity decision
		// attributed the image to, including a mere deploy/scope reference
		// (e.g. a Kubernetes manifest referencing a third-party digest), not
		// only CI-run/SLSA/source-label build provenance — see
		// singleSupplyChainImageSourceRepositoryID and the buildProvenanceRepositoryIDs
		// field comment above for the strong-evidence-only subset ranked ahead
		// of it. Only used when unambiguous — singleSupplyChainImageSourceRepositoryID
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
	if consumption.FactID != "" && versionDecision.Status == SupplyChainImpactAffectedExact {
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
	consumption supplychainmodel.PackageConsumption,
	pkgs []supplychainmodel.AffectedPackage,
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

func supplyChainConsumptionMissingEvidence(consumption supplychainmodel.PackageConsumption) []string {
	if consumption.FactID == "" || !consumption.PartialEvidence {
		return nil
	}
	var missing []string
	if property := strings.TrimSpace(consumption.UnresolvedMSBuildProperty); property != "" {
		missing = append(missing, "msbuild property unresolved: "+property)
	}
	if property := strings.TrimSpace(consumption.AmbiguousMSBuildProperty); property != "" {
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
