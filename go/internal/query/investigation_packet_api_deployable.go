// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Fixed bodies of the failure steps on GET
// /api/v0/investigations/deployable-unit/packet (#7674): the admission
// decision read, the packet build, and the scope-not-found refusal packet
// build. The read error can quote SQL and hosts, and a build error quotes
// packet contents, so neither reaches the body; each goes on the request span.
const (
	deployableUnitPacketReadFailedMessage    = "deployable-unit packet admission decision read failed"
	deployableUnitPacketBuildFailedMessage   = "deployable-unit packet build failed"
	deployableUnitRefusalPacketFailedMessage = "deployable-unit refusal packet build failed"
)

func (h *EvidenceHandler) getDeployableUnitPacket(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdmissionDecisions,
		"GET /api/v0/investigations/deployable-unit/packet",
		admissionDecisionCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), admissionDecisionCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"deployable-unit packets require the local-authoritative profile or higher",
			ErrorCodeUnsupportedCapability,
			admissionDecisionCapability,
			h.profile(),
			querycontract.RequiredProfile(admissionDecisionCapability),
		)
		return
	}
	filter, subject, ok := deployableUnitPacketFilterFromRequest(w, r)
	if !ok {
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	filter = admissionDecisionFilterWithRepositoryAccess(filter, access)
	if access.Empty() || !admissionDecisionReadFilterAllowed(filter) {
		h.writeEmptyDeployableUnitPacket(w, r)
		return
	}
	if h.AdmissionDecisions == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"deployable-unit packets require the Postgres admission decision read model",
			ErrorCodeBackendUnavailable,
			admissionDecisionCapability,
			h.profile(),
			querycontract.RequiredProfile(admissionDecisionCapability),
		)
		return
	}
	rows, err := h.AdmissionDecisions.ListAdmissionDecisions(r.Context(), filter)
	if err != nil {
		writeReadFailure(w, r, err, admissionDecisionCapability, deployableUnitPacketReadFailedMessage)
		return
	}
	decisions := make([]AdmissionDecisionResult, 0, len(rows))
	for _, row := range rows {
		decisions = append(decisions, admissionDecisionResult(row))
	}
	truth := BuildTruthEnvelope(
		h.profile(),
		admissionDecisionCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned correlation admission decision read model",
	)
	packet, err := BuildDeployableUnitPacket(decisions, subject, truth, packetBoundsFromRequest(r))
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, deployableUnitPacketBuildFailedMessage)
		return
	}
	writeInvestigationPacket(w, r, packet)
}

func deployableUnitPacketFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
) (AdmissionDecisionReadFilter, map[string]string, bool) {
	scopeID := QueryParam(r, "scope_id")
	generationID := QueryParam(r, "generation_id")
	if scopeID == "" || generationID == "" {
		WriteError(w, http.StatusBadRequest, "scope_id and generation_id are required")
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

func (h *EvidenceHandler) writeEmptyDeployableUnitPacket(w http.ResponseWriter, r *http.Request) {
	packet, err := refusalPacketForAPI(InvestigationFamilyDeployableUnit, PacketRefusalScopeNotFound)
	if err != nil {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, deployableUnitRefusalPacketFailedMessage)
		return
	}
	writeInvestigationPacket(w, r, packet)
}
