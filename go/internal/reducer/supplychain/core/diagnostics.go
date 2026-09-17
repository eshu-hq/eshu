// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// supplyChainImpactTiming records success-path phase timings for the
// supply_chain_impact reducer domain. These wrappers measure existing work only:
// they do not alter fact loading, matching, durable writes, or counter emission.
type supplyChainImpactTiming struct {
	loadScopeFactsDuration             time.Duration
	loadRepositoryFactsDuration        time.Duration
	loadManifestDependenciesDuration   time.Duration
	loadActiveEvidenceDuration         time.Duration
	loadOSPackageAdvisoryDuration      time.Duration
	loadScannerAnalysisScopeDuration   time.Duration
	loadResolvedDigestEvidenceDuration time.Duration
	loadPythonReachabilityDuration     time.Duration
	loadJVMReachabilityDuration        time.Duration
	securityAlertScopingDuration       time.Duration
	buildFindingsDuration              time.Duration
	evaluateSuppressionsDuration       time.Duration
	writeFindingsDuration              time.Duration
	emitCountersDuration               time.Duration
	totalDuration                      time.Duration
}

func supplyChainImpactSubDurations(t supplyChainImpactTiming) map[string]float64 {
	return map[string]float64{
		"load_scope_facts":              t.loadScopeFactsDuration.Seconds(),
		"load_repository_facts":         t.loadRepositoryFactsDuration.Seconds(),
		"load_manifest_dependencies":    t.loadManifestDependenciesDuration.Seconds(),
		"load_active_evidence":          t.loadActiveEvidenceDuration.Seconds(),
		"load_os_package_advisory":      t.loadOSPackageAdvisoryDuration.Seconds(),
		"load_scanner_analysis_scope":   t.loadScannerAnalysisScopeDuration.Seconds(),
		"load_resolved_digest_evidence": t.loadResolvedDigestEvidenceDuration.Seconds(),
		"load_python_reachability":      t.loadPythonReachabilityDuration.Seconds(),
		"load_jvm_reachability":         t.loadJVMReachabilityDuration.Seconds(),
		"security_alert_scoping":        t.securityAlertScopingDuration.Seconds(),
		"build_findings":                t.buildFindingsDuration.Seconds(),
		"evaluate_suppressions":         t.evaluateSuppressionsDuration.Seconds(),
		"write_findings":                t.writeFindingsDuration.Seconds(),
		"emit_counters":                 t.emitCountersDuration.Seconds(),
		"total":                         t.totalDuration.Seconds(),
	}
}

func supplyChainImpactDiagnosticSignals(
	scopeFacts int,
	repositoryFacts int,
	manifestDependencyFacts int,
	activeEvidenceFacts int,
	osPackageAdvisoryFacts int,
	osPackageAdvisoryTargetsSkipped int,
	scannerAnalysisScopeFacts int,
	resolvedDigestEvidenceFacts int,
	pythonReachabilityFacts int,
	jvmReachabilityFacts int,
	postScopeFacts int,
	securityAlertScopingApplied bool,
	securityAlertScopedOutFacts int,
	findings int,
	activeEvidenceTruncated bool,
	writtenRows int,
) map[string]float64 {
	inputReady := scopeFacts+
		repositoryFacts+
		manifestDependencyFacts+
		activeEvidenceFacts+
		osPackageAdvisoryFacts+
		scannerAnalysisScopeFacts+
		resolvedDigestEvidenceFacts+
		pythonReachabilityFacts+
		jvmReachabilityFacts > 0
	signals := reducercontract.MaterializationDiagnosticSignals(inputReady, writtenRows)
	signals["scope_facts"] = float64(scopeFacts)
	signals["repository_facts"] = float64(repositoryFacts)
	signals["manifest_dependency_facts"] = float64(manifestDependencyFacts)
	signals["active_evidence_facts"] = float64(activeEvidenceFacts)
	signals["os_package_advisory_facts"] = float64(osPackageAdvisoryFacts)
	signals["os_package_advisory_targets_skipped"] = float64(osPackageAdvisoryTargetsSkipped)
	signals["scanner_analysis_scope_facts"] = float64(scannerAnalysisScopeFacts)
	signals["resolved_digest_evidence_facts"] = float64(resolvedDigestEvidenceFacts)
	signals["python_reachability_facts"] = float64(pythonReachabilityFacts)
	signals["jvm_reachability_facts"] = float64(jvmReachabilityFacts)
	signals["post_scope_facts"] = float64(postScopeFacts)
	signals["security_alert_scoping_applied"] = boolSignal(securityAlertScopingApplied)
	signals["security_alert_scoped_out_facts"] = float64(securityAlertScopedOutFacts)
	signals["findings"] = float64(findings)
	signals["active_evidence_truncated"] = boolSignal(activeEvidenceTruncated)
	return signals
}

