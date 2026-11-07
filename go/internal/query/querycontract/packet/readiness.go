// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packet

// ReadinessSnapshot is the source-only evidence summary the
// readiness store returns. The handler classifies the readiness state from
// this snapshot plus the findings page; the store never invents findings.
type ReadinessSnapshot struct {
	EvidenceSources    []EvidenceFamily
	SourceSnapshots    []SourceSnapshot
	SourceStates       []SourceState
	UnsupportedTargets []UnsupportedTarget
	TargetIncomplete   bool
	IncompleteReasons  []string
}

const (
	// EvidenceFamilyVulnerabilityAdvisory groups CVE identity and affected
	// package/product source facts that anchor advisory matching.
	EvidenceFamilyVulnerabilityAdvisory = "vulnerability.advisory"
	// EvidenceFamilyVulnerabilityExploitability groups EPSS and KEV signals
	// used for prioritization context.
	EvidenceFamilyVulnerabilityExploitability = "vulnerability.exploitability"
	// EvidenceFamilyPackageConsumption groups owned package consumption facts
	// such as manifest dependencies and reducer-owned correlations.
	EvidenceFamilyPackageConsumption = "package.consumption"
	// EvidenceFamilyPackageRegistry groups package registry identity and
	// version metadata used for impact matching.
	EvidenceFamilyPackageRegistry = "package.registry"
	// EvidenceFamilySBOMComponent groups SBOM component source facts.
	EvidenceFamilySBOMComponent = "sbom.component"
	// EvidenceFamilySBOMAttestation groups reducer-owned SBOM attachment facts.
	EvidenceFamilySBOMAttestation = "sbom.attestation"
	// EvidenceFamilyContainerImageIdentity groups reducer-owned image identity
	// facts used for image-scoped impact matching.
	EvidenceFamilyContainerImageIdentity = "container_image.identity"
)

const (
	// FreshnessLabelFresh marks readiness backed by recent source observations.
	FreshnessLabelFresh = "fresh"
	// FreshnessLabelStale marks readiness backed by older source observations.
	FreshnessLabelStale = "stale"
	// FreshnessLabelUnknown marks readiness without observation timestamps.
	FreshnessLabelUnknown = "unknown"
)

const (
	// MissingEvidenceAdvisorySources signals no advisory facts for the scope.
	MissingEvidenceAdvisorySources = "advisory_sources"
	// MissingEvidenceOwnedPackages signals no owned package consumption facts
	// for the requested scope.
	MissingEvidenceOwnedPackages = "owned_packages"
	// MissingEvidencePackageRegistryMetadata signals missing or stale
	// package-registry metadata for a package or repository scope.
	MissingEvidencePackageRegistryMetadata = "package_registry_metadata"
	// MissingEvidenceSBOMOrImage signals no SBOM, attestation, or container
	// image identity facts for an image-scoped request.
	MissingEvidenceSBOMOrImage = "sbom_or_image_evidence"
	// MissingEvidenceTargetCollection signals an ingestion target reported
	// partial collection for the scope.
	MissingEvidenceTargetCollection = "target_collection_incomplete"
	// MissingEvidenceReadinessUnavailable signals the readiness lookup itself
	// failed; coverage cannot be classified for this response.
	MissingEvidenceReadinessUnavailable = "readiness_unavailable"
)

