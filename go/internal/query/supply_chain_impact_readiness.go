package query

import (
	"context"
	"sort"
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
	// ReadinessStateUnsupported means observed target evidence belongs to a
	// family Eshu does not yet support for vulnerability matching.
	ReadinessStateUnsupported SupplyChainImpactReadinessState = "unsupported"
	// ReadinessStateReadyZeroFindings means required evidence is present, the
	// reducer ran, and no impact finding matched the scope.
	ReadinessStateReadyZeroFindings SupplyChainImpactReadinessState = "ready_zero_findings"
	// ReadinessStateReadyWithFindings means required evidence is present and
	// reducer-owned impact findings exist for the scope.
	ReadinessStateReadyWithFindings SupplyChainImpactReadinessState = "ready_with_findings"
	// ReadinessStateReadinessUnavailable means the readiness lookup itself
	// failed; the findings page is still returned but its coverage cannot be
	// classified. Callers must not interpret zero findings as safe in this
	// state.
	ReadinessStateReadinessUnavailable SupplyChainImpactReadinessState = "readiness_unavailable"
)

// SupplyChainImpactReadinessEnvelope is the readiness payload attached to a
// vulnerability impact response so a UI, MCP client, or operator can tell
// "nothing matched" from "Eshu has not collected the evidence yet."
type SupplyChainImpactReadinessEnvelope struct {
	State              SupplyChainImpactReadinessState   `json:"readiness_state"`
	TargetScope        SupplyChainImpactTargetScope      `json:"target_scope"`
	EvidenceSources    []SupplyChainImpactEvidenceFamily `json:"evidence_sources"`
	MissingEvidence    []string                          `json:"missing_evidence,omitempty"`
	UnsupportedTargets []string                          `json:"unsupported_targets,omitempty"`
	IncompleteReasons  []string                          `json:"incomplete_reasons,omitempty"`
	Freshness          string                            `json:"freshness"`
	Counts             SupplyChainImpactReadinessCounts  `json:"counts"`
}

// SupplyChainImpactTargetScope echoes the bounded anchors the caller used so
// the readiness verdict is reproducible without re-deriving query parameters.
type SupplyChainImpactTargetScope struct {
	CVEID         string `json:"cve_id,omitempty"`
	PackageID     string `json:"package_id,omitempty"`
	RepositoryID  string `json:"repository_id,omitempty"`
	SubjectDigest string `json:"subject_digest,omitempty"`
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
// scope value used to echo TargetScope back to the caller.
type SupplyChainImpactReadinessQuery struct {
	CVEID         string
	PackageID     string
	RepositoryID  string
	SubjectDigest string
	ImpactStatus  string
}

// SupplyChainImpactReadinessSnapshot is the source-only evidence summary the
// readiness store returns. The handler classifies the readiness state from
// this snapshot plus the findings page; the store never invents findings.
type SupplyChainImpactReadinessSnapshot struct {
	EvidenceSources    []SupplyChainImpactEvidenceFamily
	UnsupportedTargets []string
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
	// MissingEvidenceSBOMOrImage signals no SBOM, attestation, or container
	// image identity facts for an image-scoped request.
	MissingEvidenceSBOMOrImage = "sbom_or_image_evidence"
	// MissingEvidenceTargetCollection signals an ingestion target reported
	// partial collection for the scope.
	MissingEvidenceTargetCollection = "target_collection_incomplete"
	// MissingEvidenceUnsupportedTarget signals observed evidence belongs to an
	// unsupported target family.
	MissingEvidenceUnsupportedTarget = "unsupported_target"
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
		// "missing advisory sources".
		missing = nil
	}
	incompleteReasons := uniqueSortedReadinessStrings(snapshot.IncompleteReasons)
	if state != ReadinessStateTargetIncomplete {
		incompleteReasons = nil
	}
	return SupplyChainImpactReadinessEnvelope{
		State:              state,
		TargetScope:        scope,
		EvidenceSources:    sources,
		MissingEvidence:    missing,
		UnsupportedTargets: uniqueSortedReadinessStrings(snapshot.UnsupportedTargets),
		IncompleteReasons:  incompleteReasons,
		Freshness:          aggregateReadinessFreshness(sources),
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
		evidenceFactCount(sources, EvidenceFamilyPackageConsumption) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyPackageRegistry) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMComponent) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyContainerImageIdentity) == 0 {
		return ReadinessStateNotConfigured
	}
	if readinessMissingContains(missing, MissingEvidenceUnsupportedTarget) && advisoryCount == 0 {
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
	if evidenceFactCount(sources, EvidenceFamilyVulnerabilityAdvisory) == 0 {
		missing = append(missing, MissingEvidenceAdvisorySources)
	}
	if scopeRequiresOwnedPackages(scope) &&
		evidenceFactCount(sources, EvidenceFamilyPackageConsumption) == 0 &&
		evidenceFactCount(sources, EvidenceFamilyPackageRegistry) == 0 {
		missing = append(missing, MissingEvidenceOwnedPackages)
	}
	if scopeRequiresImageEvidence(scope) &&
		evidenceFactCount(sources, EvidenceFamilyContainerImageIdentity) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMComponent) == 0 &&
		evidenceFactCount(sources, EvidenceFamilySBOMAttestation) == 0 {
		missing = append(missing, MissingEvidenceSBOMOrImage)
	}
	if len(snapshot.UnsupportedTargets) > 0 {
		missing = append(missing, MissingEvidenceUnsupportedTarget)
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

func scopeRequiresImageEvidence(scope SupplyChainImpactTargetScope) bool {
	return scope.SubjectDigest != ""
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

// allowedEvidenceFamilies is the closed set of family identifiers the
// envelope is allowed to surface. Anything emitted by the readiness store
// outside this set is dropped to prevent silent contract drift between the
// SQL family literals and the Go classifier.
var allowedEvidenceFamilies = map[string]struct{}{
	EvidenceFamilyVulnerabilityAdvisory:       {},
	EvidenceFamilyVulnerabilityExploitability: {},
	EvidenceFamilyPackageConsumption:          {},
	EvidenceFamilyPackageRegistry:             {},
	EvidenceFamilySBOMComponent:               {},
	EvidenceFamilySBOMAttestation:             {},
	EvidenceFamilyContainerImageIdentity:      {},
}

func normalizeEvidenceSources(sources []SupplyChainImpactEvidenceFamily) []SupplyChainImpactEvidenceFamily {
	if len(sources) == 0 {
		return []SupplyChainImpactEvidenceFamily{}
	}
	cloned := make([]SupplyChainImpactEvidenceFamily, 0, len(sources))
	for _, family := range sources {
		name := strings.TrimSpace(family.Family)
		if name == "" {
			continue
		}
		if _, ok := allowedEvidenceFamilies[name]; !ok {
			continue
		}
		cloned = append(cloned, family)
	}
	sort.SliceStable(cloned, func(i, j int) bool {
		return cloned[i].Family < cloned[j].Family
	})
	return cloned
}

func uniqueSortedReadinessStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	sort.Strings(unique)
	return unique
}

func readinessMissingContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
