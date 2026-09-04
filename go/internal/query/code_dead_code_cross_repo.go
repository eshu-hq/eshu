// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	crossRepoDeadCodeCapability = "code_quality.dead_code"
)

type crossRepoDeadCodeRequest struct {
	RepoID               string   `json:"repo_id"`
	Language             string   `json:"language"`
	Limit                int      `json:"limit"`
	ConsumerRepoIDs      []string `json:"consumer_repo_ids"`
	ExcludeDecoratedWith []string `json:"exclude_decorated_with"`
}

type crossRepoDeadCodeEvidence struct {
	ConsumerRepoID   string
	ConsumerRepoName string
	ConsumerEntityID string
	RelationshipType string
	EvidenceFamily   string
	Citation         string
	Confidence       float64
	ConfidenceLabel  string
	ResolutionMethod string
	Depth            int
	GenerationID     string
	GenerationStatus string
	ObservedAt       time.Time
	Ambiguous        bool
	NeedsEvidence    bool
	Reason           string
}

// crossRepoDeadCodeEvidenceStore reads active consumer evidence for producer
// candidates. reads names the consumer repositories the evidence page is bound
// to in SQL -- the row cap falls on those, not on a mixed set -- and the grant
// the ungranted-consumer probe runs against. The second return value is that
// probe's answer: the producer entities with a consumer outside the grant,
// which is how the handler can still answer "there is a consumer you cannot
// see" instead of "dead" (#5167).
type crossRepoDeadCodeEvidenceStore interface {
	CrossRepoDeadCodeConsumerEvidence(
		ctx context.Context,
		producerRepoID string,
		entityIDs []string,
		reads crossRepoDeadCodeConsumerReads,
	) (map[string][]crossRepoDeadCodeEvidence, crossRepoDeadCodeHiddenConsumers, error)
}

type crossRepoDeadCodeScan struct {
	Active                     []map[string]any
	Suppressed                 []map[string]any
	PolicyStats                deadCodePolicyStats
	DisplayTruncated           bool
	CandidateScanTruncated     bool
	CandidateScanLimit         int
	CandidateScanLimitPerLabel int
	CandidateScanPages         int
	CandidateScanRows          int
}

func (h *CodeHandler) handleCrossRepoDeadCode(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDeadCodeInvestigation,
		"POST /api/v0/code/dead-code/cross-repo",
		crossRepoDeadCodeCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), crossRepoDeadCodeCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"cross-repo dead code requires authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			crossRepoDeadCodeCapability,
			h.profile(),
			requiredProfile(crossRepoDeadCodeCapability),
		)
		return
	}

	var req crossRepoDeadCodeRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := normalizeCrossRepoDeadCodeRequest(&req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, crossRepoDeadCodeCapability) {
		return
	}
	if !h.applyConsumerRepositorySelectors(w, r, req.ConsumerRepoIDs, crossRepoDeadCodeCapability) {
		return
	}

	scan, err := h.scanCrossRepoDeadCodeCandidates(r.Context(), req)
	if err != nil {
		if WriteGraphReadError(w, r, err, crossRepoDeadCodeCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	evidence, hiddenConsumers, evidenceAvailable, err := h.crossRepoDeadCodeConsumerEvidence(
		r.Context(),
		req.RepoID,
		deadCodeResultEntityIDs(scan.Active),
		req.ConsumerRepoIDs,
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	boundaryEvidence := h.crossRepoDeadCodeRepositoryBoundaryEvidence(r.Context(), req.RepoID)
	buckets := h.bucketCrossRepoDeadCodeResults(r.Context(), req, scan, crossRepoDeadCodeConsumerEvidenceSet{
		Evidence:        evidence,
		HiddenConsumers: hiddenConsumers,
		Boundary:        boundaryEvidence,
		Available:       evidenceAvailable,
	})
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_id":                        req.RepoID,
		"language":                       req.Language,
		"limit":                          req.Limit,
		"consumer_repo_ids":              req.ConsumerRepoIDs,
		"query_shape":                    "bounded_cross_repo_dead_code",
		"truncated":                      scan.DisplayTruncated || scan.CandidateScanTruncated,
		"display_truncated":              scan.DisplayTruncated,
		"candidate_scan_truncated":       scan.CandidateScanTruncated,
		"candidate_scan_limit":           scan.CandidateScanLimit,
		"candidate_scan_limit_per_label": scan.CandidateScanLimitPerLabel,
		"candidate_scan_pages":           scan.CandidateScanPages,
		"candidate_scan_rows":            scan.CandidateScanRows,
		"candidate_buckets":              buckets,
		"bucket_counts":                  crossRepoDeadCodeBucketCounts(buckets),
		"analysis": buildDeadCodeAnalysisForLanguage(
			crossRepoDeadCodeAnalysisRows(buckets),
			req.ExcludeDecoratedWith,
			scan.PolicyStats,
			req.Language,
		),
	}, BuildTruthEnvelope(h.profile(), crossRepoDeadCodeCapability, TruthBasisHybrid, "resolved from bounded candidate scan plus active cross-repo consumer evidence"))
}

