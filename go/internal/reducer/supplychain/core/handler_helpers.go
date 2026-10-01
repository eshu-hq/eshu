// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h SupplyChainImpactHandler) evaluationNow() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

// loadActiveSupplyChainImpactFacts's bool return reports whether the
// underlying loader truncated the vulnerability.suppression tail of its own
// pagination for THIS round (#5466 round-7 review P1-B). It never reports
// truncated core evidence: the production reader loads every matching
// non-suppression row before it counts the tail (see
// maxSupplyChainImpactActiveEvidenceRowsPerCall), so callers treat it as the
// suppression-tail signal and nothing more (#7154).
func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	loader, ok := h.FactLoader.(activeSupplyChainImpactFactLoader)
	if !ok || filter.empty() {
		return nil, false, nil
	}
	envelopes, suppressionTail, err := loader.ListActiveSupplyChainImpactFacts(ctx, filter)
	if err != nil {
		return nil, false, factload.ClassifyFactLoadError(err)
	}
	return envelopes, suppressionTail, nil
}

const maxSupplyChainImpactActiveEvidenceLoads = 8

// loadActiveSupplyChainImpactFactsUntilStable takes its FIRST-round filter from
// the caller rather than deriving it again from envelopes. The #5709 readiness
// floor decides whether this pass can reach a producer fact by inspecting that
// same filter before the load runs (crossScopeProducerLookupPlanned). Sharing
// one value keeps the floor and the load from disagreeing about what this pass
// asked for, and keeps the pre-load envelope set scanned once rather than twice
// on a hot path. Follow-up rounds are still derived here from whatever the
// previous round returned.
//
// The returned truncation names why the loop stopped short: the round cap
// (rounds), a spent evidence budget (budget), or a bounded suppression tail
// (suppressionTail) -- the causes stay apart because only the first two make
// the finding set incomplete (#7154).
func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFactsUntilStable(
	ctx context.Context,
	envelopes []facts.Envelope,
	initialFilter SupplyChainImpactFactFilter,
	budget *supplyChainImpactEvidenceBudget,
) ([]facts.Envelope, supplyChainImpactTruncation, error) {
	requested := SupplyChainImpactFactFilter{}
	next := initialFilter
	var truncation supplyChainImpactTruncation
	for loads := 0; !next.empty(); loads++ {
		if loads >= maxSupplyChainImpactActiveEvidenceLoads {
			truncation.rounds = true
			return envelopes, truncation, nil
		}
		active, roundSuppressionTail, err := h.loadActiveSupplyChainImpactFacts(ctx, next)
		if err != nil {
			return nil, supplyChainImpactTruncation{}, err
		}
		// A single round's own per-call suppression cap is independent of the
		// round cap above (#5466 round-7 review P1-B).
		truncation.suppressionTail = truncation.suppressionTail || roundSuppressionTail
		requested = mergeSupplyChainImpactFactFilters(requested, next)
		before := len(envelopes)
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, active...)
		if !budget.charge(len(envelopes) - before) {
			// Keep what this round loaded, load nothing more.
			truncation.budget = true
			return envelopes, truncation, nil
		}
		next = supplyChainImpactFollowUpFilter(requested, supplyChainImpactFilter(envelopes))
	}
	return envelopes, truncation, nil
}

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
// vulnerability.os_package envelope, in stable first-seen order. It does not
// cap the result: a scope with many scan targets used to lose every pair past
// 256 and report truncation, so it never retracted (#7154). The number of
// envelopes those pairs can pull in is bounded by the per-intent evidence
// budget instead (supplyChainImpactEvidenceBudget).
func supplyChainImpactOSPackageScopeGenerationPairs(
	envelopes []facts.Envelope,
) []supplyChainImpactScopeGenerationPair {
	seen := make(map[string]struct{})
	var pairs []supplyChainImpactScopeGenerationPair
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityOSPackageFactKind {
			continue
		}
		scopeID := strings.TrimSpace(envelope.ScopeID)
		if scopeID == "" {
			continue
		}
		generationID := strings.TrimSpace(envelope.GenerationID)
		key := supplychainmodel.ScopeGenerationKey(scopeID, generationID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		pairs = append(pairs, supplyChainImpactScopeGenerationPair{scopeID: scopeID, generationID: generationID})
	}
	return pairs
}

