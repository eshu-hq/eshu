// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SupplyChainImpactStatus names the reducer decision for one vulnerability
// impact finding.
type SupplyChainImpactStatus string

const (
	// SupplyChainImpactAffectedExact means package identity and observed
	// version match the affected package evidence exactly.
	SupplyChainImpactAffectedExact SupplyChainImpactStatus = "affected_exact"
	// SupplyChainImpactAffectedDerived means impact follows from SBOM, image,
	// repository, or runtime joins after package identity is established.
	SupplyChainImpactAffectedDerived SupplyChainImpactStatus = "affected_derived"
	// SupplyChainImpactPossiblyAffected means advisory evidence exists but
	// package identity or version precision is incomplete.
	SupplyChainImpactPossiblyAffected SupplyChainImpactStatus = "possibly_affected"
	// SupplyChainImpactNotAffectedKnownFixed means the observed version is at
	// or beyond a source-reported fixed version under Eshu's conservative
	// numeric version comparison.
	SupplyChainImpactNotAffectedKnownFixed SupplyChainImpactStatus = "not_affected_known_fixed"
	// SupplyChainImpactUnknown means vulnerability source truth exists but Eshu
	// lacks enough package or runtime evidence to decide impact.
	SupplyChainImpactUnknown SupplyChainImpactStatus = "unknown_impact"
)

// SupplyChainImpactFactFilter bounds active evidence loading for one impact
// reducer intent.
type SupplyChainImpactFactFilter struct {
	PackageIDs        []string
	PURLs             []string
	CVEIDs            []string
	SubjectDigests    []string
	DocumentIDs       []string
	ProductCriteria   []string
	RepositoryIDs     []string
	FileRepositoryIDs []string
	ImageRefs         []string
}

// SupplyChainImpactFinding is one reducer-owned vulnerability impact finding.
//
// Severity, fixed-version, and vulnerable-range fields carry per-source
// provenance so admission preserves which advisory source supplied each
// selected value and what alternates other sources reported. Reducers select
// one value per field using documented ecosystem-aware source priority.
type SupplyChainImpactFinding struct {
	CVEID           string
	AdvisoryID      string
	PackageID       string
	Ecosystem       string
	PackageName     string
	PURL            string
	ProductCriteria string
	MatchCriteriaID string
	ObservedVersion string
	RequestedRange  string
	FixedVersion    string
	// VulnerableRange is the source-reported affected range expression for
	// the advisory the provenance selector picked. The reducer persists the
	// expression on the canonical finding payload so list-route callers see
	// the same vulnerable range as the explain route without re-loading raw
	// advisory facts.
	VulnerableRange       string
	MatchReason           string
	Status                SupplyChainImpactStatus
	Confidence            string
	CVSSScore             float64
	SeveritySource        string
	SeverityVector        string
	SeverityLabel         string
	AdvisoryPublishedAt   string
	AdvisoryUpdatedAt     string
	AlternateSeverities   []AlternateSeverity
	FixedVersionSource    string
	FixedVersionBranches  []FixedVersionBranch
	RangeSource           string
	AdvisorySources       []AdvisorySourceObservation
	EPSSProbability       string
	EPSSPercentile        string
	KnownExploited        bool
	PriorityReason        string
	PriorityScore         int
	PriorityBucket        string
	PriorityReasonCodes   []string
	PriorityContributions []SupplyChainImpactPriorityContribution
	RuntimeReachability   string
	Reachability          *SupplyChainReachability
	RepositoryID          string
	SubjectDigest         string
	ImageRef              string
	DependencyScope       string
	WorkloadIDs           []string
	DeploymentIDs         []string
	ServiceIDs            []string
	Environments          []string
	CatalogEntityRefs     []string
	CatalogOwnerRefs      []string
	DependencyPath        []string
	DependencyDepth       int
	DirectDependency      *bool
	MissingEvidence       []string
	EvidencePath          []string
	EvidenceFactIDs       []string
	CanonicalWrites       int
	// DetectionProfile records which tier this finding meets: precise for
	// exact installed-version anchors, comprehensive for range-only,
	// SBOM-derived, product-derived, malformed, or missing-version evidence.
	// Always set before the writer persists the row. Unsupported matcher
	// ecosystems are withheld from impact findings and surfaced through
	// readiness coverage gaps instead.
	DetectionProfile DetectionProfile
	// Suppression carries the VEX or operator-policy decision evaluated for
	// this finding. State is always populated; it is "active" when no
	// suppression applies. The writer persists the decision on the finding
	// payload so API and MCP callers can include or exclude suppressed
	// findings and explain the rationale.
	Suppression SupplyChainSuppressionDecision
	// Remediation is the advisory-only safe-upgrade recommendation Eshu
	// computes for this finding (issue #595). The reducer never auto-opens
	// pull requests; this block exists so API and MCP callers can explain
	// the upgrade path without re-reading raw advisory or lockfile facts.
	Remediation SupplyChainImpactRemediation
}

