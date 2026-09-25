// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const cicdRunCorrelationMaxLimit = 200

// Handler exposes reducer-owned CI/CD run correlation reads.
type Handler struct {
	Content      querycontract.ContentStore
	Correlations querycontract.CICDRunCorrelationStore
	Aggregates   RunCorrelationAggregateStore
	// CollectorReadiness answers the configured-collector probe for the gated
	// CI/CD run-correlation list tool so an empty page reports not_configured
	// when the ci_cd_run collector is disabled. It is optional: a nil store
	// leaves the collector_readiness envelope off the response.
	CollectorReadiness querycontract.CollectorListReadinessStore
	Profile            querycontract.QueryProfile
}

// Mount registers CI/CD query routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations", h.listRunCorrelations)
	h.cicdRunCorrelationAggregateRoutes(mux)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *Handler) listRunCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCICDRunCorrelations,
		"GET /api/v0/ci-cd/run-correlations",
		Capability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), Capability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"CI/CD run correlations require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}
	limit, ok := requiredRunCorrelationLimit(w, r)
	if !ok {
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	repositorySelector := querycontract.QueryParam(r, "repository_id")
	filter := querycontract.CICDRunCorrelationFilter{
		ScopeID:            querycontract.QueryParam(r, "scope_id"),
		RepositoryID:       repositorySelector,
		CommitSHA:          querycontract.QueryParam(r, "commit_sha"),
		Provider:           querycontract.QueryParam(r, "provider"),
		ProviderRunID:      querycontract.FirstNonEmpty(querycontract.QueryParam(r, "provider_run_id"), querycontract.QueryParam(r, "run_id")),
		ArtifactDigest:     querycontract.QueryParam(r, "artifact_digest"),
		ImageRef:           querycontract.QueryParam(r, "image_ref"),
		Environment:        querycontract.QueryParam(r, "environment"),
		Outcome:            querycontract.QueryParam(r, "outcome"),
		AfterCorrelationID: querycontract.QueryParam(r, "after_correlation_id"),
		Limit:              limit + 1,
	}
	if !filter.HasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id, repository_id, commit_sha, provider_run_id, artifact_digest, image_ref, or environment is required")
		return
	}
	if filter.ProviderRunID != "" && filter.Provider == "" && !filter.HasProviderRunDisambiguator() {
		querycontract.WriteError(w, http.StatusBadRequest, "provider is required when provider_run_id is the only anchor")
		return
	}
	if access.Empty() {
		h.writeEmptyRunCorrelationPage(w, r, limit)
		return
	}
	// A nil graph is deliberate: the read model needs no graph traversal, so
	// selector resolution must not issue a live graph read on this route.
	repositoryID, ok := selector.ResolveForRequestWithAccess(
		w,
		r,
		nil,
		h.Content,
		repositorySelector,
		access,
		Capability,
	)
	if !ok {
		return
	}
	filter.RepositoryID = repositoryID
	if h.Correlations == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"CI/CD run correlations require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}

	filter = runCorrelationFilterWithRepositoryAccess(filter, access)
	rows, err := h.Correlations.ListCICDRunCorrelations(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]querycontract.CICDRunCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, querycontract.CICDRunCorrelationResult(row))
	}
	body := map[string]any{
		"correlations":     results,
		"count":            len(results),
		"limit":            limit,
		"truncated":        truncated,
		"evidence_summary": h.runCorrelationEvidenceSummary(r.Context(), repositoryID, results, truncated),
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_correlation_id": results[len(results)-1].CorrelationID,
		}
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorCICDRun, len(results), truncated)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		Capability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; deployment promotion stays absent unless artifact identity evidence is exact",
	))
}

func (h *Handler) writeEmptyRunCorrelationPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	body := map[string]any{
		"correlations":     []querycontract.CICDRunCorrelationResult{},
		"count":            0,
		"limit":            limit,
		"truncated":        false,
		"evidence_summary": h.runCorrelationEvidenceSummary(r.Context(), "", nil, false),
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorCICDRun, 0, false)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		Capability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; deployment promotion stays absent unless artifact identity evidence is exact",
	))
}

func runCorrelationFilterWithRepositoryAccess(
	filter querycontract.CICDRunCorrelationFilter,
	access querycontract.RepositoryAccessFilter,
) querycontract.CICDRunCorrelationFilter {
	if !access.Scoped() {
		return filter
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter
}

func requiredRunCorrelationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > cicdRunCorrelationMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", cicdRunCorrelationMaxLimit))
		return 0, false
	}
	return limit, true
}
