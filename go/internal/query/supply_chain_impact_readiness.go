// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

// SupplyChainImpactReadinessState classifies one bounded vulnerability impact
// answer so callers can tell missing target coverage from a clean result.
type SupplyChainImpactReadinessState string

const (
	// ReadinessStateNotConfigured means Eshu has no advisory ingestion for the
	// requested scope, so a zero-finding answer cannot be interpreted as safe.
	ReadinessStateNotConfigured SupplyChainImpactReadinessState = "not_configured"
	// ReadinessStateTargetIncomplete means at least one required target source
	// reported partial or in-flight collection for the requested scope.
	ReadinessStateTargetIncomplete SupplyChainImpactReadinessState = "target_incomplete"
	// ReadinessStateEvidenceIncomplete means some target evidence exists but a
	// required join family is missing for the requested scope.
	ReadinessStateEvidenceIncomplete SupplyChainImpactReadinessState = "evidence_incomplete"
	// ReadinessStateReadyZeroFindings means required evidence is present, the
	// reducer ran, and no impact finding matched the scope.
	ReadinessStateReadyZeroFindings SupplyChainImpactReadinessState = "ready_zero_findings"
	// ReadinessStateReadyWithFindings means required evidence is present and
	// reducer-owned impact findings exist for the scope.
	ReadinessStateReadyWithFindings SupplyChainImpactReadinessState = "ready_with_findings"
	// ReadinessStateAmbiguousScope means the bounded scope matched multiple
	// reducer-owned impact findings, so callers must narrow the request before
	// interpreting readiness as clean or affected.
	ReadinessStateAmbiguousScope SupplyChainImpactReadinessState = "ambiguous_scope"
	// ReadinessStateReadinessUnavailable means the readiness lookup itself
	// failed; the findings page is still returned but its coverage cannot be
	// classified. Callers must not interpret zero findings as safe in this
	// state.
	ReadinessStateReadinessUnavailable SupplyChainImpactReadinessState = "readiness_unavailable"
	// ReadinessStateUnsupported means Eshu observed real target evidence
	// (owned dependency in an unsupported ecosystem, package-manager file
	// flagged with an unsupported lockfile feature, VCS/path/URL/editable
	// dependency source, malformed/unsupported SBOM, oversized
	// package-registry metadata, or unsupported image manifest) the matcher
	// cannot resolve into a finding. Callers must not interpret this as clean
	// or affected.
	ReadinessStateUnsupported SupplyChainImpactReadinessState = "unsupported"
)

// SupplyChainImpactReadinessEnvelope is the readiness payload attached to a
// vulnerability impact response so a UI, MCP client, or operator can tell
// "nothing matched" from "Eshu has not collected the evidence yet."
type SupplyChainImpactReadinessEnvelope struct {
	State              SupplyChainImpactReadinessState      `json:"readiness_state"`
	TargetScope        SupplyChainImpactTargetScope         `json:"target_scope"`
	EvidenceSources    []SupplyChainImpactEvidenceFamily    `json:"evidence_sources"`
	SourceSnapshots    []SupplyChainImpactSourceSnapshot    `json:"source_snapshots,omitempty"`
	SourceStates       []SupplyChainImpactSourceState       `json:"source_states,omitempty"`
	UnsupportedTargets []SupplyChainImpactUnsupportedTarget `json:"unsupported_targets,omitempty"`
	MissingEvidence    []string                             `json:"missing_evidence,omitempty"`
	IncompleteReasons  []string                             `json:"incomplete_reasons,omitempty"`
	Freshness          string                               `json:"freshness"`
	Counts             SupplyChainImpactReadinessCounts     `json:"counts"`
}

// SupplyChainImpactTargetScope echoes the bounded anchors the caller used so
// the readiness verdict is reproducible without re-deriving query parameters.
type SupplyChainImpactTargetScope struct {
	CVEID         string `json:"cve_id,omitempty"`
	AdvisoryID    string `json:"advisory_id,omitempty"`
	PackageID     string `json:"package_id,omitempty"`
	RepositoryID  string `json:"repository_id,omitempty"`
	SubjectDigest string `json:"subject_digest,omitempty"`
	ImageRef      string `json:"image_ref,omitempty"`
	Ecosystem     string `json:"ecosystem,omitempty"`
	WorkloadID    string `json:"workload_id,omitempty"`
	ServiceID     string `json:"service_id,omitempty"`
	Environment   string `json:"environment,omitempty"`
	Severity      string `json:"severity,omitempty"`
	ImpactStatus  string `json:"impact_status,omitempty"`
}

