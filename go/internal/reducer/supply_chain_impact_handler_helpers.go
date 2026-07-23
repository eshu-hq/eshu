// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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

// maxSupplyChainImpactScannerAnalysisScopeLoads bounds how many distinct
// os_package (ScopeID, GenerationID) pairs
// loadSupplyChainImpactScannerAnalysisScopeFacts will query for a sibling
// scanner_worker.analysis fact within one intent. In production, os_package
// facts arrive from the active-evidence SQL stage carrying their own
// scan-target ScopeID — a different scope than the intent's
// vulnerability-intelligence scope (see classifySupplyChainImpactPackage and
// supplyChainScopeGenerationKey in supply_chain_impact_index.go for the join
// this feeds). This cap keeps a pathological generation with an unbounded
// number of distinct scan targets from turning the sibling load into
// unbounded per-intent fan-out.
const maxSupplyChainImpactScannerAnalysisScopeLoads = 256

// supplyChainImpactScopeGenerationPair is one distinct (ScopeID, GenerationID)
// pair collected from loaded os_package envelopes so
// loadSupplyChainImpactScannerAnalysisScopeFacts can load each scan target's
// sibling scanner_worker.analysis fact exactly once.
type supplyChainImpactScopeGenerationPair struct {
	scopeID      string
	generationID string
}

// supplyChainImpactOSPackageScopeGenerationPairs returns the distinct,
// non-empty (ScopeID, GenerationID) pairs carried by every loaded
// vulnerability.os_package envelope, in stable first-seen order, bounded by
// maxSupplyChainImpactScannerAnalysisScopeLoads. The second return reports
// whether the bound truncated the result.
func supplyChainImpactOSPackageScopeGenerationPairs(
	envelopes []facts.Envelope,
) ([]supplyChainImpactScopeGenerationPair, bool) {
	seen := make(map[string]struct{})
	var pairs []supplyChainImpactScopeGenerationPair
	truncated := false
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityOSPackageFactKind {
			continue
		}
		scopeID := strings.TrimSpace(envelope.ScopeID)
		if scopeID == "" {
			continue
		}
		generationID := strings.TrimSpace(envelope.GenerationID)
		key := supplyChainScopeGenerationKey(scopeID, generationID)
		if _, ok := seen[key]; ok {
			continue
		}
		if len(pairs) >= maxSupplyChainImpactScannerAnalysisScopeLoads {
			truncated = true
			break
		}
		seen[key] = struct{}{}
		pairs = append(pairs, supplyChainImpactScopeGenerationPair{scopeID: scopeID, generationID: generationID})
	}
	return pairs, truncated
}

// loadSupplyChainImpactScannerAnalysisScopeFacts loads the sibling
// scanner_worker.analysis fact for every distinct os_package scan scope
// already present in envelopes. supplyChainImpactFactKinds intentionally
// omits ScannerWorkerAnalysisFactKind from the intent-scope base load: in
// production the analysis fact lives in the os_package's own scan scope, not
// the intent's vulnerability-intelligence scope, so it can only be reached by
// querying each os_package's ScopeID+GenerationID directly. Without this
// stage classifySupplyChainImpactPackage's digest join
// (supplyChainScopeGenerationKey) never has a scanner analysis to match and
// SubjectDigest stays blank for every os_package finding.
func (h SupplyChainImpactHandler) loadSupplyChainImpactScannerAnalysisScopeFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, bool, error) {
	pairs, truncated := supplyChainImpactOSPackageScopeGenerationPairs(envelopes)
	var loaded []facts.Envelope
	for _, pair := range pairs {
		scoped, err := loadFactsForKinds(
			ctx,
			h.FactLoader,
			pair.scopeID,
			pair.generationID,
			[]string{facts.ScannerWorkerAnalysisFactKind},
		)
		if err != nil {
			return nil, false, err
		}
		loaded = appendUniqueSupplyChainImpactFacts(loaded, scoped...)
	}
	return loaded, truncated, nil
}

// maxSupplyChainImpactResolvedDigestLoads bounds how many distinct image
// digests loadSupplyChainImpactResolvedDigestEvidenceFacts seeds into its
// single re-run of the active-evidence reader per intent. Mirrors
// maxSupplyChainImpactScannerAnalysisScopeLoads: in production these digests
// come one-per-distinct-scan-target from the scanner-analysis-scope stage
// immediately above, so a pathological generation with an unbounded number of
// distinct scan targets cannot also turn this digest-seeded re-run into an
// unbounded active-evidence read.
const maxSupplyChainImpactResolvedDigestLoads = 256

// loadSupplyChainImpactResolvedDigestEvidenceFacts re-runs the active-evidence
// reader (loadActiveSupplyChainImpactFacts) seeded with the image digests
// scannerAnalysisEnvelopes just resolved, so reducer_container_image_identity
// (and any other active-evidence kind the same digest branch of
// listActiveSupplyChainImpactFactsQuery matches) gets loaded for an
// os_package finding whose SubjectDigest only becomes known at the
// scanner-analysis-scope stage immediately above.
//
// This closes a phase-ordering gap (issue #5464): the ORIGINAL active-evidence
// stage (loadActiveSupplyChainImpactFactsUntilStable, called earlier in
// loadSupplyChainImpactEvidence) runs BEFORE any os_package's scanned digest
// exists, because that digest is only resolved by
// loadSupplyChainImpactScannerAnalysisScopeFacts — which itself must run
// AFTER loadSupplyChainImpactOSPackageAdvisoryFacts, since it keys its
// scan-scope lookup off the os_package envelopes that stage adds. So the
// digest could never reach the original active-evidence filter,
// reducer_container_image_identity for a pure OS-package finding was never
// loaded, finding.RepositoryID stayed empty, and every downstream
// repository-keyed join (matchingSupplyChainWorkloads/DeploymentLanes/Services
// in supply_chain_impact_runtime.go) early-returned nil for that finding. This
// stage is purely ADDITIVE — it does not reorder or replace the earlier
// stage, which still resolves whatever digests, package IDs, or CVE IDs were
// already known at that point.
//
// This is a single, non-looping call: unlike
// loadActiveSupplyChainImpactFactsUntilStable, a loaded
// reducer_container_image_identity fact contributes no filter value
// supplyChainImpactFilter derives back into SubjectDigests/PackageIDs/etc (its
// own case only feeds RepositoryIDs/ImageRefs from fields already known), so a
// second round would never discover anything new from it.
func (h SupplyChainImpactHandler) loadSupplyChainImpactResolvedDigestEvidenceFacts(
	ctx context.Context,
	scannerAnalysisEnvelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	filter := supplyChainImpactFilter(scannerAnalysisEnvelopes)
	if len(filter.SubjectDigests) == 0 {
		return nil, nil
	}
	if len(filter.SubjectDigests) > maxSupplyChainImpactResolvedDigestLoads {
		filter.SubjectDigests = filter.SubjectDigests[:maxSupplyChainImpactResolvedDigestLoads]
	}
	return h.loadActiveSupplyChainImpactFacts(ctx, filter)
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
