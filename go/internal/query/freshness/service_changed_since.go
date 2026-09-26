// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const serviceChangedSinceRoute = "GET /api/v0/freshness/services/changed-since"

// ServiceChangedSinceReader computes one bounded service-scope changed-since
// delta summary (#1943) that diffs a prior service materialization generation's
// evidence snapshot set against the current active generation's set. It is
// implemented by the Postgres status store and consumed here so the handler does
// not depend on a concrete database driver.
type ServiceChangedSinceReader interface {
	ComputeServiceChangedSinceDelta(context.Context, status.ServiceChangedSinceFilter) (status.ServiceChangedSinceSummary, error)
}

func (h *Handler) listServiceChangedSince(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryFreshnessServiceChangedSince,
		serviceChangedSinceRoute,
		ServiceChangedSinceCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), ServiceChangedSinceCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service changed-since summaries are not supported in this profile",
			querycontract.ErrorCodeUnsupportedCapability,
			ServiceChangedSinceCapability,
			h.profile(),
			querycontract.RequiredProfile(ServiceChangedSinceCapability),
		)
		return
	}

	filter, ok := h.parseServiceChangedSinceFilter(w, r)
	if !ok {
		return
	}

	if h.ServiceChangedSince == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"service changed-since reader is not configured",
			querycontract.ErrorCodeBackendUnavailable,
			ServiceChangedSinceCapability,
			h.profile(),
			querycontract.RequiredProfile(ServiceChangedSinceCapability),
		)
		return
	}

	if !h.serviceChangedSinceGrantAdmits(w, r, filter.ServiceID) {
		return
	}

	// #6475. The grant binds the LINEAGE ROW in the query, on its scope_id,
	// rather than anything the caller typed: an ungranted lineage (and every
	// unattributed legacy lineage, for a scoped caller) resolves to no row and
	// falls into the not-found path below, byte-identical to a service id that
	// does not exist. An explicit scope_id selector is bound the same way.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	filter.Scoped = access.Scoped()
	filter.AllowedRepositoryIDs = access.GrantedRepositoryIDs()
	filter.AllowedScopeIDs = access.GrantedScopeIDs()

	summary, err := h.ServiceChangedSince.ComputeServiceChangedSinceDelta(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("compute service changed-since delta: %v", err))
		return
	}

	// More than one admitted scope holds a lineage for this id and the caller
	// named none. The route never picks one; it lists the admitted scope ids
	// so the caller can re-ask with scope_id.
	if summary.Ambiguous() {
		h.writeServiceChangedSinceAmbiguous(w, r, summary)
		return
	}

	// An empty resolved service means the named service matched no lineage the
	// caller may read. OutsideGrant separates "the grant excluded an existing
	// lineage" from "no such lineage" on the span only.
	if summary.ServiceID == "" {
		if summary.OutsideGrant {
			h.refuseServiceChangedSinceGrant(w, r, filter.ServiceID, telemetry.ServiceChangedSinceGrantRefusalNotGranted)
			return
		}
		h.writeServiceChangedSinceNotFound(w, r, filter.ServiceID)
		return
	}

	// The service resolved but the since reference matched no prior generation.
	if summary.SinceGenerationID == "" && !summary.Unavailable {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotFound,
			fmt.Sprintf("no service generation %q for service_id %q", filter.SinceGenerationID, filter.ServiceID),
			querycontract.ErrorCodeNotFound,
			ServiceChangedSinceCapability,
			h.profile(),
			querycontract.RequiredProfile(ServiceChangedSinceCapability),
		)
		return
	}

	span.SetAttributes(serviceChangedSinceSpanAttributes(summary)...)

	body := map[string]any{
		"service_id":                   summary.ServiceID,
		"scope_id":                     summary.ScopeID,
		"unattributed":                 summary.Unattributed,
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

	querycontract.WriteSuccess(w, r, http.StatusOK, body, h.serviceChangedSinceTruthEnvelope(summary))
}