func normalizeCrossRepoDeadCodeRequest(req *crossRepoDeadCodeRequest) error {
	if strings.TrimSpace(req.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	if req.Limit <= 0 {
		req.Limit = deadCodeDefaultLimit
	}
	if req.Limit > deadCodeMaxLimit {
		req.Limit = deadCodeMaxLimit
	}
	req.Language = normalizeDeadCodeLanguage(req.Language)
	req.ConsumerRepoIDs = cleanCrossRepoDeadCodeStrings(req.ConsumerRepoIDs)
	return nil
}

func (h *CodeHandler) applyConsumerRepositorySelectors(
	w http.ResponseWriter,
	r *http.Request,
	consumerRepoIDs []string,
	capability string,
) bool {
	for i := range consumerRepoIDs {
		if !h.applyRepositorySelectorForCapability(w, r, &consumerRepoIDs[i], capability) {
			return false
		}
	}
	return true
}

func (h *CodeHandler) scanCrossRepoDeadCodeCandidates(
	ctx context.Context,
	req crossRepoDeadCodeRequest,
) (crossRepoDeadCodeScan, error) {
	pageLimit := deadCodeCandidateQueryLimit(req.Limit)
	totalLimit := deadCodeCandidateScanLimit(req.Limit)
	scan := crossRepoDeadCodeScan{
		Active:                     make([]map[string]any, 0, req.Limit+1),
		Suppressed:                 make([]map[string]any, 0),
		CandidateScanLimit:         totalLimit,
		CandidateScanLimitPerLabel: totalLimit,
	}
	seenEntityIDs := make(map[string]struct{}, req.Limit+1)
	schedule := newDeadCodeCandidateSchedule(
		deadCodeCandidateLabelsForLanguage(req.Language),
		pageLimit,
		totalLimit,
	)

	for {
		page, ok := schedule.nextPage()
		if !ok {
			break
		}
		rows, err := h.deadCodeCandidateRows(ctx, req.RepoID, page.Label, req.Language, page.Limit, page.Offset)
		if err != nil {
			return scan, err
		}
		scan.CandidateScanPages++
		rowCount := len(rows)
		scan.CandidateScanRows += rowCount
		schedule.record(page, rowCount)
		rows = filterDuplicateDeadCodeRows(rows, seenEntityIDs)
		results, contentByID, err := h.buildDeadCodeResults(ctx, rows)
		if err != nil {
			return scan, err
		}
		downgraded := h.loadDeadCodeDowngradedRoots(ctx, results)
		active, suppressed, stats := partitionDeadCodeInvestigationResults(
			results,
			contentByID,
			req.ExcludeDecoratedWith,
			downgraded,
		)
		addDeadCodePolicyStats(&scan.PolicyStats, stats)
		scan.Suppressed = append(scan.Suppressed, suppressed...)
		active, err = h.filterCrossRepoDeadCodeResultsWithoutProducerLocalIncomingEdges(ctx, active, page.Label)
		if err != nil {
			return scan, err
		}
		scan.Active = append(scan.Active, active...)
		if len(scan.Active) > req.Limit {
			scan.DisplayTruncated = true
			scan.Active = scan.Active[:req.Limit]
			return scan, nil
		}
	}
	scan.CandidateScanTruncated = schedule.candidateScanTruncated()
	return scan, nil
}

// crossRepoDeadCodeConsumerEvidenceSet is everything the bucketing pass needs
// about consumers: the grant-bound evidence page per producer entity, the
// producer entities the ungranted-consumer probe flagged, the
// repository-boundary fallback, and whether the content store answered at all.
//
// HiddenConsumers carries no consumer identity and no consumer count -- only
// which producer entities have a consumer outside the caller's grant. A
// flagged entity adds one to that entity's hidden count, which is what turns
// its answer into unknown_needs_evidence with permission_hidden_consumer
// instead of dead. The count is deliberately not a total: the route needs the
// yes/no, and a total would cost the enumeration this probe exists to avoid.
//
// A request that named a consumer selector gets an empty set, because the probe
// is not run for it. That is the structural half of the rule the selector needs:
// a consumer the caller did not ask about must not override the evidence of one
// it did, and the probe cannot report one if it never runs.
type crossRepoDeadCodeConsumerEvidenceSet struct {
	Evidence        map[string][]crossRepoDeadCodeEvidence
	HiddenConsumers crossRepoDeadCodeHiddenConsumers
	Boundary        []crossRepoDeadCodeEvidence
	Available       bool
}

func (h *CodeHandler) crossRepoDeadCodeConsumerEvidence(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
	consumerRepoIDs []string,
) (map[string][]crossRepoDeadCodeEvidence, crossRepoDeadCodeHiddenConsumers, bool, error) {
	store, ok := h.Content.(crossRepoDeadCodeEvidenceStore)
	if !ok {
		return map[string][]crossRepoDeadCodeEvidence{}, crossRepoDeadCodeHiddenConsumers{}, false, nil
	}
	// The consumer side takes the caller's own grant, not the producer anchor:
	// producerRepoID is already grant-resolved by the selector, but the
	// consumers this read returns belong to other repositories.
	reads, ok := crossRepoDeadCodeConsumerReadPlan(codeGrantAccessFilter(ctx), consumerRepoIDs)
	if !ok {
		// Nothing this caller may read can answer the question they asked, and
		// an unbounded read is not the fallback. Reporting the evidence as
		// unavailable keeps every candidate at unknown_needs_evidence instead
		// of letting an unread consumer become "dead".
		return map[string][]crossRepoDeadCodeEvidence{}, crossRepoDeadCodeHiddenConsumers{}, false, nil
	}
	evidence, hidden, err := store.CrossRepoDeadCodeConsumerEvidence(
		ctx,
		producerRepoID,
		entityIDs,
		reads,
	)
	if err != nil {
		return nil, nil, true, err
	}
	return evidence, hidden, true, nil
}

func (h *CodeHandler) bucketCrossRepoDeadCodeResults(
	ctx context.Context,
	req crossRepoDeadCodeRequest,
	scan crossRepoDeadCodeScan,
	consumers crossRepoDeadCodeConsumerEvidenceSet,
) map[string]any {
	allowedConsumers := crossRepoDeadCodeConsumerSet(req.ConsumerRepoIDs)
	access := codeGrantAccessFilter(ctx)
	buckets := map[string]any{
		"dead":             []any{},
		"live_by_consumer": []any{},
		"unknown":          []any{},
		"suppressed":       crossRepoDeadCodeAnySlice(scan.Suppressed),
	}
	for _, result := range scan.Active {
		entityID := StringVal(result, "entity_id")
		row := cloneCrossRepoDeadCodeResult(result)
		visible, hidden := filterCrossRepoDeadCodeEvidence(consumers.Evidence[entityID], allowedConsumers, access)
		// The probe already applied the grant in SQL and reports no consumer
		// identity, so an entity it flagged adds exactly one to the hidden
		// count. It runs only for a request that named no consumer selector, so
		// a consumer the caller excluded can never be what raises this count
		// and turn a symbol a requested consumer proves live into
		// unknown_needs_evidence.
		hiddenCount := len(hidden)
		if consumers.HiddenConsumers.has(entityID) {
			hiddenCount++
		}
		if len(visible) == 0 && hiddenCount == 0 {
			boundaryVisible, boundaryHidden := filterCrossRepoDeadCodeEvidence(consumers.Boundary, allowedConsumers, access)
			visible = append(visible, boundaryVisible...)
			hiddenCount += len(boundaryHidden)
		}
		row["consumer_evidence"] = crossRepoDeadCodeEvidenceMaps(visible)
		if hiddenCount > 0 {
			row["hidden_consumer_evidence_count"] = hiddenCount
		}

		// A strong granted consumer outranks a hidden one, the order
		// applyDeadCodeIncomingEdges (code_dead_code_scan.go) applies on the
		// other two dead-code routes; the invariant is in this package's
		// AGENTS.md. The count stays on the row, so a live answer still says a
		// consumer is hidden. Only the count is outranked -- every other reason,
		// consumer_evidence_truncated included, still forces unknown.
		strongLiveEvidence := crossRepoDeadCodeHasStrongLiveEvidence(visible)
		unknownHiddenCount := hiddenCount
		if strongLiveEvidence {
			unknownHiddenCount = 0
		}
		reasons := crossRepoDeadCodeUnknownReasons(row, visible, unknownHiddenCount, consumers.Available)
		if len(reasons) > 0 {
			row["classification"] = "unknown_needs_evidence"
			row["needs_evidence_reasons"] = reasons
			row["confidence_label"] = "unknown"
			buckets["unknown"] = append(buckets["unknown"].([]any), row)
			continue
		}
		if strongLiveEvidence {
			row["classification"] = "live_by_consumer"
			row["confidence_label"] = crossRepoDeadCodeStrongestConfidenceLabel(visible)
			buckets["live_by_consumer"] = append(buckets["live_by_consumer"].([]any), row)
			continue
		}
		row["classification"] = "dead"
		row["confidence_label"] = "medium"
		row["evidence_citations"] = []any{
			"content_entities:" + req.RepoID + "/" + entityID,
			"code_reachability_rows:no_active_cross_repo_consumer_evidence",
		}
		buckets["dead"] = append(buckets["dead"].([]any), row)
	}
	return buckets
}

func crossRepoDeadCodeHasStrongLiveEvidence(evidence []crossRepoDeadCodeEvidence) bool {
	for _, item := range evidence {
		if item.NeedsEvidence || item.Ambiguous || !strings.EqualFold(item.GenerationStatus, "active") {
			continue
		}
		if item.Confidence > codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName) {
			return true
		}
	}
	return false
}

