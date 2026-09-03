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
	"go.opentelemetry.io/otel/trace"
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
// Admission is exclusive, not existential: it takes one correlation inside the
// grant AND none outside it. A catalog service id is relative to the catalog
// that declared it and is never tenant-qualified, so two tenants that both run
// a service called `api` write the same service_id -- and the lineage tables
// key on that id alone, with the reducer's writer conflicting on it, so there
// is one generation lineage for the id and nothing recording whose. Admitting
// on one granted correlation would serve whichever tenant materialized last.
// Splitting the lineage needs a scope column on those tables; until that
// lands, a contested id is refused.
//
// Three deliberate fail-closed cases, and one gap that is not closed:
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
//   - A service id correlated from outside the grant as well as inside it is
//     refused even though the caller genuinely owns one of the correlations.
//     Returning the shared lineage would hand the caller another tenant's
//     counts and evidence keys, and returning a filtered one is impossible:
//     the lineage carries nothing to filter on.
//   - One correlation is enough to contest the id when it could not resolve to
//     a single repository. The reducer's ambiguous branches leave
//     repository_id empty and list every match in candidate_repository_ids,
//     and those decisions are still materialized, so a row naming one
//     repository the caller owns and one it does not reaches the same lineage.
//     The outside-grant statement reports such a row rather than treating the
//     one granted candidate as covering it (#6472 review, P1-B). This is why
//     the two statements are not complements: admission asks whether the
//     caller has SOME claim, exclusivity asks whether anyone else has ANY.
//
// The gap, stated because the contract sentence on this route is bounded to
// match it (#6475): both correlation reads join ingestion_scopes on the
// scope's active generation and require generation.status = 'active', so a
// correlation that has aged out -- the component removed from the other
// tenant's catalog, that scope deactivated, the tenant offboarded -- is
// invisible to the exclusivity probe. Its lineage generation, meanwhile, stays
// the active one for the id, because nothing prunes
// service_materialization_generations. The id then stops looking contested and
// the caller is admitted onto that lineage. The writing scope cannot be
// recovered to close this here: source_intent_id is nullable, carries no
// foreign key, and points at shared_projection_intents rows that generation
// retention deletes (deleteSharedProjectionIntentsForGenerationsQuery), whose
// scope_id can be empty on legacy rows. Closing it needs the scope column on
// the lineage tables and a (scope, service_id) writer key, which is #6475.
// TestServiceChangedSinceSharedServiceIDIsRefused pins the behaviour so the
// contract sentence and the code cannot drift apart.
//
// Every refusal is recorded on the handler span before it returns
// (refuseServiceChangedSinceGrant), because the caller-facing body cannot say
// which one fired without turning the route back into an existence oracle.
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
		h.refuseServiceChangedSinceGrant(w, r, serviceID, telemetry.ServiceChangedSinceGrantRefusalEmptyGrant)
		return false
	}
	if h.ServiceOwnership == nil {
		h.refuseServiceChangedSinceGrant(w, r, serviceID, telemetry.ServiceChangedSinceGrantRefusalOwnershipUnwired)
		return false
	}

	// One row is the whole answer on both probes: the questions are whether
	// the grant covers this service at all, and whether anything outside the
	// grant also claims the id -- never which repository owns it.
	probe := ServiceCatalogCorrelationFilter{
		ServiceID:            serviceID,
		AllowedRepositoryIDs: access.GrantedRepositoryIDs(),
		AllowedScopeIDs:      access.GrantedScopeIDs(),
		Limit:                1,
	}

	granted, err := h.ServiceOwnership.ListServiceCatalogCorrelations(r.Context(), probe)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve service ownership: %v", err))
		return false
	}
	if len(granted) == 0 {
		h.refuseServiceChangedSinceGrant(w, r, serviceID, telemetry.ServiceChangedSinceGrantRefusalNotGranted)
		return false
	}

	// The exclusivity half. It runs only once the grant already covers the
	// service, so an ungranted caller still pays for one query rather than two.
	//
	// The two probes are separate statements, so a correlation written between
	// them is not seen. That window is inherent to checking before the read
	// rather than to using two statements -- one combined query would leave
	// the same gap between itself and the lineage read -- and it is bounded by
	// how long the reducer takes to publish a new catalog generation, which is
	// far longer than the gap.
	probe.OutsideGrant = true
	contested, err := h.ServiceOwnership.ListServiceCatalogCorrelations(r.Context(), probe)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve service ownership: %v", err))
		return false
	}
	if len(contested) > 0 {
		h.refuseServiceChangedSinceGrant(w, r, serviceID, telemetry.ServiceChangedSinceGrantRefusalSharedOwnership)
		return false
	}
	return true
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
func (h *FreshnessHandler) refuseServiceChangedSinceGrant(
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