// serviceChangedSinceGrantAdmits runs the pre-read refusals for a scoped
// caller and reports whether the lineage read may run. It writes the refusal
// itself when the answer is no.
//
// Since #6475 the grant itself binds in the lineage SQL, on the scope_id every
// service_materialization_generations row now carries
// (resolveServiceChangedSinceScopeQuery): a scoped caller resolves only the
// lineages of scopes its grant admits, never an unattributed legacy lineage,
// and never another scope's prior generation. That retired the two
// reducer_service_catalog_correlation probes this function used to run --
// including the shared_ownership refusal that turned away a caller whose
// service id another tenant also declared. Two tenants holding one catalog id
// now each read their own lineage.
//
// What stays here is one fail-closed pre-read refusal: a scoped caller whose
// grant names no repository and no scope. The SQL already resolves nothing for
// it (`= ANY('{}')` is false), so this is defense in depth that also names the
// cause on the span. The route reads no service-catalog correlation store, so
// whether one is wired does not change its answer.
//
// Every refusal is recorded on the handler span before it returns
// (refuseServiceChangedSinceGrant), because the caller-facing body cannot say
// which one fired without turning the route back into an existence oracle.
func (h *Handler) serviceChangedSinceGrantAdmits(
	w http.ResponseWriter,
	r *http.Request,
	serviceID string,
) bool {
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if !access.Scoped() {
		// Shared, admin, and local callers read every lineage.
		return true
	}
	if access.Empty() {
		h.refuseServiceChangedSinceGrant(w, r, serviceID, telemetry.ServiceChangedSinceGrantRefusalEmptyGrant)
		return false
	}
	return true
}

// writeServiceChangedSinceAmbiguous answers a service id that more than one
// admitted ingestion scope holds a lineage for, when the caller named no
// scope_id (#6475). It is 409 Conflict with the ordinary `ambiguous` error
// code; details carries the admitted scope ids (sorted, bounded by
// changedsince.MaxServiceScopeCandidates) and whether the list was cut. The
// store never returns a scope outside the caller's grant here, so the list
// discloses nothing the caller could not already read by selecting it.
func (h *Handler) writeServiceChangedSinceAmbiguous(
	w http.ResponseWriter,
	r *http.Request,
	summary status.ServiceChangedSinceSummary,
) {
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.Int(telemetry.SpanAttrServiceChangedSinceAmbiguousScopeCount, len(summary.AmbiguousScopeIDs)),
	)
	message := fmt.Sprintf(
		"service_id %q has a materialization lineage in more than one ingestion scope; retry with scope_id set to one of details.scope_ids",
		summary.ServiceID,
	)
	details := map[string]any{
		"status":     "ambiguous",
		"service_id": summary.ServiceID,
		"scope_ids":  summary.AmbiguousScopeIDs,
		"truncated":  summary.AmbiguousTruncated,
	}
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteJSON(w, http.StatusConflict, querycontract.ResponseEnvelope{
			Truth: querycontract.BuildTruthEnvelope(
				h.profile(),
				ServiceChangedSinceCapability,
				querycontract.TruthBasisSemanticFacts,
				"refused an ambiguous service_id before computing a changed-since diff",
			),
			Error: &querycontract.ErrorEnvelope{
				Code:       querycontract.ErrorCodeAmbiguous,
				Message:    message,
				Capability: ServiceChangedSinceCapability,
				Details:    details,
			},
		})
		return
	}
	details["error"] = http.StatusText(http.StatusConflict)
	details["detail"] = message
	querycontract.WriteJSON(w, http.StatusConflict, details)
}

