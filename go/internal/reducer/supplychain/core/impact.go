// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
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
	PackageIDs []string
	PURLs      []string
	CVEIDs     []string
	// AdvisoryIDs binds both the top-level exact-match predicate and the
	// normalized suppression-scope predicate. It is populated alongside
	// CVEIDs because supplyChainCVEID prefers cve_id over advisory_id when
	// both are present.
	AdvisoryIDs       []string
	SubjectDigests    []string
	DocumentIDs       []string
	ProductCriteria   []string
	RepositoryIDs     []string
	FileRepositoryIDs []string
	ImageRefs         []string
}

type activeSupplyChainImpactFactLoader interface {
	// The bool return reports that the vulnerability.suppression tail of the
	// read was bounded (#5466 P1-B, see handler_helpers.go). It must never
	// report truncated core evidence: the handler treats it as the
	// suppression-tail signal only, which does not block retraction (#7154), so
	// an implementation that bounds core evidence must not use it.
	ListActiveSupplyChainImpactFacts(context.Context, SupplyChainImpactFactFilter) ([]facts.Envelope, bool, error)
}

// SupplyChainImpactHandler publishes vulnerability impact findings without
// turning CVSS, EPSS, or KEV signals into reachability proof.
type SupplyChainImpactHandler struct {
	FactLoader  factload.FactLoader
	Writer      SupplyChainImpactWriter
	Instruments *telemetry.Instruments
	// AuthorizedEvidenceLoader supplies protected source and advisory
	// classifications before limits for every production impact intent.
	AuthorizedEvidenceLoader AuthorizedSupplyChainImpactEvidenceLoader
	// RequireAuthorizedEvidenceLoader closes the legacy static fallback when
	// this handler runs against a target-enabled production store.
	RequireAuthorizedEvidenceLoader bool
	// authorizedStaticOnly skips legacy target-specific expansion inside the protected snapshot.
	authorizedStaticOnly bool
	// authorizedStaticAdvisories are receipt-backed public facts seeded before static expansion.
	authorizedStaticAdvisories []facts.Envelope
	// Now lets tests pin the evaluation clock used for suppression
	// expiration checks. Defaults to time.Now() in UTC.
	Now func() time.Time
	// ProducerReadiness is the #5709 cross-scope correctness floor. This domain
	// reads container_image_identity output for repository anchoring and
	// ci_cd_run_correlation output for deployment context, both published by
	// scopes other than its own vulnerability-intelligence scope. An impact pass
	// that runs before those scopes activate their generations resolves neither,
	// and would otherwise write durable findings computed without that evidence
	// that no later event disturbs. When wired, such a pass defers instead.
	// Optional: nil keeps the pre-#5709 behaviour.
	ProducerReadiness crossscope.ProducerReadiness
	// ReadinessWaits is the (scope, domain) readiness-wait ledger that
	// anchors the producer-wait bound across superseding generations
	// (#6814). Nil keeps the pre-ledger per-row bound.
	ReadinessWaits crossscope.ReadinessWaitLedger
	// EvidenceBudget overrides the per-intent expansion evidence budget
	// (maxSupplyChainImpactEvidenceEnvelopesPerIntent) when positive.
	// Optional: zero keeps the default.
	EvidenceBudget int
	// FencingTokenIssuer supplies the per-pass fencing token (#7142). Required:
	// Handle fails closed without it, because a pass with no ordering value
	// cannot be fenced against a fresher pass.
	FencingTokenIssuer SupplyChainImpactFencingTokenIssuer
	// Logger records a cross-scope readiness deferral as its own structured
	// line. Optional: nil silences it. Worth wiring -- the deferral's failure
	// class freezes attempt_count, so the queue row alone cannot tell an
	// operator how long this consumer has been waiting.
	Logger         *slog.Logger
	ScopeRepos     handle.ScopeRepos // Nil resolves graph-wide (#5385).
	WorkloadLookup handle.Lookup     // Nil keeps legacy scopes (#5385).
	// FanOut reopens the scopes that hold stale copies of the CVEs this pass
	// changed (#9695). Optional: nil keeps the pre-fan-out behavior.
	FanOut SupplyChainImpactFanOut
	// Tracer wraps the fan-out in one span. Optional.
	Tracer trace.Tracer
}

