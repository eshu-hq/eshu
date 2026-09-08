// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B4 EntityHandler/ContentReader seam stayer for #6060: methods on those types must stay in package query, so this file cannot move to service/

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

func (h *EntityHandler) investigateService(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service investigation requires authoritative platform context truth",
			"unsupported_capability",
			"platform_impact.context_overview",
			h.profile(),
			requiredProfile("platform_impact.context_overview"),
		)
		return
	}

	serviceName := PathParam(r, "service_name")
	if serviceName == "" {
		WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}
	if repositoryAccessFilterFromContext(r.Context()).Empty() {
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}

	ctx, err := h.fetchServiceWorkloadContextWithSelector(r.Context(), serviceWorkloadSelector{
		ServiceName: serviceName,
		ServiceID:   QueryParam(r, "service_id"),
		Repository:  querycontract.FirstNonEmptyString(QueryParam(r, "repo"), QueryParam(r, "repository_id"), QueryParam(r, "repo_id")),
		Environment: QueryParam(r, "environment"),
	}, "service_investigation")
	if err != nil {
		var ambiguous serviceWorkloadAmbiguousError
		if errors.As(err, &ambiguous) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusConflict,
				ErrorCodeAmbiguous,
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
		var repoAmbiguous repositorySelectorAmbiguousError
		if errors.As(err, &repoAmbiguous) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusConflict,
				ErrorCodeAmbiguous,
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
		if isRepositorySelectorNotFound(err) {
			writeServiceStoryEnvelopeError(
				w,
				r,
				http.StatusNotFound,
				ErrorCodeScopeNotFound,
				err.Error(),
				nil,
			)
			return
		}
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if ctx == nil {
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := enrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "service_investigation",
	}); err != nil {
		if writeContentSubstringIndexUnavailable(w, err) {
			return
		}
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service investigation: %v", err))
		return
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		service.BuildServiceInvestigationPacket(serviceName, ctx, serviceInvestigationOptions{
			Environment: QueryParam(r, "environment"),
			Intent:      QueryParam(r, "intent"),
			Question:    QueryParam(r, "question"),
		}),
		BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			TruthBasisHybrid,
			"resolved from service investigation coverage and platform evidence",
		),
	)
}
