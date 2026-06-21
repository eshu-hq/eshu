package query

import "sort"

// repositoryStatsItemLimit bounds the language and entity-type fan-out attached
// to a single repository stats payload so one prompt-ready read stays within the
// route budget and exposes truncation explicitly. It mirrors the
// contextStoryItemLimit bound used by the context/story routes.
const repositoryStatsItemLimit = 50

// repositoryStatsResultLimits builds the additive result_limits drilldown block
// for a singleton repository stats payload. It caps the languages and
// entity_types fan-out in place, reports deterministic ordering, and names the
// per-repository coverage drilldown plus the stats self path so callers can
// drill down without falling back to raw Cypher. The block is additive: it
// preserves the existing coverage partial_results/truncated/timeout fields.
func repositoryStatsResultLimits(response map[string]any, repoID string) map[string]any {
	languages := StringSliceVal(response, "languages")
	entityTypes := StringSliceVal(response, "entity_types")
	languageTotal := len(languages)
	entityTypeTotal := len(entityTypes)

	cappedLanguages, langTrunc := capStringRows(languages, repositoryStatsItemLimit)
	cappedEntityTypes, typeTrunc := capStringRows(entityTypes, repositoryStatsItemLimit)
	if languageTotal > 0 {
		response["languages"] = cappedLanguages
	}
	if entityTypeTotal > 0 {
		response["entity_types"] = cappedEntityTypes
	}

	return map[string]any{
		"limit":             repositoryStatsItemLimit,
		"ordering":          "deterministic",
		"language_count":    languageTotal,
		"entity_type_count": entityTypeTotal,
		"truncated":         langTrunc || typeTrunc,
		"drilldown_basis":   "repository_id",
		"drilldown_tool":    "get_repository_coverage",
		"context_path":      "/api/v0/repositories/" + repoID + "/stats",
	}
}

// repositoryStatsPartialReasons promotes the coverage map's transport-level
// limitations into an explicit, sorted partial_reasons array for a singleton
// stats payload. Callers see timeouts and missing evidence directly instead of
// inferring completeness from the coverage flags. The result is always non-nil
// so the envelope shape is stable across complete and partial reads.
func repositoryStatsPartialReasons(coverageMap map[string]any) []string {
	seen := map[string]struct{}{}
	reasons := make([]string, 0)
	add := func(reason string) {
		if reason == "" {
			return
		}
		if _, ok := seen[reason]; ok {
			return
		}
		seen[reason] = struct{}{}
		reasons = append(reasons, reason)
	}
	for _, reason := range StringSliceVal(coverageMap, "missing_evidence") {
		add(reason)
	}
	if BoolVal(coverageMap, "timeout") {
		add("content_store_coverage_timeout")
	}
	sort.Strings(reasons)
	return reasons
}

// repositoryInventoryResultLimits builds the additive result_limits drilldown
// block for the inventory (empty-selector) form of get_repository_stats served
// by the repository list route. The page limit is the bound, ordering is
// deterministic by name then id, and the drilldown names the per-repository
// stats tool plus the inventory self path. It is additive and preserves the
// existing list truncated field.
func repositoryInventoryResultLimits(page repositoryListPage, count int, truncated bool) map[string]any {
	return map[string]any{
		"limit":            page.Limit,
		"offset":           page.Offset,
		"ordering":         "deterministic",
		"repository_count": count,
		"truncated":        truncated,
		"drilldown_basis":  "repository_id",
		"drilldown_tool":   "get_repository_stats",
		"context_path":     "/api/v0/repositories",
	}
}

// repositoryInventoryPartialReasons promotes inventory paging truncation and
// missing repository group evidence into an explicit partial_reasons array. The
// result is always non-nil so the envelope shape is stable across full,
// truncated, and partially attributed inventory reads.
func repositoryInventoryPartialReasons(truncated bool, repos []map[string]any) []string {
	reasons := make([]string, 0, 2)
	if truncated {
		reasons = append(reasons, "repository_inventory_truncated")
	}
	if repositoryGroupEvidenceMissing(repos) {
		reasons = append(reasons, repositoryGroupMissingReason)
	}
	return reasons
}

// repositoryInventoryResponse wraps the bounded repository list page with the
// additive result_limits drilldown block and explicit partial_reasons slot used
// by the inventory (empty-selector) form of get_repository_stats. It preserves
// the existing repositoryListResponse fields (repositories, count, limit,
// offset, truncated) and adds total: the true repository count independent of
// page size, so callers can distinguish the per-page count from the overall
// dataset size.
func repositoryInventoryResponse(repos []map[string]any, page repositoryListPage, truncated bool, total int) map[string]any {
	response := repositoryListResponse(repos, page, truncated, total)
	response["result_limits"] = repositoryInventoryResultLimits(page, total, truncated)
	response["partial_reasons"] = repositoryInventoryPartialReasons(truncated, repos)
	return response
}

// capStringRows caps a string slice at limit and reports whether truncation
// occurred. It mirrors capMapRows for the flat string fan-out (languages,
// entity_types) carried by the repository stats payload.
func capStringRows(rows []string, limit int) ([]string, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}
