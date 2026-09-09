// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// IncidentHandler exposes incident-context read-model routes.
type IncidentHandler struct {
	Context model.IncidentContextStore
	// Authorizer resolves the durable incident→repository correlation edge that
	// bounds scoped-token reads. It is required in production so scoped tokens
	// fail closed; shared, admin, and local callers never consult it.
	Authorizer model.IncidentRepositoryAuthorizer
	Profile    querycontract.QueryProfile
}

// Mount registers incident-context query routes.
func (h *IncidentHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/incidents/{incident_id}/context", h.getIncidentContext)
}

func (h *IncidentHandler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

// incidentHandlerTracer is this package's tracer AND the seam its span
// tests swap. Seeding it from queryspan.HandlerTracer keeps the swap
// private to this package rather than mutating what every other importer
// reads.
var incidentHandlerTracer = queryspan.HandlerTracer()

func (h *IncidentHandler) getIncidentContext(w http.ResponseWriter, r *http.Request) {
	r, span := queryspan.StartHandlerSpanWith(
		incidentHandlerTracer,
		r,
		telemetry.SpanQueryIncidentContext,
		"GET /api/v0/incidents/{incident_id}/context",
		model.Capability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), model.Capability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"incident context requires the Postgres incident source fact read model",
			querycontract.ErrorCodeUnsupportedCapability,
			model.Capability,
			h.profile(),
			querycontract.RequiredProfile(model.Capability),
		)
		return
	}

	incidentID := querycontract.PathParam(r, "incident_id")
	if incidentID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "incident_id is required")
		return
	}
	limit, ok := incidentContextLimit(w, r)
	if !ok {
		return
	}
	if !validateIncidentContextTime(w, querycontract.QueryParam(r, "since"), "since") ||
		!validateIncidentContextTime(w, querycontract.QueryParam(r, "until"), "until") {
		return
	}
	if h.Context == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"incident context requires the Postgres incident source fact read model",
			querycontract.ErrorCodeBackendUnavailable,
			model.Capability,
			h.profile(),
			querycontract.RequiredProfile(model.Capability),
		)
		return
	}

	filter := model.NormalizeFilter(model.IncidentContextFilter{
		Provider:           querycontract.QueryParam(r, "provider"),
		ProviderIncidentID: incidentID,
		ScopeID:            querycontract.QueryParam(r, "scope_id"),
		ServiceID:          querycontract.QueryParam(r, "service_id"),
		Since:              querycontract.QueryParam(r, "since"),
		Until:              querycontract.QueryParam(r, "until"),
		Limit:              limit + 1,
	})
	if !h.authorizeScopedIncidentContext(w, r, filter.Provider, filter.ProviderIncidentID, filter.ScopeID) {
		return
	}
	snapshot, err := h.Context.ReadIncidentContext(r.Context(), filter)
	if err != nil {
		h.writeIncidentContextError(w, r, err)
		return
	}
	snapshot = trimIncidentContextSnapshot(snapshot, limit)
	snapshot.Query.Limit = limit
	response := model.BuildIncidentContextResponse(snapshot)
	truth := querycontract.BuildTruthEnvelope(
		h.profile(),
		model.Capability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from active incident source facts and explicit missing evidence slots; provider APIs are not called from the query path",
	)
	querycontract.WriteSuccess(w, r, http.StatusOK, incidentContextAnswerData(incidentID, response, truth), truth)
}

func (h *IncidentHandler) writeIncidentContextError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	var ambiguous model.IncidentContextAmbiguousError
	switch {
	case errors.As(err, &ambiguous):
		writeIncidentContextEnvelopeError(
			w,
			r,
			http.StatusConflict,
			querycontract.ErrorCodeAmbiguous,
			ambiguous.Error(),
			map[string]any{"candidates": ambiguous.Candidates},
		)
	case errors.Is(err, model.ErrIncidentContextNotFound):
		writeIncidentContextEnvelopeError(
			w,
			r,
			http.StatusNotFound,
			querycontract.ErrorCodeNotFound,
			err.Error(),
			nil,
		)
	default:
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeIncidentContextEnvelopeError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code querycontract.ErrorCode,
	message string,
	details map[string]any,
) {
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteJSON(w, status, querycontract.ResponseEnvelope{
			Data:  nil,
			Truth: nil,
			Error: &querycontract.ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: model.Capability,
				Details:    details,
			},
		})
		return
	}
	querycontract.WriteError(w, status, message)
}

func incidentContextLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		return model.DefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > model.MaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", model.MaxLimit))
		return 0, false
	}
	return limit, true
}

func validateIncidentContextTime(w http.ResponseWriter, value string, field string) bool {
	if value == "" {
		return true
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("%s must be RFC3339", field))
		return false
	}
	return true
}

// trimIncidentContextSnapshot is a defensive size cap on the timeline and
// related-change lists. It MUST NOT set snapshot.Truncated: that field is the
// fetched-count truth the store already computed from the raw fetched row count
// (readIncidentTimeline / readIncidentChangeCandidates), not something derived
// from the decoded len here. Deriving Truncated from len is the #4733 bug — a
// visible row dropped by typed decode shrinks len below limit and would hide a
// genuinely truncated page. The store already bounds each list to the visible
// window (requested limit), so these caps are normally no-ops; they remain only
// so the response can never exceed the requested limit if the store contract
// ever changes.
func trimIncidentContextSnapshot(
	snapshot model.IncidentContextSnapshot,
	limit int,
) model.IncidentContextSnapshot {
	if len(snapshot.Timeline) > limit {
		snapshot.Timeline = snapshot.Timeline[:limit]
	}
	if len(snapshot.RelatedChanges) > limit {
		snapshot.RelatedChanges = snapshot.RelatedChanges[:limit]
	}
	return snapshot
}
