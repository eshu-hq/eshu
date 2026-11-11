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

// readDownstreamConsumersPaging parses the limit and keyset-cursor query
// parameters of the service downstream-consumer list (#10535): absent means
// the defaults (limit 50, start of the set); a present limit must be a
// positive integer and clamps to the shared page ceiling; after_repository
// and after_repo_id travel together as the cursor naming the last served row
// (either both present or both absent). Anything else answers 400
// invalid_argument.
func readDownstreamConsumersPaging(w http.ResponseWriter, r *http.Request) (limit int, after *service.ConsumerListCursor, ok bool) {
	limit = service.ConsumerListDefaultPageSize
	if raw := querycontract.QueryParam(r, "limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			querycontract.WriteErrorEnvelope(w, r, http.StatusBadRequest, &querycontract.ErrorEnvelope{
				Code:       querycontract.ErrorCodeInvalidArgument,
				Message:    "limit must be a positive integer",
				Capability: "platform_impact.context_overview",
			})
			return 0, nil, false
		}
		limit = value
		if limit > service.ConsumerListMaxPageSize {
			limit = service.ConsumerListMaxPageSize
		}
	}
	afterRepo := querycontract.QueryParam(r, "after_repository")
	afterID := querycontract.QueryParam(r, "after_repo_id")
	if (afterRepo == "") != (afterID == "") {
		querycontract.WriteErrorEnvelope(w, r, http.StatusBadRequest, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeInvalidArgument,
			Message:    "after_repository and after_repo_id must be provided together",
			Capability: "platform_impact.context_overview",
		})
		return 0, nil, false
	}
	if afterRepo != "" {
		after = &service.ConsumerListCursor{Repository: afterRepo, RepoID: afterID}
	}
	return limit, after, true
}

// GetServiceDownstreamConsumers serves one keyset page of a service's
// downstream consumer repositories in (repository, repo_id) order (#10535).
// The selector matches get_service_story (path service_name plus service_id,
// repo, and environment query parameters); the page is recomputed from a
// widened internal read on every call and the cursor drops served rows, so
// pages are disjoint without any stored server state. A grant-filtered
// caller sees only granted rows, and an empty page is a 200 with no cursor
// (terminal), not a 404: only an unresolvable service 404s.
func (h *Handler) GetServiceDownstreamConsumers(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service downstream consumers require authoritative platform context truth",
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
	limit, after, ok := readDownstreamConsumersPaging(w, r)
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
	}, "service_downstream_consumers")
	if err != nil {
		status, errEnv := serviceStoryResolutionError(r.Context(), err)
		querycontract.WriteErrorEnvelope(w, r, status, errEnv)
		return
	}
	if workloadCtx == nil {
		querycontract.WriteErrorEnvelope(w, r, http.StatusNotFound, serviceStoryNotFoundError())
		return
	}
	repoID := querycontract.SafeStr(workloadCtx, "repo_id")
	name := querycontract.SafeStr(workloadContextOrDefault(workloadCtx, serviceName), "name")
	if name == "" {
		name = serviceName
	}
	page, listErr := service.ListServiceDownstreamConsumers(
		r.Context(), h.Neo4j, h.Content, repoID, name,
		querycontract.RepositoryAccessFilterFromContext(r.Context()),
		after, limit,
	)
	if listErr != nil {
		if querycontract.WriteGraphReadError(w, r, listErr, "platform_impact.context_overview") {
			return
		}
		tracing.WriteServerFailure(w, r, listErr, http.StatusInternalServerError, serviceDownstreamConsumersQueryFailedMessage)
		return
	}
	body := map[string]any{
		"service_name": name,
		"consumers":    page.Consumers,
		"truncated":    page.Truncated,
		"consumer_truncation_sources": map[string]any{
			"candidates":     page.Sources.Candidates,
			"evidence_files": page.Sources.EvidenceFiles,
			"hostnames":      page.Sources.Hostnames,
			"searches":       page.Sources.Searches,
			"merge_cap":      page.Sources.MergeCap,
		},
		"searched_hostnames":   page.SearchedHostnames,
		"effective_read_bound": page.EffectiveBound,
		"lower_bound":          page.LowerBound,
		"ordering":             "repository,repo_id",
		"page_size":            limit,
	}
	if page.HasNext && page.NextCursor != nil {
		body["next_cursor"] = map[string]any{
			"repository": page.NextCursor.Repository,
			"repo_id":    page.NextCursor.RepoID,
		}
	}
	if page.Sources.MergeCap {
		body["ceiling_reached"] = true
	}
	truth := querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", querycontract.TruthBasisHybrid, "paged service downstream consumers")
	querycontract.WriteSuccess(w, r, http.StatusOK, body, truth)
}

// workloadContextOrDefault keeps the canonical-name lookup on one line: the
// resolved workload context when present, else a minimal map carrying the
// requested service name.
func workloadContextOrDefault(workloadCtx map[string]any, serviceName string) map[string]any {
	if len(workloadCtx) > 0 {
		return workloadCtx
	}
	return map[string]any{"name": serviceName}
}
