// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const freshnessGenerationLifecycleRoute = "GET /api/v0/freshness/generations"

// GenerationLifecycleReader reads one bounded, ordered page of scope generation
// lifecycle drilldown rows. It is implemented by the Postgres status store and
// consumed here so the handler does not depend on a concrete database driver.
type GenerationLifecycleReader interface {
	ListGenerationLifecycle(context.Context, status.GenerationLifecycleFilter) (status.GenerationLifecyclePage, error)
}

// FreshnessHandler exposes the bounded generation lifecycle drilldown and the
// bounded changed-since delta summary so callers can inspect active, pending,
// superseded, completed, and failed generation history and diff a prior
// generation against current truth without scraping broad status payloads.
type FreshnessHandler struct {
	Generations         GenerationLifecycleReader
	ChangedSince        ChangedSinceReader
	ServiceChangedSince ServiceChangedSinceReader
	Profile             QueryProfile
}

// Mount registers freshness drilldown routes on the given mux.
func (h *FreshnessHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc(freshnessGenerationLifecycleRoute, h.listGenerationLifecycle)
	mux.HandleFunc(freshnessChangedSinceRoute, h.listChangedSince)
	mux.HandleFunc(freshnessServiceChangedSinceRoute, h.listServiceChangedSince)
}

func (h *FreshnessHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *FreshnessHandler) listGenerationLifecycle(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryFreshnessGenerationLifecycle,
		freshnessGenerationLifecycleRoute,
		freshnessGenerationLifecycleCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), freshnessGenerationLifecycleCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"generation lifecycle drilldown is not supported in this profile",
			ErrorCodeUnsupportedCapability,
			freshnessGenerationLifecycleCapability,
			h.profile(),
			requiredProfile(freshnessGenerationLifecycleCapability),
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
		ScopeID:       QueryParam(r, "scope_id"),
		Repository:    QueryParam(r, "repository"),
		CollectorKind: QueryParam(r, "collector_kind"),
		SourceSystem:  QueryParam(r, "source_system"),
		GenerationID:  QueryParam(r, "generation_id"),
		Status:        statusFilter,
		Limit:         limit,
	}.Normalize()

	if h.Generations == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"generation lifecycle reader is not configured",
			ErrorCodeBackendUnavailable,
			freshnessGenerationLifecycleCapability,
			h.profile(),
			requiredProfile(freshnessGenerationLifecycleCapability),
		)
		return
	}

	page, err := h.Generations.ListGenerationLifecycle(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list generation lifecycle: %v", err))
		return
	}

	// A named scope/repository/generation selector that matches nothing is an
	// explicit not-found, never a confident empty list.
	if len(page.Records) == 0 && filter.HasScopeSelector() {
		WriteContractError(
			w,
			r,
			http.StatusNotFound,
			generationLifecycleNotFoundMessage(filter),
			generationLifecycleNotFoundCode(filter),
			freshnessGenerationLifecycleCapability,
			h.profile(),
			requiredProfile(freshnessGenerationLifecycleCapability),
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
	WriteSuccess(w, r, http.StatusOK, body, h.truthEnvelope(page))
}

func (h *FreshnessHandler) truthEnvelope(page status.GenerationLifecyclePage) *TruthEnvelope {
	envelope := BuildTruthEnvelope(
		h.profile(),
		freshnessGenerationLifecycleCapability,
		TruthBasisSemanticFacts,
		"resolved from durable scope_generations and fact_work_items rows; generation lifecycle is persisted truth, not graph-materialized correlation",
	)
	if generationLifecycleHasBuilding(page.Records) {
		envelope.Freshness.State = FreshnessBuilding
		envelope.Freshness.Detail = "at least one returned scope has a pending or in-flight generation"
	}
	return envelope
}

func (h *FreshnessHandler) parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return status.DefaultGenerationLifecycleLimit, true
	}
	limit := QueryParamInt(r, "limit", -1)
	if limit <= 0 || limit > status.MaxGenerationLifecycleLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", status.MaxGenerationLifecycleLimit))
		return 0, false
	}
	return limit, true
}

func (h *FreshnessHandler) parseStatus(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := QueryParam(r, "status")
	if raw == "" {
		return "", true
	}
	if !knownGenerationLifecycleStatus(raw) {
		WriteError(w, http.StatusBadRequest, "status must be one of pending, active, superseded, completed, failed")
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

func generationLifecycleNotFoundCode(filter status.GenerationLifecycleFilter) ErrorCode {
	if filter.ScopeID != "" || filter.Repository != "" {
		return ErrorCodeScopeNotFound
	}
	return ErrorCodeNotFound
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
