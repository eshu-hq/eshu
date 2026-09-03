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

	if !h.serviceChangedSinceGrantAdmits(w, r, filter.ServiceID) {
		return
	}

	summary, err := h.ServiceChangedSince.ComputeServiceChangedSinceDelta(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("compute service changed-since delta: %v", err))
		return
	}

	// An empty resolved service means the named service matched no lineage.
	if summary.ServiceID == "" {
		h.writeServiceChangedSinceNotFound(w, r, filter.ServiceID)
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

// serviceChangedSinceGrantAdmits binds the caller's repository grant to the
// requested catalog service_id and reports whether the lineage read may run
// (#5167). It writes the refusal itself when the answer is no.
//
// The two sibling freshness routes bind their grant inside the shipped SQL,
// because their tables join to ingestion_scopes and can filter on
// scope_kind/source_key. service_materialization_generations and
// service_evidence_snapshots carry only service_id, so there is nothing there
// to bind; the reducer knew the owning repository when it wrote the generation
// but discarded it. The mapping is recovered from the reducer_service_catalog_
// correlation facts, written from the SAME decision set that produced the
// generation, through the read model that already applies the caller's grant
// (ListServiceCatalogCorrelations). The refusal is therefore a pre-read one,
// and it is the route's ordinary service-not-found so an ungranted service is
// indistinguishable from one that does not exist.
//
// Two deliberate fail-closed cases:
//
//   - A generation can outlive its correlation fact. The correlation read
//     requires the fact's generation to still be its scope's active one, so
//     removing a catalog entity leaves a scoped caller with not-found for a
//     service whose lineage rows still exist. That is correct, not a bug: with
//     the catalog entity gone there is no longer any evidence of who owns the
//     service, and an unowned service must not be readable by a scoped caller.
//     An unscoped operator still sees it.
//   - A nil ServiceOwnership refuses every scoped caller. A deployment that
//     cannot resolve ownership must not answer instead of resolving it.
func (h *FreshnessHandler) serviceChangedSinceGrantAdmits(
	w http.ResponseWriter,
	r *http.Request,
	serviceID string,
) bool {
	access := repositoryAccessFilterFromContext(r.Context())
	if !access.Scoped() {
		// Shared, admin, and local callers have no grant for the correlation
		// filter to intersect, so the unscoped path issues no extra query.
		return true
	}
	// Load-bearing: ServiceCatalogCorrelationFilter's grant clause collapses to
	// TRUE when both grant arrays are empty, so a scoped caller with no grant
	// would read every tenant's correlations. Refuse before the store call.
	if access.Empty() {
		h.writeServiceChangedSinceNotFound(w, r, serviceID)
		return false
	}
	if h.ServiceOwnership == nil {
		h.writeServiceChangedSinceNotFound(w, r, serviceID)
		return false
	}

	rows, err := h.ServiceOwnership.ListServiceCatalogCorrelations(
		r.Context(),
		ServiceCatalogCorrelationFilter{
			ServiceID:            serviceID,
			AllowedRepositoryIDs: access.GrantedRepositoryIDs(),
			AllowedScopeIDs:      access.GrantedScopeIDs(),
			// One admitted row is the whole answer: the question is whether
			// the grant covers this service at all, not which repository owns
			// it.
			Limit: 1,
		},
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve service ownership: %v", err))
		return false
	}
	if len(rows) == 0 {
		h.writeServiceChangedSinceNotFound(w, r, serviceID)
		return false
	}
	return true
}

// writeServiceChangedSinceNotFound is the route's single service-not-found
// answer. Both the unresolved-service path and the ungranted-service refusal go
// through it so the two responses stay byte-identical and the route cannot be
// used as an existence oracle for another tenant's services.
func (h *FreshnessHandler) writeServiceChangedSinceNotFound(
	w http.ResponseWriter,
	r *http.Request,
	serviceID string,
) {
	WriteContractError(
		w,
		r,
		http.StatusNotFound,
		fmt.Sprintf("no service materialization lineage found for service_id %q", serviceID),
		ErrorCodeServiceNotFound,
		freshnessServiceChangedSinceCapability,
		h.profile(),
		requiredProfile(freshnessServiceChangedSinceCapability),
	)
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