// SupplyChainImpactWrite carries findings for durable publication.
type SupplyChainImpactWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Findings     []SupplyChainImpactFinding
}

// SupplyChainImpactWriteResult summarizes durable impact publication.
type SupplyChainImpactWriteResult struct {
	CanonicalWrites int
	FactsWritten    int
	EvidenceSummary string
}

// SupplyChainImpactWriter persists reducer-owned impact findings.
type SupplyChainImpactWriter interface {
	WriteSupplyChainImpactFindings(context.Context, SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error)
}

type activeSupplyChainImpactFactLoader interface {
	ListActiveSupplyChainImpactFacts(context.Context, SupplyChainImpactFactFilter) ([]facts.Envelope, error)
}

// SupplyChainImpactHandler publishes vulnerability impact findings without
// turning CVSS, EPSS, or KEV signals into reachability proof.
type SupplyChainImpactHandler struct {
	FactLoader  FactLoader
	Writer      SupplyChainImpactWriter
	Instruments *telemetry.Instruments
	// Now lets tests pin the evaluation clock used for suppression
	// expiration checks. Defaults to time.Now() in UTC.
	Now func() time.Time
}

// Handle executes one supply-chain impact reducer intent.
func (h SupplyChainImpactHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainSupplyChainImpact {
		return Result{}, fmt.Errorf("supply_chain_impact handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("supply chain impact fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("supply chain impact writer is required")
	}

	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, supplyChainImpactFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load supply chain impact facts: %w", err)
	}
	repositories, err := h.loadActiveSupplyChainImpactRepositoryFacts(ctx, envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("load active supply chain impact repository facts: %w", err)
	}
	envelopes = append(envelopes, repositories...)
	manifestDependencies, err := h.loadActivePackageManifestDependencyFacts(ctx, envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("load active package manifest dependency facts: %w", err)
	}
	envelopes = append(envelopes, manifestDependencies...)
	activeEvidenceTruncated := false
	envelopes, activeEvidenceTruncated, err = h.loadActiveSupplyChainImpactFactsUntilStable(ctx, envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("load active supply chain impact facts: %w", err)
	}
	pythonReachabilityEvidence, err := h.loadPythonReachabilityEvidenceFacts(ctx, envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("load Python reachability evidence facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, pythonReachabilityEvidence...)
	jvmReachabilityFacts, err := h.loadActiveJVMReachabilityFacts(ctx, envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("load active JVM reachability facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, jvmReachabilityFacts...)
	if supplyChainImpactUsesSecurityAlertScope(intent, envelopes) {
		envelopes = scopeSupplyChainImpactEvidenceToSecurityAlerts(envelopes)
	}

	findings := BuildSupplyChainImpactFindings(envelopes)
	if activeEvidenceTruncated {
		findings = markSupplyChainImpactFindingsActiveExpansionTruncated(findings)
	}
	suppressions := BuildVulnerabilitySuppressions(envelopes)
	now := h.evaluationNow()
	for i := range findings {
		findings[i].Suppression = EvaluateSupplyChainSuppression(findings[i], suppressions, now)
	}
	counts := supplyChainImpactCounts(findings)
	suppressionCounts := supplyChainSuppressionCounts(findings)
	remediationCounts := supplyChainRemediationCounts(findings)
	writeResult, err := h.Writer.WriteSupplyChainImpactFindings(ctx, SupplyChainImpactWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Findings:     findings,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write supply chain impact findings: %w", err)
	}
	h.emitCounters(ctx, counts, suppressionCounts, remediationCounts)

	evidenceSummary := supplyChainImpactSummary(len(findings), counts, suppressionCounts, writeResult.CanonicalWrites)
	if activeEvidenceTruncated {
		evidenceSummary += " active_evidence_truncated=true"
	}
	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainSupplyChainImpact,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: evidenceSummary,
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func (h SupplyChainImpactHandler) evaluationNow() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeSupplyChainImpactFactLoader)
	if !ok || filter.empty() {
		return nil, nil
	}
	envelopes, err := loader.ListActiveSupplyChainImpactFacts(ctx, filter)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

const maxSupplyChainImpactActiveEvidenceLoads = 8

func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFactsUntilStable(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, bool, error) {
	requested := SupplyChainImpactFactFilter{}
	next := supplyChainImpactFilter(envelopes)
	for loads := 0; !next.empty(); loads++ {
		if loads >= maxSupplyChainImpactActiveEvidenceLoads {
			return envelopes, true, nil
		}
		active, err := h.loadActiveSupplyChainImpactFacts(ctx, next)
		if err != nil {
			return nil, false, err
		}
		requested = mergeSupplyChainImpactFactFilters(requested, next)
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, active...)
		next = supplyChainImpactFollowUpFilter(requested, supplyChainImpactFilter(envelopes))
	}
	return envelopes, false, nil
}

func (h SupplyChainImpactHandler) emitCounters(
	ctx context.Context,
	counts map[SupplyChainImpactStatus]int,
	suppressionCounts map[SupplyChainSuppressionState]int,
	remediationCounts map[supplyChainRemediationKey]int,
) {
	if h.Instruments == nil {
		return
	}
	for _, status := range supplyChainImpactStatuses() {
		if counts[status] == 0 {
			continue
		}
		h.Instruments.SupplyChainImpactFindings.Add(ctx, int64(counts[status]), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainSupplyChainImpact)),
			telemetry.AttrOutcome(string(status)),
		))
	}
	if h.Instruments.SupplyChainSuppressionDecisions != nil {
		for _, state := range SupplyChainSuppressionStates() {
			if suppressionCounts[state] == 0 {
				continue
			}
			h.Instruments.SupplyChainSuppressionDecisions.Add(ctx, int64(suppressionCounts[state]), metric.WithAttributes(
				telemetry.AttrDomain(string(DomainSupplyChainImpact)),
				telemetry.AttrOutcome(string(state)),
			))
		}
	}
	if h.Instruments.SupplyChainRemediationDecisions != nil {
		for key, count := range remediationCounts {
			if count == 0 {
				continue
			}
			h.Instruments.SupplyChainRemediationDecisions.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrDomain(string(DomainSupplyChainImpact)),
				telemetry.AttrOutcome(key.confidence),
				telemetry.AttrReason(key.reason),
			))
		}
	}
}