// loadSupplyChainImpactScannerAnalysisScopeFacts loads the sibling
// scanner_worker.analysis fact for every distinct os_package scan scope
// already present in envelopes. supplyChainImpactFactKinds intentionally
// omits ScannerWorkerAnalysisFactKind from the intent-scope base load: in
// production the analysis fact lives in the os_package's own scan scope, not
// the intent's vulnerability-intelligence scope, so it can only be reached by
// querying each os_package's ScopeID+GenerationID directly. Without this
// stage classifySupplyChainImpactPackage's digest join
// (supplychainmodel.ScopeGenerationKey) never has a scanner analysis to match and
// SubjectDigest stays blank for every os_package finding.
//
// Every pair is queried; the loop stops early only when the evidence budget is
// spent, and the caller reads that through budget.exhausted().
func (h SupplyChainImpactHandler) loadSupplyChainImpactScannerAnalysisScopeFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
	budget *supplyChainImpactEvidenceBudget,
) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	for _, pair := range supplyChainImpactOSPackageScopeGenerationPairs(envelopes) {
		scoped, err := factload.LoadFactsForKinds(
			ctx,
			h.FactLoader,
			pair.scopeID,
			pair.generationID,
			[]string{facts.ScannerWorkerAnalysisFactKind},
		)
		if err != nil {
			return nil, err
		}
		before := len(loaded)
		loaded = appendUniqueSupplyChainImpactFacts(loaded, scoped...)
		if !budget.charge(len(loaded) - before) {
			break
		}
	}
	return loaded, nil
}

// supplyChainImpactFilterChunkSize is how many values of one disjunctive
// filter field (image digests, repository ids) a single active-evidence read
// carries. A filter matches a row when ANY of its values does, so splitting the
// values across reads and unioning the rows returns exactly what one read of
// all of them would; the chunk size only bounds the size of one SQL parameter
// array. It replaces the drop-past-256 cap those stages carried until #7154.
const supplyChainImpactFilterChunkSize = 256

// chunkSupplyChainImpactFilterValues splits values into consecutive chunks of
// at most size, preserving order. It returns no chunks for no values.
func chunkSupplyChainImpactFilterValues(values []string, size int) [][]string {
	var chunks [][]string
	for start := 0; start < len(values); start += size {
		chunks = append(chunks, values[start:min(start+size, len(values))])
	}
	return chunks
}

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
// in runtime.go) early-returned nil for that finding. This
// stage is purely ADDITIVE — it does not reorder or replace the earlier
// stage, which still resolves whatever digests, package IDs, or CVE IDs were
// already known at that point.
//
// This is a single pass over the digests, not a loop: unlike
// loadActiveSupplyChainImpactFactsUntilStable, a loaded
// reducer_container_image_identity fact contributes no filter value
// supplyChainImpactFilter derives back into SubjectDigests/PackageIDs/etc (its
// own case only feeds RepositoryIDs/ImageRefs from fields already known), so a
// second round would never discover anything new from it. The digests are read
// in chunks of supplyChainImpactFilterChunkSize; the first chunk carries the
// rest of the filter (image refs), later chunks carry digests only so the same
// refs are not read again (#7154).
//
// The bool return is the suppression-tail signal of the underlying reads.
func (h SupplyChainImpactHandler) loadSupplyChainImpactResolvedDigestEvidenceFacts(
	ctx context.Context,
	scannerAnalysisEnvelopes []facts.Envelope,
	budget *supplyChainImpactEvidenceBudget,
) ([]facts.Envelope, bool, error) {
	filter := supplyChainImpactFilter(scannerAnalysisEnvelopes)
	if len(filter.SubjectDigests) == 0 {
		return nil, false, nil
	}
	var loaded []facts.Envelope
	suppressionTail := false
	for i, digests := range chunkSupplyChainImpactFilterValues(filter.SubjectDigests, supplyChainImpactFilterChunkSize) {
		chunkFilter := SupplyChainImpactFactFilter{SubjectDigests: digests}
		if i == 0 {
			chunkFilter = filter
			chunkFilter.SubjectDigests = digests
		}
		chunk, chunkTail, err := h.loadActiveSupplyChainImpactFacts(ctx, chunkFilter)
		if err != nil {
			return nil, false, err
		}
		suppressionTail = suppressionTail || chunkTail
		before := len(loaded)
		loaded = appendUniqueSupplyChainImpactFacts(loaded, chunk...)
		if !budget.charge(len(loaded) - before) {
			break
		}
	}
	return loaded, suppressionTail, nil
}