// Handle executes one supply-chain impact reducer intent.
func (h SupplyChainImpactHandler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	totalStarted := time.Now()

	if intent.Domain != reducercontract.DomainSupplyChainImpact {
		return reducercontract.Result{}, fmt.Errorf("supply_chain_impact handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil && h.AuthorizedEvidenceLoader == nil {
		return reducercontract.Result{}, fmt.Errorf("supply chain impact evidence loader is required")
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("supply chain impact writer is required")
	}
	if h.FencingTokenIssuer == nil {
		return reducercontract.Result{}, fmt.Errorf("supply chain impact fencing token issuer is required")
	}

	// Issued BEFORE the evidence load, never at write-commit time: the token
	// orders passes by when they began reading evidence, so a pass that read its
	// evidence and then stalled cannot publish over a later one (#7142, see
	// SupplyChainImpactFencingTokenIssuer for what it does not order). A pass the
	// readiness floor defers below burns a value, a harmless sequence gap.
	fencingToken, err := h.FencingTokenIssuer.NextSupplyChainImpactFencingToken(ctx)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("issue supply chain impact fencing token: %w", err)
	}

	loaded, timing, err := h.loadSupplyChainImpactEvidence(ctx, intent)
	if err != nil {
		return reducercontract.Result{}, err
	}
	envelopes := loaded.envelopes

	phaseStarted := time.Now()
	findings, quarantinedVulnerabilityFacts, negatedProductsExcluded, err := buildAuthorizedSupplyChainImpactFindings(loaded)
	if err != nil {
		// A non-decode error (transient fact-load or other fatal condition
		// factdecode.PartitionDecodeFailures did NOT quarantine) fails the whole intent so
		// the durable queue triages it correctly.
		return reducercontract.Result{}, fmt.Errorf("build supply chain impact findings: %w", err)
	}
	if loaded.truncation.partial() {
		findings = markSupplyChainImpactFindingsActiveExpansionTruncated(findings)
	}
	timing.buildFindingsDuration = time.Since(phaseStarted)

	phaseStarted = time.Now()
	suppressionEnvelopes := envelopes
	if loaded.asAuthorized != nil {
		suppressionEnvelopes = loaded.asAuthorized.SuppressionEnvelopes
	}
	suppressions, quarantinedSuppressions, err := BuildVulnerabilitySuppressions(suppressionEnvelopes)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("build vulnerability suppressions: %w", err)
	}
	quarantinedVulnerabilityFacts = append(quarantinedVulnerabilityFacts, quarantinedSuppressions...)
	if loaded.suppressionEvidenceTruncated {
		// Selection is newest AuthoredAt and then SuppressionID across the
		// complete adjacent candidate set. A bounded prefix cannot prove its
		// retained winner is globally preferred, so discard every candidate
		// and keep findings visible rather than persist a false audit winner.
		suppressions = nil
	}
	suppressions, err = h.resolveSuppressionWorkloads(ctx, intent, suppressions)
	if err != nil {
		return reducercontract.Result{}, fmt.Errorf("resolve suppression workload scopes: %w", err)
	}
	// Per-fact isolation: a malformed vulnerability or suppression fact is
	// quarantined as a visible input_invalid dead-letter while valid facts
	// still contribute to findings and suppression decisions.
	inputInvalidCount := factdecode.RecordQuarantinedFacts(
		ctx,
		h.Instruments,
		reducercontract.DomainSupplyChainImpact,
		intent.ScopeID,
		intent.GenerationID,
		quarantinedVulnerabilityFacts,
	)
	applySupplyChainImpactSuppressions(findings, suppressions, h.evaluationNow())
	counts := supplyChainImpactCounts(findings)
	suppressionCounts := supplyChainSuppressionCounts(findings)
	remediationCounts := supplyChainRemediationCounts(findings)
	timing.evaluateSuppressionsDuration = time.Since(phaseStarted)
	emitAnchorProbe6702(ctx, h.Logger, intent, loaded.floorArmed, loaded.crossScopeProducerDelta, false, envelopes, findings)

	phaseStarted = time.Now()
	write := h.impactWrite(intent, findings, loaded, fencingToken)
	writeResult, err := h.Writer.WriteSupplyChainImpactFindings(ctx, write)
	if err != nil {
		h.emitWriteSuperseded(ctx, intent, fencingToken, err)
		return reducercontract.Result{}, fmt.Errorf("write supply chain impact findings: %w", err)
	}
	timing.writeFindingsDuration = time.Since(phaseStarted)

	// After the writer commit and before the queue acknowledgement (Handle
	// returns first): reopen the scopes that hold stale copies of what this pass
	// changed. A failed read or reopen fails the pass so the queue retries it.
	fanOut, err := h.fanOutHolders(ctx, intent, writeResult)
	if err != nil {
		return reducercontract.Result{}, err
	}

	phaseStarted = time.Now()
	h.emitCounters(ctx, counts, suppressionCounts, remediationCounts)
	h.emitSeverityDecisions(ctx, findings)
	h.emitRetraction(ctx, intent, writeResult.FactsRetracted)
	h.emitEvidenceTruncation(ctx, intent, loaded, len(findings))
	timing.emitCountersDuration = time.Since(phaseStarted)
	timing.totalDuration = time.Since(totalStarted)

	evidenceSummary := supplyChainImpactSummary(len(findings), counts, suppressionCounts, writeResult.CanonicalWrites)
	if loaded.truncation.partial() {
		evidenceSummary += " active_evidence_truncated=true"
	}
	if loaded.truncation.suppressionTail {
		evidenceSummary += " suppression_evidence_truncated=true"
	}
	subSignals := supplyChainImpactDiagnosticSignals(
		loaded.scopeFacts,
		loaded.repositoryFacts,
		loaded.manifestDependencyFacts,
		loaded.activeEvidenceFacts,
		loaded.osPackageAdvisoryFacts,
		loaded.osPackageAdvisoryTargetsSkipped,
		loaded.scannerAnalysisScopeFacts,
		loaded.resolvedDigestEvidenceFacts,
		loaded.pythonReachabilityFacts,
		loaded.jvmReachabilityFactCount,
		loaded.postSecurityAlertScopeFacts,
		loaded.securityAlertScopingApplied,
		loaded.securityAlertScopedOutFacts,
		len(findings),
		loaded.truncation.partial(),
		writeResult.FactsWritten,
	)
	subSignals["evidence_truncated_rounds"] = boolSignal(loaded.truncation.rounds)
	subSignals["evidence_truncated_budget"] = boolSignal(loaded.truncation.budget)
	subSignals["suppression_evidence_truncated"] = boolSignal(loaded.truncation.suppressionTail)
	subSignals["evidence_envelopes"] = float64(loaded.evidenceEnvelopeCount())
	subSignals["evidence_expansion_envelopes"] = float64(loaded.expansionEnvelopes)
	subSignals[advisoryRetractedFindingsSubSignal] = float64(supplyChainAdvisoryRetractedCount(findings))
	for key, value := range factdecode.InputInvalidSubSignals(inputInvalidCount) {
		subSignals[key] = value
	}
	subSignals["findings_retracted"] = float64(writeResult.FactsRetracted)
	fanOut.mergeSubSignals(subSignals)
	subSignals[negatedProductsExcludedSubSignal] = float64(negatedProductsExcluded)
	subDurations := supplyChainImpactSubDurations(timing)
	loaded.activeEvidence.mergeInto(subSignals, subDurations)
	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainSupplyChainImpact,
		Status:          reducercontract.ResultStatusSucceeded,
		EvidenceSummary: evidenceSummary,
		CanonicalWrites: writeResult.CanonicalWrites,
		SubDurations:    subDurations,
		SubSignals:      subSignals,
	}, nil
}

