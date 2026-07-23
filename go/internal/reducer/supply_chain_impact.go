// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

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
	totalStarted := time.Now()

	if intent.Domain != DomainSupplyChainImpact {
		return Result{}, fmt.Errorf("supply_chain_impact handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("supply chain impact fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("supply chain impact writer is required")
	}

	loaded, timing, err := h.loadSupplyChainImpactEvidence(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	envelopes := loaded.envelopes

	phaseStarted := time.Now()
	findings, quarantinedVulnerabilityFacts, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// partitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return Result{}, fmt.Errorf("build supply chain impact findings: %w", err)
	}
	// Per-fact isolation: a malformed vulnerability.* fact (a missing required
	// identity field) is quarantined as a visible input_invalid dead-letter —
	// counter + structured error log — while every valid fact still
	// contributes to the findings computed above.
	inputInvalidCount := recordQuarantinedFacts(ctx, h.Instruments, DomainSupplyChainImpact, intent.ScopeID, intent.GenerationID, quarantinedVulnerabilityFacts)
	if loaded.activeEvidenceTruncated {
		findings = markSupplyChainImpactFindingsActiveExpansionTruncated(findings)
	}
	timing.buildFindingsDuration = time.Since(phaseStarted)

	phaseStarted = time.Now()
	suppressions := BuildVulnerabilitySuppressions(envelopes)
	now := h.evaluationNow()
	for i := range findings {
		findings[i].Suppression = EvaluateSupplyChainSuppression(findings[i], suppressions, now)
	}
	counts := supplyChainImpactCounts(findings)
	suppressionCounts := supplyChainSuppressionCounts(findings)
	remediationCounts := supplyChainRemediationCounts(findings)
	timing.evaluateSuppressionsDuration = time.Since(phaseStarted)

	phaseStarted = time.Now()
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
	timing.writeFindingsDuration = time.Since(phaseStarted)

	phaseStarted = time.Now()
	h.emitCounters(ctx, counts, suppressionCounts, remediationCounts)
	timing.emitCountersDuration = time.Since(phaseStarted)
	timing.totalDuration = time.Since(totalStarted)

	evidenceSummary := supplyChainImpactSummary(len(findings), counts, suppressionCounts, writeResult.CanonicalWrites)
	if loaded.activeEvidenceTruncated {
		evidenceSummary += " active_evidence_truncated=true"
	}
	subSignals := supplyChainImpactDiagnosticSignals(
		loaded.scopeFacts,
		loaded.repositoryFacts,
		loaded.manifestDependencyFacts,
		loaded.activeEvidenceFacts,
		loaded.osPackageAdvisoryFacts,
		loaded.scannerAnalysisScopeFacts,
		loaded.pythonReachabilityFacts,
		loaded.jvmReachabilityFactCount,
		loaded.postSecurityAlertScopeFacts,
		loaded.securityAlertScopingApplied,
		loaded.securityAlertScopedOutFacts,
		len(findings),
		loaded.activeEvidenceTruncated,
		writeResult.FactsWritten,
	)
	for key, value := range inputInvalidSubSignals(inputInvalidCount) {
		subSignals[key] = value
	}
	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainSupplyChainImpact,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: evidenceSummary,
		CanonicalWrites: writeResult.CanonicalWrites,
		SubDurations:    supplyChainImpactSubDurations(timing),
		SubSignals:      subSignals,
	}, nil
}

// BuildSupplyChainImpactFindings classifies vulnerability source facts against
// explicit package, SBOM, image, and repository evidence.
//
// Multi-source CVE and affected_package observations for the same advisory
// identity are consolidated into one finding so callers see a single row per
// (cve_id, package_id) anchor with full per-source provenance, instead of one
// row per advisory source overwriting the others at the writer.
//
// A vulnerability.* fact whose payload is missing a required identity field is
// excluded from the index (mirroring the pre-typing behavior of dropping a
// fact with a blank required string), matching this function's fixed,
// error-free signature that the existing table tests already assert against.
// SupplyChainImpactHandler.Handle calls the quarantine-aware
// buildSupplyChainImpactFindingsWithQuarantine instead, so the reducer intent
// path still reports a visible input_invalid dead-letter (counter + structured
// log) for the malformed fact while this function stays a pure,
// table-test-friendly classifier with no telemetry side effects.
func BuildSupplyChainImpactFindings(envelopes []facts.Envelope) []SupplyChainImpactFinding {
	findings, _, _ := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	return findings
}

// buildSupplyChainImpactFindingsWithQuarantine is the quarantine-aware
// counterpart BuildSupplyChainImpactFindings delegates to and
// SupplyChainImpactHandler.Handle calls directly, so the reducer intent path
// can report each malformed vulnerability.* fact as a visible input_invalid
// dead-letter via recordQuarantinedFacts. A non-decode error (a fatal
// condition partitionDecodeFailures did not quarantine) is returned so the
// caller fails the whole intent for durable triage. The classification logic
// itself is unchanged from BuildSupplyChainImpactFindings.
func buildSupplyChainImpactFindingsWithQuarantine(envelopes []facts.Envelope) ([]SupplyChainImpactFinding, []quarantinedFact, error) {
	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
	if err != nil {
		return nil, nil, err
	}
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
	findings, securityAlertQuarantined, err := appendSecurityAlertImpactFindings(findings, envelopes, index)
	if err != nil {
		return nil, nil, err
	}
	// Merge the security-alert decode quarantines with the vulnerability
	// quarantines so SupplyChainImpactHandler.Handle records every malformed
	// fact of either family as a per-fact input_invalid dead-letter through the
	// same recordQuarantinedFacts path, and a poisoned security_alert fact never
	// aborts the whole supply_chain_impact generation.
	quarantined = append(quarantined, securityAlertQuarantined...)
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].CVEID != findings[j].CVEID {
			return findings[i].CVEID < findings[j].CVEID
		}
		if findings[i].PackageID != findings[j].PackageID {
			return findings[i].PackageID < findings[j].PackageID
		}
		return findings[i].ProductCriteria < findings[j].ProductCriteria
	})
	return findings, quarantined, nil
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

// supplyChainImpactFindingHasOwnedAnchor reports whether finding is backed by
// evidence this reducer actually observed, so appendSupplyChainImpactFinding
// keeps it rather than dropping an unanchored guess. RepositoryID and
// SubjectDigest are the two general anchors every ecosystem can supply. An
// os_package match is its own anchor even when neither of those is set: it
// already required repositoryClass=="vendor" plus a matching
// VendorAdvisorySource (osPackageMatchesAffectedPackage) before
// classifySupplyChainImpactPackage ever reached this finding, so the finding
// is anchored to a real scanned installation, not a guess — its accuracy does
// not depend on whether a sibling scanner_worker.analysis fact happened to
// resolve a real image digest for it (issue #5463 deleted the scope_id
// fallback that used to make SubjectDigest non-blank here as a side effect;
// this keeps the owned-anchor gate independently correct without resurrecting
// scope_id as a fake digest).
func supplyChainImpactFindingHasOwnedAnchor(finding SupplyChainImpactFinding) bool {
	if strings.TrimSpace(finding.RepositoryID) != "" || strings.TrimSpace(finding.SubjectDigest) != "" {
		return true
	}
	return slices.Contains(finding.EvidencePath, facts.VulnerabilityOSPackageFactKind)
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