// loadSupplyChainImpactPeerIdentityFacts performs the additional
// active-evidence load by RepositoryIDs extracted from
// resolvedDigestEnvelopes, so the reconciliation loop in
// classifySupplyChainImpactPackage has peer identities (same source
// repository, different digest) to compare against. Without this stage,
// index.images contains only identities whose digest matches the scanner's
// digest — the reconciliation always finds no peer and is a silent no-op
// (issue #5468). A container_image_identity fact's filter contribution
// (supplyChainImpactFilter) feeds RepositoryIDs and ImageRefs — NOT
// SubjectDigests — so this pass cannot recurse. The repository ids are read in
// chunks of supplyChainImpactFilterChunkSize rather than dropped past a cap
// (#7154).
//
// The bool return is the suppression-tail signal of the underlying reads.
func (h SupplyChainImpactHandler) loadSupplyChainImpactPeerIdentityFacts(
	ctx context.Context,
	resolvedDigestEnvelopes []facts.Envelope,
	budget *supplyChainImpactEvidenceBudget,
) ([]facts.Envelope, bool, error) {
	filter := supplyChainImpactFilter(resolvedDigestEnvelopes)
	if len(filter.RepositoryIDs) == 0 {
		return nil, false, nil
	}
	var loaded []facts.Envelope
	suppressionTail := false
	for i, repositoryIDs := range chunkSupplyChainImpactFilterValues(filter.RepositoryIDs, supplyChainImpactFilterChunkSize) {
		// SubjectDigests stay cleared: they only name the scanner's digest, and
		// the whole point of this stage is to load identities for the SAME
		// repository with DIFFERENT digests.
		chunkFilter := SupplyChainImpactFactFilter{RepositoryIDs: repositoryIDs}
		if i == 0 {
			chunkFilter = filter
			chunkFilter.RepositoryIDs = repositoryIDs
			chunkFilter.SubjectDigests = nil
		}
		chunk, chunkTail, err := h.loadActiveSupplyChainImpactFacts(ctx, chunkFilter)
		if err != nil {
			return nil, false, err
		}
		suppressionTail = suppressionTail || chunkTail
		before := len(loaded)
		loaded = appendUniqueSupplyChainImpactFacts(loaded, chunk...)
		if !budget.charge(len(loaded) - before) {
			break
		}
	}
	return loaded, suppressionTail, nil
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
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
			telemetry.AttrOutcome(string(status)),
		))
	}
	if h.Instruments.SupplyChainSuppressionDecisions != nil {
		for _, state := range SupplyChainSuppressionStates() {
			if suppressionCounts[state] == 0 {
				continue
			}
			h.Instruments.SupplyChainSuppressionDecisions.Add(ctx, int64(suppressionCounts[state]), metric.WithAttributes(
				telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
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
				telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
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

// emitRetraction records the superseded findings one pass tombstoned (#6831):
// the eshu_dp_supply_chain_impact_findings_retracted_total counter plus one
// structured log line naming the (scope, generation), so an operator can see
// which finding sets are being rewritten without querying fact_records.
// Nothing is emitted for a pass that retracted nothing. A pass with partial
// evidence never retracts, so it is visible through the existing
// active_evidence_truncated evidence-summary marker and sub-signal instead.
func (h SupplyChainImpactHandler) emitRetraction(
	ctx context.Context,
	intent reducercontract.Intent,
	retracted int,
) {
	if retracted == 0 {
		return
	}
	if h.Instruments != nil && h.Instruments.SupplyChainImpactFindingsRetracted != nil {
		h.Instruments.SupplyChainImpactFindingsRetracted.Add(ctx, int64(retracted), metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		))
	}
	if h.Logger != nil {
		h.Logger.InfoContext(ctx, "supply chain impact superseded findings retracted",
			slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
			slog.String("scope_id", intent.ScopeID),
			slog.String("generation_id", intent.GenerationID),
			slog.String("intent_id", intent.IntentID),
			slog.Int("findings_retracted", retracted),
		)
	}
}

// emitEvidenceTruncation records a pass whose bounded evidence load stopped
// short for a cause that can hide a live finding (#7154): the
// eshu_dp_supply_chain_impact_evidence_truncated_total counter per cause plus
// one WARN line naming the scope and generation, so an operator can find the
// scope that keeps its stale findings. Nothing is emitted for a complete pass
// or for a suppression-tail-only truncation, which does not stop retraction.
func (h SupplyChainImpactHandler) emitEvidenceTruncation(
	ctx context.Context,
	intent reducercontract.Intent,
	loaded supplyChainImpactLoadedEvidence,
	findings int,
) {
	causes := loaded.truncation.causes()
	if len(causes) == 0 {
		return
	}
	for _, cause := range causes {
		if h.Instruments != nil && h.Instruments.SupplyChainImpactEvidenceTruncated != nil {
			h.Instruments.SupplyChainImpactEvidenceTruncated.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
				telemetry.AttrReason(cause),
			))
		}
		if h.Logger != nil {
			h.Logger.WarnContext(ctx, "supply chain impact evidence truncated; findings not retracted",
				slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
				slog.String("scope_id", intent.ScopeID),
				slog.String("generation_id", intent.GenerationID),
				slog.String("intent_id", intent.IntentID),
				slog.String("cause", cause),
				slog.Int("evidence_envelopes", len(loaded.envelopes)),
				slog.Int("expansion_envelopes", loaded.expansionEnvelopes),
				slog.Int("findings", findings),
			)
		}
	}
}

// emitWriteSuperseded records a pass the writer rejected because a fresher pass
// was already admitted for the same (scope, generation) (#7142): the
// eshu_dp_supply_chain_impact_write_superseded_total counter plus one WARN line
// naming the scope, generation and token. The pass returns the retryable
// superseded error and the queue re-runs it with a fresher token, so a steady
// rate under continuous ingest is normal churn; a scope that stays superseded
// points at two workers repeatedly overtaking each other, or at a sequence that
// lags the admitted watermark (a restore or manual reset). Any other error is
// ignored here.
func (h SupplyChainImpactHandler) emitWriteSuperseded(
	ctx context.Context,
	intent reducercontract.Intent,
	fencingToken int64,
	err error,
) {
	var superseded supplyChainImpactWriteSupersededError
	if !errors.As(err, &superseded) {
		return
	}
	if h.Instruments != nil && h.Instruments.SupplyChainImpactWriteSuperseded != nil {
		h.Instruments.SupplyChainImpactWriteSuperseded.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		))
	}
	if h.Logger != nil {
		h.Logger.WarnContext(ctx, "supply chain impact write superseded",
			slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
			slog.String("scope_id", intent.ScopeID),
			slog.String("generation_id", intent.GenerationID),
			slog.String("intent_id", intent.IntentID),
			slog.Int64(telemetry.LogKeyFencingToken, fencingToken),
		)
	}
}
