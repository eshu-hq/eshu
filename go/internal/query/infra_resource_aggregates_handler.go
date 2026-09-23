// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const infraResourceAggregateCapability = "platform_impact.infra_resource_aggregate"

// infraResourceAggregateRoutes registers the cheap-summary aggregate routes
// alongside the existing infra search route.
func (h *InfraHandler) infraResourceAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/infra/resources/count", h.countInfraResources)
	mux.HandleFunc("GET /api/v0/infra/resources/inventory", h.infraResourceInventory)
}

func (h *InfraHandler) countInfraResources(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryInfraResourceAggregate,
		"GET /api/v0/infra/resources/count",
		infraResourceAggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), infraResourceAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"infra resource aggregates require the authoritative graph",
			ErrorCodeUnsupportedCapability,
			infraResourceAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(infraResourceAggregateCapability),
		)
		return
	}
	if h.Aggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"infra resource aggregates require the authoritative graph",
			ErrorCodeBackendUnavailable,
			infraResourceAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(infraResourceAggregateCapability),
		)
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	recordScopeGrantInlineCap(r.Context(), h.Instruments, access, "infra_resource_count")
	if access.Empty() {
		h.writeEmptyInfraResourceCount(w, r)
		return
	}
	filter, ok := infraResourceAggregateFilterFromRequest(w, r)
	if !ok {
		return
	}
	filter = applyInfraResourceAggregateAccess(filter, access)
	count, err := h.Aggregates.CountInfraResources(r.Context(), filter)
	if err != nil {
		if WriteGraphReadError(w, r, err, infraResourceAggregateCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_resources": count.TotalResources,
		"by_provider":     count.ByProvider,
		"by_environment":  count.ByEnvironment,
		"by_label":        count.ByLabel,
		"scope":           infraResourceAggregateScope(filter),
	}, infraResourceAggregateTruth(h.profile(), count.Source,
		"per-provider / per-environment / per-label rollups stay separate"))
}

func (h *InfraHandler) infraResourceInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryInfraResourceAggregate,
		"GET /api/v0/infra/resources/inventory",
		infraResourceAggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), infraResourceAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"infra resource aggregates require the authoritative graph",
			ErrorCodeUnsupportedCapability,
			infraResourceAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(infraResourceAggregateCapability),
		)
		return
	}
	if h.Aggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"infra resource aggregates require the authoritative graph",
			ErrorCodeBackendUnavailable,
			infraResourceAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(infraResourceAggregateCapability),
		)
		return
	}

	dimension := InfraResourceInventoryDimension(QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = InfraResourceInventoryByProvider
	}
	if !isSupportedInfraResourceInventoryDimension(dimension) {
		WriteError(w, http.StatusBadRequest, "group_by must be one of provider, environment, resource_category, resource_service, label")
		return
	}
	limit, ok := parseInfraResourceAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parseInfraResourceAggregateOffset(w, r)
	if !ok {
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	recordScopeGrantInlineCap(r.Context(), h.Instruments, access, "infra_resource_inventory")
	if access.Empty() {
		h.writeEmptyInfraResourceInventory(w, r, dimension, limit, offset)
		return
	}
	filter, ok := infraResourceAggregateFilterFromRequest(w, r)
	if !ok {
		return
	}
	filter = applyInfraResourceAggregateAccess(filter, access)

	rows, source, err := h.Aggregates.InfraResourceInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		if WriteGraphReadError(w, r, err, infraResourceAggregateCapability) {
			return
		}
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
		"next_offset": nextInfraResourceAggregateOffset(offset, limit, truncated),
		"scope":       infraResourceAggregateScope(filter),
	}
	WriteSuccess(w, r, http.StatusOK, body, infraResourceAggregateTruth(h.profile(), source,
		"one grouped bucket per row, ordered by count desc"))
}

// infraResourceAggregateTruth maps the serving store onto the truth envelope.
// Table-backed reads are derived: the content writer fills
// infra_resource_entities per repository just before the canonical graph
// write, so for one projection stage the table and the graph can disagree for
// that repository. A read served by the table alone reports content_index; a
// read that also needed the graph pass reports hybrid; a read that needed only
// the graph (graph-only labels, scoped callers, or before the read model is
// ready)
// reports authoritative_graph.
func infraResourceAggregateTruth(profile QueryProfile, source InfraResourceAggregateSource, detail string) *TruthEnvelope {
	if source == InfraResourceAggregateSourceReadModel {
		return BuildTruthEnvelope(profile, infraResourceAggregateCapability, TruthBasisContentIndex,
			"resolved from the Postgres infra read model, derived from the same content rows the canonical graph writer projects; "+detail)
	}
	if source == InfraResourceAggregateSourceHybrid {
		return BuildTruthEnvelope(profile, infraResourceAggregateCapability, TruthBasisHybrid,
			"content-derived infrastructure nodes resolved from the Postgres infra read model; CloudResource, "+
				"TerraformStateResource, and the Terraform state projector's TerraformModule/TerraformOutput nodes "+
				"from the authoritative graph; "+detail)
	}
	return BuildTruthEnvelope(profile, infraResourceAggregateCapability, TruthBasisAuthoritativeGraph,
		"resolved from the authoritative infrastructure graph; "+detail)
}

