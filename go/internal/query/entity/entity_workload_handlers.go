// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// GetWorkloadContext retrieves the context for a specific workload. Exported so the staying contract endpoint test keeps driving the handler; see #6060. The
// response carries the canonical truth envelope plus an additive result_limits
// drilldown block and an explicit partial_reasons slot so a prompt-ready caller
// sees bounds and missing evidence without raw Cypher.
func (h *EntityHandler) GetWorkloadContext(w http.ResponseWriter, r *http.Request) {
	if !querycontract.RequireContextOverview(w, r, h.profile(), "workload context requires authoritative platform context truth") {
		return
	}
	workloadID := querycontract.PathParam(r, "workload_id")
	if workloadID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "workload_id is required")
		return
	}
	if querycontract.RepositoryAccessFilterFromContext(r.Context()).Empty() {
		querycontract.WriteError(w, http.StatusNotFound, "workload not found")
		return
	}

	ctx, err := h.fetchWorkloadContext(r.Context(), "w.id = $workload_id", map[string]any{"workload_id": workloadID})
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		querycontract.WriteError(w, http.StatusNotFound, "workload not found")
		return
	}
	if err := service.EnrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, service.ServiceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "workload_context",
	}); err != nil {
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich workload context: %v", err))
		return
	}

	ctx["result_limits"] = querycontract.WorkloadContextResultLimits(ctx, workloadID, "context")
	ctx["partial_reasons"] = querycontract.ContextPartialReasons(ctx)
	querycontract.WriteSuccess(w, r, http.StatusOK, ctx, workloadContextTruthEnvelope(h.profile(), "context"))
}

// GetWorkloadStory retrieves a narrative summary for a workload. Exported so the staying contract endpoint test keeps driving the handler; see #6060. The response
// carries the canonical truth envelope plus an additive result_limits drilldown
// block and an explicit partial_reasons slot, matching the workload context
// route so answer composition sees consistent envelope metadata.
func (h *EntityHandler) GetWorkloadStory(w http.ResponseWriter, r *http.Request) {
	if !querycontract.RequireContextOverview(w, r, h.profile(), "workload story requires authoritative platform context truth") {
		return
	}
	workloadID := querycontract.PathParam(r, "workload_id")
	if workloadID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "workload_id is required")
		return
	}
	if querycontract.RepositoryAccessFilterFromContext(r.Context()).Empty() {
		querycontract.WriteError(w, http.StatusNotFound, "workload not found")
		return
	}

	ctx, err := h.fetchWorkloadContext(r.Context(), "w.id = $workload_id", map[string]any{"workload_id": workloadID})
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		querycontract.WriteError(w, http.StatusNotFound, "workload not found")
		return
	}
	if err := service.EnrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, service.ServiceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "workload_story",
	}); err != nil {
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich workload story: %v", err))
		return
	}

	story := buildWorkloadStory(ctx)
	response := map[string]any{
		"workload_id":     workloadID,
		"name":            ctx["name"],
		"story":           story,
		"result_limits":   querycontract.WorkloadContextResultLimits(ctx, workloadID, "story"),
		"partial_reasons": querycontract.ContextPartialReasons(ctx),
	}
	querycontract.AttachEvidenceBoundaries(response, "get_workload_story")
	querycontract.WriteSuccess(w, r, http.StatusOK, response, workloadContextTruthEnvelope(h.profile(), "story"))
}
