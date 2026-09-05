// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *SupplyChainHandler) getImpactPacket(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactExplanation,
		"GET /api/v0/investigations/supply-chain/impact/packet",
		supplyChainImpactExplanationCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), supplyChainImpactExplanationCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact packets require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			supplyChainImpactExplanationCapability,
			h.profile(),
			requiredProfile(supplyChainImpactExplanationCapability),
		)
		return
	}
	access := repositoryAccessFilterFromContext(r.Context())
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, QueryParam(r, "repository_id"), access, supplyChainImpactExplanationCapability)
	if !ok {
		return
	}
	if access.Scoped() && repositoryID == "" {
		packet, err := refusalPacketForAPI(InvestigationFamilySupplyChainImpact, PacketRefusalScopeNotFound)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeInvestigationPacket(w, r, packet)
		return
	}
	filter := impact.TrimSupplyChainImpactExplanationFilter(impact.SupplyChainImpactExplanationFilter{
		FindingID:     QueryParam(r, "finding_id"),
		AdvisoryID:    QueryParam(r, "advisory_id"),
		CVEID:         QueryParam(r, "cve_id"),
		PackageID:     QueryParam(r, "package_id"),
		RepositoryID:  repositoryID,
		SubjectDigest: QueryParam(r, "subject_digest"),
		ImageRef:      QueryParam(r, "image_ref"),
		WorkloadID:    QueryParam(r, "workload_id"),
		ServiceID:     QueryParam(r, "service_id"),
	})
	if !filter.HasBoundedScope() {
		WriteError(w, http.StatusBadRequest, "finding_id, or advisory_id/cve_id plus package_id, repository_id, subject_digest, image_ref, workload_id, or service_id is required")
		return
	}
	if h.ImpactExplanations == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact packets require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			supplyChainImpactExplanationCapability,
			h.profile(),
			requiredProfile(supplyChainImpactExplanationCapability),
		)
		return
	}

	row, err := h.ImpactExplanations.ExplainSupplyChainImpact(r.Context(), filter)
	if errors.Is(err, impact.ErrSupplyChainImpactExplanationNotFound) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.ReadinessScope(), nil, false)
		body := impact.BuildSupplyChainImpactNoEvidenceExplanation(filter, readiness)
		truth := BuildTruthEnvelope(
			h.profile(),
			supplyChainImpactExplanationCapability,
			TruthBasisSemanticFacts,
			"no reducer-owned impact finding matched the bounded explanation scope; readiness explains missing evidence",
		)
		packet, err := BuildSupplyChainImpactPacket(body, truth, packetBoundsFromRequest(r))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeInvestigationPacket(w, r, packet)
		return
	}
	if errors.Is(err, impact.ErrSupplyChainImpactExplanationAmbiguous) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.ReadinessScope(), nil, false)
		body := impact.BuildSupplyChainImpactAmbiguousExplanation(
			filter,
			readiness,
			impact.SupplyChainImpactExplanationAmbiguousCandidateCount(err),
		)
		truth := BuildTruthEnvelope(
			h.profile(),
			supplyChainImpactExplanationCapability,
			TruthBasisSemanticFacts,
			"bounded explanation scope matched multiple reducer-owned impact findings; provide finding_id or a narrower advisory/package/repository/image/workload/service scope",
		)
		packet, err := BuildSupplyChainImpactPacket(body, truth, packetBoundsFromRequest(r))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeInvestigationPacket(w, r, packet)
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	scope := impact.FindingReadinessScope(row.Finding, filter)
	findingResult := impact.SupplyChainImpactFindingResult(row.Finding)
	readiness := h.readSupplyChainImpactReadinessForScope(r, scope, []impact.SupplyChainImpactFindingResult{findingResult}, false)
	body := impact.BuildSupplyChainImpactExplanation(filter, row, readiness)
	truth := BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactExplanationCapability,
		TruthBasisSemanticFacts,
		"resolved from one reducer-owned impact finding and its bounded evidence fact ids; reachability and deployment anchors are reported only when evidence exists",
	)
	packet, err := BuildSupplyChainImpactPacket(body, truth, packetBoundsFromRequest(r))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeInvestigationPacket(w, r, packet)
}
