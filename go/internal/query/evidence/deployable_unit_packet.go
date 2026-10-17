// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DeployableUnitPacketResponder composes and writes the portable
// investigation packet for a deployable-unit admission explanation. Root
// package query implements it from the lane-B packet envelope
// (BuildDeployableUnitPacket, the refusal composer, and the packet writer);
// cmd wiring injects that implementation into Handler.PacketResponder.
//
// The seam exists because the packet envelope types live in root, which
// this package cannot import without a cycle through root's compatibility
// aliases. The handler passes only leaf values (the admission decision
// results, the request subject, and the querycontract truth envelope);
// bounds still come from the live request inside root's implementation,
// exactly as before the move. If lane-B moves the envelope to an
// importable leaf, collapse this seam back to direct calls and delete the
// responder.
//
// When investigation/packet leaf lands, delete the seam and call the leaf
// directly.
type DeployableUnitPacketResponder interface {
	// RespondDeployableUnitPacket composes decisions and subject into the
	// portable packet (bounds from the request's max_source_facts, contract
	// defaults when absent) and writes it with truth.
	RespondDeployableUnitPacket(
		w http.ResponseWriter,
		r *http.Request,
		decisions []AdmissionDecisionResult,
		subject map[string]string,
		truth *querycontract.TruthEnvelope,
	)
	// RespondDeployableUnitScopeRefusal writes the scope-not-found refusal
	// packet for a scoped caller whose subject resolved to no granted
	// repository.
	RespondDeployableUnitScopeRefusal(w http.ResponseWriter, r *http.Request)
}

// deployableUnitPacketReadFailedMessage is the fixed body of the failed
// admission decision read on GET
// /api/v0/investigations/deployable-unit/packet (#7674). The read error can
// quote SQL and hosts, so it goes on the request span, never into the body.
const deployableUnitPacketReadFailedMessage = "deployable-unit packet admission decision read failed"

func (h *Handler) getDeployableUnitPacket(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdmissionDecisions,
		"GET /api/v0/investigations/deployable-unit/packet",
		AdmissionDecisionCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), AdmissionDecisionCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"deployable-unit packets require the local-authoritative profile or higher",
			querycontract.ErrorCodeUnsupportedCapability,
			AdmissionDecisionCapability,
			h.profile(),
			querycontract.RequiredProfile(AdmissionDecisionCapability),
		)
		return
	}
	filter, subject, ok := deployableUnitPacketFilterFromRequest(w, r)
	if !ok {
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	filter = admissionDecisionFilterWithRepositoryAccess(filter, access)
	if h.PacketResponder == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"deployable-unit packet responder not configured",
			querycontract.ErrorCodeBackendUnavailable,
			AdmissionDecisionCapability,
			h.profile(),
			querycontract.RequiredProfile(AdmissionDecisionCapability),
		)
		return
	}
	if access.Empty() || !admissionDecisionReadFilterAllowed(filter) {
		h.writeEmptyDeployableUnitPacket(w, r)
		return
	}
	if h.AdmissionDecisions == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"deployable-unit packets require the Postgres admission decision read model",
			querycontract.ErrorCodeBackendUnavailable,
			AdmissionDecisionCapability,
			h.profile(),
			querycontract.RequiredProfile(AdmissionDecisionCapability),
		)
		return
	}
	rows, err := h.AdmissionDecisions.ListAdmissionDecisions(r.Context(), filter)
	if err != nil {
		writeReadFailure(w, r, err, AdmissionDecisionCapability, deployableUnitPacketReadFailedMessage)
		return
	}
	decisions := make([]AdmissionDecisionResult, 0, len(rows))
	for _, row := range rows {
		decisions = append(decisions, AdmissionDecisionResultFromRow(row))
	}
	truth := querycontract.BuildTruthEnvelope(
		h.profile(),
		AdmissionDecisionCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned correlation admission decision read model",
	)
	h.PacketResponder.RespondDeployableUnitPacket(w, r, decisions, subject, truth)
}

func deployableUnitPacketFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
) (AdmissionDecisionReadFilter, map[string]string, bool) {
	scopeID := querycontract.QueryParam(r, "scope_id")
	generationID := querycontract.QueryParam(r, "generation_id")
	if scopeID == "" || generationID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id and generation_id are required")
		return AdmissionDecisionReadFilter{}, nil, false
	}
	subject := map[string]string{
		"scope_id":      scopeID,
		"generation_id": generationID,
	}
	filter := AdmissionDecisionReadFilter{
		Domain:       "deployable_unit_correlation",
		ScopeID:      scopeID,
		GenerationID: generationID,
		Limit:        admissionDecisionMaxLimit,
	}
	repositoryID := impact.FirstNonEmptyQueryParam(r, "repository_id", "repo_id")
	if repositoryID != "" {
		filter.AnchorKind = "repository"
		filter.AnchorID = repositoryID
		subject["repository_id"] = repositoryID
	}
	return filter, subject, true
}

func (h *Handler) writeEmptyDeployableUnitPacket(w http.ResponseWriter, r *http.Request) {
	h.PacketResponder.RespondDeployableUnitScopeRefusal(w, r)
}
