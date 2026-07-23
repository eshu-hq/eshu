// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"slices"
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

type crossRepoDeadCodeEvidenceStore interface {
	CrossRepoDeadCodeConsumerEvidence(
		ctx context.Context,
		producerRepoID string,
		entityIDs []string,
	) (map[string][]crossRepoDeadCodeEvidence, error)
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
	evidence, evidenceAvailable, err := h.crossRepoDeadCodeConsumerEvidence(
		r.Context(),
		req.RepoID,
		deadCodeResultEntityIDs(scan.Active),
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	boundaryEvidence := h.crossRepoDeadCodeRepositoryBoundaryEvidence(r.Context(), req.RepoID)
	buckets := h.bucketCrossRepoDeadCodeResults(r.Context(), req, scan, evidence, boundaryEvidence, evidenceAvailable)
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

func (h *CodeHandler) crossRepoDeadCodeConsumerEvidence(
	ctx context.Context,
	producerRepoID string,
	entityIDs []string,
) (map[string][]crossRepoDeadCodeEvidence, bool, error) {
	store, ok := h.Content.(crossRepoDeadCodeEvidenceStore)
	if !ok {
		return map[string][]crossRepoDeadCodeEvidence{}, false, nil
	}
	evidence, err := store.CrossRepoDeadCodeConsumerEvidence(ctx, producerRepoID, entityIDs)
	if err != nil {
		return nil, true, err
	}
	return evidence, true, nil
}

func (h *CodeHandler) bucketCrossRepoDeadCodeResults(
	ctx context.Context,
	req crossRepoDeadCodeRequest,
	scan crossRepoDeadCodeScan,
	evidenceByEntity map[string][]crossRepoDeadCodeEvidence,
	boundaryEvidence []crossRepoDeadCodeEvidence,
	evidenceAvailable bool,
) map[string]any {
	allowedConsumers := crossRepoDeadCodeConsumerSet(req.ConsumerRepoIDs)
	access := repositoryAccessFilterFromContext(ctx)
	buckets := map[string]any{
		"dead":             []any{},
		"live_by_consumer": []any{},
		"unknown":          []any{},
		"suppressed":       crossRepoDeadCodeAnySlice(scan.Suppressed),
	}
	for _, result := range scan.Active {
		entityID := StringVal(result, "entity_id")
		row := cloneCrossRepoDeadCodeResult(result)
		visible, hidden := filterCrossRepoDeadCodeEvidence(evidenceByEntity[entityID], allowedConsumers, access)
		if len(visible) == 0 && len(hidden) == 0 {
			boundaryVisible, boundaryHidden := filterCrossRepoDeadCodeEvidence(boundaryEvidence, allowedConsumers, access)
			visible = append(visible, boundaryVisible...)
			hidden = append(hidden, boundaryHidden...)
		}
		row["consumer_evidence"] = crossRepoDeadCodeEvidenceMaps(visible)
		if len(hidden) > 0 {
			row["hidden_consumer_evidence_count"] = len(hidden)
		}

		reasons := crossRepoDeadCodeUnknownReasons(row, visible, hidden, evidenceAvailable)
		if len(reasons) > 0 {
			row["classification"] = "unknown_needs_evidence"
			row["needs_evidence_reasons"] = reasons
			row["confidence_label"] = "unknown"
			buckets["unknown"] = append(buckets["unknown"].([]any), row)
			continue
		}
		if crossRepoDeadCodeHasStrongLiveEvidence(visible) {
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

func filterCrossRepoDeadCodeEvidence(
	evidence []crossRepoDeadCodeEvidence,
	allowedConsumers map[string]struct{},
	access repositoryAccessFilter,
) ([]crossRepoDeadCodeEvidence, []crossRepoDeadCodeEvidence) {
	visible := make([]crossRepoDeadCodeEvidence, 0, len(evidence))
	hidden := make([]crossRepoDeadCodeEvidence, 0)
	for _, row := range evidence {
		if row.NeedsEvidence && row.ConsumerRepoID == "" {
			visible = append(visible, row)
			continue
		}
		if len(allowedConsumers) > 0 {
			if _, ok := allowedConsumers[row.ConsumerRepoID]; !ok {
				continue
			}
		}
		if !access.allowsRepositoryID(row.ConsumerRepoID) {
			hidden = append(hidden, row)
			continue
		}
		visible = append(visible, row)
	}
	return visible, hidden
}

func crossRepoDeadCodeUnknownReasons(
	row map[string]any,
	evidence []crossRepoDeadCodeEvidence,
	hidden []crossRepoDeadCodeEvidence,
	evidenceAvailable bool,
) []string {
	reasons := make([]string, 0)
	if !evidenceAvailable {
		reasons = append(reasons, "cross_repo_evidence_unavailable")
	}
	if len(hidden) > 0 {
		reasons = append(reasons, "permission_hidden_consumer")
	}
	if row["classification"] == deadCodeClassificationAmbiguous {
		reasons = append(reasons, "candidate_ambiguous")
	}
	for _, item := range evidence {
		if item.NeedsEvidence || item.Ambiguous || !strings.EqualFold(item.GenerationStatus, "active") {
			reason := strings.TrimSpace(item.Reason)
			if reason == "" && !strings.EqualFold(item.GenerationStatus, "active") {
				reason = "stale_generation"
			}
			if reason == "" {
				reason = "needs_evidence"
			}
			reasons = append(reasons, reason)
			continue
		}
		if item.Confidence <= codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName) {
			reasons = append(reasons, "ambiguous_consumer_ownership")
		}
	}
	slices.Sort(reasons)
	return slices.Compact(reasons)
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
