// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Mount registers the cheap-summary aggregate routes alongside the existing
// CI/CD run correlation list route.
func (h *Handler) cicdRunCorrelationAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations/count", h.countRunCorrelations)
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations/inventory", h.runCorrelationInventory)
}

func (h *Handler) countRunCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCICDRunCorrelationAggregate,
		"GET /api/v0/ci-cd/run-correlations/count",
		AggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), AggregateCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"CI/CD run correlation aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			AggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(AggregateCapability),
		)
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	filter, ok := h.cicdRunCorrelationAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	if !validateRunCorrelationAggregateOutcome(w, filter) {
		return
	}
	if access.Empty() {
		h.writeEmptyRunCorrelationAggregateCount(w, r)
		return
	}
	if h.Aggregates == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"CI/CD run correlation aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			AggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(AggregateCapability),
		)
		return
	}
	count, err := h.Aggregates.CountRunCorrelations(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_correlations": count.TotalCorrelations,
		"by_outcome":         count.ByOutcome,
		"by_environment":     count.ByEnvironment,
		"by_provider":        count.ByProvider,
		"scope":              cicdRunCorrelationAggregateScope(filter),
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		AggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; outcome, environment, and provider rollups stay separate",
	))
}

func (h *Handler) runCorrelationInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCICDRunCorrelationAggregate,
		"GET /api/v0/ci-cd/run-correlations/inventory",
		AggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), AggregateCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"CI/CD run correlation aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			AggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(AggregateCapability),
		)
		return
	}

	dimension := RunCorrelationInventoryDimension(querycontract.QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = RunCorrelationInventoryByOutcome
	}
	if !isSupportedRunCorrelationDimension(dimension) {
		querycontract.WriteError(w, http.StatusBadRequest, "group_by must be one of outcome, environment, repository_id, provider")
		return
	}
	limit, ok := parseRunCorrelationAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parseRunCorrelationAggregateOffset(w, r)
	if !ok {
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	filter, ok := h.cicdRunCorrelationAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	if !validateRunCorrelationAggregateOutcome(w, filter) {
		return
	}
	if access.Empty() {
		h.writeEmptyRunCorrelationInventory(w, r, dimension, limit, offset)
		return
	}
	if h.Aggregates == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"CI/CD run correlation aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			AggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(AggregateCapability),
		)
		return
	}

	rows, err := h.Aggregates.RunCorrelationInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	body := map[string]any{
		"buckets":     rows,
		"count":       len(rows),
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   truncated,
		"next_offset": nextRunCorrelationAggregateOffset(offset, limit, truncated),
		"scope":       cicdRunCorrelationAggregateScope(filter),
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		AggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; one grouped bucket per row, ordered by count desc",
	))
}

func (h *Handler) cicdRunCorrelationAggregateFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	access querycontract.RepositoryAccessFilter,
) (RunCorrelationAggregateFilter, bool) {
	repositorySelector := querycontract.QueryParam(r, "repository_id")
	repositoryID := repositorySelector
	if !access.Empty() {
		var ok bool
		repositoryID, ok = selector.ResolveForRequestWithAccess(
			w,
			r,
			nil,
			h.Content,
			repositorySelector,
			access,
			AggregateCapability,
		)
		if !ok {
			return RunCorrelationAggregateFilter{}, false
		}
	}
	filter := RunCorrelationAggregateFilter{
		ScopeID:        querycontract.QueryParam(r, "scope_id"),
		RepositoryID:   repositoryID,
		CommitSHA:      querycontract.QueryParam(r, "commit_sha"),
		Provider:       querycontract.QueryParam(r, "provider"),
		ArtifactDigest: querycontract.QueryParam(r, "artifact_digest"),
		ImageRef:       querycontract.QueryParam(r, "image_ref"),
		Environment:    querycontract.QueryParam(r, "environment"),
		Outcome:        querycontract.QueryParam(r, "outcome"),
	}
	return cicdRunCorrelationAggregateFilterWithRepositoryAccess(filter, access), true
}