// infraResourceAggregateFilterFromRequest parses the request, validates the
// category against the closed enum (k8s / terraform / argocd / crossplane /
// helm / cloud), and returns false after writing a 400 when the category is
// unknown. Other filter values pass through as bound parameters at Cypher
// time and don't need a Go-side enum.
func infraResourceAggregateFilterFromRequest(w http.ResponseWriter, r *http.Request) (InfraResourceAggregateFilter, bool) {
	category := strings.ToLower(strings.TrimSpace(QueryParam(r, "category")))
	if category != "" {
		if _, ok := infraCategoryLabels[category]; !ok {
			WriteError(w, http.StatusBadRequest, "category must be one of k8s, terraform, argocd, crossplane, helm, cloud")
			return InfraResourceAggregateFilter{}, false
		}
	}
	return InfraResourceAggregateFilter{
		Category:         category,
		Kind:             QueryParam(r, "kind"),
		ResourceType:     QueryParam(r, "resource_type"),
		Provider:         QueryParam(r, "provider"),
		Environment:      QueryParam(r, "environment"),
		ResourceService:  QueryParam(r, "resource_service"),
		ResourceCategory: QueryParam(r, "resource_category"),
	}, true
}

func infraResourceAggregateScope(filter InfraResourceAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.Category != "" {
		out["category"] = filter.Category
	}
	if filter.Kind != "" {
		out["kind"] = filter.Kind
	}
	if filter.ResourceType != "" {
		out["resource_type"] = filter.ResourceType
	}
	if filter.Provider != "" {
		out["provider"] = filter.Provider
	}
	if filter.Environment != "" {
		out["environment"] = filter.Environment
	}
	if filter.ResourceService != "" {
		out["resource_service"] = filter.ResourceService
	}
	if filter.ResourceCategory != "" {
		out["resource_category"] = filter.ResourceCategory
	}
	return out
}

func isSupportedInfraResourceInventoryDimension(d InfraResourceInventoryDimension) bool {
	switch d {
	case InfraResourceInventoryByProvider,
		InfraResourceInventoryByEnvironment,
		InfraResourceInventoryByResourceCategory,
		InfraResourceInventoryByResourceService,
		InfraResourceInventoryByLabel:
		return true
	default:
		return false
	}
}

const (
	infraResourceAggregateDefaultLimit = 100
	infraResourceAggregateMinLimit     = 1
	// infraResourceAggregateMaxOffset matches the OpenAPI offset bound and
	// keeps SKIP scans bounded. Past this point callers should narrow scope
	// (category, provider, environment) or fall back to the list endpoint.
	infraResourceAggregateMaxOffset = 10000
)

func parseInfraResourceAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return infraResourceAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < infraResourceAggregateMinLimit {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > InfraResourceAggregateMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseInfraResourceAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > infraResourceAggregateMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// nextInfraResourceAggregateOffset returns the next offset when a truncated
// page can be continued without exceeding the documented offset bound, and
// nil otherwise.
func nextInfraResourceAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > infraResourceAggregateMaxOffset {
		return nil
	}
	return next
}

// readModelLegError picks the error a read-model read reports once both of
// its concurrent legs have returned. A failing leg cancels its sibling, so the
// sibling then reports context.Canceled; the failing leg's own error is the
// cause and wins. That keeps a graph-leg ErrGraphReadDeadline or
// ErrGraphUnavailable (504/503 through WriteGraphReadError) from being masked
// by the table leg's cancellation (a generic 500). When neither error is a
// sibling cancellation, the table error wins, as before.
func readModelLegError(tableErr, graphErr error, tablePrefix, graphPrefix string) error {
	tableCanceled := errors.Is(tableErr, context.Canceled)
	graphCanceled := errors.Is(graphErr, context.Canceled)
	switch {
	case tableErr != nil && tableCanceled && graphErr != nil && !graphCanceled:
		return fmt.Errorf("%s: %w", graphPrefix, graphErr)
	case tableErr != nil:
		return fmt.Errorf("%s: %w", tablePrefix, tableErr)
	case graphErr != nil:
		return fmt.Errorf("%s: %w", graphPrefix, graphErr)
	default:
		return nil
	}
}
