// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// IncidentHandler exposes incident-context read-model routes.
type IncidentHandler struct {
	Context IncidentContextStore
	// Authorizer resolves the durable incident→repository correlation edge that
	// bounds scoped-token reads. It is required in production so scoped tokens
	// fail closed; shared, admin, and local callers never consult it.
	Authorizer IncidentRepositoryAuthorizer
	Profile    QueryProfile
}

// Mount registers incident-context query routes.
func (h *IncidentHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/incidents/{incident_id}/context", h.getIncidentContext)
}

func (h *IncidentHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *IncidentHandler) getIncidentContext(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryIncidentContext,
		"GET /api/v0/incidents/{incident_id}/context",
		incidentContextCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), incidentContextCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"incident context requires the Postgres incident source fact read model",
			ErrorCodeUnsupportedCapability,
			incidentContextCapability,
			h.profile(),
			requiredProfile(incidentContextCapability),
		)
		return
	}

	incidentID := PathParam(r, "incident_id")
	if incidentID == "" {
		WriteError(w, http.StatusBadRequest, "incident_id is required")
		return
	}
	limit, ok := incidentContextLimit(w, r)
	if !ok {
		return
	}
	if !validateIncidentContextTime(w, QueryParam(r, "since"), "since") ||
		!validateIncidentContextTime(w, QueryParam(r, "until"), "until") {
		return
	}
	if h.Context == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"incident context requires the Postgres incident source fact read model",
			ErrorCodeBackendUnavailable,
			incidentContextCapability,
			h.profile(),
			requiredProfile(incidentContextCapability),
		)
		return
	}

	filter := normalizeIncidentContextFilter(IncidentContextFilter{
		Provider:           QueryParam(r, "provider"),
		ProviderIncidentID: incidentID,
		ScopeID:            QueryParam(r, "scope_id"),
		ServiceID:          QueryParam(r, "service_id"),
		Since:              QueryParam(r, "since"),
		Until:              QueryParam(r, "until"),
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
	response := BuildIncidentContextResponse(snapshot)
	truth := BuildTruthEnvelope(
		h.profile(),
		incidentContextCapability,
		TruthBasisSemanticFacts,
		"resolved from active incident source facts and explicit missing evidence slots; provider APIs are not called from the query path",
	)
	WriteSuccess(w, r, http.StatusOK, incidentContextAnswerData(incidentID, response, truth), truth)
}

func (h *IncidentHandler) writeIncidentContextError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	var ambiguous IncidentContextAmbiguousError
	switch {
	case errors.As(err, &ambiguous):
		writeIncidentContextEnvelopeError(
			w,
			r,
			http.StatusConflict,
			ErrorCodeAmbiguous,
			ambiguous.Error(),
			map[string]any{"candidates": ambiguous.Candidates},
		)
	case errors.Is(err, ErrIncidentContextNotFound):
		writeIncidentContextEnvelopeError(
			w,
			r,
			http.StatusNotFound,
			ErrorCodeNotFound,
			err.Error(),
			nil,
		)
	default:
		WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeIncidentContextEnvelopeError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code ErrorCode,
	message string,
	details map[string]any,
) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data:  nil,
			Truth: nil,
			Error: &ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: incidentContextCapability,
				Details:    details,
			},
		})
		return
	}
	WriteError(w, status, message)
}

func incidentContextLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return incidentContextDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > incidentContextMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", incidentContextMaxLimit))
		return 0, false
	}
	return limit, true
}

func validateIncidentContextTime(w http.ResponseWriter, value string, field string) bool {
	if value == "" {
		return true
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("%s must be RFC3339", field))
		return false
	}
	return true
}

func trimIncidentContextSnapshot(
	snapshot IncidentContextSnapshot,
	limit int,
) IncidentContextSnapshot {
	if len(snapshot.Timeline) > limit {
		snapshot.Timeline = snapshot.Timeline[:limit]
		snapshot.Truncated = true
	}
	if len(snapshot.RelatedChanges) > limit {
		snapshot.RelatedChanges = snapshot.RelatedChanges[:limit]
		snapshot.Truncated = true
	}
	return snapshot
}