// supplyChainRemediationKey bounds the remediation counter cardinality to
// the closed product of (confidence, reason) labels.
type supplyChainRemediationKey struct {
	confidence string
	reason     string
}

func supplyChainRemediationCounts(findings []SupplyChainImpactFinding) map[supplyChainRemediationKey]int {
	out := make(map[supplyChainRemediationKey]int)
	for _, finding := range findings {
		confidence := strings.TrimSpace(finding.Remediation.Confidence)
		reason := strings.TrimSpace(finding.Remediation.Reason)
		if confidence == "" && reason == "" {
			continue
		}
		if confidence == "" {
			confidence = SupplyChainRemediationConfidenceUnknown
		}
		out[supplyChainRemediationKey{confidence: confidence, reason: reason}]++
	}
	return out
}

// BuildSupplyChainImpactFindings classifies vulnerability source facts against
// explicit package, SBOM, image, and repository evidence.
//
// Multi-source CVE and affected_package observations for the same advisory
// identity are consolidated into one finding so callers see a single row per
// (cve_id, package_id) anchor with full per-source provenance, instead of one
// row per advisory source overwriting the others at the writer.
func BuildSupplyChainImpactFindings(envelopes []facts.Envelope) []SupplyChainImpactFinding {
	index := buildSupplyChainImpactIndex(envelopes)
	cveGroups := groupSupplyChainCVEsByID(index.cves)
	findings := make([]SupplyChainImpactFinding, 0, len(index.affectedPackages)+len(index.affectedProducts))
	for _, cveID := range sortedCVEKeys(cveGroups) {
		group := cveGroups[cveID]
		affected := index.affectedPackages[cveID]
		if len(affected) > 0 {
			pkgGroups := groupSupplyChainAffectedByPackage(affected)
			for _, packageID := range sortedPackageKeys(pkgGroups) {
				pkgs := pkgGroups[packageID]
				findings = appendSupplyChainImpactFinding(findings, classifySupplyChainImpactPackage(group, pkgs, index))
			}
			continue
		}
		products := index.affectedProducts[cveID]
		if len(products) > 0 {
			for _, product := range products {
				findings = appendSupplyChainImpactFinding(findings, classifySupplyChainImpactProduct(group.representative(), product, index))
			}
			continue
		}
	}
	findings = appendSecurityAlertImpactFindings(findings, envelopes, index)
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].CVEID != findings[j].CVEID {
			return findings[i].CVEID < findings[j].CVEID
		}
		if findings[i].PackageID != findings[j].PackageID {
			return findings[i].PackageID < findings[j].PackageID
		}
		return findings[i].ProductCriteria < findings[j].ProductCriteria
	})
	return findings
}

