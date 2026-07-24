// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const containerImageIdentityAggregateCapability = "supply_chain.container_image_identities.aggregate"

// containerImageIdentityAggregateRoutes registers the cheap-summary aggregate
// routes alongside the existing identity list route. The
// SupplyChainHandler.Mount in supply_chain.go invokes it.
func (h *SupplyChainHandler) containerImageIdentityAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/supply-chain/container-images/identities/count", h.countContainerImageIdentities)
	mux.HandleFunc("GET /api/v0/supply-chain/container-images/identities/inventory", h.containerImageIdentityInventory)
}

func (h *SupplyChainHandler) countContainerImageIdentities(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryContainerImageIdentityAggregate,
		"GET /api/v0/supply-chain/container-images/identities/count",
		containerImageIdentityAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), containerImageIdentityAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"container image identity aggregates require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			containerImageIdentityAggregateCapability,
			h.profile(),
			requiredProfile(containerImageIdentityAggregateCapability),
		)
		return
	}
	if h.ContainerImageAggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"container image identity aggregates require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			containerImageIdentityAggregateCapability,
			h.profile(),
			requiredProfile(containerImageIdentityAggregateCapability),
		)
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptyContainerImageIdentityCount(w, r)
		return
	}
	sourceRepositoryID, ok := h.resolveContainerImageSourceRepositorySelector(w, r, QueryParam(r, "source_repository_id"), access, containerImageIdentityAggregateCapability)
	if !ok {
		return
	}
	filter := containerImageIdentityAggregateFilterFromRequest(r, sourceRepositoryID)
	filter.AllowedSourceRepositoryIDs = access.repositorySearchIDs()
	if !validateContainerImageIdentityAggregateOutcome(w, filter) {
		return
	}
	count, err := h.ContainerImageAggregates.CountContainerImageIdentities(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body := map[string]any{
		"total_identities":     count.TotalIdentities,
		"by_outcome":           count.ByOutcome,
		"by_identity_strength": count.ByIdentityStrength,
		"scope":                containerImageIdentityAggregateScope(filter),
	}
	if filter.SourceRepositoryID != "" {
		body["source_bridge"] = containerImageIdentityAggregateSourceBridge(filter.SourceRepositoryID, count)
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		containerImageIdentityAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned container image identity facts; outcome and identity_strength stay separate",
	))
}

func (h *SupplyChainHandler) containerImageIdentityInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryContainerImageIdentityAggregate,
		"GET /api/v0/supply-chain/container-images/identities/inventory",
		containerImageIdentityAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), containerImageIdentityAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"container image identity aggregates require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			containerImageIdentityAggregateCapability,
			h.profile(),
			requiredProfile(containerImageIdentityAggregateCapability),
		)
		return
	}
	if h.ContainerImageAggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"container image identity aggregates require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			containerImageIdentityAggregateCapability,
			h.profile(),
			requiredProfile(containerImageIdentityAggregateCapability),
		)
		return
	}

	dimension := ContainerImageIdentityInventoryDimension(QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = ContainerImageIdentityInventoryByOutcome
	}
	if !isSupportedContainerImageIdentityDimension(dimension) {
		WriteError(w, http.StatusBadRequest, "group_by must be one of outcome, identity_strength, repository_id")
		return
	}
	limit, ok := parseContainerImageIdentityAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parseContainerImageIdentityAggregateOffset(w, r)
	if !ok {
		return
	}
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptyContainerImageIdentityInventory(w, r, dimension, limit, offset)
		return
	}
	sourceRepositoryID, ok := h.resolveContainerImageSourceRepositorySelector(w, r, QueryParam(r, "source_repository_id"), access, containerImageIdentityAggregateCapability)
	if !ok {
		return
	}
	filter := containerImageIdentityAggregateFilterFromRequest(r, sourceRepositoryID)
	filter.AllowedSourceRepositoryIDs = access.repositorySearchIDs()
	if !validateContainerImageIdentityAggregateOutcome(w, filter) {
		return
	}

	rows, err := h.ContainerImageAggregates.ContainerImageIdentityInventory(r.Context(), filter, dimension, limit+1, offset)
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
		"next_offset": nextContainerImageIdentityAggregateOffset(offset, limit, truncated),
		"scope":       containerImageIdentityAggregateScope(filter),
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		containerImageIdentityAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned container image identity facts; one grouped bucket per row, ordered by count desc",
	))
}