// impactWrite assembles the writer input for one pass: the findings, the
// truncation flag, the fencing token, the fan-out recovery and owed seams
// (#9695), and the protected scan publication witnesses when the evidence load
// was authorized.
func (h SupplyChainImpactHandler) impactWrite(
	intent reducercontract.Intent,
	findings []SupplyChainImpactFinding,
	loaded supplyChainImpactLoadedEvidence,
	fencingToken int64,
) SupplyChainImpactWrite {
	write := SupplyChainImpactWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Findings:     findings,
		// A load truncated for a cause that can change a finding's identity
		// is not the complete finding set, so the writer must not retract
		// rows it did not reach (#6831). A suppression-tail truncation does
		// not qualify: suppression is not part of a finding's identity, so
		// such a pass still converges (#7154).
		PartialEvidence: loaded.truncation.partial(),
		FencingToken:    fencingToken,
		Recovery:        fanOutOwed(intent) && len(fanOutRemaining(intent)) == 0,
		Owed:            h.owedRecorder(intent),
	}
	if loaded.asAuthorized != nil {
		write.ScanPublications = loaded.asAuthorized.ScanPublications
		write.StaticAdvisoryEvidence = loaded.asAuthorized.StaticAdvisoryEvidence
		write.StaticNonAdvisoryEvidenceFactIDs = loaded.asAuthorized.StaticNonAdvisoryEvidenceFactIDs
		write.ProtectedCompleteSet = true
	}
	return write
}