func boolSignal(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

// anchorProbe6702Digest is the pinned subject digest of the CVE-2026-00010
// OS-package finding the golden corpus asserts the repository anchor on.
// anchorProbe6702CVE selects the finding; anchorProbe6702Repository is the
// deploying repository the consensus winner must resolve to.
const (
	anchorProbe6702Digest     = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	anchorProbe6702CVE        = "CVE-2026-00010"
	anchorProbe6702Repository = "repository:r_217415d9"
)

// anchorProbe6702Class names the per-pass outcome the #6702 instrumented
// probe distinguishes. H1a/H1b are the two documented consumer-load residual
// windows from docs/internal/evidence/5709-supply-chain-consumer.md; H2 is
// writer variance (every identity row ambiguous). Healthy and the armed-but-
// silent case are recorded so a passing gate run is evidence too, not just
// reds.
type anchorProbe6702Class string

const (
	anchorProbe6702NotApplicable  anchorProbe6702Class = "not_applicable"
	anchorProbe6702Healthy        anchorProbe6702Class = "healthy"
	anchorProbe6702H1bNoFloor     anchorProbe6702Class = "h1b_no_floor"
	anchorProbe6702H1aBatchDisarm anchorProbe6702Class = "h1a_batch_disarm"
	anchorProbe6702H2AllTierC     anchorProbe6702Class = "h2_all_tier_c"
	anchorProbe6702EvidenceAbsent anchorProbe6702Class = "evidence_absent_armed"
	anchorProbe6702WrongWinner    anchorProbe6702Class = "wrong_winner"
	anchorProbe6702UnexpectedTier anchorProbe6702Class = "unexpected_tier"
)

// anchorProbe6702Result is the bounded per-pass capture for the pinned
// digest: identity row count by anchor tier, the consensus winner, and the
// finding's own anchor. It carries no envelopes, only counts and the small
// bounded strings the log line needs.
type anchorProbe6702Result struct {
	digestPresent     bool
	identityRows      int
	tierA             int
	tierB             int
	tierC             int
	winnerFound       bool
	winnerTier        int
	winnerRepository  string
	findingPresent    bool
	findingRepository string
	findingDigest     string
}

// probeSupplyChainAnchor6702 captures the #6702 probe inputs for one pass.
// It is pure: no logging, no deferral decision, safe to call from tests and
// both production call sites (commit and deferral paths).
func probeSupplyChainAnchor6702(envelopes []facts.Envelope, findings []SupplyChainImpactFinding) anchorProbe6702Result {
	var result anchorProbe6702Result
	result.winnerTier = -1
	for _, envelope := range envelopes {
		if envelope.FactKind != reducercontract.ContainerImageIdentityFactKind {
			continue
		}
		row := supplyChainImageIdentityFromEnvelope(envelope)
		if row.digest != anchorProbe6702Digest {
			continue
		}
		result.digestPresent = true
		result.identityRows++
		switch supplyChainImageIdentityAnchorTier(row) {
		case 0:
			result.tierA++
		case 1:
			result.tierB++
		default:
			result.tierC++
		}
	}
	// The consensus fold is skipped when no pinned-digest row is present:
	// ordinary generations then pay one kind-filter scan and no second
	// consensus pass (Handle already computes one while building findings).
	if result.digestPresent {
		if winners := bestSupplyChainImageIdentitiesByDigest(envelopes); winners != nil {
			if winner, ok := winners[anchorProbe6702Digest]; ok {
				result.winnerFound = true
				result.winnerTier = supplyChainImageIdentityAnchorTier(winner)
				result.winnerRepository = singleSupplyChainImageSourceRepositoryID(winner)
			}
		}
	}
	for _, finding := range findings {
		if finding.CVEID != anchorProbe6702CVE {
			continue
		}
		result.findingPresent = true
		result.findingRepository = finding.RepositoryID
		result.findingDigest = finding.SubjectDigest
		break
	}
	return result
}

// classifyAnchorProbe6702 maps one probe capture to its outcome class.
// floorArmed reports whether the #5709 floor could have deferred this pass
// (producer-reachable initial filter on a wired seam); batchProducerHits is
// the batch-wide cross-scope producer delta the floor decided on. The finding
// anchor decides when present; on the deferral path (no findings yet) the
// consensus winner stands in.
func classifyAnchorProbe6702(result anchorProbe6702Result, floorArmed bool, batchProducerHits int) anchorProbe6702Class {
	if !result.findingPresent && !result.digestPresent {
		return anchorProbe6702NotApplicable
	}
	anchor := result.winnerRepository
	if result.findingPresent {
		anchor = result.findingRepository
	}
	if anchor == anchorProbe6702Repository {
		return anchorProbe6702Healthy
	}
	if anchor != "" {
		return anchorProbe6702WrongWinner
	}
	if result.identityRows == 0 {
		if !floorArmed {
			return anchorProbe6702H1bNoFloor
		}
		if batchProducerHits > 0 {
			return anchorProbe6702H1aBatchDisarm
		}
		return anchorProbe6702EvidenceAbsent
	}
	if result.tierC == result.identityRows {
		return anchorProbe6702H2AllTierC
	}
	return anchorProbe6702UnexpectedTier
}

// crossScopeProducerDelta6702 sums the per-producer cross-scope delta the
// #5709 floor decides on into the single batch-wide number the H1a
// discriminator needs: a committing pass with zero pinned-digest rows but a
// positive batch delta had its floor disarmed by another finding's evidence.
func crossScopeProducerDelta6702(
	before map[reducercontract.Domain]int,
	envelopes []facts.Envelope,
) int {
	resolved := countSupplyChainImpactCrossScopeProducerFacts(envelopes)
	delta := 0
	for producer, count := range resolved {
		delta += count - before[producer]
	}
	return delta
}

// emitAnchorProbe6702 writes the bounded #6702 probe line when this pass
// touched the pinned digest or finding. It is a no-op when neither is
// present or the logger is nil, so ordinary generations pay one scan and no
// log volume. Keys are fixed low-cardinality strings; only repository IDs
// from the closed corpus set and the pinned digest appear as values.
func emitAnchorProbe6702(
	ctx context.Context,
	logger *slog.Logger,
	intent reducercontract.Intent,
	floorArmed bool,
	batchProducerHits int,
	deferred bool,
	envelopes []facts.Envelope,
	findings []SupplyChainImpactFinding,
) {
	if logger == nil {
		return
	}
	result := probeSupplyChainAnchor6702(envelopes, findings)
	if !result.findingPresent && !result.digestPresent {
		return
	}
	class := classifyAnchorProbe6702(result, floorArmed, batchProducerHits)
	logger.InfoContext(ctx, "supply_chain_anchor_probe_6702",
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.String("probe_class", string(class)),
		slog.String("digest", anchorProbe6702Digest),
		slog.String("cve_id", anchorProbe6702CVE),
		slog.Bool("floor_armed", floorArmed),
		slog.Int("batch_producer_hits", batchProducerHits),
		slog.Bool("deferred", deferred),
		slog.Int("identity_rows", result.identityRows),
		slog.Int("tier_a", result.tierA),
		slog.Int("tier_b", result.tierB),
		slog.Int("tier_c", result.tierC),
		slog.Bool("winner_found", result.winnerFound),
		slog.Int("winner_tier", result.winnerTier),
		slog.String("winner_repository", result.winnerRepository),
		slog.Bool("finding_present", result.findingPresent),
		slog.String("finding_repository", result.findingRepository),
		slog.String("finding_digest", result.findingDigest),
	)
}
