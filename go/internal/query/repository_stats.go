// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	repositoryStatsContentCoverageShape = "content_store_repository_coverage"
	repositoryStatsIdentityOnlyShape    = "repository_identity_only"
	repositoryStatsReadTimeout          = 2 * time.Second
)

// getRepositoryStats returns bounded repository statistics from read models. The
// response carries the canonical truth envelope plus an additive result_limits
// drilldown block and an explicit partial_reasons slot so a prompt-ready caller
// sees fan-out bounds and missing evidence without raw Cypher, while the
// existing coverage partial_results/truncated/timeout fields are preserved.
func (h *RepositoryHandler) getRepositoryStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), repositoryStatsReadTimeout)
	defer cancel()

	repoID, ok := h.resolveRepositoryStatsPathSelector(ctx, w, r)
	if !ok {
		return
	}

	timer := startRepositoryQueryStage(ctx, h.Logger, "repository_stats", repoID, "repository_lookup")
	repo, repoSource, err := h.repositoryStatsRepositoryRef(ctx, repoID)
	timer.Done(
		ctx,
		slog.Bool("found", repo != nil),
		slog.Bool("error", err != nil),
		slog.String("source_backend", repoSource),
		slog.String("query_shape", "repository_id_lookup"),
	)
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, repositoryStatsErrorStatus(err), fmt.Sprintf("query repository failed: %v", err))
		return
	}
	if repo == nil {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_stats", repoID, "content_coverage")
	coverage, coverageErr := h.repositoryStatsContentCoverage(ctx, repoID)
	coverageMap := repositoryStatsCoverageMap(coverage, coverageErr, h.Content != nil)
	timer.Done(ctx, repositoryStatsCoverageLogAttrs(coverageMap, coverageErr)...)

	response := repositoryStatsResponse(repo, coverage, coverageMap)
	response["result_limits"] = repositoryStatsResultLimits(response, repoID)
	response["partial_reasons"] = repositoryStatsPartialReasons(coverageMap)

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		response,
		BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			repositoryStatsTruthBasis(coverageMap),
			"resolved from bounded repository identity and content coverage; missing coverage remains explicit",
		),
	)
}

func (h *RepositoryHandler) resolveRepositoryStatsPathSelector(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
) (string, bool) {
	repoSelector := PathParam(r, "repo_id")
	if repoSelector == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required")
		return "", false
	}
	repoID, err := h.resolveRepositorySelector(ctx, repoSelector)
	if err != nil {
		// Selector resolution issues its own graph read, so a bounded backend
		// timeout/outage must map to 503/504 rather than being downgraded to
		// 400 by the generic branch below. repositoryStatsErrorStatus only
		// recognizes context.DeadlineExceeded, not the ErrGraphReadDeadline/
		// ErrGraphUnavailable sentinels, which never wrap it.
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return "", false
		}
		status := repositoryStatsErrorStatus(err)
		if status == http.StatusInternalServerError {
			status = http.StatusBadRequest
		}
		if isRepositorySelectorNotFound(err) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return "", false
	}
	return repoID, true
}

func (h *RepositoryHandler) repositoryStatsRepositoryRef(
	ctx context.Context,
	repoID string,
) (any, string, error) {
	if h != nil && h.Neo4j != nil {
		row, err := h.Neo4j.RunSingle(ctx, repositoryBaseCypher, map[string]any{"repo_id": repoID})
		if err != nil {
			return nil, "graph", err
		}
		if row != nil {
			return RepoRefFromRow(row), "graph", nil
		}
	}
	if h != nil && h.Content != nil {
		repo, err := h.Content.ResolveRepository(ctx, repoID)
		if err != nil {
			return nil, "content_store", err
		}
		if repo != nil {
			return repositoryCatalogMap(*repo), "content_store", nil
		}
	}
	return nil, "unavailable", nil
}

func (h *RepositoryHandler) repositoryStatsContentCoverage(
	ctx context.Context,
	repoID string,
) (RepositoryContentCoverage, error) {
	if h == nil || h.Content == nil {
		return RepositoryContentCoverage{}, nil
	}
	coverage, err := h.Content.RepositoryCoverage(ctx, repoID)
	if err != nil {
		return RepositoryContentCoverage{}, err
	}
	if !coverage.Available || !repositoryStatsCoverageHasEvidence(coverage) {
		return RepositoryContentCoverage{}, nil
	}
	return coverage, nil
}

func repositoryStatsResponse(
	repo any,
	coverage RepositoryContentCoverage,
	coverageMap map[string]any,
) map[string]any {
	stats := map[string]any{
		"repository":   repo,
		"file_count":   nil,
		"languages":    []string{},
		"entity_count": nil,
		"entity_types": []string{},
		"coverage":     coverageMap,
	}
	if !coverage.Available {
		return stats
	}
	stats["file_count"] = coverage.FileCount
	stats["languages"] = repositoryStatsLanguageNames(coverage.Languages)
	stats["entity_count"] = coverage.EntityCount
	stats["entity_types"] = repositoryStatsEntityTypeNames(coverage.EntityTypes)
	return stats
}