// refuseServiceChangedSinceGrant records the grant refusal on the handler span
// and then writes the route's ordinary service-not-found (#5167 review, P1-2).
//
// The span is the only place the refusal is visible. The response body is
// deliberately byte-identical to an unknown service's, and the middleware has
// already ADMITTED the request, so the scoped-route deny audit event does not
// fire for a handler-level refusal either. Without these attributes an operator
// paged with "tenant A's token gets not-found for a service it owns" cannot
// separate a grant refusal from missing lineage from an unwired ownership
// store.
//
// Both attributes are server-side, so they add no oracle for the caller. The
// reason comes from the closed telemetry.ServiceChangedSinceGrantRefusal*
// vocabulary and never carries the service id, tenant, workspace, repository,
// or scope: a per-tenant identifier here would leak into every trace backend
// that samples the route.
func (h *Handler) refuseServiceChangedSinceGrant(
	w http.ResponseWriter,
	r *http.Request,
	serviceID string,
	reason string,
) {
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.Bool(telemetry.SpanAttrServiceChangedSinceGrantRefused, true),
		attribute.String(telemetry.SpanAttrServiceChangedSinceGrantRefusedReason, reason),
	)
	h.writeServiceChangedSinceNotFound(w, r, serviceID)
}

// writeServiceChangedSinceNotFound is the route's single service-not-found
// answer. Both the unresolved-service path and the ungranted-service refusal go
// through it so the two responses stay byte-identical and the route cannot be
// used as an existence oracle for another tenant's services.
func (h *Handler) writeServiceChangedSinceNotFound(
	w http.ResponseWriter,
	r *http.Request,
	serviceID string,
) {
	querycontract.WriteContractError(
		w,
		r,
		http.StatusNotFound,
		fmt.Sprintf("no service materialization lineage found for service_id %q", serviceID),
		querycontract.ErrorCodeServiceNotFound,
		ServiceChangedSinceCapability,
		h.profile(),
		querycontract.RequiredProfile(ServiceChangedSinceCapability),
	)
}

func (h *Handler) parseServiceChangedSinceFilter(w http.ResponseWriter, r *http.Request) (status.ServiceChangedSinceFilter, bool) {
	filter := status.ServiceChangedSinceFilter{
		ServiceID:         querycontract.QueryParam(r, "service_id"),
		ScopeID:           querycontract.QueryParam(r, "scope_id"),
		SinceGenerationID: querycontract.QueryParam(r, "since_generation_id"),
	}

	if !filter.HasServiceSelector() {
		querycontract.WriteError(w, http.StatusBadRequest, "service_id is required")
		return status.ServiceChangedSinceFilter{}, false
	}
	if !filter.HasSinceReference() {
		querycontract.WriteError(w, http.StatusBadRequest, "since_generation_id is required")
		return status.ServiceChangedSinceFilter{}, false
	}

	limit, ok := h.parseChangedSinceLimit(w, r)
	if !ok {
		return status.ServiceChangedSinceFilter{}, false
	}
	filter.SampleLimit = limit

	return filter.Normalize(), true
}

func (h *Handler) serviceChangedSinceTruthEnvelope(summary status.ServiceChangedSinceSummary) *querycontract.TruthEnvelope {
	envelope := querycontract.BuildTruthEnvelope(
		h.profile(),
		ServiceChangedSinceCapability,
		querycontract.TruthBasisSemanticFacts,
		"diffed from durable service_evidence_snapshots keyed by (generation_id, service_evidence_key); service changed-since is persisted reducer snapshot truth, not live graph-materialized correlation",
	)
	switch {
	case summary.Unavailable:
		envelope.Freshness.State = querycontract.FreshnessUnavailable
		envelope.Freshness.Detail = "the service has no current active materialization generation, so a changed-since diff cannot be computed yet"
		WithCause(envelope, CausePendingRepoGeneration)
	case summary.Building:
		envelope.Freshness.State = querycontract.FreshnessBuilding
		envelope.Freshness.Detail = "the service has a pending materialization generation in flight; the current active generation may change"
		WithCause(envelope, CausePendingRepoGeneration)
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
		attribute.Bool(telemetry.SpanAttrServiceChangedSinceUnattributed, summary.Unattributed),
		attribute.String(telemetry.SpanAttrServiceChangedSinceSinceGenerationID, summary.SinceGenerationID),
		attribute.String(telemetry.SpanAttrServiceChangedSinceCurrentGenerationID, summary.CurrentActiveGenerationID),
		attribute.Int(telemetry.SpanAttrServiceChangedSinceChangedCount, changed),
		attribute.Bool(telemetry.SpanAttrServiceChangedSinceUnavailable, summary.Unavailable),
	}
}
