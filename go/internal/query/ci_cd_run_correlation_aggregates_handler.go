// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const cicdRunCorrelationAggregateCapability = "ci_cd.run_correlations.aggregate"

// Mount registers the cheap-summary aggregate routes alongside the existing
// CI/CD run correlation list route.
func (h *CICDHandler) cicdRunCorrelationAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations/count", h.countRunCorrelations)
	mux.HandleFunc("GET /api/v0/ci-cd/run-correlations/inventory", h.runCorrelationInventory)
}

func (h *CICDHandler) countRunCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCICDRunCorrelationAggregate,
		"GET /api/v0/ci-cd/run-correlations/count",
		cicdRunCorrelationAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), cicdRunCorrelationAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"CI/CD run correlation aggregates require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			cicdRunCorrelationAggregateCapability,
			h.profile(),
			requiredProfile(cicdRunCorrelationAggregateCapability),
		)
		return
	}
	access := repositoryAccessFilterFromContext(r.Context())
	filter, ok := h.cicdRunCorrelationAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	if !validateCICDRunCorrelationAggregateOutcome(w, filter) {
		return
	}
	if access.empty() {
		h.writeEmptyCICDRunCorrelationAggregateCount(w, r)
		return
	}
	if h.Aggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"CI/CD run correlation aggregates require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			cicdRunCorrelationAggregateCapability,
			h.profile(),
			requiredProfile(cicdRunCorrelationAggregateCapability),
		)
		return
	}
	count, err := h.Aggregates.CountCICDRunCorrelations(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_correlations": count.TotalCorrelations,
		"by_outcome":         count.ByOutcome,
		"by_environment":     count.ByEnvironment,
		"by_provider":        count.ByProvider,
		"scope":              cicdRunCorrelationAggregateScope(filter),
	}, BuildTruthEnvelope(
		h.profile(),
		cicdRunCorrelationAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; outcome, environment, and provider rollups stay separate",
	))
}

func (h *CICDHandler) runCorrelationInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCICDRunCorrelationAggregate,
		"GET /api/v0/ci-cd/run-correlations/inventory",
		cicdRunCorrelationAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), cicdRunCorrelationAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"CI/CD run correlation aggregates require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			cicdRunCorrelationAggregateCapability,
			h.profile(),
			requiredProfile(cicdRunCorrelationAggregateCapability),
		)
		return
	}

	dimension := CICDRunCorrelationInventoryDimension(QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = CICDRunCorrelationInventoryByOutcome
	}
	if !isSupportedCICDRunCorrelationDimension(dimension) {
		WriteError(w, http.StatusBadRequest, "group_by must be one of outcome, environment, repository_id, provider")
		return
	}
	limit, ok := parseCICDRunCorrelationAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parseCICDRunCorrelationAggregateOffset(w, r)
	if !ok {
		return
	}
	access := repositoryAccessFilterFromContext(r.Context())
	filter, ok := h.cicdRunCorrelationAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	if !validateCICDRunCorrelationAggregateOutcome(w, filter) {
		return
	}
	if access.empty() {
		h.writeEmptyCICDRunCorrelationInventory(w, r, dimension, limit, offset)
		return
	}
	if h.Aggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"CI/CD run correlation aggregates require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			cicdRunCorrelationAggregateCapability,
			h.profile(),
			requiredProfile(cicdRunCorrelationAggregateCapability),
		)
		return
	}

	rows, err := h.Aggregates.CICDRunCorrelationInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
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
		"next_offset": nextCICDRunCorrelationAggregateOffset(offset, limit, truncated),
		"scope":       cicdRunCorrelationAggregateScope(filter),
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		cicdRunCorrelationAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; one grouped bucket per row, ordered by count desc",
	))
}