// SupplyChainImpactEvidenceFamily summarizes one source-evidence family for the
// requested scope without leaking package names or advisory bodies.
type SupplyChainImpactEvidenceFamily struct {
	Family           string `json:"family"`
	FactCount        int    `json:"fact_count"`
	LatestObservedAt string `json:"latest_observed_at,omitempty"`
	Freshness        string `json:"freshness,omitempty"`
}

// SupplyChainImpactSourceSnapshot exposes source-cache and observation metadata
// for vulnerability source snapshots without returning raw advisory payloads.
type SupplyChainImpactSourceSnapshot struct {
	Source               string `json:"source"`
	Ecosystem            string `json:"ecosystem,omitempty"`
	CacheArtifactVersion string `json:"cache_artifact_version,omitempty"`
	SnapshotDigest       string `json:"snapshot_digest,omitempty"`
	LastUpdatedAt        string `json:"last_updated_at,omitempty"`
	Freshness            string `json:"freshness,omitempty"`
	Complete             bool   `json:"complete"`
	WarningCode          string `json:"warning_code,omitempty"`
	WarningMessage       string `json:"warning_message,omitempty"`
}

// SupplyChainImpactReadinessCounts surfaces enough numeric coverage to diagnose
// a zero or partial answer without exposing raw payloads.
type SupplyChainImpactReadinessCounts struct {
	FindingsReturned   int            `json:"findings_returned"`
	FindingsTruncated  bool           `json:"findings_truncated"`
	FindingsByStatus   map[string]int `json:"findings_by_status,omitempty"`
	EvidenceFactsTotal int            `json:"evidence_facts_total"`
}

// SupplyChainImpactReadinessQuery is the bounded readiness lookup the handler
// runs alongside the findings page. ImpactStatus is intentionally not used by
// the source-fact counts because source facts have no impact-status field;
// it is preserved here so the call site can build the query from the same
// scope value used to echo TargetScope back to the caller. AdvisoryID narrows
// source-advisory rows only when another fact anchor is present.
type SupplyChainImpactReadinessQuery struct {
	CVEID         string
	AdvisoryID    string
	PackageID     string
	RepositoryID  string
	SubjectDigest string
	ImageRef      string
	Ecosystem     string
	WorkloadID    string
	ServiceID     string
	Environment   string
	Severity      string
	ImpactStatus  string
}

// hasFactAnchor reports whether the query carries an anchor that source facts
// can be filtered by. impact_status is a reducer-finding attribute that does
// not appear on source facts, so an impact_status-only query has no anchor.
func (q SupplyChainImpactReadinessQuery) hasFactAnchor() bool {
	return strings.TrimSpace(q.CVEID) != "" ||
		strings.TrimSpace(q.PackageID) != "" ||
		strings.TrimSpace(q.RepositoryID) != "" ||
		strings.TrimSpace(q.SubjectDigest) != "" ||
		strings.TrimSpace(q.ImageRef) != ""
}

// SupplyChainImpactReadinessSnapshot is the source-only evidence summary the
// readiness store returns. The handler classifies the readiness state from
// this snapshot plus the findings page; the store never invents findings.
type SupplyChainImpactReadinessSnapshot struct {
	EvidenceSources    []SupplyChainImpactEvidenceFamily
	SourceSnapshots    []SupplyChainImpactSourceSnapshot
	SourceStates       []SupplyChainImpactSourceState
	UnsupportedTargets []SupplyChainImpactUnsupportedTarget
	TargetIncomplete   bool
	IncompleteReasons  []string
}