func repositoryStatsTruthBasis(coverageMap map[string]any) TruthBasis {
	if StringVal(coverageMap, "source_backend") == "content_store" {
		return TruthBasisContentIndex
	}
	return TruthBasisHybrid
}

func repositoryStatsCoverageMap(
	coverage RepositoryContentCoverage,
	coverageErr error,
	contentConfigured bool,
) map[string]any {
	if coverage.Available && coverageErr == nil {
		coverageMap := map[string]any{
			"source_backend":         "content_store",
			"query_shape":            repositoryStatsContentCoverageShape,
			"counts_available":       true,
			"entity_types_available": true,
			"whole_graph_traversal":  false,
			"partial_results":        false,
			"truncated":              false,
			"timeout":                false,
			"timeout_budget":         repositoryStatsReadTimeout.String(),
			"missing_evidence":       []string{},
			"file_count_source":      "content_files",
			"entity_count_source":    "content_entities",
			"languages_source":       "content_files",
			"entity_types_source":    "content_entities",
		}
		if latest := latestCoverageTimestamp(coverage.FileIndexedAt, coverage.EntityIndexedAt); !latest.IsZero() {
			coverageMap["content_last_indexed_at"] = formatCoverageTimestamp(latest)
		}
		return coverageMap
	}

	missingEvidence := []string{"content_store_coverage"}
	lastError := ""
	timeout := repositoryStatsErrIsTimeout(coverageErr)
	switch {
	case timeout:
		missingEvidence = []string{"content_store_coverage_timeout"}
		lastError = fmt.Sprintf("content store coverage exceeded %s route timeout", repositoryStatsReadTimeout)
	case coverageErr != nil:
		missingEvidence = []string{"content_store_coverage_error"}
		lastError = coverageErr.Error()
	case !contentConfigured:
		lastError = "content store not configured"
	default:
		lastError = "content store coverage unavailable"
	}

	coverageMap := map[string]any{
		"source_backend":         "unavailable",
		"query_shape":            repositoryStatsIdentityOnlyShape,
		"counts_available":       false,
		"entity_types_available": false,
		"whole_graph_traversal":  false,
		"partial_results":        true,
		"truncated":              timeout,
		"timeout":                timeout,
		"timeout_budget":         repositoryStatsReadTimeout.String(),
		"missing_evidence":       missingEvidence,
		"last_error":             lastError,
	}
	return coverageMap
}

func repositoryStatsErrorStatus(err error) int {
	if repositoryStatsErrIsTimeout(err) {
		return http.StatusGatewayTimeout
	}
	return http.StatusInternalServerError
}

func repositoryStatsErrIsTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func repositoryStatsCoverageLogAttrs(coverageMap map[string]any, coverageErr error) []slog.Attr {
	return []slog.Attr{
		slog.Bool("error", coverageErr != nil),
		slog.String("source_backend", StringVal(coverageMap, "source_backend")),
		slog.String("query_shape", StringVal(coverageMap, "query_shape")),
		slog.Bool("counts_available", BoolVal(coverageMap, "counts_available")),
		slog.Bool("entity_types_available", BoolVal(coverageMap, "entity_types_available")),
		slog.Bool("whole_graph_traversal", BoolVal(coverageMap, "whole_graph_traversal")),
		slog.Bool("partial_results", BoolVal(coverageMap, "partial_results")),
		slog.Bool("truncated", BoolVal(coverageMap, "truncated")),
		slog.Bool("timeout", BoolVal(coverageMap, "timeout")),
		slog.Any("missing_evidence", coverageMap["missing_evidence"]),
	}
}

func repositoryStatsLanguageNames(languages []RepositoryLanguageCount) []string {
	names := make([]string, 0, len(languages))
	for _, language := range languages {
		if language.Language == "" {
			continue
		}
		names = append(names, language.Language)
	}
	return names
}

func repositoryStatsEntityTypeNames(entityTypes []RepositoryEntityTypeCount) []string {
	names := make([]string, 0, len(entityTypes))
	for _, entityType := range entityTypes {
		if entityType.EntityType == "" {
			continue
		}
		names = append(names, entityType.EntityType)
	}
	return names
}

func repositoryStatsCoverageHasEvidence(coverage RepositoryContentCoverage) bool {
	return coverage.FileCount > 0 ||
		coverage.EntityCount > 0 ||
		len(coverage.Languages) > 0 ||
		len(coverage.EntityTypes) > 0 ||
		!coverage.FileIndexedAt.IsZero() ||
		!coverage.EntityIndexedAt.IsZero()
}
