package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *EvidenceHandler) getDeployableUnitPacket(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdmissionDecisions,
		"GET /api/v0/investigations/deployable-unit/packet",
		admissionDecisionCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), admissionDecisionCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"deployable-unit packets require the local-authoritative profile or higher",
			ErrorCodeUnsupportedCapability,
			admissionDecisionCapability,
			h.profile(),
			requiredProfile(admissionDecisionCapability),
		)
		return
	}
	filter, subject, ok := deployableUnitPacketFilterFromRequest(w, r)
	if !ok {
		return
	}
	access := repositoryAccessFilterFromContext(r.Context())
	filter = admissionDecisionFilterWithRepositoryAccess(filter, access)
	if access.empty() || !admissionDecisionReadFilterAllowed(filter) {
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
			requiredProfile(admissionDecisionCapability),
		)
		return
	}
	rows, err := h.AdmissionDecisions.ListAdmissionDecisions(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
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
		WriteError(w, http.StatusInternalServerError, err.Error())
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
	repositoryID := firstNonEmptyQueryParam(r, "repository_id", "repo_id")
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
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeInvestigationPacket(w, r, packet)
}
