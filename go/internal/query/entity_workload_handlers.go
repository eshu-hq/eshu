// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
)

// getWorkloadContext retrieves the context for a specific workload. The
// response carries the canonical truth envelope plus an additive result_limits
// drilldown block and an explicit partial_reasons slot so a prompt-ready caller
// sees bounds and missing evidence without raw Cypher.
func (h *EntityHandler) getWorkloadContext(w http.ResponseWriter, r *http.Request) {
	if !requireContextOverview(w, r, h.profile(), "workload context requires authoritative platform context truth") {
		return
	}
	workloadID := PathParam(r, "workload_id")
	if workloadID == "" {
		WriteError(w, http.StatusBadRequest, "workload_id is required")
		return
	}
	if repositoryAccessFilterFromContext(r.Context()).empty() {
		WriteError(w, http.StatusNotFound, "workload not found")
		return
	}

	ctx, err := h.fetchWorkloadContext(r.Context(), "w.id = $workload_id", map[string]any{"workload_id": workloadID})
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		WriteError(w, http.StatusNotFound, "workload not found")
		return
	}
	if err := enrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "workload_context",
	}); err != nil {
		if writeContentSubstringIndexUnavailable(w, err) {
			return
		}
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich workload context: %v", err))
		return
	}

	ctx["result_limits"] = workloadContextResultLimits(ctx, workloadID, "context")
	ctx["partial_reasons"] = contextPartialReasons(ctx)
	WriteSuccess(w, r, http.StatusOK, ctx, workloadContextTruthEnvelope(h.profile(), "context"))
}

// getWorkloadStory retrieves a narrative summary for a workload. The response
// carries the canonical truth envelope plus an additive result_limits drilldown
// block and an explicit partial_reasons slot, matching the workload context
// route so answer composition sees consistent envelope metadata.
func (h *EntityHandler) getWorkloadStory(w http.ResponseWriter, r *http.Request) {
	if !requireContextOverview(w, r, h.profile(), "workload story requires authoritative platform context truth") {
		return
	}
	workloadID := PathParam(r, "workload_id")
	if workloadID == "" {
		WriteError(w, http.StatusBadRequest, "workload_id is required")
		return
	}
	if repositoryAccessFilterFromContext(r.Context()).empty() {
		WriteError(w, http.StatusNotFound, "workload not found")
		return
	}

	ctx, err := h.fetchWorkloadContext(r.Context(), "w.id = $workload_id", map[string]any{"workload_id": workloadID})
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		WriteError(w, http.StatusNotFound, "workload not found")
		return
	}
	if err := enrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "workload_story",
	}); err != nil {
		if writeContentSubstringIndexUnavailable(w, err) {
			return
		}
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich workload story: %v", err))
		return
	}

	story := buildWorkloadStory(ctx)
	response := map[string]any{
		"workload_id":     workloadID,
		"name":            ctx["name"],
		"story":           story,
		"result_limits":   workloadContextResultLimits(ctx, workloadID, "story"),
		"partial_reasons": contextPartialReasons(ctx),
	}
	attachEvidenceBoundaries(response, "get_workload_story")
	WriteSuccess(w, r, http.StatusOK, response, workloadContextTruthEnvelope(h.profile(), "story"))
}
