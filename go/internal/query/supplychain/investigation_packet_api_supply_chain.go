// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SupplyChainImpactPacketResponder composes and writes the portable
// investigation packet for a supply-chain impact explanation. Root package
// query implements it from the lane-B packet envelope
// (BuildSupplyChainImpactPacket, the refusal composer, and the packet
// writer); cmd wiring injects that implementation into
// SupplyChainHandler.PacketResponder.
//
// The seam exists because the packet envelope types live in root, which
// this package cannot import without a cycle through root's compatibility
// aliases. The hub passes only leaf values (the impact explanation body
// and the querycontract truth envelope); bounds still come from the live
// request inside root's implementation, exactly as before the move. If
// lane-B moves the envelope to an importable leaf, collapse this seam back
// to direct calls and delete the responder.
type SupplyChainImpactPacketResponder interface {
	// RespondSupplyChainImpactPacket composes body and truth into the
	// portable packet (bounds from the request's max_source_facts, contract
	// defaults when absent) and writes it.
	RespondSupplyChainImpactPacket(
		w http.ResponseWriter,
		r *http.Request,
		body impact.SupplyChainImpactExplanationResult,
		truth *querycontract.TruthEnvelope,
	)
	// RespondSupplyChainImpactScopeRefusal writes the scope-not-found
	// refusal packet for a scoped caller whose subject resolved to no
	// granted repository.
	RespondSupplyChainImpactScopeRefusal(w http.ResponseWriter, r *http.Request)
}

func (h *SupplyChainHandler) getImpactPacket(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactExplanation,
		"GET /api/v0/investigations/supply-chain/impact/packet",
		SupplyChainImpactExplanationCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SupplyChainImpactExplanationCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact packets require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SupplyChainImpactExplanationCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactExplanationCapability),
		)
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, querycontract.QueryParam(r, "repository_id"), access, SupplyChainImpactExplanationCapability)
	if !ok {
		return
	}
	if h.PacketResponder == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact packets require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SupplyChainImpactExplanationCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactExplanationCapability),
		)
		return
	}
	if access.Scoped() && repositoryID == "" {
		h.PacketResponder.RespondSupplyChainImpactScopeRefusal(w, r)
		return
	}
	filter := impact.TrimSupplyChainImpactExplanationFilter(impact.SupplyChainImpactExplanationFilter{
		FindingID:     querycontract.QueryParam(r, "finding_id"),
		AdvisoryID:    querycontract.QueryParam(r, "advisory_id"),
		CVEID:         querycontract.QueryParam(r, "cve_id"),
		PackageID:     querycontract.QueryParam(r, "package_id"),
		RepositoryID:  repositoryID,
		SubjectDigest: querycontract.QueryParam(r, "subject_digest"),
		ImageRef:      querycontract.QueryParam(r, "image_ref"),
		WorkloadID:    querycontract.QueryParam(r, "workload_id"),
		ServiceID:     querycontract.QueryParam(r, "service_id"),
	})
	if !filter.HasBoundedScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "finding_id, or advisory_id/cve_id plus package_id, repository_id, subject_digest, image_ref, workload_id, or service_id is required")
		return
	}
	if h.ImpactExplanations == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact packets require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SupplyChainImpactExplanationCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactExplanationCapability),
		)
		return
	}

	row, err := h.ImpactExplanations.ExplainSupplyChainImpact(r.Context(), filter)
	if errors.Is(err, impact.ErrSupplyChainImpactExplanationNotFound) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.ReadinessScope(), nil, false)
		body := impact.BuildSupplyChainImpactNoEvidenceExplanation(filter, readiness)
		truth := querycontract.BuildTruthEnvelope(
			h.profile(),
			SupplyChainImpactExplanationCapability,
			querycontract.TruthBasisSemanticFacts,
			"no reducer-owned impact finding matched the bounded explanation scope; readiness explains missing evidence",
		)
		h.PacketResponder.RespondSupplyChainImpactPacket(w, r, body, truth)
		return
	}
	if errors.Is(err, impact.ErrSupplyChainImpactExplanationAmbiguous) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.ReadinessScope(), nil, false)
		body := impact.BuildSupplyChainImpactAmbiguousExplanation(
			filter,
			readiness,
			impact.SupplyChainImpactExplanationAmbiguousCandidateCount(err),
		)
		truth := querycontract.BuildTruthEnvelope(
			h.profile(),
			SupplyChainImpactExplanationCapability,
			querycontract.TruthBasisSemanticFacts,
			"bounded explanation scope matched multiple reducer-owned impact findings; provide finding_id or a narrower advisory/package/repository/image/workload/service scope",
		)
		h.PacketResponder.RespondSupplyChainImpactPacket(w, r, body, truth)
		return
	}
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	scope := impact.FindingReadinessScope(row.Finding, filter)
	findingResult := impact.SupplyChainImpactFindingResult(row.Finding)
	readiness := h.readSupplyChainImpactReadinessForScope(r, scope, []impact.SupplyChainImpactFindingResult{findingResult}, false)
	body := impact.BuildSupplyChainImpactExplanation(filter, row, readiness)
	truth := querycontract.BuildTruthEnvelope(
		h.profile(),
		SupplyChainImpactExplanationCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from one reducer-owned impact finding and its bounded evidence fact ids; reachability and deployment anchors are reported only when evidence exists",
	)
	h.PacketResponder.RespondSupplyChainImpactPacket(w, r, body, truth)
}