// BuildReadiness combines the bounded findings page with a
// source-evidence snapshot to produce one readiness envelope. The function is
// deterministic and never mutates its inputs.
func BuildReadiness(
	scope TargetScope,
	findings []FindingResult,
	truncated bool,
	snapshot ReadinessSnapshot,
) ReadinessEnvelope {
	sources := NormalizeEvidenceSources(snapshot.EvidenceSources)
	sourceStates := normalizeSourceStates(snapshot.SourceStates)
	snapshot.SourceStates = sourceStates
	if sourceSnapshotsIncomplete(snapshot.SourceSnapshots) {
		snapshot.TargetIncomplete = true
	}
	if sourceStatesIncomplete(sourceStates) {
		snapshot.TargetIncomplete = true
		snapshot.IncompleteReasons = append(snapshot.IncompleteReasons, sourceStateIncompleteReasons(sourceStates)...)
	}
	unsupportedTargets := NormalizeUnsupportedTargets(snapshot.UnsupportedTargets)
	snapshot.UnsupportedTargets = unsupportedTargets
	counts := ReadinessCounts{
		FindingsReturned:   len(findings),
		FindingsTruncated:  truncated,
		FindingsByStatus:   CountFindingsByStatus(findings),
		EvidenceFactsTotal: sumEvidenceFactCount(sources),
	}
	missing := classifyMissingEvidence(scope, sources, snapshot)
	state := classifyReadinessState(findings, sources, snapshot, missing)
	if isReadyState(state) {
		// A ready answer is internally consistent: clear missing-evidence
		// reasons so clients do not see "ready_with_findings" alongside
		// "missing advisory sources". Unsupported targets stay because
		// they are coverage-gap evidence, not a finding contradiction.
		missing = nil
	}
	incompleteReasons := UniqueSortedReadinessStrings(snapshot.IncompleteReasons)
	if state != ReadinessStateTargetIncomplete && state != ReadinessStateTargetSkipped {
		incompleteReasons = nil
	}
	freshness := aggregateReadinessFreshness(sources)
	freshness = combineReadinessFreshness(freshness, aggregateSourceStateFreshness(sourceStates))
	return ReadinessEnvelope{
		State:              state,
		TargetScope:        scope,
		EvidenceSources:    sources,
		SourceSnapshots:    NormalizeSourceSnapshots(snapshot.SourceSnapshots),
		SourceStates:       sourceStates,
		UnsupportedTargets: unsupportedTargets,
		MissingEvidence:    missing,
		IncompleteReasons:  incompleteReasons,
		Freshness:          freshness,
		Counts:             counts,
	}
}

// BuildReadinessUnavailable returns a readiness envelope used
// when the readiness lookup itself failed. The findings page is still returned
// to the caller but the envelope explicitly says coverage cannot be classified.
func BuildReadinessUnavailable(
	scope TargetScope,
	findings []FindingResult,
	truncated bool,
) ReadinessEnvelope {
	return ReadinessEnvelope{
		State:           ReadinessStateReadinessUnavailable,
		TargetScope:     scope,
		EvidenceSources: []EvidenceFamily{},
		MissingEvidence: []string{MissingEvidenceReadinessUnavailable},
		Freshness:       FreshnessLabelUnknown,
		Counts: ReadinessCounts{
			FindingsReturned:  len(findings),
			FindingsTruncated: truncated,
			FindingsByStatus:  CountFindingsByStatus(findings),
		},
	}
}

func scopeRequiresOwnedPackages(scope TargetScope) bool {
	return scope.RepositoryID != "" || scope.PackageID != ""
}

func scopeRequiresPackageRegistryMetadata(scope TargetScope, sources []EvidenceFamily) bool {
	if scope.PackageID != "" {
		return true
	}
	return scope.RepositoryID != "" && evidenceFactCount(sources, EvidenceFamilyPackageConsumption) > 0
}

func scopeRequiresImageEvidence(scope TargetScope) bool {
	return scope.SubjectDigest != "" || scope.ImageRef != ""
}

func scopeRequiresServiceOrWorkloadEvidence(scope TargetScope) bool {
	return scope.WorkloadID != "" || scope.ServiceID != ""
}

// CountFindingsByStatus counts findings by status.
func CountFindingsByStatus(findings []FindingResult) map[string]int {
	if len(findings) == 0 {
		return nil
	}
	counts := make(map[string]int, len(findings))
	for _, finding := range findings {
		if finding.Status == "" {
			continue
		}
		counts[finding.Status]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func sumEvidenceFactCount(sources []EvidenceFamily) int {
	total := 0
	for _, family := range sources {
		total += family.FactCount
	}
	return total
}

func evidenceFactCount(sources []EvidenceFamily, name string) int {
	for _, family := range sources {
		if family.Family == name {
			return family.FactCount
		}
	}
	return 0
}

func evidenceFamilyFreshness(sources []EvidenceFamily, name string) string {
	freshness := FreshnessLabelUnknown
	for _, family := range sources {
		if family.Family == name {
			freshness = combineReadinessFreshness(freshness, family.Freshness)
		}
	}
	return freshness
}

func aggregateReadinessFreshness(sources []EvidenceFamily) string {
	state := FreshnessLabelUnknown
	for _, family := range sources {
		switch family.Freshness {
		case FreshnessLabelStale:
			return FreshnessLabelStale
		case FreshnessLabelFresh:
			state = FreshnessLabelFresh
		}
	}
	return state
}
