// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func (h *RepositoryHandler) repositoryStoryContentCoverage(
	ctx context.Context,
	repoID string,
) (*querycontract.RepositoryContentCoverage, map[string]any, error) {
	coverage, err := h.repositoryStatsContentCoverage(ctx, repoID)
	coverageMap := repositoryStatsCoverageMap(coverage, err, h != nil && h.Content != nil)
	if err != nil || !coverage.Available {
		return nil, repositoryStoryCoverageSummary(nil, coverageMap), err
	}
	return &coverage, repositoryStoryCoverageSummary(&coverage, coverageMap), nil
}

func repositoryStoryCoverageSummaryOrDefault(coverageSummary map[string]any) map[string]any {
	if len(coverageSummary) > 0 {
		return coverageSummary
	}
	return repositoryStoryCoverageSummary(nil, repositoryStatsCoverageMap(
		querycontract.RepositoryContentCoverage{},
		nil,
		false,
	))
}

func repositoryStoryCoverageSummary(
	coverage *querycontract.RepositoryContentCoverage,
	coverageMap map[string]any,
) map[string]any {
	missingEvidence := append([]string{}, querycontract.StringSliceVal(coverageMap, "missing_evidence")...)
	summary := map[string]any{
		"status":                 "unavailable",
		"reason":                 "content_store_coverage_unavailable",
		"source_backend":         querycontract.StringVal(coverageMap, "source_backend"),
		"query_shape":            querycontract.StringVal(coverageMap, "query_shape"),
		"counts_available":       querycontract.BoolVal(coverageMap, "counts_available"),
		"entity_types_available": querycontract.BoolVal(coverageMap, "entity_types_available"),
		"whole_graph_traversal":  querycontract.BoolVal(coverageMap, "whole_graph_traversal"),
		"missing_evidence":       missingEvidence,
	}
	if lastError := querycontract.StringVal(coverageMap, "last_error"); lastError != "" {
		summary["last_error"] = lastError
	}
	if coverage == nil || !coverage.Available || !querycontract.BoolVal(coverageMap, "counts_available") {
		return summary
	}

	summary["status"] = "available"
	summary["reason"] = "content_store_coverage"
	summary["file_count"] = coverage.FileCount
	summary["entity_count"] = coverage.EntityCount
	summary["languages"] = repositoryStatsLanguageNames(coverage.Languages)
	summary["entity_types"] = repositoryStatsEntityTypeNames(coverage.EntityTypes)
	if latest := querycontract.StringVal(coverageMap, "content_last_indexed_at"); latest != "" {
		summary["content_last_indexed_at"] = latest
	}
	return summary
}

func repositoryStoryCoverageLimitations(coverageSummary map[string]any) []string {
	if querycontract.StringVal(coverageSummary, "status") == "available" {
		return []string{}
	}
	missingEvidence := querycontract.StringSliceVal(coverageSummary, "missing_evidence")
	if len(missingEvidence) == 0 {
		return []string{"content_store_coverage"}
	}
	return append([]string(nil), missingEvidence...)
}
