// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// GetServiceContext retrieves the context for a service by name. Exported so
// the staying graph-read-error tests keep driving the handler; see #6060.
//
// Split out of handler.go (#6786 review follow-up) to keep that file under
// the repository's 500-line cap after the review round's fixes.
func (h *Handler) GetServiceContext(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service context requires authoritative platform context truth",
			"unsupported_capability",
			"platform_impact.context_overview",
			h.profile(),
			querycontract.RequiredProfile("platform_impact.context_overview"),
		)
		return
	}

	serviceName := querycontract.PathParam(r, "service_name")
	if serviceName == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}
	if querycontract.RepositoryAccessFilterFromContext(r.Context()).Empty() {
		querycontract.WriteError(w, http.StatusNotFound, "service not found")
		return
	}

	ctx, err := h.fetchServiceWorkloadContext(r.Context(), serviceName, "service_context")
	if err != nil {
		if querycontract.WriteWorkloadSelectorOverflow(w, err) {
			return
		}
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		querycontract.WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := service.EnrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, service.QueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "service_context",
	}); err != nil {
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service context: %v", err))
		return
	}

	langBreakdown, toolBreakdown, fingerprintDegraded := repository.QueryServiceTechFingerprint(r.Context(), h.Neo4j, ctx)
	if len(fingerprintDegraded) > 0 {
		// A failed language or source-tool read degrades to a named limitation
		// (#6810) instead of an empty breakdown that reads as "none".
		ctx["limitations"] = append(querycontract.StringSliceVal(ctx, "limitations"), fingerprintDegraded...)
	}
	if len(langBreakdown) > 0 || len(toolBreakdown) > 0 {
		if len(langBreakdown) > 0 {
			ctx["language_breakdown"] = langBreakdown
		}
		if len(toolBreakdown) > 0 {
			ctx["source_tool_breakdown"] = toolBreakdown
		}
	}

	// Promote "limitations" into the OpenAPI-promised "partial_reasons" field
	// (round-11 review follow-up to #5764, PR #5936): the WorkloadContext
	// schema this route shares with getWorkloadContext documents
	// "partial_reasons" as always present, and getWorkloadContext already
	// makes that true. Without this call an infrastructure-read degradation
	// or truncation landed in "limitations" but never reached the stable
	// partial-reason field the contract promises HTTP and MCP callers.
	ctx["partial_reasons"] = querycontract.ContextPartialReasons(ctx)
	querycontract.WriteSuccess(w, r, http.StatusOK, ctx, querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", querycontract.TruthBasisHybrid, "resolved from service context and platform evidence"))
}