func cicdRunCorrelationAggregateFilterWithRepositoryAccess(
	filter RunCorrelationAggregateFilter,
	access querycontract.RepositoryAccessFilter,
) RunCorrelationAggregateFilter {
	if !access.Scoped() {
		return filter
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter
}

func (h *Handler) writeEmptyRunCorrelationAggregateCount(
	w http.ResponseWriter,
	r *http.Request,
) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_correlations": 0,
		"by_outcome":         map[string]int{},
		"by_environment":     map[string]int{},
		"by_provider":        map[string]int{},
		"scope":              map[string]string{},
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		AggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; outcome, environment, and provider rollups stay separate",
	))
}

func (h *Handler) writeEmptyRunCorrelationInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension RunCorrelationInventoryDimension,
	limit int,
	offset int,
) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":     []RunCorrelationInventoryRow{},
		"count":       0,
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   false,
		"next_offset": nil,
		"scope":       map[string]string{},
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		AggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; one grouped bucket per row, ordered by count desc",
	))
}

func cicdRunCorrelationAggregateScope(filter RunCorrelationAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.ScopeID != "" {
		out["scope_id"] = filter.ScopeID
	}
	if filter.RepositoryID != "" {
		out["repository_id"] = filter.RepositoryID
	}
	if filter.CommitSHA != "" {
		out["commit_sha"] = filter.CommitSHA
	}
	if filter.Provider != "" {
		out["provider"] = filter.Provider
	}
	if filter.ArtifactDigest != "" {
		out["artifact_digest"] = filter.ArtifactDigest
	}
	if filter.ImageRef != "" {
		out["image_ref"] = filter.ImageRef
	}
	if filter.Environment != "" {
		out["environment"] = filter.Environment
	}
	if filter.Outcome != "" {
		out["outcome"] = filter.Outcome
	}
	return out
}

func isSupportedRunCorrelationDimension(d RunCorrelationInventoryDimension) bool {
	switch d {
	case RunCorrelationInventoryByOutcome,
		RunCorrelationInventoryByEnvironment,
		RunCorrelationInventoryByRepository,
		RunCorrelationInventoryByProvider:
		return true
	default:
		return false
	}
}

// validateRunCorrelationAggregateOutcome rejects unknown outcome filters
// with a 400 so a typo does not silently return zero counts. Matches the
// enum advertised by openapi/paths/cicd/routes.go for the list endpoint and the
// new aggregate routes.
func validateRunCorrelationAggregateOutcome(w http.ResponseWriter, filter RunCorrelationAggregateFilter) bool {
	if filter.Outcome == "" || isSupportedRunCorrelationOutcome(filter.Outcome) {
		return true
	}
	querycontract.WriteError(w, http.StatusBadRequest, "outcome must be one of exact, derived, ambiguous, unresolved, rejected")
	return false
}

const (
	cicdRunCorrelationAggregateDefaultLimit = 100
	cicdRunCorrelationAggregateMinLimit     = 1
	// cicdRunCorrelationAggregateMaxOffset matches the OpenAPI offset bound
	// and keeps Postgres OFFSET scans bounded. Past this point callers
	// should narrow scope (repository_id, provider, environment) or fall
	// back to the list endpoint with anchored pagination.
	cicdRunCorrelationAggregateMaxOffset = 10000
)

func parseRunCorrelationAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		return cicdRunCorrelationAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < cicdRunCorrelationAggregateMinLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > RunCorrelationAggregateMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseRunCorrelationAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > cicdRunCorrelationAggregateMaxOffset {
		querycontract.WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// nextRunCorrelationAggregateOffset returns the next offset when a
// truncated page can be continued without exceeding the documented offset
// bound, and nil otherwise. Callers serialize the nil as JSON null so
// generated clients see a clean end-of-stream marker.
func nextRunCorrelationAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > cicdRunCorrelationAggregateMaxOffset {
		return nil
	}
	return next
}
