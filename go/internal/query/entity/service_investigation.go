// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryselector"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// InvestigateService serves the service-investigation route. Exported so the staying graph-read-error test keeps driving the handler; see #6060.
func (h *EntityHandler) InvestigateService(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service investigation requires authoritative platform context truth",
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

	ctx, err := h.fetchServiceWorkloadContextWithSelector(r.Context(), ServiceWorkloadSelector{
		ServiceName: serviceName,
		ServiceID:   querycontract.QueryParam(r, "service_id"),
		Repository:  querycontract.FirstNonEmptyString(querycontract.QueryParam(r, "repo"), querycontract.QueryParam(r, "repository_id"), querycontract.QueryParam(r, "repo_id")),
		Environment: querycontract.QueryParam(r, "environment"),
	}, "service_investigation")
	if err != nil {
		var ambiguous serviceWorkloadAmbiguousError
		if errors.As(err, &ambiguous) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusConflict,
				querycontract.ErrorCodeAmbiguous,
				ambiguous.Error(),
				map[string]any{
					"status":     "ambiguous",
					"selector":   ambiguous.Selector,
					"candidates": serviceWorkloadCandidateMaps(ambiguous.Candidates),
					"truncated":  ambiguous.Truncated,
				},
			)
			return
		}
		var repoAmbiguous queryselector.AmbiguousError
		if errors.As(err, &repoAmbiguous) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusConflict,
				querycontract.ErrorCodeAmbiguous,
				repoAmbiguous.Error(),
				map[string]any{
					"status":     "ambiguous",
					"selector":   repoAmbiguous.Selector,
					"candidates": repoAmbiguous.Matches,
					"truncated":  false,
				},
			)
			return
		}
		if queryselector.IsNotFound(err) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusNotFound,
				querycontract.ErrorCodeScopeNotFound,
				err.Error(),
				nil,
			)
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
		Operation:                 "service_investigation",
	}); err != nil {
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service investigation: %v", err))
		return
	}

	querycontract.WriteSuccess(
		w,
		r,
		http.StatusOK,
		service.BuildServiceInvestigationPacket(serviceName, ctx, service.InvestigationOptions{
			Environment: querycontract.QueryParam(r, "environment"),
			Intent:      querycontract.QueryParam(r, "intent"),
			Question:    querycontract.QueryParam(r, "question"),
		}),
		querycontract.BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			querycontract.TruthBasisHybrid,
			"resolved from service investigation coverage and platform evidence",
		),
	)
}
