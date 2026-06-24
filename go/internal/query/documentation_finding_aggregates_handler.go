// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const documentationFindingAggregateCapability = "documentation_findings.aggregate"

// documentationFindingAggregateRoutes registers the cheap-summary aggregate
// routes alongside the existing documentation findings list route.
func (h *DocumentationHandler) documentationFindingAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/documentation/findings/count", h.countDocumentationFindings)
	mux.HandleFunc("GET /api/v0/documentation/findings/inventory", h.documentationFindingInventory)
}

func (h *DocumentationHandler) countDocumentationFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDocumentationAggregate,
		"GET /api/v0/documentation/findings/count",
		documentationFindingAggregateCapability,
	)
	defer span.End()

	if h.unsupported(w, r, documentationFindingAggregateCapability) {
		return
	}
	if h.Aggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"documentation finding aggregates require the durable fact store",
			ErrorCodeBackendUnavailable,
			documentationFindingAggregateCapability,
			h.profile(),
			requiredProfile(documentationFindingAggregateCapability),
		)
		return
	}

	filter := documentationFindingAggregateFilterFromRequest(r)
	var ok bool
	filter, ok = documentationFindingAggregateFilterWithRepositoryAccess(r.Context(), filter)
	if !ok {
		WriteSuccess(w, r, http.StatusOK, documentationFindingEmptyAggregateCountResponse(filter), BuildTruthEnvelope(
			h.profile(),
			documentationFindingAggregateCapability,
			TruthBasisSemanticFacts,
			"resolved from durable documentation finding facts; scoped token grants authorize no repositories",
		))
		return
	}
	count, err := h.Aggregates.CountDocumentationFindings(r.Context(), filter)
	if err != nil {
		// Match the rest of the documentation handler family: surface a
		// stable internal-error envelope instead of the raw Postgres/query
		// string. Leaking the underlying error string would (a) drift from
		// the existing documentation error contract and (b) expose the
		// database query shape to unauthenticated callers.
		writeDocumentationInternalError(w, r)
		return
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_findings":     count.TotalFindings,
		"by_status":          count.ByStatus,
		"by_truth_level":     count.ByTruthLevel,
		"by_freshness_state": count.ByFreshnessState,
		"scope":              documentationFindingAggregateScope(filter),
	}, BuildTruthEnvelope(
		h.profile(),
		documentationFindingAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from durable documentation finding facts; per-status / per-truth-level / per-freshness rollups stay separate",
	))
}

func (h *DocumentationHandler) documentationFindingInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryDocumentationAggregate,
		"GET /api/v0/documentation/findings/inventory",
		documentationFindingAggregateCapability,
	)
	defer span.End()

	if h.unsupported(w, r, documentationFindingAggregateCapability) {
		return
	}
	if h.Aggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"documentation finding aggregates require the durable fact store",
			ErrorCodeBackendUnavailable,
			documentationFindingAggregateCapability,
			h.profile(),
			requiredProfile(documentationFindingAggregateCapability),
		)
		return
	}

	dimension := DocumentationFindingInventoryDimension(QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = DocumentationFindingInventoryByStatus
	}
	if !isSupportedDocumentationFindingInventoryDimension(dimension) {
		WriteError(w, http.StatusBadRequest, "group_by must be one of status, truth_level, freshness_state, finding_type, source_id")
		return
	}
	limit, ok := parseDocumentationFindingAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parseDocumentationFindingAggregateOffset(w, r)
	if !ok {
		return
	}
	filter := documentationFindingAggregateFilterFromRequest(r)
	var scopedOK bool
	filter, scopedOK = documentationFindingAggregateFilterWithRepositoryAccess(r.Context(), filter)
	if !scopedOK {
		body := map[string]any{
			"buckets":     []DocumentationFindingInventoryRow{},
			"count":       0,
			"limit":       limit,
			"offset":      offset,
			"group_by":    string(dimension),
			"truncated":   false,
			"next_offset": nil,
			"scope":       documentationFindingAggregateScope(filter),
		}
		WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
			h.profile(),
			documentationFindingAggregateCapability,
			TruthBasisSemanticFacts,
			"resolved from durable documentation finding facts; scoped token grants authorize no repositories",
		))
		return
	}

	rows, err := h.Aggregates.DocumentationFindingInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		// Match the rest of the documentation handler family: stable
		// internal-error envelope, no raw Postgres/query string in the
		// response body.
		writeDocumentationInternalError(w, r)
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
		"next_offset": nextDocumentationFindingAggregateOffset(offset, limit, truncated),
		"scope":       documentationFindingAggregateScope(filter),
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		documentationFindingAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from durable documentation finding facts; one grouped bucket per row, ordered by count desc",
	))
}

func documentationFindingAggregateFilterFromRequest(r *http.Request) DocumentationFindingAggregateFilter {
	return DocumentationFindingAggregateFilter{
		ScopeID:        QueryParam(r, "scope_id"),
		FindingType:    QueryParam(r, "finding_type"),
		SourceID:       QueryParam(r, "source_id"),
		DocumentID:     QueryParam(r, "document_id"),
		Status:         QueryParam(r, "status"),
		TruthLevel:     QueryParam(r, "truth_level"),
		FreshnessState: QueryParam(r, "freshness_state"),
	}
}

func documentationFindingAggregateScope(filter DocumentationFindingAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.ScopeID != "" {
		out["scope_id"] = filter.ScopeID
	}
	if filter.FindingType != "" {
		out["finding_type"] = filter.FindingType
	}
	if filter.SourceID != "" {
		out["source_id"] = filter.SourceID
	}
	if filter.DocumentID != "" {
		out["document_id"] = filter.DocumentID
	}
	if filter.Status != "" {
		out["status"] = filter.Status
	}
	if filter.TruthLevel != "" {
		out["truth_level"] = filter.TruthLevel
	}
	if filter.FreshnessState != "" {
		out["freshness_state"] = filter.FreshnessState
	}
	return out
}

func documentationFindingEmptyAggregateCountResponse(filter DocumentationFindingAggregateFilter) map[string]any {
	return map[string]any{
		"total_findings":     0,
		"by_status":          map[string]int{},
		"by_truth_level":     map[string]int{},
		"by_freshness_state": map[string]int{},
		"scope":              documentationFindingAggregateScope(filter),
	}
}

func isSupportedDocumentationFindingInventoryDimension(d DocumentationFindingInventoryDimension) bool {
	switch d {
	case DocumentationFindingInventoryByStatus,
		DocumentationFindingInventoryByTruthLevel,
		DocumentationFindingInventoryByFreshnessState,
		DocumentationFindingInventoryByFindingType,
		DocumentationFindingInventoryBySourceID:
		return true
	default:
		return false
	}
}

const (
	documentationFindingAggregateDefaultLimit = 100
	documentationFindingAggregateMinLimit     = 1
	// documentationFindingAggregateMaxOffset matches the OpenAPI offset
	// bound and keeps Postgres OFFSET scans bounded. Past this point
	// callers should narrow scope (scope_id, finding_type, source_id,
	// document_id) or fall back to the list endpoint.
	documentationFindingAggregateMaxOffset = 10000
)

func parseDocumentationFindingAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return documentationFindingAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < documentationFindingAggregateMinLimit {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > DocumentationFindingAggregateMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseDocumentationFindingAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > documentationFindingAggregateMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// nextDocumentationFindingAggregateOffset returns the next offset when a
// truncated page can be continued without exceeding the documented offset
// bound, and nil otherwise.
func nextDocumentationFindingAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > documentationFindingAggregateMaxOffset {
		return nil
	}
	return next
}
