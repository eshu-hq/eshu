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

const freshnessServiceChangedSinceRoute = "GET /api/v0/freshness/services/changed-since"

// ServiceChangedSinceReader computes one bounded service-scope changed-since
// delta summary (#1943) that diffs a prior service materialization generation's
// evidence snapshot set against the current active generation's set. It is
// implemented by the Postgres status store and consumed here so the handler does
// not depend on a concrete database driver.
type ServiceChangedSinceReader interface {
	ComputeServiceChangedSinceDelta(context.Context, status.ServiceChangedSinceFilter) (status.ServiceChangedSinceSummary, error)
}

func (h *FreshnessHandler) listServiceChangedSince(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryFreshnessServiceChangedSince,
		freshnessServiceChangedSinceRoute,
		freshnessServiceChangedSinceCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), freshnessServiceChangedSinceCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service changed-since summaries are not supported in this profile",
			ErrorCodeUnsupportedCapability,
			freshnessServiceChangedSinceCapability,
			h.profile(),
			requiredProfile(freshnessServiceChangedSinceCapability),
		)
		return
	}

	filter, ok := h.parseServiceChangedSinceFilter(w, r)
	if !ok {
		return
	}

	if h.ServiceChangedSince == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"service changed-since reader is not configured",
			ErrorCodeBackendUnavailable,
			freshnessServiceChangedSinceCapability,
			h.profile(),
			requiredProfile(freshnessServiceChangedSinceCapability),
		)
		return
	}

	summary, err := h.ServiceChangedSince.ComputeServiceChangedSinceDelta(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("compute service changed-since delta: %v", err))
		return
	}

	// An empty resolved service means the named service matched no lineage.
	if summary.ServiceID == "" {
		WriteContractError(
			w,
			r,
			http.StatusNotFound,
			fmt.Sprintf("no service materialization lineage found for service_id %q", filter.ServiceID),
			ErrorCodeServiceNotFound,
			freshnessServiceChangedSinceCapability,
			h.profile(),
			requiredProfile(freshnessServiceChangedSinceCapability),
		)
		return
	}

	// The service resolved but the since reference matched no prior generation.
	if summary.SinceGenerationID == "" && !summary.Unavailable {
		WriteContractError(
			w,
			r,
			http.StatusNotFound,
			fmt.Sprintf("no service generation %q for service_id %q", filter.SinceGenerationID, filter.ServiceID),
			ErrorCodeNotFound,
			freshnessServiceChangedSinceCapability,
			h.profile(),
			requiredProfile(freshnessServiceChangedSinceCapability),
		)
		return
	}

	span.SetAttributes(serviceChangedSinceSpanAttributes(summary)...)

	body := map[string]any{
		"service_id":                   summary.ServiceID,
		"since_generation_id":          summary.SinceGenerationID,
		"current_active_generation_id": summary.CurrentActiveGenerationID,
		"sample_limit":                 summary.SampleLimit,
		"categories":                   summary.Categories,
		"unavailable":                  summary.Unavailable,
	}
	if summary.SinceObservedAt != "" {
		body["since_observed_at"] = summary.SinceObservedAt
	}
	if summary.CurrentObservedAt != "" {
		body["current_observed_at"] = summary.CurrentObservedAt
	}

	WriteSuccess(w, r, http.StatusOK, body, h.serviceChangedSinceTruthEnvelope(summary))
}

func (h *FreshnessHandler) parseServiceChangedSinceFilter(w http.ResponseWriter, r *http.Request) (status.ServiceChangedSinceFilter, bool) {
	filter := status.ServiceChangedSinceFilter{
		ServiceID:         QueryParam(r, "service_id"),
		SinceGenerationID: QueryParam(r, "since_generation_id"),
	}

	if !filter.HasServiceSelector() {
		WriteError(w, http.StatusBadRequest, "service_id is required")
		return status.ServiceChangedSinceFilter{}, false
	}
	if !filter.HasSinceReference() {
		WriteError(w, http.StatusBadRequest, "since_generation_id is required")
		return status.ServiceChangedSinceFilter{}, false
	}

	limit, ok := h.parseChangedSinceLimit(w, r)
	if !ok {
		return status.ServiceChangedSinceFilter{}, false
	}
	filter.SampleLimit = limit

	return filter.Normalize(), true
}

func (h *FreshnessHandler) serviceChangedSinceTruthEnvelope(summary status.ServiceChangedSinceSummary) *TruthEnvelope {
	envelope := BuildTruthEnvelope(
		h.profile(),
		freshnessServiceChangedSinceCapability,
		TruthBasisSemanticFacts,
		"diffed from durable service_evidence_snapshots keyed by (generation_id, service_evidence_key); service changed-since is persisted reducer snapshot truth, not live graph-materialized correlation",
	)
	switch {
	case summary.Unavailable:
		envelope.Freshness.State = FreshnessUnavailable
		envelope.Freshness.Detail = "the service has no current active materialization generation, so a changed-since diff cannot be computed yet"
		WithFreshnessCause(envelope, FreshnessCausePendingRepoGeneration)
	case summary.Building:
		envelope.Freshness.State = FreshnessBuilding
		envelope.Freshness.Detail = "the service has a pending materialization generation in flight; the current active generation may change"
		WithFreshnessCause(envelope, FreshnessCausePendingRepoGeneration)
	}
	return envelope
}

func serviceChangedSinceSpanAttributes(summary status.ServiceChangedSinceSummary) []attribute.KeyValue {
	changed := 0
	for _, category := range summary.Categories {
		changed += category.Counts.Added +
			category.Counts.Updated +
			category.Counts.Retired +
			category.Counts.Superseded
	}
	return []attribute.KeyValue{
		attribute.String(telemetry.SpanAttrServiceChangedSinceServiceID, summary.ServiceID),
		attribute.String(telemetry.SpanAttrServiceChangedSinceSinceGenerationID, summary.SinceGenerationID),
		attribute.String(telemetry.SpanAttrServiceChangedSinceCurrentGenerationID, summary.CurrentActiveGenerationID),
		attribute.Int(telemetry.SpanAttrServiceChangedSinceChangedCount, changed),
		attribute.Bool(telemetry.SpanAttrServiceChangedSinceUnavailable, summary.Unavailable),
	}
}
