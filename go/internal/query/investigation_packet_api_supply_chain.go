// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"

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
	if access.scoped() && repositoryID == "" {
		packet, err := refusalPacketForAPI(InvestigationFamilySupplyChainImpact, PacketRefusalScopeNotFound)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeInvestigationPacket(w, r, packet)
		return
	}
	filter := trimSupplyChainImpactExplanationFilter(SupplyChainImpactExplanationFilter{
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
	if !filter.hasBoundedScope() {
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
	if errors.Is(err, ErrSupplyChainImpactExplanationNotFound) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.readinessScope(), nil, false)
		body := BuildSupplyChainImpactNoEvidenceExplanation(filter, readiness)
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
	if errors.Is(err, ErrSupplyChainImpactExplanationAmbiguous) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.readinessScope(), nil, false)
		body := BuildSupplyChainImpactAmbiguousExplanation(
			filter,
			readiness,
			supplyChainImpactExplanationAmbiguousCandidateCount(err),
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

	// Codex P1-A (same root cause as the explain route): run the authorized
	// cloud-runtime probe on this bounded, single-finding row before the
	// shared assembler resolves deployment_truth_tier/version_resolution_tier,
	// so this packet surface never disagrees with list/explain about which
	// tier won for the same finding.
	//
	// This is NOT cheap because it is bounded to one finding: the probe's
	// Cypher matches on CloudResource.running_image_digest
	// (supply_chain_impact_cloud_runtime_probe.go), and that property has no
	// graph index (go/internal/graph/schema_tables_indexes.go only indexes
	// CloudResource.arn/resource_id/resource_type), so the read is a
	// CloudResource label scan whose cost is independent of len($digests) --
	// one digest pays the same scan as list's full page. The common explain
	// case (a finding whose digest is not running) is the worst case: a full
	// scan that returns zero rows, which LIMIT cannot short-circuit. Accepted
	// deliberately anyway: a wrong security tier is worse than the scan cost,
	// and list already pays this same cost on every request.
	rows := []SupplyChainImpactFindingRow{row.Finding}
	if err := h.applySupplyChainCloudRuntimeEvidence(r.Context(), access, rows); err != nil {
		if WriteGraphReadError(w, r, err, supplyChainImpactExplanationCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, "supply-chain impact runtime evidence probe failed")
		return
	}
	// Same root cause as the cloud-runtime probe above, and the same fix as
	// the explain route: row.RuntimeContext must be resolved before the row
	// feeds the shared assembler, or this route silently omits a finding's
	// current workload/service/deployment context on the internal Finding
	// object even where list would resolve one. (The packet's own JSON
	// response does not currently echo runtime_context anywhere in its
	// transformed shape -- see supplyChainPacketSummary/supplyChainPacketDecisions
	// -- so this keeps the row internally correct and consistent with the
	// other two routes rather than fixing an externally observable
	// disagreement on this specific field for packet today.)
	if err := h.applySupplyChainRuntimeContext(r.Context(), rows, access); err != nil {
		if WriteGraphReadError(w, r, err, supplyChainImpactExplanationCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, "supply-chain impact runtime context probe failed")
		return
	}
	row.Finding = rows[0]

	scope := findingReadinessScope(row.Finding, filter)
	findingResult := SupplyChainImpactFindingResult(row.Finding)
	readiness := h.readSupplyChainImpactReadinessForScope(r, scope, []SupplyChainImpactFindingResult{findingResult}, false)
	body := BuildSupplyChainImpactExplanation(filter, row, readiness)
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
