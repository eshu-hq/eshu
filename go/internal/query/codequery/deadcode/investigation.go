// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	deadCodeInvestigationCapability = "code_quality.dead_code"
	deadCodeInvestigationMaxOffset  = 2000
	deadCodeSuppressedBucketLimit   = 50
)

type DeadCodeInvestigationRequest struct {
	RepoID               string   `json:"repo_id"`
	Language             string   `json:"language"`
	Limit                int      `json:"limit"`
	Offset               int      `json:"offset"`
	ExcludeDecoratedWith []string `json:"exclude_decorated_with"`
}

type DeadCodeInvestigationScan struct {
	CleanupReady               []map[string]any
	Ambiguous                  []map[string]any
	Suppressed                 []map[string]any
	PolicyStats                codemodel.DeadCodePolicyStats
	DisplayTruncated           bool
	CandidateScanTruncated     bool
	SuppressedTruncated        bool
	CandidateScanLimit         int
	CandidateScanLimitPerLabel int
	CandidateScanPages         int
	CandidateScanRows          int
	ActiveCandidatesSeen       int
}

// HandleDeadCodeInvestigation returns the prompt-oriented dead-code packet used
// by MCP clients that need coverage, paging, candidate buckets, and drill-down
// handles without interpreting the lower-level analysis payload themselves.
func (a *Analyzer) HandleDeadCodeInvestigation(w http.ResponseWriter, r *http.Request) {
	r, span := a.deps.StartSpan(
		r,
		telemetry.SpanQueryDeadCodeInvestigation,
		"POST /api/v0/code/dead-code/investigate",
		deadCodeInvestigationCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(a.deps.Profile, deadCodeInvestigationCapability) {
		a.deps.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dead code investigation requires authoritative graph mode",
			querycontract.ErrorCodeUnsupportedCapability,
			deadCodeInvestigationCapability,
			a.deps.Profile,
			querycontract.RequiredProfile(deadCodeInvestigationCapability),
		)
		return
	}

	var req DeadCodeInvestigationRequest
	if err := a.deps.ReadJSON(r, &req); err != nil {
		a.deps.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := normalizeDeadCodeInvestigationRequest(&req); err != nil {
		a.deps.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !a.deps.ApplySelector(w, r, &req.RepoID, deadCodeInvestigationCapability) {
		return
	}

	scan, err := a.ScanDeadCodeInvestigation(r.Context(), req)
	if err != nil {
		if a.deps.WriteGraphReadError(w, r, err, deadCodeInvestigationCapability) {
			return
		}
		a.deps.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	coverage, err := a.deadCodeInvestigationCoverage(r.Context(), req, scan)
	if err != nil {
		a.deps.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	allReturned := deadCodeInvestigationAllReturned(scan)
	analysis := codemodel.BuildDeadCodeAnalysisForLanguage(allReturned, req.ExcludeDecoratedWith, scan.PolicyStats, req.Language)

	a.deps.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_id":                        req.RepoID,
		"language":                       req.Language,
		"limit":                          req.Limit,
		"offset":                         req.Offset,
		"truncated":                      scan.DisplayTruncated || scan.CandidateScanTruncated,
		"display_truncated":              scan.DisplayTruncated,
		"candidate_scan_truncated":       scan.CandidateScanTruncated,
		"suppressed_truncated":           scan.SuppressedTruncated,
		"next_offset":                    deadCodeInvestigationNextOffset(req, scan),
		"candidate_scan_limit":           scan.CandidateScanLimit,
		"candidate_scan_limit_per_label": scan.CandidateScanLimitPerLabel,
		"candidate_scan_pages":           scan.CandidateScanPages,
		"candidate_scan_rows":            scan.CandidateScanRows,
		"coverage":                       coverage,
		"candidate_buckets": map[string]any{
			"cleanup_ready": scan.CleanupReady,
			"ambiguous":     scan.Ambiguous,
			"suppressed":    scan.Suppressed,
		},
		"bucket_counts": deadCodeInvestigationBucketCounts(scan),
		"root_policy":   deadCodeInvestigationRootPolicy(analysis, scan),
		"language_maturity": deadCodeInvestigationLanguageMap(
			req.Language,
			codemodel.DeadCodeLanguageMaturityReport(),
		),
		"exactness_blockers": deadCodeInvestigationLanguageMap(
			req.Language,
			codemodel.DeadCodeLanguageExactnessBlockerReport(),
		),
		"observed_exactness_blockers": analysis["dead_code_observed_exactness_blockers"],
		"recommended_next_calls":      a.deps.NextCalls(scan),
		"analysis":                    analysis,
	}, querycontract.BuildTruthEnvelope(a.deps.Profile, deadCodeInvestigationCapability, querycontract.TruthBasisHybrid, "resolved from bounded dead-code investigation with coverage and root-policy metadata"))
}

func normalizeDeadCodeInvestigationRequest(req *DeadCodeInvestigationRequest) error {
	if req.Limit <= 0 {
		req.Limit = codeshaping.DeadCodeDefaultLimit
	}
	if req.Limit > DeadCodeMaxLimit {
		req.Limit = DeadCodeMaxLimit
	}
	if req.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if req.Offset > deadCodeInvestigationMaxOffset {
		return fmt.Errorf("offset must be <= %d", deadCodeInvestigationMaxOffset)
	}
	req.Language = normalizeDeadCodeLanguage(req.Language)
	return nil
}

func (a *Analyzer) ScanDeadCodeInvestigation(
	ctx context.Context,
	req DeadCodeInvestigationRequest,
) (DeadCodeInvestigationScan, error) {
	displayWindow := req.Offset + req.Limit
	pageLimit := codeshaping.DeadCodeCandidateQueryLimit(displayWindow)
	totalLimit := codeshaping.DeadCodeCandidateScanLimit(displayWindow)
	scan := DeadCodeInvestigationScan{
		CleanupReady:               make([]map[string]any, 0),
		Ambiguous:                  make([]map[string]any, 0),
		Suppressed:                 make([]map[string]any, 0),
		CandidateScanLimit:         totalLimit,
		CandidateScanLimitPerLabel: totalLimit,
	}
	seenEntityIDs := make(map[string]struct{}, displayWindow+1)
	schedule := codeshaping.NewDeadCodeCandidateSchedule(
		DeadCodeCandidateLabelsForLanguage(req.Language),
		pageLimit,
		totalLimit,
	)

	for {
		page, ok := schedule.NextPage()
		if !ok {
			break
		}
		rows, err := a.deps.CandidateRows(ctx, req.RepoID, page.Label, req.Language, page.Limit, page.Offset)
		if err != nil {
			return scan, err
		}
		scan.CandidateScanPages++
		rowCount := len(rows)
		scan.CandidateScanRows += rowCount
		schedule.Record(page, rowCount)
		rows = FilterDuplicateDeadCodeRows(rows, seenEntityIDs)
		results, contentByID, err := a.buildDeadCodeResults(ctx, rows)
		if err != nil {
			return scan, err
		}
		downgraded := a.LoadDeadCodeDowngradedRoots(ctx, results)
		active, suppressed, stats := partitionDeadCodeInvestigationResults(
			results,
			contentByID,
			req.ExcludeDecoratedWith,
			downgraded,
		)
		addDeadCodePolicyStats(&scan.PolicyStats, stats)
		scan.addSuppressed(suppressed)
		active, err = a.FilterDeadCodeResultsWithoutIncomingEdges(ctx, active, page.Label)
		if err != nil {
			return scan, err
		}
		if scan.addActive(active, req) {
			return scan, nil
		}
	}
	scan.CandidateScanTruncated = schedule.CandidateScanTruncated()
	return scan, nil
}

func partitionDeadCodeInvestigationResults(
	results []map[string]any,
	contentByID map[string]*EntityContent,
	excludedDecorators []string,
	downgraded codemodel.DeadCodeDowngradedRoots,
) ([]map[string]any, []map[string]any, codemodel.DeadCodePolicyStats) {
	active := make([]map[string]any, 0, len(results))
	suppressed := make([]map[string]any, 0)
	stats := codemodel.DeadCodePolicyStats{}
	normalizedDecorators := normalizedDeadCodeDecoratorExclusions(excludedDecorators)

	for _, result := range results {
		entity := contentByID[querycontract.StringVal(result, "entity_id")]
		if deadCodeResultExcludedByDefault(result, entity, &stats, downgraded) {
			result["classification"] = codemodel.DeadCodeClassificationExcluded
			result["suppression_reasons"] = deadCodeSuppressionReasons(result, "default_root_policy")
			attachDeadCodeSourceHandle(result)
			suppressed = append(suppressed, result)
			continue
		}
		if deadCodeResultMatchesDecoratorExclusion(result, normalizedDecorators) {
			result["classification"] = codemodel.DeadCodeClassificationExcluded
			result["suppression_reasons"] = []string{"user_decorator_exclusion"}
			attachDeadCodeSourceHandle(result)
			suppressed = append(suppressed, result)
			continue
		}
		result["classification"] = codemodel.DeadCodeResultClassification(result, entity)
		attachDeadCodeSourceHandle(result)
		active = append(active, result)
	}
	return active, suppressed, stats
}

func normalizedDeadCodeDecoratorExclusions(excluded []string) []string {
	if len(excluded) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(excluded))
	for _, decorator := range excluded {
		if value := codemodel.NormalizeDecoratorName(decorator); value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func deadCodeResultMatchesDecoratorExclusion(result map[string]any, excluded []string) bool {
	if len(excluded) == 0 {
		return false
	}
	metadata, ok := result["metadata"].(map[string]any)
	return ok && codemodel.ResultMatchesDecoratorExclusion(metadata, excluded)
}

func deadCodeSuppressionReasons(result map[string]any, fallback string) []string {
	metadata, _ := result["metadata"].(map[string]any)
	rootKinds := querycontract.StringSliceVal(metadata, "dead_code_root_kinds")
	if len(rootKinds) == 0 {
		return []string{fallback}
	}
	reasons := make([]string, 0, len(rootKinds))
	for _, rootKind := range rootKinds {
		if strings.TrimSpace(rootKind) != "" {
			reasons = append(reasons, "modeled_root:"+rootKind)
		}
	}
	if len(reasons) == 0 {
		return []string{fallback}
	}
	slices.Sort(reasons)
	return reasons
}

func (scan *DeadCodeInvestigationScan) addSuppressed(results []map[string]any) {
	for _, result := range results {
		if len(scan.Suppressed) >= deadCodeSuppressedBucketLimit {
			scan.SuppressedTruncated = true
			return
		}
		scan.Suppressed = append(scan.Suppressed, result)
	}
}

func (scan *DeadCodeInvestigationScan) addActive(results []map[string]any, req DeadCodeInvestigationRequest) bool {
	for _, result := range results {
		scan.ActiveCandidatesSeen++
		if scan.ActiveCandidatesSeen <= req.Offset {
			continue
		}
		if len(scan.CleanupReady)+len(scan.Ambiguous) >= req.Limit {
			scan.DisplayTruncated = true
			return true
		}
		switch querycontract.StringVal(result, "classification") {
		case codemodel.DeadCodeClassificationUnused:
			if deadCodeInvestigationCleanupReadyAllowed(result) {
				scan.CleanupReady = append(scan.CleanupReady, result)
				continue
			}
			result["classification"] = codemodel.DeadCodeClassificationAmbiguous
			result["ambiguity_reasons"] = DeadCodeInvestigationAmbiguityReasons(result)
			scan.Ambiguous = append(scan.Ambiguous, result)
		default:
			if _, ok := result["ambiguity_reasons"]; !ok {
				result["ambiguity_reasons"] = DeadCodeInvestigationAmbiguityReasons(result)
			}
			scan.Ambiguous = append(scan.Ambiguous, result)
		}
	}
	return false
}

func deadCodeInvestigationCleanupReadyAllowed(result map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(querycontract.StringVal(result, "language"))) {
	case "javascript", "jsx", "typescript", "tsx":
		return false
	default:
		return true
	}
}

// DeadCodeInvestigationAmbiguityReasons explains why a result stays ambiguous.
// It is exported because the staying incoming-probe tests in codequery name it.
func DeadCodeInvestigationAmbiguityReasons(result map[string]any) []string {
	// The hidden-consumer reason comes first: it says the answer could not be
	// decided from what the caller may read, which outranks any reason derived
	// from the rows they can.
	if codemodel.DeadCodeResultHasHiddenConsumer(result) {
		return []string{codemodel.DeadCodeHiddenConsumerReason}
	}
	if reason, ok := codemodel.DeadCodeWeakIncomingAmbiguityReason(result); ok {
		return []string{reason}
	}
	language := strings.ToLower(strings.TrimSpace(querycontract.StringVal(result, "language")))
	switch language {
	case "typescript", "tsx":
		return []string{"typescript_dead_code_precision_unvalidated"}
	case "javascript", "jsx":
		return []string{"javascript_dead_code_precision_unvalidated"}
	}
	metadata, _ := result["metadata"].(map[string]any)
	reasons := querycontract.StringSliceVal(metadata, "exactness_blockers")
	if len(reasons) == 0 {
		return []string{"derived_dead_code_truth"}
	}
	slices.Sort(reasons)
	return reasons
}

func attachDeadCodeSourceHandle(result map[string]any) {
	result["source_handle"] = map[string]any{
		"repo_id":       querycontract.StringVal(result, "repo_id"),
		"relative_path": querycontract.StringVal(result, "file_path"),
		"entity_id":     querycontract.StringVal(result, "entity_id"),
		"start_line":    querycontract.IntVal(result, "start_line"),
		"end_line":      querycontract.IntVal(result, "end_line"),
	}
}

func (a *Analyzer) deadCodeInvestigationCoverage(
	ctx context.Context,
	req DeadCodeInvestigationRequest,
	scan DeadCodeInvestigationScan,
) (map[string]any, error) {
	coverage := map[string]any{
		"query_shape":                "bounded_dead_code_investigation",
		"scope_type":                 deadCodeInvestigationScopeType(req.RepoID),
		"repo_id":                    req.RepoID,
		"language":                   req.Language,
		"limit":                      req.Limit,
		"offset":                     req.Offset,
		"paging_mode":                "filtered_active_candidate_offset",
		"truncated":                  scan.DisplayTruncated || scan.CandidateScanTruncated,
		"suppressed_truncated":       scan.SuppressedTruncated,
		"candidate_scan_truncated":   scan.CandidateScanTruncated,
		"candidate_scan_rows":        scan.CandidateScanRows,
		"candidate_scan_pages":       scan.CandidateScanPages,
		"candidate_scan_limit":       scan.CandidateScanLimit,
		"active_candidates_seen":     scan.ActiveCandidatesSeen,
		"content_coverage_available": false,
		"freshness_state":            "not_reported",
	}
	if scan.DisplayTruncated || scan.CandidateScanTruncated {
		coverage["next_offset"] = req.Offset + req.Limit
	}
	if strings.TrimSpace(req.RepoID) == "" || a == nil || a.deps.Content == nil {
		return coverage, nil
	}
	contentCoverage, err := a.deps.Content.RepositoryCoverage(ctx, req.RepoID)
	if err != nil {
		return nil, fmt.Errorf("query repository content coverage: %w", err)
	}
	coverage["content_coverage_available"] = contentCoverage.Available
	if !contentCoverage.Available {
		return coverage, nil
	}
	coverage["file_count"] = contentCoverage.FileCount
	coverage["entity_count"] = contentCoverage.EntityCount
	coverage["languages"] = querycontract.CoverageLanguageMaps(contentCoverage.Languages)
	if latest := latestDeadCodeCoverageTimestamp(contentCoverage); !latest.IsZero() {
		coverage["content_last_indexed_at"] = latest.Format(time.RFC3339Nano)
		coverage["freshness_state"] = "content_index_available"
	}
	return coverage, nil
}

func deadCodeInvestigationScopeType(repoID string) string {
	if strings.TrimSpace(repoID) == "" {
		return "whole_index"
	}
	return "repository"
}

func latestDeadCodeCoverageTimestamp(coverage RepositoryContentCoverage) time.Time {
	if coverage.FileIndexedAt.After(coverage.EntityIndexedAt) {
		return coverage.FileIndexedAt
	}
	return coverage.EntityIndexedAt
}

func deadCodeInvestigationAllReturned(scan DeadCodeInvestigationScan) []map[string]any {
	results := make([]map[string]any, 0, len(scan.CleanupReady)+len(scan.Ambiguous)+len(scan.Suppressed))
	results = append(results, scan.CleanupReady...)
	results = append(results, scan.Ambiguous...)
	results = append(results, scan.Suppressed...)
	return results
}

func deadCodeInvestigationBucketCounts(scan DeadCodeInvestigationScan) map[string]any {
	return map[string]any{
		"cleanup_ready":        len(scan.CleanupReady),
		"ambiguous":            len(scan.Ambiguous),
		"suppressed":           len(scan.Suppressed),
		"suppressed_truncated": scan.SuppressedTruncated,
	}
}

func deadCodeInvestigationRootPolicy(analysis map[string]any, scan DeadCodeInvestigationScan) map[string]any {
	return map[string]any{
		"root_categories_used":                    analysis["root_categories_used"],
		"modeled_entrypoints":                     analysis["modeled_entrypoints"],
		"modeled_framework_roots":                 analysis["modeled_framework_roots"],
		"modeled_public_api":                      analysis["modeled_public_api"],
		"tests_excluded":                          analysis["tests_excluded"],
		"generated_code_excluded":                 analysis["generated_code_excluded"],
		"framework_roots_from_parser_metadata":    scan.PolicyStats.ParserMetadataFrameworkRoots,
		"framework_roots_from_source_fallback":    scan.PolicyStats.SourceFallbackFrameworkRoots,
		"go_semantic_roots_from_parser_metadata":  scan.PolicyStats.GoSemanticRootsFromMetadata,
		"roots_skipped_missing_source":            scan.PolicyStats.RootsSkippedMissingSource,
		"requires_source_before_cleanup_decision": true,
	}
}

func deadCodeInvestigationLanguageMap[T any](language string, values map[string]T) map[string]any {
	if strings.TrimSpace(language) == "" {
		all := make(map[string]any, len(values))
		for key, value := range values {
			all[key] = value
		}
		return all
	}
	if value, ok := values[language]; ok {
		return map[string]any{language: value}
	}
	return map[string]any{language: "unsupported_language"}
}

func deadCodeInvestigationNextOffset(req DeadCodeInvestigationRequest, scan DeadCodeInvestigationScan) any {
	if scan.DisplayTruncated || scan.CandidateScanTruncated {
		return req.Offset + req.Limit
	}
	return nil
}
