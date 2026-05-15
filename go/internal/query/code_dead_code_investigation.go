package query

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	deadCodeInvestigationCapability = "code_quality.dead_code"
	deadCodeInvestigationMaxOffset  = 2000
	deadCodeSuppressedBucketLimit   = 50
)

type deadCodeInvestigationRequest struct {
	RepoID               string   `json:"repo_id"`
	Language             string   `json:"language"`
	Limit                int      `json:"limit"`
	Offset               int      `json:"offset"`
	ExcludeDecoratedWith []string `json:"exclude_decorated_with"`
}

type deadCodeInvestigationScan struct {
	CleanupReady           []map[string]any
	Ambiguous              []map[string]any
	Suppressed             []map[string]any
	PolicyStats            deadCodePolicyStats
	DisplayTruncated       bool
	CandidateScanTruncated bool
	SuppressedTruncated    bool
	CandidateScanLimit     int
	CandidateScanPages     int
	CandidateScanRows      int
	ActiveCandidatesSeen   int
}

// handleDeadCodeInvestigation returns the prompt-oriented dead-code packet used
// by MCP clients that need coverage, paging, candidate buckets, and drill-down
// handles without interpreting the lower-level analysis payload themselves.
func (h *CodeHandler) handleDeadCodeInvestigation(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDeadCodeInvestigation,
		"POST /api/v0/code/dead-code/investigate",
		deadCodeInvestigationCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), deadCodeInvestigationCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dead code investigation requires authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			deadCodeInvestigationCapability,
			h.profile(),
			requiredProfile(deadCodeInvestigationCapability),
		)
		return
	}

	var req deadCodeInvestigationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := normalizeDeadCodeInvestigationRequest(&req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}

	scan, err := h.scanDeadCodeInvestigation(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	coverage, err := h.deadCodeInvestigationCoverage(r.Context(), req, scan)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	allReturned := deadCodeInvestigationAllReturned(scan)
	analysis := buildDeadCodeAnalysis(allReturned, req.ExcludeDecoratedWith, scan.PolicyStats)

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_id":                  req.RepoID,
		"language":                 req.Language,
		"limit":                    req.Limit,
		"offset":                   req.Offset,
		"truncated":                scan.DisplayTruncated || scan.CandidateScanTruncated,
		"display_truncated":        scan.DisplayTruncated,
		"candidate_scan_truncated": scan.CandidateScanTruncated,
		"candidate_scan_limit":     scan.CandidateScanLimit,
		"candidate_scan_pages":     scan.CandidateScanPages,
		"candidate_scan_rows":      scan.CandidateScanRows,
		"coverage":                 coverage,
		"candidate_buckets": map[string]any{
			"cleanup_ready": scan.CleanupReady,
			"ambiguous":     scan.Ambiguous,
			"suppressed":    scan.Suppressed,
		},
		"bucket_counts": deadCodeInvestigationBucketCounts(scan),
		"root_policy":   deadCodeInvestigationRootPolicy(analysis, scan),
		"language_maturity": deadCodeInvestigationLanguageMap(
			req.Language,
			deadCodeLanguageMaturityReport(),
		),
		"exactness_blockers": deadCodeInvestigationLanguageMap(
			req.Language,
			deadCodeLanguageExactnessBlockerReport(),
		),
		"observed_exactness_blockers": analysis["dead_code_observed_exactness_blockers"],
		"recommended_next_calls":      deadCodeInvestigationNextCalls(scan),
		"analysis":                    analysis,
	}, BuildTruthEnvelope(h.profile(), deadCodeInvestigationCapability, TruthBasisHybrid, "resolved from bounded dead-code investigation with coverage and root-policy metadata"))
}