func containerImageIdentityAggregateFilterFromRequest(
	r *http.Request,
	sourceRepositoryID string,
) ContainerImageIdentityAggregateFilter {
	return ContainerImageIdentityAggregateFilter{
		Digest:             QueryParam(r, "digest"),
		ImageRef:           QueryParam(r, "image_ref"),
		SourceRepositoryID: sourceRepositoryID,
		RepositoryID:       QueryParam(r, "repository_id"),
		Outcome:            QueryParam(r, "outcome"),
	}
}

func containerImageIdentityAggregateScope(filter ContainerImageIdentityAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.Digest != "" {
		out["digest"] = filter.Digest
	}
	if filter.ImageRef != "" {
		out["image_ref"] = filter.ImageRef
	}
	if filter.SourceRepositoryID != "" {
		out["source_repository_id"] = filter.SourceRepositoryID
	}
	if filter.RepositoryID != "" {
		out["repository_id"] = filter.RepositoryID
	}
	if filter.Outcome != "" {
		out["outcome"] = filter.Outcome
	}
	return out
}

func containerImageIdentityAggregateSourceBridge(
	sourceRepositoryID string,
	count ContainerImageIdentityAggregateCount,
) ContainerImageIdentitySourceBridge {
	bridge := ContainerImageIdentitySourceBridge{SourceRepositoryID: sourceRepositoryID}
	if count.TotalIdentities == 0 {
		bridge.MissingEvidence = []string{
			"deployment_image_reference_missing",
			"image_registry_observation_missing",
			"source_to_image_correlation_missing",
		}
	}
	return bridge
}

// validateContainerImageIdentityAggregateOutcome rejects unknown outcome
// filters with a 400 so a typo like `outcome=exact-digest` does not silently
// return zero counts. Mirrors the same guard on the list endpoint
// (supply_chain.go), which enumerates only `exact_digest` and `tag_resolved`
// — the same values advertised in OpenAPI for these aggregate routes.
func validateContainerImageIdentityAggregateOutcome(w http.ResponseWriter, filter ContainerImageIdentityAggregateFilter) bool {
	if filter.Outcome == "" || isSupportedContainerImageIdentityOutcome(filter.Outcome) {
		return true
	}
	WriteError(w, http.StatusBadRequest, "outcome must be exact_digest or tag_resolved")
	return false
}

func isSupportedContainerImageIdentityDimension(d ContainerImageIdentityInventoryDimension) bool {
	switch d {
	case ContainerImageIdentityInventoryByOutcome,
		ContainerImageIdentityInventoryByIdentityStrength,
		ContainerImageIdentityInventoryByRepository:
		return true
	default:
		return false
	}
}

const (
	containerImageIdentityAggregateDefaultLimit = 100
	containerImageIdentityAggregateMinLimit     = 1
	// containerImageIdentityAggregateMaxOffset matches the OpenAPI offset
	// bound and keeps Postgres OFFSET scans bounded. Past this point callers
	// should narrow scope (repository_id, outcome) or fall back to the list
	// endpoint with anchored pagination.
	containerImageIdentityAggregateMaxOffset = 10000
)

func parseContainerImageIdentityAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return containerImageIdentityAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < containerImageIdentityAggregateMinLimit {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > ContainerImageIdentityAggregateMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseContainerImageIdentityAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > containerImageIdentityAggregateMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// nextContainerImageIdentityAggregateOffset returns the next offset when a
// truncated page can be continued without exceeding the documented offset
// bound, and nil otherwise. Callers serialize the nil as JSON null so
// generated clients see a clean end-of-stream marker.
func nextContainerImageIdentityAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > containerImageIdentityAggregateMaxOffset {
		return nil
	}
	return next
}