func appendSupplyChainImpactFinding(
	findings []SupplyChainImpactFinding,
	finding SupplyChainImpactFinding,
) []SupplyChainImpactFinding {
	if !supplyChainImpactFindingHasOwnedAnchor(finding) {
		return findings
	}
	if supplyChainImpactFindingHasUnsupportedMatcher(finding) {
		return findings
	}
	finding.DetectionProfile = classifySupplyChainImpactDetectionProfile(finding)
	finding = withSupplyChainReachability(finding)
	finding = withSupplyChainImpactPriority(finding)
	finding.Remediation = BuildSupplyChainImpactRemediation(finding)
	return append(findings, finding)
}

func supplyChainImpactFindingHasOwnedAnchor(finding SupplyChainImpactFinding) bool {
	return strings.TrimSpace(finding.RepositoryID) != "" || strings.TrimSpace(finding.SubjectDigest) != ""
}

func supplyChainImpactFindingHasUnsupportedMatcher(finding SupplyChainImpactFinding) bool {
	if strings.TrimSpace(finding.MatchReason) != supplyChainVersionReasonUnsupportedEcosystem {
		return false
	}
	return normalizedSupplyChainVersionEcosystem(finding.Ecosystem) != "os"
}

func supplyChainImpactFactKinds() []string {
	return []string{
		facts.VulnerabilityCVEFactKind,
		facts.VulnerabilityAffectedPackageFactKind,
		facts.VulnerabilityAffectedProductFactKind,
		facts.VulnerabilityEPSSScoreFactKind,
		facts.VulnerabilityKnownExploitedFactKind,
		facts.VulnerabilitySuppressionFactKind,
		facts.VulnerabilityGoModuleEvidenceFactKind,
		facts.VulnerabilityGoCallReachabilityFactKind,
		facts.SecurityAlertRepositoryAlertFactKind,
		facts.PackageRegistryPackageFactKind,
		facts.SBOMComponentFactKind,
		facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageReferrerFactKind,
		sbomAttestationAttachmentFactKind,
		containerImageIdentityFactKind,
		packageConsumptionCorrelationFactKind,
		cicdRunCorrelationFactKind,
		platformMaterializationFactKind,
		workloadIdentityFactKind,
		serviceCatalogCorrelationFactKind,
		factKindFile,
	}
}

func supplyChainImpactStatuses() []SupplyChainImpactStatus {
	return []SupplyChainImpactStatus{
		SupplyChainImpactAffectedExact,
		SupplyChainImpactAffectedDerived,
		SupplyChainImpactPossiblyAffected,
		SupplyChainImpactNotAffectedKnownFixed,
		SupplyChainImpactUnknown,
	}
}