func normalizeDeadCodeInvestigationRequest(req *deadCodeInvestigationRequest) error {
	if req.Limit <= 0 {
		req.Limit = deadCodeDefaultLimit
	}
	if req.Limit > deadCodeMaxLimit {
		req.Limit = deadCodeMaxLimit
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

func (h *CodeHandler) scanDeadCodeInvestigation(
	ctx context.Context,
	req deadCodeInvestigationRequest,
) (deadCodeInvestigationScan, error) {
	displayWindow := req.Offset + req.Limit
	pageLimit := deadCodeCandidateQueryLimit(displayWindow)
	scan := deadCodeInvestigationScan{
		CleanupReady:       make([]map[string]any, 0),
		Ambiguous:          make([]map[string]any, 0),
		Suppressed:         make([]map[string]any, 0),
		CandidateScanLimit: deadCodeCandidateScanLimit(displayWindow),
	}
	seenEntityIDs := make(map[string]struct{}, displayWindow+1)

	for _, label := range deadCodeCandidateLabelsForLanguage(req.Language) {
		for rawOffset := 0; rawOffset < scan.CandidateScanLimit; rawOffset += pageLimit {
			limit := min(pageLimit, scan.CandidateScanLimit-rawOffset)
			rows, err := h.deadCodeCandidateRows(ctx, req.RepoID, label, req.Language, limit, rawOffset)
			if err != nil {
				return scan, err
			}
			scan.CandidateScanPages++
			rowCount := len(rows)
			scan.CandidateScanRows += rowCount
			rows = filterDuplicateDeadCodeRows(rows, seenEntityIDs)
			results, contentByID, err := h.buildDeadCodeResults(ctx, rows)
			if err != nil {
				return scan, err
			}
			active, suppressed, stats := partitionDeadCodeInvestigationResults(
				results,
				contentByID,
				req.ExcludeDecoratedWith,
			)
			addDeadCodePolicyStats(&scan.PolicyStats, stats)
			scan.addSuppressed(suppressed)
			active, err = h.filterDeadCodeResultsWithoutIncomingEdges(ctx, active, label)
			if err != nil {
				return scan, err
			}
			if scan.addActive(active, req) {
				return scan, nil
			}
			if rowCount < limit {
				break
			}
			if rawOffset+rowCount >= scan.CandidateScanLimit {
				scan.CandidateScanTruncated = true
				return scan, nil
			}
		}
	}
	return scan, nil
}

func partitionDeadCodeInvestigationResults(
	results []map[string]any,
	contentByID map[string]*EntityContent,
	excludedDecorators []string,
) ([]map[string]any, []map[string]any, deadCodePolicyStats) {
	active := make([]map[string]any, 0, len(results))
	suppressed := make([]map[string]any, 0)
	stats := deadCodePolicyStats{}
	normalizedDecorators := normalizedDeadCodeDecoratorExclusions(excludedDecorators)

	for _, result := range results {
		entity := contentByID[StringVal(result, "entity_id")]
		if deadCodeResultExcludedByDefault(result, entity, &stats) {
			result["classification"] = deadCodeClassificationExcluded
			result["suppression_reasons"] = deadCodeSuppressionReasons(result, "default_root_policy")
			attachDeadCodeSourceHandle(result)
			suppressed = append(suppressed, result)
			continue
		}
		if deadCodeResultMatchesDecoratorExclusion(result, normalizedDecorators) {
			result["classification"] = deadCodeClassificationExcluded
			result["suppression_reasons"] = []string{"user_decorator_exclusion"}
			attachDeadCodeSourceHandle(result)
			suppressed = append(suppressed, result)
			continue
		}
		result["classification"] = deadCodeResultClassification(result, entity)
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
		if value := normalizeDecoratorName(decorator); value != "" {
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
	return ok && resultMatchesDecoratorExclusion(metadata, excluded)
}

func deadCodeSuppressionReasons(result map[string]any, fallback string) []string {
	metadata, _ := result["metadata"].(map[string]any)
	rootKinds := StringSliceVal(metadata, "dead_code_root_kinds")
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

func (scan *deadCodeInvestigationScan) addSuppressed(results []map[string]any) {
	for _, result := range results {
		if len(scan.Suppressed) >= deadCodeSuppressedBucketLimit {
			scan.SuppressedTruncated = true
			return
		}
		scan.Suppressed = append(scan.Suppressed, result)
	}
}

func (scan *deadCodeInvestigationScan) addActive(results []map[string]any, req deadCodeInvestigationRequest) bool {
	for _, result := range results {
		scan.ActiveCandidatesSeen++
		if scan.ActiveCandidatesSeen <= req.Offset {
			continue
		}
		if len(scan.CleanupReady)+len(scan.Ambiguous) >= req.Limit {
			scan.DisplayTruncated = true
			return true
		}
		switch StringVal(result, "classification") {
		case deadCodeClassificationUnused:
			if deadCodeInvestigationCleanupReadyAllowed(result) {
				scan.CleanupReady = append(scan.CleanupReady, result)
				continue
			}
			result["classification"] = deadCodeClassificationAmbiguous
			result["ambiguity_reasons"] = deadCodeInvestigationAmbiguityReasons(result)
			scan.Ambiguous = append(scan.Ambiguous, result)
		default:
			if _, ok := result["ambiguity_reasons"]; !ok {
				result["ambiguity_reasons"] = deadCodeInvestigationAmbiguityReasons(result)
			}
			scan.Ambiguous = append(scan.Ambiguous, result)
		}
	}
	return false
}

func deadCodeInvestigationCleanupReadyAllowed(result map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(StringVal(result, "language"))) {
	case "javascript", "jsx", "typescript", "tsx":
		return false
	default:
		return true
	}
}

func deadCodeInvestigationAmbiguityReasons(result map[string]any) []string {
	language := strings.ToLower(strings.TrimSpace(StringVal(result, "language")))
	switch language {
	case "typescript", "tsx":
		return []string{"typescript_dead_code_precision_unvalidated"}
	case "javascript", "jsx":
		return []string{"javascript_dead_code_precision_unvalidated"}
	}
	metadata, _ := result["metadata"].(map[string]any)
	reasons := StringSliceVal(metadata, "exactness_blockers")
	if len(reasons) == 0 {
		return []string{"derived_dead_code_truth"}
	}
	slices.Sort(reasons)
	return reasons
}

func attachDeadCodeSourceHandle(result map[string]any) {
	result["source_handle"] = map[string]any{
		"repo_id":       StringVal(result, "repo_id"),
		"relative_path": StringVal(result, "file_path"),
		"entity_id":     StringVal(result, "entity_id"),
		"start_line":    IntVal(result, "start_line"),
		"end_line":      IntVal(result, "end_line"),
	}
}

func (h *CodeHandler) deadCodeInvestigationCoverage(
	ctx context.Context,
	req deadCodeInvestigationRequest,
	scan deadCodeInvestigationScan,
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
	if strings.TrimSpace(req.RepoID) == "" || h == nil || h.Content == nil {
		return coverage, nil
	}
	contentCoverage, err := h.Content.RepositoryCoverage(ctx, req.RepoID)
	if err != nil {
		return nil, fmt.Errorf("query repository content coverage: %w", err)
	}
	coverage["content_coverage_available"] = contentCoverage.Available
	if !contentCoverage.Available {
		return coverage, nil
	}
	coverage["file_count"] = contentCoverage.FileCount
	coverage["entity_count"] = contentCoverage.EntityCount
	coverage["languages"] = coverageLanguageMaps(contentCoverage.Languages)
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

func deadCodeInvestigationAllReturned(scan deadCodeInvestigationScan) []map[string]any {
	results := make([]map[string]any, 0, len(scan.CleanupReady)+len(scan.Ambiguous)+len(scan.Suppressed))
	results = append(results, scan.CleanupReady...)
	results = append(results, scan.Ambiguous...)
	results = append(results, scan.Suppressed...)
	return results
}

func deadCodeInvestigationBucketCounts(scan deadCodeInvestigationScan) map[string]any {
	return map[string]any{
		"cleanup_ready":        len(scan.CleanupReady),
		"ambiguous":            len(scan.Ambiguous),
		"suppressed":           len(scan.Suppressed),
		"suppressed_truncated": scan.SuppressedTruncated,
	}
}

func deadCodeInvestigationRootPolicy(analysis map[string]any, scan deadCodeInvestigationScan) map[string]any {
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

func deadCodeInvestigationNextCalls(scan deadCodeInvestigationScan) []map[string]any {
	candidates := append([]map[string]any{}, scan.CleanupReady...)
	candidates = append(candidates, scan.Ambiguous...)
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	next := make([]map[string]any, 0, len(candidates)*2)
	for _, candidate := range candidates {
		entityID := StringVal(candidate, "entity_id")
		if entityID == "" {
			continue
		}
		next = append(next, map[string]any{
			"tool":      "get_entity_content",
			"arguments": map[string]any{"entity_id": entityID},
			"reason":    "read the exact source before changing or deleting the candidate",
		})
		next = append(next, map[string]any{
			"tool": "get_code_relationship_story",
			"arguments": map[string]any{
				"entity_id":          entityID,
				"direction":          "incoming",
				"relationship_type":  "CALLS",
				"include_transitive": false,
				"limit":              25,
				"offset":             0,
			},
			"reason": "verify direct caller evidence before treating the candidate as cleanup-ready",
		})
	}
	return next
}