func (h *CICDHandler) cicdRunCorrelationAggregateFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	access repositoryAccessFilter,
) (CICDRunCorrelationAggregateFilter, bool) {
	repositorySelector := QueryParam(r, "repository_id")
	repositoryID := repositorySelector
	if !access.empty() {
		var ok bool
		repositoryID, ok = resolveRepositorySelectorForRequestWithAccess(
			w,
			r,
			nil,
			h.Content,
			repositorySelector,
			access,
			cicdRunCorrelationAggregateCapability,
		)
		if !ok {
			return CICDRunCorrelationAggregateFilter{}, false
		}
	}
	filter := CICDRunCorrelationAggregateFilter{
		ScopeID:        QueryParam(r, "scope_id"),
		RepositoryID:   repositoryID,
		CommitSHA:      QueryParam(r, "commit_sha"),
		Provider:       QueryParam(r, "provider"),
		ArtifactDigest: QueryParam(r, "artifact_digest"),
		ImageRef:       QueryParam(r, "image_ref"),
		Environment:    QueryParam(r, "environment"),
		Outcome:        QueryParam(r, "outcome"),
	}
	return cicdRunCorrelationAggregateFilterWithRepositoryAccess(filter, access), true
}

func cicdRunCorrelationAggregateFilterWithRepositoryAccess(
	filter CICDRunCorrelationAggregateFilter,
	access repositoryAccessFilter,
) CICDRunCorrelationAggregateFilter {
	if !access.scoped() {
		return filter
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
	return filter
}

func (h *CICDHandler) writeEmptyCICDRunCorrelationAggregateCount(
	w http.ResponseWriter,
	r *http.Request,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_correlations": 0,
		"by_outcome":         map[string]int{},
		"by_environment":     map[string]int{},
		"by_provider":        map[string]int{},
		"scope":              map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		cicdRunCorrelationAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; outcome, environment, and provider rollups stay separate",
	))
}

func (h *CICDHandler) writeEmptyCICDRunCorrelationInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension CICDRunCorrelationInventoryDimension,
	limit int,
	offset int,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":     []CICDRunCorrelationInventoryRow{},
		"count":       0,
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   false,
		"next_offset": nil,
		"scope":       map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		cicdRunCorrelationAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned CI/CD run correlation facts; one grouped bucket per row, ordered by count desc",
	))
}

func cicdRunCorrelationAggregateScope(filter CICDRunCorrelationAggregateFilter) map[string]string {
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

func isSupportedCICDRunCorrelationDimension(d CICDRunCorrelationInventoryDimension) bool {
	switch d {
	case CICDRunCorrelationInventoryByOutcome,
		CICDRunCorrelationInventoryByEnvironment,
		CICDRunCorrelationInventoryByRepository,
		CICDRunCorrelationInventoryByProvider:
		return true
	default:
		return false
	}
}

// validateCICDRunCorrelationAggregateOutcome rejects unknown outcome filters
// with a 400 so a typo does not silently return zero counts. Matches the
// enum advertised by openapi_paths_cicd.go for the list endpoint and the
// new aggregate routes.
func validateCICDRunCorrelationAggregateOutcome(w http.ResponseWriter, filter CICDRunCorrelationAggregateFilter) bool {
	if filter.Outcome == "" || isSupportedCICDRunCorrelationOutcome(filter.Outcome) {
		return true
	}
	WriteError(w, http.StatusBadRequest, "outcome must be one of exact, derived, ambiguous, unresolved, rejected")
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

func parseCICDRunCorrelationAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return cicdRunCorrelationAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < cicdRunCorrelationAggregateMinLimit {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > CICDRunCorrelationAggregateMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseCICDRunCorrelationAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > cicdRunCorrelationAggregateMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// nextCICDRunCorrelationAggregateOffset returns the next offset when a
// truncated page can be continued without exceeding the documented offset
// bound, and nil otherwise. Callers serialize the nil as JSON null so
// generated clients see a clean end-of-stream marker.
func nextCICDRunCorrelationAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > cicdRunCorrelationAggregateMaxOffset {
		return nil
	}
	return next
}