// SupplyChainImpactReadinessStore reads bounded source-fact counts so the
// handler can build a readiness envelope without traversing the graph.
type SupplyChainImpactReadinessStore interface {
	ReadSupplyChainImpactReadiness(
		context.Context,
		SupplyChainImpactReadinessQuery,
	) (SupplyChainImpactReadinessSnapshot, error)
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

// BuildSupplyChainImpactReadiness combines the bounded findings page with a
// source-evidence snapshot to produce one readiness envelope. The function is
// deterministic and never mutates its inputs.
func BuildSupplyChainImpactReadiness(
	scope SupplyChainImpactTargetScope,
	findings []SupplyChainImpactFindingResult,
	truncated bool,
	snapshot SupplyChainImpactReadinessSnapshot,
) SupplyChainImpactReadinessEnvelope {
	sources := normalizeEvidenceSources(snapshot.EvidenceSources)
	sourceStates := normalizeSourceStates(snapshot.SourceStates)
	snapshot.SourceStates = sourceStates
	if sourceStatesIncomplete(sourceStates) {
		snapshot.TargetIncomplete = true
		snapshot.IncompleteReasons = append(snapshot.IncompleteReasons, sourceStateIncompleteReasons(sourceStates)...)
	}
	unsupportedTargets := normalizeUnsupportedTargets(snapshot.UnsupportedTargets)
	snapshot.UnsupportedTargets = unsupportedTargets
	counts := SupplyChainImpactReadinessCounts{
		FindingsReturned:   len(findings),
		FindingsTruncated:  truncated,
		FindingsByStatus:   countFindingsByStatus(findings),
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
	incompleteReasons := uniqueSortedReadinessStrings(snapshot.IncompleteReasons)
	if state != ReadinessStateTargetIncomplete {
		incompleteReasons = nil
	}
	freshness := aggregateReadinessFreshness(sources)
	freshness = combineReadinessFreshness(freshness, aggregateSourceStateFreshness(sourceStates))
	return SupplyChainImpactReadinessEnvelope{
		State:              state,
		TargetScope:        scope,
		EvidenceSources:    sources,
		SourceSnapshots:    normalizeSourceSnapshots(snapshot.SourceSnapshots),
		SourceStates:       sourceStates,
		UnsupportedTargets: unsupportedTargets,
		MissingEvidence:    missing,
		IncompleteReasons:  incompleteReasons,
		Freshness:          freshness,
		Counts:             counts,
	}
}

// BuildSupplyChainImpactReadinessUnavailable returns a readiness envelope used
// when the readiness lookup itself failed. The findings page is still returned
// to the caller but the envelope explicitly says coverage cannot be classified.
func BuildSupplyChainImpactReadinessUnavailable(
	scope SupplyChainImpactTargetScope,
	findings []SupplyChainImpactFindingResult,
	truncated bool,
) SupplyChainImpactReadinessEnvelope {
	return SupplyChainImpactReadinessEnvelope{
		State:           ReadinessStateReadinessUnavailable,
		TargetScope:     scope,
		EvidenceSources: []SupplyChainImpactEvidenceFamily{},
		MissingEvidence: []string{MissingEvidenceReadinessUnavailable},
		Freshness:       FreshnessLabelUnknown,
		Counts: SupplyChainImpactReadinessCounts{
			FindingsReturned:  len(findings),
			FindingsTruncated: truncated,
			FindingsByStatus:  countFindingsByStatus(findings),
		},
	}
}

func classifyReadinessState(
	findings []SupplyChainImpactFindingResult,
	sources []SupplyChainImpactEvidenceFamily,
	snapshot SupplyChainImpactReadinessSnapshot,
	missing []string,
) SupplyChainImpactReadinessState {
	if len(findings) > 0 {
		return ReadinessStateReadyWithFindings
	}
	advisoryCount := evidenceFactCount(sources, EvidenceFamilyVulnerabilityAdvisory)
	// target_incomplete is only meaningful when scope-relevant advisory
	// evidence is still missing. If advisory facts already exist for the
	// scope, an in-flight snapshot for an unrelated source does not change
	// the answer for this caller.
	if advisoryCount == 0 && snapshot.TargetIncomplete {
		return ReadinessStateTargetIncomplete
	}
	if advisoryCount == 0 &&
		len(snapshot.SourceStates) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyPackageConsumption) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyPackageRegistry) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMComponent) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyContainerImageIdentity) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyScannerWorkerAnalysis) == 0 {
		// A scanner_worker.analysis fact is real observed coverage for the
		// requested image even when every other family is empty (issue
		// #5467): the OS-package scan tier can be the ONLY evidence Eshu has
		// for a scanned image, and that must not read as "not configured".
		return ReadinessStateNotConfigured
	}
	// Unsupported target evidence outranks both evidence_incomplete AND
	// ready_zero_findings: when Eshu observed real target evidence the
	// matcher cannot resolve, the answer is not "we are missing data" or
	// "we ran cleanly" but "we have data we cannot match." Surfacing
	// ready_zero_findings here would let callers read the response as
	// clean while a real coverage gap exists. Findings still outrank
	// unsupported (handled by the early ready_with_findings return above)
	// because an admitted finding is the reducer's positive verdict;
	// unsupported_targets[] is then surfaced additively so the gap is
	// still visible.
	if len(snapshot.UnsupportedTargets) > 0 {
		return ReadinessStateUnsupported
	}
	if len(missing) > 0 {
		return ReadinessStateEvidenceIncomplete
	}
	return ReadinessStateReadyZeroFindings
}