func crossRepoDeadCodeStrongestConfidenceLabel(evidence []crossRepoDeadCodeEvidence) string {
	best := 0.0
	label := ""
	for _, item := range evidence {
		if item.Confidence > best {
			best = item.Confidence
			label = item.ConfidenceLabel
		}
	}
	if label == "" {
		return crossRepoDeadCodeConfidenceLabel(best)
	}
	return label
}

func crossRepoDeadCodeEvidenceMaps(evidence []crossRepoDeadCodeEvidence) []any {
	rows := make([]any, 0, len(evidence))
	for _, item := range evidence {
		if item.ConfidenceLabel == "" {
			item.ConfidenceLabel = crossRepoDeadCodeConfidenceLabel(item.Confidence)
		}
		if item.EvidenceFamily == "" {
			item.EvidenceFamily = "code_reachability"
		}
		row := map[string]any{
			"consumer_repo_id":   item.ConsumerRepoID,
			"consumer_repo_name": item.ConsumerRepoName,
			"consumer_entity_id": item.ConsumerEntityID,
			"relationship_type":  item.RelationshipType,
			"evidence_family":    item.EvidenceFamily,
			"citation":           item.Citation,
			"confidence":         item.Confidence,
			"confidence_label":   item.ConfidenceLabel,
			"resolution_method":  item.ResolutionMethod,
			"depth":              item.Depth,
			"generation_id":      item.GenerationID,
			"generation_status":  item.GenerationStatus,
			"ambiguous":          item.Ambiguous,
			"needs_evidence":     item.NeedsEvidence,
		}
		if !item.ObservedAt.IsZero() {
			row["observed_at"] = item.ObservedAt.Format(time.RFC3339Nano)
		}
		if item.Reason != "" {
			row["reason"] = item.Reason
		}
		rows = append(rows, row)
	}
	return rows
}

func crossRepoDeadCodeConfidenceLabel(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "high"
	case confidence > codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName):
		return "medium"
	case confidence > 0:
		return "low"
	default:
		return "unknown"
	}
}

func crossRepoDeadCodeConsumerSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func cleanCrossRepoDeadCodeStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func cloneCrossRepoDeadCodeResult(row map[string]any) map[string]any {
	clone := make(map[string]any, len(row)+4)
	for key, value := range row {
		clone[key] = value
	}
	return clone
}
