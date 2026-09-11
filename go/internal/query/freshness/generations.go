// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const generationLifecycleRoute = "GET /api/v0/freshness/generations"

// GenerationLifecycleReader reads one bounded, ordered page of scope generation
// lifecycle drilldown rows. It is implemented by the Postgres status store and
// consumed here so the handler does not depend on a concrete database driver.
type GenerationLifecycleReader interface {
	ListGenerationLifecycle(context.Context, status.GenerationLifecycleFilter) (status.GenerationLifecyclePage, error)
}

// Handler exposes the bounded generation lifecycle drilldown and the
// bounded changed-since delta summary so callers can inspect active, pending,
// superseded, completed, and failed generation history and diff a prior
// generation against current truth without scraping broad status payloads.
type Handler struct {
	Generations  GenerationLifecycleReader
	ChangedSince ChangedSinceReader
	// ServiceChangedSince reads the per-service evidence lineage. Its tables
	// carry only service_id, so the caller's grant cannot be bound inside its
	// SQL the way the two repository-scope readers above bind theirs.
	ServiceChangedSince ServiceChangedSinceReader
	// ServiceOwnership resolves a catalog service_id to its owning repository
	// under the caller's grant (#5167), which is what lets
	// listServiceChangedSince refuse an ungranted service before touching the
	// lineage tables. Leaving it nil fails a scoped caller closed on that
	// route; an unscoped caller never consults it.
	ServiceOwnership service.CatalogCorrelationStore
	Profile          querycontract.QueryProfile
}

// Mount registers freshness drilldown routes on the given mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc(generationLifecycleRoute, h.listGenerationLifecycle)
	mux.HandleFunc(changedSinceRoute, h.listChangedSince)
	mux.HandleFunc(serviceChangedSinceRoute, h.listServiceChangedSince)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *Handler) listGenerationLifecycle(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryFreshnessGenerationLifecycle,
		generationLifecycleRoute,
		GenerationLifecycleCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), GenerationLifecycleCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"generation lifecycle drilldown is not supported in this profile",
			querycontract.ErrorCodeUnsupportedCapability,
			GenerationLifecycleCapability,
			h.profile(),
			querycontract.RequiredProfile(GenerationLifecycleCapability),
		)
		return
	}

	limit, ok := h.parseLimit(w, r)
	if !ok {
		return
	}
	statusFilter, ok := h.parseStatus(w, r)
	if !ok {
		return
	}

	filter := status.GenerationLifecycleFilter{
		ScopeID:       querycontract.QueryParam(r, "scope_id"),
		Repository:    querycontract.QueryParam(r, "repository"),
		CollectorKind: querycontract.QueryParam(r, "collector_kind"),
		SourceSystem:  querycontract.QueryParam(r, "source_system"),
		GenerationID:  querycontract.QueryParam(r, "generation_id"),
		Status:        statusFilter,
		Limit:         limit,
	}.Normalize()

	if h.Generations == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"generation lifecycle reader is not configured",
			querycontract.ErrorCodeBackendUnavailable,
			GenerationLifecycleCapability,
			h.profile(),
			querycontract.RequiredProfile(GenerationLifecycleCapability),
		)
		return
	}

	// #5167 F-6. The grant is bound in the query rather than checked against
	// the selector fields: this route also filters on generation_id,
	// collector_kind, source_system and status, so a caller supplying only
	// generation_id would reach any tenant's generation while both selector
	// fields sat empty. An empty grant selects nothing, which is the
	// fail-closed half.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	filter.Scoped = access.Scoped()
	filter.AllowedRepositoryIDs = access.GrantedRepositoryIDs()
	filter.AllowedScopeIDs = access.GrantedScopeIDs()

	page, err := h.Generations.ListGenerationLifecycle(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list generation lifecycle: %v", err))
		return
	}

	// A named scope/repository/generation selector that matches nothing is an
	// explicit not-found, never a confident empty list.
	if len(page.Records) == 0 && filter.HasScopeSelector() {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotFound,
			generationLifecycleNotFoundMessage(filter),
			generationLifecycleNotFoundCode(filter),
			GenerationLifecycleCapability,
			h.profile(),
			querycontract.RequiredProfile(GenerationLifecycleCapability),
		)
		return
	}

	span.SetAttributes(generationLifecycleSpanAttributes(page)...)

	body := map[string]any{
		"generations": page.Records,
		"count":       len(page.Records),
		"limit":       page.Limit,
		"truncated":   page.Truncated,
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, h.truthEnvelope(page))
}

