// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	cicdRunCorrelationsCapability = "ci_cd.run_correlations.list"
	cicdRunCorrelationMaxLimit    = 200
)

// CICDHandler exposes reducer-owned CI/CD run correlation reads.
type CICDHandler struct {
	Content      ContentStore
	Correlations CICDRunCorrelationStore
	Aggregates   CICDRunCorrelationAggregateStore
	// CollectorReadiness answers the configured-collector probe for the gated
	// CI/CD run-correlation list tool so an empty page reports not_configured
	// when the ci_cd_run collector is disabled. It is optional: a nil store
	// leaves the collector_readiness envelope off the response.
	CollectorReadiness CollectorListReadinessStore
	Profile            QueryProfile
}

// CICDRunCorrelationResult is one reducer-owned CI/CD run correlation row.
// It is an alias onto querycontract so the moved repository artifact
// evidence assembly can name it from outside this package (#6060, lane B B3).
type CICDRunCorrelationResult = querycontract.CICDRunCorrelationResult

// Mount registers CI/CD query routes.
func (h *CICDHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations", h.listRunCorrelations)
	h.cicdRunCorrelationAggregateRoutes(mux)
}

func (h *CICDHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *CICDHandler) listRunCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCICDRunCorrelations,
		"GET /api/v0/ci-cd/run-correlations",
		cicdRunCorrelationsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), cicdRunCorrelationsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"CI/CD run correlations require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			cicdRunCorrelationsCapability,
			h.profile(),
			requiredProfile(cicdRunCorrelationsCapability),
		)
		return
	}
	limit, ok := requiredCICDRunCorrelationLimit(w, r)
	if !ok {
		return
	}
	access := repositoryAccessFilterFromContext(r.Context())
	repositorySelector := QueryParam(r, "repository_id")
	filter := CICDRunCorrelationFilter{
		ScopeID:            QueryParam(r, "scope_id"),
		RepositoryID:       repositorySelector,
		CommitSHA:          QueryParam(r, "commit_sha"),
		Provider:           QueryParam(r, "provider"),
		ProviderRunID:      firstNonEmpty(QueryParam(r, "provider_run_id"), QueryParam(r, "run_id")),
		ArtifactDigest:     QueryParam(r, "artifact_digest"),
		ImageRef:           QueryParam(r, "image_ref"),
		Environment:        QueryParam(r, "environment"),
		Outcome:            QueryParam(r, "outcome"),
		AfterCorrelationID: QueryParam(r, "after_correlation_id"),
		Limit:              limit + 1,
	}
	if !filter.HasScope() {
		WriteError(w, http.StatusBadRequest, "scope_id, repository_id, commit_sha, provider_run_id, artifact_digest, image_ref, or environment is required")
		return
	}
	if filter.ProviderRunID != "" && filter.Provider == "" && !filter.HasProviderRunDisambiguator() {
		WriteError(w, http.StatusBadRequest, "provider is required when provider_run_id is the only anchor")
		return
	}
	if access.Empty() {
		h.writeEmptyCICDRunCorrelationPage(w, r, limit)
		return
	}
	repositoryID, ok := resolveRepositorySelectorForRequestWithAccess(
		w,
		r,
		nil,
		h.Content,
		repositorySelector,
		access,
		cicdRunCorrelationsCapability,
	)
	if !ok {
		return
	}
	filter.RepositoryID = repositoryID
	if h.Correlations == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"CI/CD run correlations require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			cicdRunCorrelationsCapability,
			h.profile(),
			requiredProfile(cicdRunCorrelationsCapability),
		)
		return
	}

	filter = cicdRunCorrelationFilterWithRepositoryAccess(filter, access)
	rows, err := h.Correlations.ListCICDRunCorrelations(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]CICDRunCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, CICDRunCorrelationResult(row))
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		cicdRunCorrelationsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; deployment promotion stays absent unless artifact identity evidence is exact",
	))
}

func (h *CICDHandler) writeEmptyCICDRunCorrelationPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	body := map[string]any{
		"correlations":     []CICDRunCorrelationResult{},
		"count":            0,
		"limit":            limit,
		"truncated":        false,
		"evidence_summary": h.runCorrelationEvidenceSummary(r.Context(), "", nil, false),
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorCICDRun, 0, false)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		cicdRunCorrelationsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; deployment promotion stays absent unless artifact identity evidence is exact",
	))
}

func cicdRunCorrelationFilterWithRepositoryAccess(
	filter CICDRunCorrelationFilter,
	access repositoryAccessFilter,
) CICDRunCorrelationFilter {
	if !access.Scoped() {
		return filter
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter
}

func requiredCICDRunCorrelationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > cicdRunCorrelationMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", cicdRunCorrelationMaxLimit))
		return 0, false
	}
	return limit, true
}