// applySupplyChainImpactSuppressions evaluates per-finding suppressions at one
// instant. A target-bound finding keeps the static view: no target-bound
// suppression authority is loaded yet, so it evaluates against no
// suppressions rather than a static scope rule.
func applySupplyChainImpactSuppressions(findings []SupplyChainImpactFinding, suppressions []vulnerabilitySuppression, now time.Time) {
	for i := range findings {
		if findings[i].ScanTargetID != "" {
			findings[i].Suppression = EvaluateSupplyChainSuppression(findings[i], nil, now)
			continue
		}
		findings[i].Suppression = EvaluateSupplyChainSuppression(findings[i], suppressions, now)
	}
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

// negatedProductsExcludedSubSignal is the Result.SubSignals key counting NVD CPE
// matches a pass excluded as negated; 0 when none, so a clean pass is visible (#9696).
const negatedProductsExcludedSubSignal = "negated_affected_products_excluded"

// buildSupplyChainImpactFindingsWithQuarantine is the quarantine-aware
// counterpart BuildSupplyChainImpactFindings delegates to. It wraps
// buildSupplyChainImpactFindingsAndExclusions, which Handle calls so the intent
// path can report each malformed vulnerability.* fact as a visible input_invalid
// dead-letter via factdecode.RecordQuarantinedFacts. A non-decode error is
// returned so the caller fails the whole intent for durable triage.
func buildSupplyChainImpactFindingsWithQuarantine(envelopes []facts.Envelope) ([]SupplyChainImpactFinding, []factdecode.QuarantinedFact, error) {
	findings, quarantined, _, err := buildSupplyChainImpactFindingsAndExclusions(envelopes)
	return findings, quarantined, err
}

// buildSupplyChainImpactFindingsAndExclusions is the quarantine-aware builder plus
// the count of NVD CPE matches the index excluded as negated.
func buildSupplyChainImpactFindingsAndExclusions(envelopes []facts.Envelope) ([]SupplyChainImpactFinding, []factdecode.QuarantinedFact, int, error) {
	index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
	if err != nil {
		return nil, nil, 0, err
	}
	findings := appendSupplyChainImpactIndexFindings(nil, index)
	findings, securityAlertQuarantined, err := appendSecurityAlertImpactFindings(findings, envelopes, index)
	if err != nil {
		return nil, nil, 0, err
	}
	// Merge the security-alert decode quarantines with the vulnerability
	// quarantines so SupplyChainImpactHandler.Handle records every malformed
	// fact of either family as a per-fact input_invalid dead-letter through the
	// same factdecode.RecordQuarantinedFacts path, and a poisoned security_alert fact never
	// aborts the whole supply_chain_impact generation.
	quarantined = append(quarantined, securityAlertQuarantined...)
	findings = coalesceAndSortSupplyChainImpactFindings(findings)
	return findings, quarantined, index.negatedProductsExcluded, nil
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
	return slices.Contains(finding.EvidencePath, chain.VulnerabilityOSPackageFactKind)
}

func supplyChainImpactFindingHasUnsupportedMatcher(finding SupplyChainImpactFinding) bool {
	if strings.TrimSpace(finding.MatchReason) != supplyChainVersionReasonUnsupportedEcosystem {
		return false
	}
	return normalizedSupplyChainVersionEcosystem(finding.Ecosystem) != "os"
}

// supplyChainImpactFactKinds is the set of fact kinds loaded from the intent's
// OWN scope/generation via factload.LoadFactsForKinds. It intentionally OMITS
// chain.VulnerabilityOSPackageFactKind and facts.ScannerWorkerAnalysisFactKind
// even though both feed supply-chain-impact classification: neither lives in
// the intent's vulnerability-intelligence scope, so listing them here would
// query the wrong scope and return nothing. They are loaded cross-scope by two
// dedicated stages in loadSupplyChainImpactEvidence instead —
// loadSupplyChainImpactOSPackageAdvisoryFacts pulls vulnerability.os_package by
// ecosystem through the advisory-target reader (#5705/#5463), and
// loadSupplyChainImpactScannerAnalysisScopeFacts then loads each os_package's
// sibling scanner_worker.analysis from that package's OWN scan scope to stamp
// SubjectDigest (#5463). Adding either kind here is the tempting-but-wrong fix
// flagged in review: it cannot reach a cross-scope fact. The end-to-end path is
// proven by TestSupplyChainImpactHandlerLoadsScannerAnalysisFromOSPackageScanScope
// and the golden-corpus list_supply_chain_impact_findings floor (CVE-2026-00010).
func supplyChainImpactFactKinds() []string {
	return []string{
		chain.VulnerabilityCVEFactKind,
		chain.VulnerabilityAffectedPackageFactKind,
		chain.VulnerabilityAffectedProductFactKind,
		chain.VulnerabilityEPSSScoreFactKind,
		chain.VulnerabilityKnownExploitedFactKind,
		chain.VulnerabilitySuppressionFactKind,
		chain.VulnerabilityGoModuleEvidenceFactKind,
		chain.VulnerabilityGoCallReachabilityFactKind,
		facts.SecurityAlertRepositoryAlertFactKind,
		chain.PackageRegistryPackageFactKind,
		chain.SBOMComponentFactKind,
		chain.OCIImageManifestFactKind,
		chain.OCIImageIndexFactKind,
		chain.OCIImageTagObservationFactKind,
		chain.OCIImageReferrerFactKind,
		reducercontract.SBOMAttestationAttachmentFactKind,
		reducercontract.ContainerImageIdentityFactKind,
		correlation.PackageConsumptionFactKind,
		cicdrun.CICDRunCorrelationFactKind,
		reducercontract.PlatformMaterializationFactKind,
		reducercontract.WorkloadIdentityFactKind,
		reducercontract.ServiceCatalogCorrelationFactKind,
		factload.FactKindFile,
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