func (h *Handler) truthEnvelope(page status.GenerationLifecyclePage) *querycontract.TruthEnvelope {
	envelope := querycontract.BuildTruthEnvelope(
		h.profile(),
		GenerationLifecycleCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from durable scope_generations and fact_work_items rows; generation lifecycle is persisted truth, not graph-materialized correlation",
	)
	if generationLifecycleHasBuilding(page.Records) {
		envelope.Freshness.State = querycontract.FreshnessBuilding
		envelope.Freshness.Detail = "at least one returned scope has a pending or in-flight generation"
	}
	return envelope
}

func (h *Handler) parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		return status.DefaultGenerationLifecycleLimit, true
	}
	limit := querycontract.QueryParamInt(r, "limit", -1)
	if limit <= 0 || limit > status.MaxGenerationLifecycleLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", status.MaxGenerationLifecycleLimit))
		return 0, false
	}
	return limit, true
}

func (h *Handler) parseStatus(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := querycontract.QueryParam(r, "status")
	if raw == "" {
		return "", true
	}
	if !knownGenerationLifecycleStatus(raw) {
		querycontract.WriteError(w, http.StatusBadRequest, "status must be one of pending, active, superseded, completed, failed")
		return "", false
	}
	return raw, true
}

func knownGenerationLifecycleStatus(value string) bool {
	switch value {
	case "pending", "active", "superseded", "completed", "failed":
		return true
	default:
		return false
	}
}

func generationLifecycleHasBuilding(records []status.GenerationLifecycleRecord) bool {
	for _, record := range records {
		if record.Status == "pending" || record.QueueStatus.Outstanding > 0 {
			return true
		}
	}
	return false
}

func generationLifecycleNotFoundCode(filter status.GenerationLifecycleFilter) querycontract.ErrorCode {
	if filter.ScopeID != "" || filter.Repository != "" {
		return querycontract.ErrorCodeScopeNotFound
	}
	return querycontract.ErrorCodeNotFound
}

func generationLifecycleNotFoundMessage(filter status.GenerationLifecycleFilter) string {
	switch {
	case filter.GenerationID != "":
		return fmt.Sprintf("no generation lifecycle record for generation_id %q", filter.GenerationID)
	case filter.ScopeID != "":
		return fmt.Sprintf("no generation lifecycle records for scope_id %q", filter.ScopeID)
	case filter.Repository != "":
		return fmt.Sprintf("no generation lifecycle records for repository %q", filter.Repository)
	default:
		return "no generation lifecycle records matched the requested scope"
	}
}

func generationLifecycleSpanAttributes(page status.GenerationLifecyclePage) []attribute.KeyValue {
	activeCount := 0
	failureCount := 0
	for _, record := range page.Records {
		if record.IsActive {
			activeCount++
		}
		if record.LatestFailure != nil {
			failureCount++
		}
	}
	return []attribute.KeyValue{
		attribute.Int(telemetry.SpanAttrGenerationLifecycleResultCount, len(page.Records)),
		attribute.Bool(telemetry.SpanAttrGenerationLifecycleTruncated, page.Truncated),
		attribute.Int(telemetry.SpanAttrGenerationLifecycleActiveCount, activeCount),
		attribute.Int(telemetry.SpanAttrGenerationLifecycleFailureCount, failureCount),
	}
}