func classifyMissingEvidence(
	scope SupplyChainImpactTargetScope,
	sources []SupplyChainImpactEvidenceFamily,
	snapshot SupplyChainImpactReadinessSnapshot,
) []string {
	var missing []string
	if snapshot.TargetIncomplete &&
		evidenceFactCount(sources, EvidenceFamilyVulnerabilityAdvisory) == 0 {
		missing = append(missing, MissingEvidenceTargetCollection)
	}
	if evidenceFactCount(sources, EvidenceFamilyVulnerabilityAdvisory) == 0 &&
		!sourceStatesHaveFreshSuccess(snapshot.SourceStates) {
		missing = append(missing, MissingEvidenceAdvisorySources)
	}
	if advisoryFreshness := evidenceFamilyFreshness(sources, EvidenceFamilyVulnerabilityAdvisory); advisoryFreshness == FreshnessLabelStale {
		missing = append(missing, MissingEvidenceAdvisorySources)
	}
	// package.registry without a package_id anchor is global metadata, not
	// proof of consumption for the requested repository. Repo-only scopes
	// must demonstrate ownership through package.consumption (manifest or
	// reducer correlation); registry counts only satisfy owned_packages when
	// the request is anchored on a specific package.
	if scopeRequiresOwnedPackages(scope) {
		consumption := evidenceFactCount(sources, EvidenceFamilyPackageConsumption)
		registry := evidenceFactCount(sources, EvidenceFamilyPackageRegistry)
		switch {
		case consumption == 0 && scope.PackageID == "":
			missing = append(missing, MissingEvidenceOwnedPackages)
		case consumption == 0 && registry == 0:
			missing = append(missing, MissingEvidenceOwnedPackages)
		}
	}
	if scopeRequiresPackageRegistryMetadata(scope, sources) {
		registryFreshness := evidenceFamilyFreshness(sources, EvidenceFamilyPackageRegistry)
		if evidenceFactCount(sources, EvidenceFamilyPackageRegistry) == 0 ||
			registryFreshness == FreshnessLabelStale ||
			registryFreshness == FreshnessLabelUnknown {
			missing = append(missing, MissingEvidencePackageRegistryMetadata)
		}
	}
	if scopeRequiresImageEvidence(scope) &&
		evidenceFactCount(sources, EvidenceFamilyContainerImageIdentity) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMComponent) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMAttestation) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyScannerWorkerAnalysis) == 0 {
		// A scanner_worker.analysis fact for the requested image proves the
		// OS-package scan tier ran, independent of SBOM/image-identity
		// evidence (issue #5467). vulnerability.os_package is deliberately
		// NOT checked here: a completed scan can legitimately find zero
		// installed packages (a distroless image), so os_package==0 must not
		// reopen this gate once scanner_worker.analysis already proves the
		// scan happened.
		missing = append(missing, MissingEvidenceSBOMOrImage)
	}
	if scopeRequiresServiceOrWorkloadEvidence(scope) {
		missing = append(missing, serviceCatalogAnchorMissingReason)
	}
	if len(snapshot.UnsupportedTargets) > 0 {
		missing = append(missing, MissingEvidenceUnsupportedTargets)
	}
	return uniqueSortedReadinessStrings(missing)
}

func isReadyState(state SupplyChainImpactReadinessState) bool {
	switch state {
	case ReadinessStateReadyWithFindings, ReadinessStateReadyZeroFindings:
		return true
	default:
		return false
	}
}

func scopeRequiresOwnedPackages(scope SupplyChainImpactTargetScope) bool {
	return scope.RepositoryID != "" || scope.PackageID != ""
}

func scopeRequiresPackageRegistryMetadata(scope SupplyChainImpactTargetScope, sources []SupplyChainImpactEvidenceFamily) bool {
	if scope.PackageID != "" {
		return true
	}
	return scope.RepositoryID != "" && evidenceFactCount(sources, EvidenceFamilyPackageConsumption) > 0
}

func scopeRequiresImageEvidence(scope SupplyChainImpactTargetScope) bool {
	return scope.SubjectDigest != "" || scope.ImageRef != ""
}

func scopeRequiresServiceOrWorkloadEvidence(scope SupplyChainImpactTargetScope) bool {
	return scope.WorkloadID != "" || scope.ServiceID != ""
}

func countFindingsByStatus(findings []SupplyChainImpactFindingResult) map[string]int {
	if len(findings) == 0 {
		return nil
	}
	counts := make(map[string]int, len(findings))
	for _, finding := range findings {
		if finding.ImpactStatus == "" {
			continue
		}
		counts[finding.ImpactStatus]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func sumEvidenceFactCount(sources []SupplyChainImpactEvidenceFamily) int {
	total := 0
	for _, family := range sources {
		total += family.FactCount
	}
	return total
}

func evidenceFactCount(sources []SupplyChainImpactEvidenceFamily, name string) int {
	for _, family := range sources {
		if family.Family == name {
			return family.FactCount
		}
	}
	return 0
}

func evidenceFamilyFreshness(sources []SupplyChainImpactEvidenceFamily, name string) string {
	freshness := FreshnessLabelUnknown
	for _, family := range sources {
		if family.Family == name {
			freshness = combineReadinessFreshness(freshness, family.Freshness)
		}
	}
	return freshness
}

func aggregateReadinessFreshness(sources []SupplyChainImpactEvidenceFamily) string {
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
