// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// readDownstreamReadLimit parses the optional downstream_read_limit query
// parameter (#10477): absent means the legacy default (zero, so enrichment
// keeps its MaxDepth-derived bound); a present value must be a non-negative
// integer, clamped to the shared ceiling, and the effective bound is
// returned. A non-numeric or negative value answers 400 invalid_argument.
func readDownstreamReadLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "downstream_read_limit")
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		querycontract.WriteErrorEnvelope(w, r, http.StatusBadRequest, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeInvalidArgument,
			Message:    "downstream_read_limit must be a non-negative integer",
			Capability: "platform_impact.context_overview",
		})
		return 0, false
	}
	return querycontract.BoundedDownstreamReadLimit(value), true
}

// readEndpointListPaging parses the limit/offset query parameters of the
// service API-surface endpoint list (#10477): absent means the defaults
// (limit 50, offset 0); a present limit must be a positive integer and clamps
// to the shared page ceiling, a present offset must be a non-negative
// integer. Anything else answers 400 invalid_argument.
func readEndpointListPaging(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	limit = service.ServiceAPISurfaceListDefaultLimit
	if raw := querycontract.QueryParam(r, "limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			querycontract.WriteErrorEnvelope(w, r, http.StatusBadRequest, &querycontract.ErrorEnvelope{
				Code:       querycontract.ErrorCodeInvalidArgument,
				Message:    "limit must be a positive integer",
				Capability: "platform_impact.context_overview",
			})
			return 0, 0, false
		}
		limit = value
		if limit > service.ServiceAPISurfaceListMaxLimit {
			limit = service.ServiceAPISurfaceListMaxLimit
		}
	}
	if raw := querycontract.QueryParam(r, "offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			querycontract.WriteErrorEnvelope(w, r, http.StatusBadRequest, &querycontract.ErrorEnvelope{
				Code:       querycontract.ErrorCodeInvalidArgument,
				Message:    "offset must be a non-negative integer",
				Capability: "platform_impact.context_overview",
			})
			return 0, 0, false
		}
		offset = value
	}
	return limit, offset, true
}

// GetServiceAPISurfaceEndpoints serves one page of a service's API surface
// endpoints in ORDER BY path, endpoint_id order (#10477). The selector
// matches get_service_story (path service_name plus service_id, repo, and
// environment query parameters); the offset applies at the Cypher read, so
// pages are disjoint without re-running the dossier enrichment. The dossier
// story and context keep their 50-row preview and point here.
func (h *Handler) GetServiceAPISurfaceEndpoints(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service API surface endpoints require authoritative platform context truth",
			"unsupported_capability",
			"platform_impact.context_overview",
			h.profile(),
			querycontract.RequiredProfile("platform_impact.context_overview"),
		)
		return
	}
	if querycontract.RepositoryAccessFilterFromContext(r.Context()).Empty() {
		querycontract.WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	limit, offset, ok := readEndpointListPaging(w, r)
	if !ok {
		return
	}
	serviceName := querycontract.PathParam(r, "service_name")
	if serviceName == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}
	workloadCtx, err := h.fetchServiceWorkloadContextWithSelector(r.Context(), ServiceWorkloadSelector{
		ServiceName: serviceName,
		ServiceID:   querycontract.QueryParam(r, "service_id"),
		Repository:  querycontract.QueryParam(r, "repo"),
		Environment: querycontract.QueryParam(r, "environment"),
	}, "service_api_surface_endpoints")
	if err != nil {
		status, errEnv := serviceStoryResolutionError(r.Context(), err)
		querycontract.WriteErrorEnvelope(w, r, status, errEnv)
		return
	}
	if workloadCtx == nil {
		querycontract.WriteErrorEnvelope(w, r, http.StatusNotFound, serviceStoryNotFoundError())
		return
	}
	page, listErr := service.ListServiceAPISurfaceEndpoints(
		r.Context(), h.Neo4j, querycontract.SafeStr(workloadCtx, "repo_id"), limit, offset,
	)
	if listErr != nil {
		if querycontract.WriteGraphReadError(w, r, listErr, "platform_impact.context_overview") {
			return
		}
		tracing.WriteServerFailure(w, r, listErr, http.StatusInternalServerError, serviceAPISurfaceEndpointsQueryFailedMessage)
		return
	}
	// A resolved service with no endpoints (or an offset past the end) is a
	// 200 empty page carrying the true total, not a 404: the service exists,
	// the set is just empty. Only an unresolvable read (nil page) 404s.
	if len(page) == 0 {
		querycontract.WriteError(w, http.StatusNotFound, "service API surface not found")
		return
	}
	name := querycontract.SafeStr(workloadCtx, "name")
	if name == "" {
		name = serviceName
	}
	page["service_name"] = name
	truth := querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", querycontract.TruthBasisHybrid, "paged service API surface endpoints")
	querycontract.WriteSuccess(w, r, http.StatusOK, page, truth)
}
