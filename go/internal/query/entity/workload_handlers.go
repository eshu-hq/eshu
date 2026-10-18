// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// workloadContextLookupClause maps a caller-supplied workload selector to the
// lookup rung fetchWorkloadContextDecision runs. #5385 flip wave: a workload:
// handle matches no post-flip node id, so it reads the bounded name-candidate
// rung with the stripped bare name; the existing lookupWorkloadRows name
// branch then applies grant admission with lowest-admitted-id-wins. Full
// scoped ids keep the exact unique-constrained id rung. Bare inputs keep the
// id rung unchanged: they matched nothing before the flip and still match
// nothing, so no contract widens.
func workloadContextLookupClause(workloadID string) (string, map[string]any) {
	if workloadid.ClassifyWorkloadSelector(workloadID) == workloadid.SelectorKindHandle {
		return "w.name = $workload_id", map[string]any{"workload_id": workloadid.StripWorkloadHandlePrefix(workloadID)}
	}
	return "w.id = $workload_id", map[string]any{"workload_id": workloadID}
}

// GetWorkloadContext retrieves the context for a specific workload. Exported so the staying contract endpoint test keeps driving the handler; see #6060. The
// response carries the canonical truth envelope plus an additive result_limits
// drilldown block and an explicit partial_reasons slot so a prompt-ready caller
// sees bounds and missing evidence without raw Cypher.
func (h *Handler) GetWorkloadContext(w http.ResponseWriter, r *http.Request) {
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

	evidenceDetail, ok := readContextEvidenceDetail(w, r)
	if !ok {
		return
	}

	whereClause, lookupParams := workloadContextLookupClause(workloadID)
	ctx, err := h.fetchWorkloadContext(r.Context(), whereClause, lookupParams)
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", workloadContextQueryFailedMessage)
		return
	}

	if ctx == nil {
		querycontract.WriteError(w, http.StatusNotFound, "workload not found")
		return
	}
	if err := service.EnrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, service.QueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Instruments:               h.Instruments,
		Operation:                 "workload_context",
	}); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		if unavailableSentinel(r.Context(), err, querycontract.ErrContentSubstringIndexesNotReady) {
			querycontract.WriteError(w, http.StatusServiceUnavailable, querycontract.ErrContentSubstringIndexesNotReady.Error())
			return
		}
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, workloadContextEnrichmentFailedMessage)
		return
	}

	ctx["result_limits"] = querycontract.WorkloadContextResultLimits(ctx, workloadID, "context")
	ctx["partial_reasons"] = querycontract.ContextPartialReasons(ctx)
	truth := workloadContextTruthEnvelope(h.profile(), "context")
	truth.Omissions = querycontract.ApplyContextEvidenceDetail(ctx, evidenceDetail)
	querycontract.WriteSuccess(w, r, http.StatusOK, ctx, truth)
}

// GetWorkloadStory retrieves a narrative summary for a workload. Exported so the staying contract endpoint test keeps driving the handler; see #6060. The response
// carries the canonical truth envelope plus an additive result_limits drilldown
// block and an explicit partial_reasons slot, matching the workload context
// route so answer composition sees consistent envelope metadata.
func (h *Handler) GetWorkloadStory(w http.ResponseWriter, r *http.Request) {
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

	whereClause, lookupParams := workloadContextLookupClause(workloadID)
	ctx, err := h.fetchWorkloadContext(r.Context(), whereClause, lookupParams)
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", workloadStoryQueryFailedMessage)
		return
	}

	if ctx == nil {
		querycontract.WriteError(w, http.StatusNotFound, "workload not found")
		return
	}
	if err := service.EnrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, service.QueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Instruments:               h.Instruments,
		Operation:                 "workload_story",
	}); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		if unavailableSentinel(r.Context(), err, querycontract.ErrContentSubstringIndexesNotReady) {
			querycontract.WriteError(w, http.StatusServiceUnavailable, querycontract.ErrContentSubstringIndexesNotReady.Error())
			return
		}
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, workloadStoryEnrichmentFailedMessage)
		return
	}

	story := buildWorkloadStory(ctx)
	// #5385 flip wave: echo the resolved node id, not the raw path param.
	// A handle request resolves to the scoped node; echoing the handle
	// would re-emit the retired handle==id equation the flip removes.
	resolvedID := querycontract.StringVal(ctx, "id")
	if resolvedID == "" {
		resolvedID = workloadID
	}
	response := map[string]any{
		"workload_id":     resolvedID,
		"name":            ctx["name"],
		"story":           story,
		"result_limits":   querycontract.WorkloadContextResultLimits(ctx, resolvedID, "story"),
		"partial_reasons": querycontract.ContextPartialReasons(ctx),
	}
	evidence.AttachEvidenceBoundaries(response, "get_workload_story")
	querycontract.WriteSuccess(w, r, http.StatusOK, response, workloadContextTruthEnvelope(h.profile(), "story"))
}
